// Package main — server.go is the JSON-RPC 2.0 transport that
// implements the Model Context Protocol over newline-delimited
// stdin/stdout (commonly called "stdio MCP" in the spec).
//
// MCP framing on stdio is dead simple: each line of stdin/stdout
// is one JSON-RPC message. No length prefix, no Content-Length
// header (that's the HTTP variant). Servers MUST NOT write
// anything to stdout that isn't a framed JSON-RPC message —
// otherwise the host parser desyncs and the session crashes. All
// of basement-mcp's logging therefore goes to stderr.
//
// The MCP methods we implement:
//
//   - initialize                — handshake; advertise tools capability
//   - notifications/initialized — client confirms handshake; we no-op
//   - tools/list                — return our tool catalog
//   - tools/call                — dispatch to a handler in tools.go
//
// Any other method gets a -32601 "Method not found" reply. The MCP
// spec also defines resources/* and prompts/* surfaces but
// basement-mcp doesn't currently advertise those capabilities, so
// the client should never call them — if it does we reply with
// "Method not found" rather than partially-implementing.
//
// JSON-RPC error codes used here:
//
//   - -32600 Invalid Request          (malformed envelope)
//   - -32601 Method not found
//   - -32602 Invalid params
//   - -32603 Internal error           (handler panicked / bad API)
//   - -32000 Server error             (basement API returned 5xx)
//
// The "isError + content" pattern from MCP tools/call is layered
// on top of these: even when the basement call fails, the RPC
// response itself is "success" — the tool result carries
// isError=true so the LLM can see and react to the failure.

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/mattjackson/basement/internal/clilib"
	"github.com/mattjackson/basement/internal/version"
)

// MCP protocol version we advertise. 2024-11-05 is the long-stable
// baseline. Clients are expected to negotiate down to a version
// they support; if they ask for something newer we still answer
// with our supported version per the MCP spec's compatibility
// guidance.
const mcpProtocolVersion = "2024-11-05"

// JSON-RPC 2.0 envelope shapes. We allow id to be missing
// (notifications) or null, plus integer or string per the spec —
// json.RawMessage preserves whichever the client sent so our
// response echoes the exact shape.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// JSON-RPC standard error codes.
const (
	errCodeParseError     = -32700
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602
	errCodeInternalError  = -32603

	// Server-defined range. -32000 to -32099 are reserved per spec
	// for implementation-defined server errors. We use -32000 for
	// "upstream basement-server error" so a host can distinguish
	// "my MCP server crashed" from "the storage backend said no".
	errCodeUpstreamError = -32000
)

// initializeResult is the payload of the initialize handshake's
// reply. capabilities advertises which optional MCP surfaces we
// support (tools only); serverInfo names the binary so the host
// can display it in a tool-picker UI.
type initializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    serverCapabilities `json:"capabilities"`
	ServerInfo      serverInfo         `json:"serverInfo"`
	Instructions    string             `json:"instructions,omitempty"`
}

type serverCapabilities struct {
	Tools *toolsCapability `json:"tools,omitempty"`
}

type toolsCapability struct {
	// listChanged is whether we proactively notify the client
	// when the tool list changes. We don't — the catalog is
	// fixed at build time — so the field is false.
	ListChanged bool `json:"listChanged"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Server is the stdio JSON-RPC server. It owns the basement HTTP
// client (so handlers can issue requests against the upstream
// API) and the logger (everything stderr-bound). The reading loop
// is single-threaded — MCP stdio is request/response per line —
// so we don't need locking around the client or logger.
//
// stdoutMu guards concurrent writes only if a future change adds
// background notifications (e.g. resources/listChanged); right
// now everything is serialised by Serve's loop, but the mutex is
// cheap and keeps the invariant explicit.
type Server struct {
	client       *clilib.Client
	logger       *slog.Logger
	tools        []Tool
	toolByName   map[string]Tool
	stdoutMu     sync.Mutex
	initialized  bool
	clientInfo   serverInfo // recorded from initialize.params.clientInfo
}

// NewServer wires the tool catalog (built in tools.go) to a
// freshly-constructed Server. Tools and toolByName are populated
// once here so tools/list is O(n) over a slice and tools/call is
// O(1) by map lookup.
func NewServer(client *clilib.Client, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	s := &Server{
		client: client,
		logger: logger,
	}
	s.tools = buildTools(s)
	s.toolByName = make(map[string]Tool, len(s.tools))
	for _, t := range s.tools {
		s.toolByName[t.Name] = t
	}
	return s
}

// Serve drives the stdio loop. Each newline-delimited line on in
// is one JSON-RPC message; responses are written to out as
// newline-delimited JSON. Returns nil on clean EOF (host closed
// the pipe) and any IO error on a broken transport.
//
// We use bufio.Scanner with a generous buffer because MCP
// messages — particularly tools/list responses with many tools —
// can exceed the default 64KB Scanner line limit. 1MB is enough
// headroom for any tool list we'd ever ship.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		s.handleLine(out, line)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("stdin scan: %w", err)
	}
	return nil
}

// handleLine parses one JSON-RPC frame and dispatches it. Parse
// failures get a JSON-RPC "parse error" response (per spec, with
// null id). Notifications (no id field) get no response at all.
func (s *Server) handleLine(out io.Writer, line []byte) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		s.logger.Warn("parse JSON-RPC frame", "error", err, "raw", string(line))
		s.writeResponse(out, rpcResponse{
			JSONRPC: "2.0",
			Error: &rpcError{
				Code:    errCodeParseError,
				Message: "Parse error: " + err.Error(),
			},
		})
		return
	}

	if req.JSONRPC != "2.0" {
		s.replyError(out, req.ID, errCodeInvalidRequest, "jsonrpc version must be \"2.0\"")
		return
	}

	// Notification = no id. We must not respond to notifications
	// per spec. handleMethod is structured to return early without
	// touching out in that case.
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"

	switch req.Method {
	case "initialize":
		if isNotification {
			return
		}
		s.handleInitialize(out, req)
	case "notifications/initialized":
		// Client confirms the handshake. We mark our state and move
		// on. No response — it's a notification.
		s.initialized = true
		s.logger.Info("MCP client initialized", "name", s.clientInfo.Name, "version", s.clientInfo.Version)
	case "ping":
		// MCP ping is a no-op heartbeat. Return an empty result.
		if isNotification {
			return
		}
		s.writeResponse(out, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}})
	case "tools/list":
		if isNotification {
			return
		}
		s.handleToolsList(out, req)
	case "tools/call":
		if isNotification {
			return
		}
		s.handleToolsCall(out, req)
	default:
		if isNotification {
			// Per spec, notifications for unknown methods are
			// silently ignored.
			return
		}
		s.replyError(out, req.ID, errCodeMethodNotFound, "method not found: "+req.Method)
	}
}

// handleInitialize answers the initialize handshake. We accept
// whatever protocolVersion the client asks for and reply with our
// own supported version per the MCP compatibility guidance — the
// client is responsible for downgrading its own behaviour.
func (s *Server) handleInitialize(out io.Writer, req rpcRequest) {
	var params struct {
		ProtocolVersion string          `json:"protocolVersion"`
		ClientInfo      serverInfo      `json:"clientInfo"`
		Capabilities    json.RawMessage `json:"capabilities"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.replyError(out, req.ID, errCodeInvalidParams, "invalid initialize params: "+err.Error())
			return
		}
	}
	s.clientInfo = params.ClientInfo

	s.writeResponse(out, rpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: initializeResult{
			ProtocolVersion: mcpProtocolVersion,
			Capabilities: serverCapabilities{
				Tools: &toolsCapability{ListChanged: false},
			},
			ServerInfo: serverInfo{
				Name:    "basement-mcp",
				Version: version.Get().Version,
			},
			Instructions: "basement-mcp exposes basement storage operations as MCP tools. " +
				"Use basement_list_regions to discover what's available, then drill down into " +
				"buckets and objects. Tools whose names start with basement_list_audit or " +
				"basement_create_* require admin/user capabilities on the upstream service " +
				"account.",
		},
	})
}

// handleToolsList responds with our static tool catalog. Each
// tool's InputSchema is already a json.RawMessage so we hand it
// through verbatim — MCP clients pass these straight to their
// LLM as the function schema, so any extra Go-side translation
// would just risk schema drift.
func (s *Server) handleToolsList(out io.Writer, req rpcRequest) {
	type listed struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	tools := make([]listed, 0, len(s.tools))
	for _, t := range s.tools {
		tools = append(tools, listed{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	s.writeResponse(out, rpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]any{"tools": tools},
	})
}

// toolCallResult is the MCP tools/call response shape. content is
// a list of typed blocks (we only emit "text") and isError flags
// whether the tool succeeded — distinct from RPC-level errors
// because a failed tool call is a successful RPC interaction.
type toolCallResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// handleToolsCall dispatches a tools/call to the registered
// handler. Handlers receive the raw arguments json (so they can
// validate against their own schema) and return either text
// content or an error.
func (s *Server) handleToolsCall(out io.Writer, req rpcRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.replyError(out, req.ID, errCodeInvalidParams, "invalid tools/call params: "+err.Error())
		return
	}
	tool, ok := s.toolByName[params.Name]
	if !ok {
		s.replyError(out, req.ID, errCodeMethodNotFound, "unknown tool: "+params.Name)
		return
	}

	// Default to an empty JSON object so handlers can decode into a
	// struct with all-omitempty fields without first checking for nil.
	args := params.Arguments
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}

	ctx := context.Background()
	text, err := tool.Handler(ctx, args)
	if err != nil {
		// Tool-level failure: still a successful RPC, but isError
		// flag tells the LLM the call didn't do what it intended.
		// We include the error message verbatim so the model can
		// reason about it (e.g. "the region wasn't found, try
		// listing regions first").
		s.writeResponse(out, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: toolCallResult{
				Content: []contentBlock{{Type: "text", Text: err.Error()}},
				IsError: true,
			},
		})
		return
	}

	s.writeResponse(out, rpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: toolCallResult{
			Content: []contentBlock{{Type: "text", Text: text}},
		},
	})
}

// replyError is a convenience around writeResponse for the
// "JSON-RPC error envelope" pattern.
func (s *Server) replyError(out io.Writer, id json.RawMessage, code int, message string) {
	s.writeResponse(out, rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &rpcError{
			Code:    code,
			Message: message,
		},
	})
}

// writeResponse marshals + emits a single JSON-RPC frame followed
// by a newline. Errors here are logged but not returned — the
// transport will surface them on the next loop iteration when
// scanner.Err() fires.
func (s *Server) writeResponse(out io.Writer, resp rpcResponse) {
	buf, err := json.Marshal(resp)
	if err != nil {
		s.logger.Error("marshal rpc response", "error", err)
		return
	}
	s.stdoutMu.Lock()
	defer s.stdoutMu.Unlock()
	if _, err := out.Write(append(buf, '\n')); err != nil {
		s.logger.Error("write rpc response", "error", err)
	}
}
