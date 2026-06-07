// Package main — server_test.go covers the JSON-RPC plumbing
// plus the tools/call -> upstream HTTP fan-out. Tests use a
// bytes.Buffer pair for stdin/stdout and an httptest.Server for
// the basement-server upstream so the suite stays hermetic — no
// live deployment, no real config file on disk.

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/clilib"
)

// testServer wires a Server against a controllable upstream
// httptest backend. handler is the upstream request handler the
// test supplies; the returned upstream URL is what the client
// points at.
type testServer struct {
	srv      *Server
	upstream *httptest.Server
	calls    *callRecorder
}

// callRecorder captures upstream HTTP calls so tests can assert
// on the path / method / auth header / body the MCP tool emitted.
type callRecorder struct {
	mu    sync.Mutex
	calls []recordedCall
}

type recordedCall struct {
	Method string
	Path   string
	Query  string
	Auth   string
	Body   string
}

func (c *callRecorder) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	// Restore so the downstream handler can re-read.
	r.Body = io.NopCloser(bytes.NewReader(body))
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, recordedCall{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
		Auth:   r.Header.Get("Authorization"),
		Body:   string(body),
	})
}

func (c *callRecorder) last() recordedCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.calls) == 0 {
		return recordedCall{}
	}
	return c.calls[len(c.calls)-1]
}

func newTestServer(t *testing.T, h http.HandlerFunc) *testServer {
	t.Helper()
	rec := &callRecorder{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		h(w, r)
	}))
	t.Cleanup(upstream.Close)
	client := clilib.NewClient(upstream.URL, "BMNT00000000aaaa", "test-secret")
	srv := NewServer(client, slog.New(slog.DiscardHandler))
	return &testServer{srv: srv, upstream: upstream, calls: rec}
}

// roundtrip sends one JSON-RPC frame through Serve and returns the
// next written response. Serve runs in a goroutine because we
// close stdin once we've sent the frame to make it terminate; the
// returned response is whatever showed up on stdout before EOF.
func (ts *testServer) roundtrip(t *testing.T, req map[string]any) map[string]any {
	t.Helper()
	in, inWriter := io.Pipe()
	out := &bytes.Buffer{}
	done := make(chan error, 1)
	go func() { done <- ts.srv.Serve(in, out) }()

	buf, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal req: %v", err)
	}
	if _, err := inWriter.Write(append(buf, '\n')); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	// Close stdin to terminate Serve's loop.
	_ = inWriter.Close()

	if err := <-done; err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if out.Len() == 0 {
		return nil // notification — no response
	}
	line := out.Bytes()
	// Take just the first frame in case multiple were produced.
	if idx := bytes.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	var resp map[string]any
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("decode response %q: %v", string(line), err)
	}
	return resp
}

// roundtripFrames sends multiple JSON-RPC frames through a single
// Serve session and returns the last non-empty response frame. Used
// to drive the initialize handshake before a tools/* call now that
// the server enforces handshake-first.
func (ts *testServer) roundtripFrames(t *testing.T, reqs ...map[string]any) map[string]any {
	t.Helper()
	in, inWriter := io.Pipe()
	out := &bytes.Buffer{}
	done := make(chan error, 1)
	go func() { done <- ts.srv.Serve(in, out) }()

	for _, req := range reqs {
		buf, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal req: %v", err)
		}
		if _, err := inWriter.Write(append(buf, '\n')); err != nil {
			t.Fatalf("write stdin: %v", err)
		}
	}
	_ = inWriter.Close()

	if err := <-done; err != nil {
		t.Fatalf("Serve: %v", err)
	}
	// Return the last frame written to stdout.
	frames := bytes.Split(bytes.TrimRight(out.Bytes(), "\n"), []byte("\n"))
	var last []byte
	for _, f := range frames {
		if len(bytes.TrimSpace(f)) > 0 {
			last = f
		}
	}
	if last == nil {
		return nil
	}
	var resp map[string]any
	if err := json.Unmarshal(last, &resp); err != nil {
		t.Fatalf("decode response %q: %v", string(last), err)
	}
	return resp
}

// initFrames returns the initialize + notifications/initialized
// frames that must precede any tools/* call.
func initFrames() []map[string]any {
	return []map[string]any{
		{
			"jsonrpc": "2.0",
			"id":      "init",
			"method":  "initialize",
			"params": map[string]any{
				"protocolVersion": "2024-11-05",
				"clientInfo":      map[string]any{"name": "test", "version": "1.0"},
				"capabilities":    map[string]any{},
			},
		},
		{
			"jsonrpc": "2.0",
			"method":  "notifications/initialized",
		},
	}
}

// roundtripAfterInit handshakes, then sends req, returning req's
// response.
func (ts *testServer) roundtripAfterInit(t *testing.T, req map[string]any) map[string]any {
	t.Helper()
	return ts.roundtripFrames(t, append(initFrames(), req)...)
}

// --- Initialize ---------------------------------------------------------

func TestInitializeHandshake(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		// Initialize doesn't touch upstream — fail loudly if it does.
		http.Error(w, "should not be called", http.StatusInternalServerError)
	})
	resp := ts.roundtrip(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"clientInfo":      map[string]any{"name": "claude-test", "version": "1.0"},
			"capabilities":    map[string]any{},
		},
	})
	if resp["error"] != nil {
		t.Fatalf("initialize errored: %v", resp["error"])
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("result missing or wrong type: %#v", resp["result"])
	}
	if v := result["protocolVersion"]; v != "2024-11-05" {
		t.Errorf("protocolVersion = %v, want 2024-11-05", v)
	}
	caps, _ := result["capabilities"].(map[string]any)
	if _, hasTools := caps["tools"]; !hasTools {
		t.Errorf("capabilities.tools missing: %#v", caps)
	}
	info, _ := result["serverInfo"].(map[string]any)
	if info["name"] != "basement-mcp" {
		t.Errorf("serverInfo.name = %v", info["name"])
	}
}

// --- Handshake enforcement --------------------------------------------

// TestToolsCallBeforeInitializeRejected confirms the server refuses
// tools/call until the initialize handshake completes, then accepts
// it afterwards. Per MCP, non-initialization requests must not be
// processed before the initialize / notifications/initialized
// exchange.
func TestToolsCallBeforeInitializeRejected(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("upstream must not be called before handshake completes")
		http.Error(w, "should not be called", http.StatusInternalServerError)
	})
	resp := ts.roundtrip(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      100,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "basement_list_regions",
			"arguments": map[string]any{},
		},
	})
	if resp["error"] == nil {
		t.Fatalf("expected error for pre-handshake tools/call, got %#v", resp)
	}
	errObj := resp["error"].(map[string]any)
	if int(errObj["code"].(float64)) != errCodeInvalidRequest {
		t.Errorf("code = %v, want %d (InvalidRequest)", errObj["code"], errCodeInvalidRequest)
	}
}

// TestToolsListBeforeInitializeRejected mirrors the above for
// tools/list.
func TestToolsListBeforeInitializeRejected(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {})
	resp := ts.roundtrip(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      101,
		"method":  "tools/list",
	})
	if resp["error"] == nil {
		t.Fatalf("expected error for pre-handshake tools/list, got %#v", resp)
	}
	errObj := resp["error"].(map[string]any)
	if int(errObj["code"].(float64)) != errCodeInvalidRequest {
		t.Errorf("code = %v, want %d (InvalidRequest)", errObj["code"], errCodeInvalidRequest)
	}
}

// --- tools/list --------------------------------------------------------

func TestToolsList(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "should not be called", http.StatusInternalServerError)
	})
	resp := ts.roundtripAfterInit(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})
	if resp["error"] != nil {
		t.Fatalf("tools/list errored: %v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools missing: %#v", result)
	}

	// Lock the catalog: the senior plan calls for these ten tools.
	want := map[string]bool{
		"basement_list_regions":        false,
		"basement_list_buckets":        false,
		"basement_list_objects":        false,
		"basement_get_object_metadata": false,
		"basement_search":              false,
		"basement_list_backups":        false,
		"basement_list_federations":    false,
		"basement_list_audit":          false,
		"basement_create_share":        false,
		"basement_create_backup_run":   false,
	}
	for _, t0 := range tools {
		entry := t0.(map[string]any)
		name := entry["name"].(string)
		if _, expected := want[name]; expected {
			want[name] = true
		} else {
			t.Errorf("unexpected tool in catalog: %s", name)
		}
		if entry["description"] == "" {
			t.Errorf("tool %s missing description", name)
		}
		if _, ok := entry["inputSchema"]; !ok {
			t.Errorf("tool %s missing inputSchema", name)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected tool not in catalog: %s", name)
		}
	}
}

// --- tools/call: list_regions ----------------------------------------

func TestToolsCallListRegions(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/user/regions" {
			http.Error(w, "wrong path: "+r.URL.Path, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"reg-1","alias":"primary","endpoint":"https://s3.example.com"}]`))
	})
	resp := ts.roundtripAfterInit(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "basement_list_regions",
			"arguments": map[string]any{},
		},
	})
	if resp["error"] != nil {
		t.Fatalf("tools/call errored: %v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("tool returned isError=true: %#v", result)
	}
	content := result["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content len = %d, want 1", len(content))
	}
	block := content[0].(map[string]any)
	text := block["text"].(string)
	if !strings.Contains(text, "reg-1") || !strings.Contains(text, "primary") {
		t.Errorf("payload not echoed back: %s", text)
	}

	// Bearer header MUST be on the upstream call.
	last := ts.calls.last()
	if last.Auth != "Bearer BMNT00000000aaaa:test-secret" {
		t.Errorf("upstream Authorization = %q", last.Auth)
	}
}

// --- tools/call: list_objects translates query params correctly -------

func TestToolsCallListObjectsQuery(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"objects":[],"commonPrefixes":[]}`))
	})
	_ = ts.roundtripAfterInit(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "basement_list_objects",
			"arguments": map[string]any{
				"region_id": "reg-42",
				"bucket":    "photos",
				"prefix":    "2026/",
				"limit":     50,
			},
		},
	})
	got := ts.calls.last()
	if got.Path != "/api/v1/user/regions/reg-42/buckets/photos/objects" {
		t.Errorf("path = %q", got.Path)
	}
	// Query param order isn't guaranteed by url.Values.Encode, but
	// each of these must appear.
	for _, want := range []string{"prefix=2026%2F", "limit=50"} {
		if !strings.Contains(got.Query, want) {
			t.Errorf("query %q missing %q", got.Query, want)
		}
	}
}

// --- tools/call: search returns NOT_IMPLEMENTED as isError --------

func TestToolsCallSearchPlaceholder(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "search shouldn't hit upstream", http.StatusInternalServerError)
	})
	resp := ts.roundtripAfterInit(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      5,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "basement_search",
			"arguments": map[string]any{"query": "broadcom"},
		},
	})
	result := resp["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("search must return isError=true: %#v", result)
	}
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "NOT_IMPLEMENTED") {
		t.Errorf("expected NOT_IMPLEMENTED message, got %q", text)
	}
}

// --- tools/call: API error maps to isError + message ---------------

func TestToolsCallAPIErrorMapsToIsError(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"SERVICE_ACCOUNT_REVOKED","message":"This service account has been revoked"}}`))
	})
	resp := ts.roundtripAfterInit(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      6,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "basement_list_regions",
			"arguments": map[string]any{},
		},
	})
	// RPC level is still success — the tool result carries the failure.
	if resp["error"] != nil {
		t.Fatalf("RPC should not error for upstream-failure: %v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError=true")
	}
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "SERVICE_ACCOUNT_REVOKED") {
		t.Errorf("error code not surfaced: %s", text)
	}
}

// --- tools/call: unknown tool ------------------------------------

func TestToolsCallUnknownTool(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "should not be called", http.StatusInternalServerError)
	})
	resp := ts.roundtripAfterInit(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "basement_no_such_tool",
		},
	})
	if resp["error"] == nil {
		t.Fatal("expected RPC error for unknown tool")
	}
	errObj := resp["error"].(map[string]any)
	if int(errObj["code"].(float64)) != errCodeMethodNotFound {
		t.Errorf("code = %v, want %d", errObj["code"], errCodeMethodNotFound)
	}
}

// --- Method routing ----------------------------------------------

func TestUnknownMethod(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {})
	resp := ts.roundtrip(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      8,
		"method":  "resources/list", // we don't advertise resources
	})
	if resp["error"] == nil {
		t.Fatal("expected method-not-found")
	}
}

func TestParseError(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {})
	in, inW := io.Pipe()
	out := &bytes.Buffer{}
	done := make(chan error, 1)
	go func() { done <- ts.srv.Serve(in, out) }()
	_, _ = inW.Write([]byte("not json\n"))
	_ = inW.Close()
	if err := <-done; err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(bytes.TrimRight(out.Bytes(), "\n"), &resp); err != nil {
		t.Fatalf("decode resp: %v: %q", err, out.String())
	}
	if resp["error"] == nil {
		t.Fatal("expected parse error envelope")
	}
}

func TestNotificationNoResponse(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {})
	resp := ts.roundtrip(t, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		// no id -> notification
	})
	if resp != nil {
		t.Errorf("notifications must not get a response, got %#v", resp)
	}
}

// --- create_share builds expected wire shape --------------------

func TestCreateShareWireShape(t *testing.T) {
	// Pin the clock so the RFC3339 string is deterministic.
	prev := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFunc = prev })

	var sawBody map[string]any
	ts := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &sawBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":"abc123","url":"https://example.com/share/abc123"}`))
	})
	resp := ts.roundtripAfterInit(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      9,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "basement_create_share",
			"arguments": map[string]any{
				"region_id":          "reg-1",
				"bucket":             "shared",
				"key":                "report.pdf",
				"expires_in_seconds": 3600,
			},
		},
	})
	result := resp["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Fatalf("unexpected isError: %#v", result)
	}
	if sawBody["connectionId"] != "reg-1" {
		t.Errorf("connectionId = %v", sawBody["connectionId"])
	}
	if sawBody["bucketId"] != "shared" {
		t.Errorf("bucketId = %v", sawBody["bucketId"])
	}
	if sawBody["key"] != "report.pdf" {
		t.Errorf("key = %v", sawBody["key"])
	}
	expected := "2026-05-22T13:00:00Z"
	if got, _ := sawBody["expiresAt"].(string); got != expected {
		t.Errorf("expiresAt = %q, want %q", got, expected)
	}
	if _, hasPrefix := sawBody["prefix"]; hasPrefix {
		t.Errorf("prefix should be omitted when key is set: %v", sawBody)
	}
}

// --- create_share rejects ambiguous prefix+key combo -----------

func TestCreateShareRejectsBothPrefixAndKey(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("upstream should not be called when args are invalid")
	})
	resp := ts.roundtripAfterInit(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      10,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "basement_create_share",
			"arguments": map[string]any{
				"region_id": "r",
				"bucket":    "b",
				"prefix":    "x/",
				"key":       "y",
			},
		},
	})
	result := resp["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); !isErr {
		t.Fatalf("expected isError=true: %#v", result)
	}
}

// --- backup run posts to expected path ---------------------------

func TestBackupRunPath(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"bk-99","status":"queued"}`))
	})
	_ = ts.roundtripAfterInit(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      11,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "basement_create_backup_run",
			"arguments": map[string]any{"backup_id": "bk-99"},
		},
	})
	last := ts.calls.last()
	if last.Method != http.MethodPost {
		t.Errorf("method = %s", last.Method)
	}
	if last.Path != "/api/v1/user/backups/bk-99/run" {
		t.Errorf("path = %s", last.Path)
	}
}

// --- helper: handlers exist for every tool -----------------------

func TestEveryToolHasHandler(t *testing.T) {
	ts := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {})
	if len(ts.srv.tools) == 0 {
		t.Fatal("tool catalog empty")
	}
	for _, tool := range ts.srv.tools {
		if tool.Handler == nil {
			t.Errorf("tool %s has nil handler", tool.Name)
		}
		if len(tool.InputSchema) == 0 {
			t.Errorf("tool %s has empty inputSchema", tool.Name)
		}
		// Schema must be valid JSON object.
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Errorf("tool %s schema not JSON: %v", tool.Name, err)
		}
	}
}

// Smoke: build a tool list independent of NewServer so we know
// the catalog isn't accidentally empty during refactors.
func TestBuildToolsNotEmpty(t *testing.T) {
	c := clilib.NewClient("http://example.test", "k", "s")
	srv := NewServer(c, slog.New(slog.DiscardHandler))
	tools := buildTools(srv)
	if len(tools) < 10 {
		t.Errorf("tools count = %d, want >= 10", len(tools))
	}
}
