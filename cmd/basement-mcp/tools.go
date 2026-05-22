// Package main — tools.go declares the MCP tool catalog
// basement-mcp exposes to AI agents, and the handler functions
// that translate each tool call into a basement-server API
// request via the clilib client.
//
// Tools are deliberately read-mostly for v1.8.0c. Three write-side
// tools (basement_create_share, basement_create_backup_run, and
// the placeholder basement_search) are included because the
// senior backlog asks for the user-facing "back this up" + "share
// this" affordances; deeper write surfaces (creating buckets,
// rotating keys, deleting backups) wait until v1.9 once we have
// real-world telemetry on which calls operators want LLMs to make.
//
// Each tool's input schema is hand-authored JSON Schema (the MCP
// wire shape) rather than reflected from Go structs — keeping the
// schema explicit means we can describe each field with prose the
// LLM uses to decide when to call the tool, which is the whole
// point of MCP.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/mattjackson/basement/internal/clilib"
)

// Tool is one MCP tool: a name, a description for the LLM, an
// input schema, and a handler that translates the call into a
// basement-server API request.
//
// Handler signature: receives raw JSON arguments (the model's
// generated tool-call arguments) and returns either a text
// response (the result the LLM sees) or an error. Errors from the
// upstream API are returned as errors so server.go can wrap them
// with isError=true; successful results are JSON-serialised so
// the LLM gets structured data it can quote back to the user.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	Handler     func(ctx context.Context, args json.RawMessage) (string, error)
}

// buildTools returns the static tool catalog wired to s.client.
// We construct it once per Server (in NewServer) so handler
// closures can capture the client without paying the cost on
// every tools/list call.
func buildTools(s *Server) []Tool {
	return []Tool{
		{
			Name: "basement_list_regions",
			Description: "List all storage regions the authenticated service account can see. " +
				"A region is one configured backend endpoint (S3, MinIO, Garage, etc.). Returns " +
				"region IDs, aliases (operator-chosen labels), and endpoints. Use this first to " +
				"discover what storage is available before drilling into buckets.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			Handler:     handleListRegions(s.client),
		},
		{
			Name: "basement_list_buckets",
			Description: "List buckets visible to the service account inside a specific region. " +
				"Returns bucket names + creation timestamps. Requires a region_id from " +
				"basement_list_regions.",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"region_id":{"type":"string","description":"Region identifier from basement_list_regions"}
				},
				"required":["region_id"],
				"additionalProperties":false
			}`),
			Handler: handleListBuckets(s.client),
		},
		{
			Name: "basement_list_objects",
			Description: "List objects in a bucket within a region. Supports prefix filtering " +
				"(prefix='reports/2026/') and folder-style navigation via delimiter='/' (which " +
				"is the default — pass delimiter='' for a flat recursive listing). Returns " +
				"object keys, sizes, last-modified timestamps, and any common prefixes (folders).",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"region_id":{"type":"string","description":"Region identifier"},
					"bucket":{"type":"string","description":"Bucket name"},
					"prefix":{"type":"string","description":"Key prefix to filter by (optional)"},
					"delimiter":{"type":"string","description":"Folder delimiter — default '/'; pass '' for flat recursive listing"},
					"limit":{"type":"integer","description":"Max items to return (default 100, server-capped)","minimum":1}
				},
				"required":["region_id","bucket"],
				"additionalProperties":false
			}`),
			Handler: handleListObjects(s.client),
		},
		{
			Name: "basement_get_object_metadata",
			Description: "Get metadata (size, content type, last modified, ETag) for a single " +
				"object without downloading its body. Useful for confirming an object exists or " +
				"checking its size before a downstream operation. The object body is NEVER " +
				"streamed through basement-mcp — for downloads, use basement_create_share to " +
				"mint a presigned URL the user can fetch directly.",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"region_id":{"type":"string"},
					"bucket":{"type":"string"},
					"key":{"type":"string","description":"Full object key"}
				},
				"required":["region_id","bucket","key"],
				"additionalProperties":false
			}`),
			Handler: handleGetObjectMetadata(s.client),
		},
		{
			Name: "basement_search",
			Description: "Search across buckets and objects by free-text query. Returns matching " +
				"objects and the bucket/region they live in. NOTE: this tool is a forward-" +
				"compatible placeholder — full-text search ships in v1.9. Calling it today " +
				"returns NOT_IMPLEMENTED so agents can detect the gap and fall back to " +
				"basement_list_objects + client-side filtering.",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"query":{"type":"string","description":"Free-text search query"}
				},
				"required":["query"],
				"additionalProperties":false
			}`),
			Handler: handleSearchPlaceholder(),
		},
		{
			Name: "basement_list_backups",
			Description: "List user-owned backup configurations. Returns backup IDs, source and " +
				"destination region/bucket pairs, cron schedules, last run status, and disabled " +
				"flags. Use this to find a backup_id before invoking basement_create_backup_run.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			Handler:     handleListBackups(s.client),
		},
		{
			Name: "basement_list_federations",
			Description: "List user-owned federated buckets — replication pairs between two " +
				"basement regions that keep a bucket synchronised across backends. Returns " +
				"federation IDs, the primary + replica region/bucket pairs, replication mode, " +
				"and health status (which side is currently the active primary).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			Handler:     handleListFederations(s.client),
		},
		{
			Name: "basement_list_audit",
			Description: "List audit events (admin tool — requires host:manage_policies on the " +
				"upstream service account). Supports actor/action/resource substring filters " +
				"and a result filter ('success' | 'failure'). Returns up to 'limit' events " +
				"(default 50, max 1000) ordered newest-first.",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"actor":{"type":"string","description":"Filter by actor user ID (exact match)"},
					"action":{"type":"string","description":"Substring filter on action (case-insensitive)"},
					"resource":{"type":"string","description":"Substring filter on resource (case-insensitive)"},
					"result":{"type":"string","enum":["success","failure",""],"description":"Result filter"},
					"limit":{"type":"integer","minimum":1,"maximum":1000,"description":"Max events to return (default 50)"}
				},
				"additionalProperties":false
			}`),
			Handler: handleListAudit(s.client),
		},
		{
			Name: "basement_create_share",
			Description: "Mint a public share token for an object or prefix. Returns the " +
				"share token + public URL the user can hand out. Exactly one of 'prefix' or " +
				"'key' must be set: prefix shares an entire subtree (recipients can browse + " +
				"download), key shares a single object. Optionally accepts expires_in_seconds " +
				"for time-bounded access.",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"region_id":{"type":"string","description":"Region identifier (resolved to a connection internally)"},
					"bucket":{"type":"string","description":"Bucket name or ID"},
					"prefix":{"type":"string","description":"Prefix to share (mutually exclusive with key)"},
					"key":{"type":"string","description":"Single object key to share (mutually exclusive with prefix)"},
					"expires_in_seconds":{"type":"integer","minimum":1,"description":"Share lifetime in seconds (optional; omit for no expiry)"},
					"download_limit":{"type":"integer","minimum":1,"description":"Max downloads (optional)"}
				},
				"required":["region_id","bucket"],
				"additionalProperties":false
			}`),
			Handler: handleCreateShare(s.client),
		},
		{
			Name: "basement_create_backup_run",
			Description: "Trigger an immediate run of an existing backup configuration, " +
				"bypassing the cron schedule. Returns a queued status — the run completes " +
				"asynchronously and shows up in subsequent basement_list_backups calls as " +
				"the latest run history entry. The backup must not be disabled.",
			InputSchema: json.RawMessage(`{
				"type":"object",
				"properties":{
					"backup_id":{"type":"string","description":"Backup ID from basement_list_backups"}
				},
				"required":["backup_id"],
				"additionalProperties":false
			}`),
			Handler: handleCreateBackupRun(s.client),
		},
	}
}

// --- Handlers ---------------------------------------------------------------
//
// Each handler is a closure over the *clilib.Client so server.go can
// dispatch without ferrying the client through every call frame.
// They share two conventions:
//
//   - JSON-RPC arguments are validated via a struct + json.Unmarshal;
//     bad input returns a clear error the LLM can read.
//   - Successful results are re-encoded as pretty JSON text so the
//     LLM (and the human watching the transcript) gets a readable
//     structure rather than a one-line blob.

// prettyJSON marshals v with 2-space indentation. We swallow the
// marshal error and return a fallback string because every value
// we pass here came out of json.Decoder ten lines ago — if it
// fails to re-encode that's an internal bug, not a user-facing
// one, but we still want the LLM to see *something* useful.
func prettyJSON(v any) string {
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("<failed to format JSON result: %v>", err)
	}
	return string(buf)
}

func handleListRegions(c *clilib.Client) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		var resp any
		if err := c.GetJSON(ctx, "user/regions", &resp); err != nil {
			return "", err
		}
		return prettyJSON(resp), nil
	}
}

func handleListBuckets(c *clilib.Client) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, args json.RawMessage) (string, error) {
		var in struct {
			RegionID string `json:"region_id"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		if in.RegionID == "" {
			return "", fmt.Errorf("region_id is required")
		}
		path := fmt.Sprintf("user/regions/%s/buckets", url.PathEscape(in.RegionID))
		var resp any
		if err := c.GetJSON(ctx, path, &resp); err != nil {
			return "", err
		}
		return prettyJSON(resp), nil
	}
}

func handleListObjects(c *clilib.Client) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, args json.RawMessage) (string, error) {
		var in struct {
			RegionID  string `json:"region_id"`
			Bucket    string `json:"bucket"`
			Prefix    string `json:"prefix"`
			Delimiter *string `json:"delimiter"`
			Limit     int    `json:"limit"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		if in.RegionID == "" || in.Bucket == "" {
			return "", fmt.Errorf("region_id and bucket are required")
		}
		q := url.Values{}
		if in.Prefix != "" {
			q.Set("prefix", in.Prefix)
		}
		// delimiter is special: the server defaults to "/", but the
		// caller can request a flat listing by passing "" explicitly.
		// We honour that distinction here — only emit ?delimiter=
		// when the caller actually included the field.
		if in.Delimiter != nil {
			q.Set("delimiter", *in.Delimiter)
		}
		if in.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", in.Limit))
		}
		path := fmt.Sprintf("user/regions/%s/buckets/%s/objects",
			url.PathEscape(in.RegionID), url.PathEscape(in.Bucket))
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
		var resp any
		if err := c.GetJSON(ctx, path, &resp); err != nil {
			return "", err
		}
		return prettyJSON(resp), nil
	}
}

// handleGetObjectMetadata uses the list-objects endpoint with the
// object's exact key as a prefix + limit=1 — that returns the
// object's full ObjectInfo (size, lastModified, etag) without a
// separate HEAD endpoint on the user-region surface. If the
// backend's first match isn't the requested key exactly, we treat
// that as "not found" so the LLM doesn't get a false positive
// from a prefix-match neighbour.
func handleGetObjectMetadata(c *clilib.Client) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, args json.RawMessage) (string, error) {
		var in struct {
			RegionID string `json:"region_id"`
			Bucket   string `json:"bucket"`
			Key      string `json:"key"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		if in.RegionID == "" || in.Bucket == "" || in.Key == "" {
			return "", fmt.Errorf("region_id, bucket, and key are required")
		}
		q := url.Values{}
		q.Set("prefix", in.Key)
		q.Set("delimiter", "") // flat — don't roll the exact-match key into a CommonPrefix
		q.Set("limit", "1")
		path := fmt.Sprintf("user/regions/%s/buckets/%s/objects?%s",
			url.PathEscape(in.RegionID), url.PathEscape(in.Bucket), q.Encode())

		var page struct {
			Objects []map[string]any `json:"objects"`
		}
		if err := c.GetJSON(ctx, path, &page); err != nil {
			return "", err
		}
		if len(page.Objects) == 0 {
			return "", fmt.Errorf("object not found: %s", in.Key)
		}
		// Defensive: the backend may return a prefix-neighbour if
		// the exact key doesn't exist. Compare key field directly.
		first := page.Objects[0]
		if k, ok := first["key"].(string); ok && k != in.Key {
			return "", fmt.Errorf("object not found: %s (closest match: %s)", in.Key, k)
		}
		return prettyJSON(first), nil
	}
}

// handleSearchPlaceholder returns NOT_IMPLEMENTED so agents can
// detect the gap. v1.9 will replace this with a real index-backed
// search; until then the placeholder keeps the tool catalog
// shape stable so client configs don't need to change at the
// upgrade.
func handleSearchPlaceholder() func(context.Context, json.RawMessage) (string, error) {
	return func(_ context.Context, args json.RawMessage) (string, error) {
		var in struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		if strings.TrimSpace(in.Query) == "" {
			return "", fmt.Errorf("query is required")
		}
		return "", fmt.Errorf("NOT_IMPLEMENTED: basement_search ships in v1.9. " +
			"For now, list objects with basement_list_objects and filter client-side.")
	}
}

func handleListBackups(c *clilib.Client) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		var resp any
		if err := c.GetJSON(ctx, "user/backups", &resp); err != nil {
			return "", err
		}
		return prettyJSON(resp), nil
	}
}

func handleListFederations(c *clilib.Client) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		var resp any
		if err := c.GetJSON(ctx, "user/federated-buckets", &resp); err != nil {
			return "", err
		}
		return prettyJSON(resp), nil
	}
}

func handleListAudit(c *clilib.Client) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, args json.RawMessage) (string, error) {
		var in struct {
			Actor    string `json:"actor"`
			Action   string `json:"action"`
			Resource string `json:"resource"`
			Result   string `json:"result"`
			Limit    int    `json:"limit"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		q := url.Values{}
		if in.Actor != "" {
			q.Set("actor", in.Actor)
		}
		if in.Action != "" {
			q.Set("action", in.Action)
		}
		if in.Resource != "" {
			q.Set("resource", in.Resource)
		}
		if in.Result != "" {
			q.Set("result", in.Result)
		}
		if in.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", in.Limit))
		}
		path := "admin/audit"
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
		var resp any
		if err := c.GetJSON(ctx, path, &resp); err != nil {
			return "", err
		}
		return prettyJSON(resp), nil
	}
}

func handleCreateShare(c *clilib.Client) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, args json.RawMessage) (string, error) {
		var in struct {
			RegionID         string `json:"region_id"`
			Bucket           string `json:"bucket"`
			Prefix           string `json:"prefix"`
			Key              string `json:"key"`
			ExpiresInSeconds int    `json:"expires_in_seconds"`
			DownloadLimit    int    `json:"download_limit"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		if in.RegionID == "" || in.Bucket == "" {
			return "", fmt.Errorf("region_id and bucket are required")
		}
		hasPrefix := in.Prefix != ""
		hasKey := in.Key != ""
		if hasPrefix == hasKey {
			return "", fmt.Errorf("exactly one of prefix or key must be set")
		}

		// The user-shares endpoint accepts a connectionId field that
		// the server bridges from region IDs (v1.1.0g). So we can
		// pass region_id through as connectionId; if it's already a
		// connection ID it falls through unchanged.
		body := map[string]any{
			"connectionId": in.RegionID,
			"bucketId":     in.Bucket,
		}
		if hasPrefix {
			body["prefix"] = in.Prefix
		}
		if hasKey {
			body["key"] = in.Key
		}
		if in.ExpiresInSeconds > 0 {
			body["expiresAt"] = rfcTimeFromOffset(in.ExpiresInSeconds)
		}
		if in.DownloadLimit > 0 {
			body["downloadLimit"] = in.DownloadLimit
		}
		var resp any
		if err := c.PostJSON(ctx, "user/shares", body, &resp); err != nil {
			return "", err
		}
		return prettyJSON(resp), nil
	}
}

func handleCreateBackupRun(c *clilib.Client) func(context.Context, json.RawMessage) (string, error) {
	return func(ctx context.Context, args json.RawMessage) (string, error) {
		var in struct {
			BackupID string `json:"backup_id"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		if in.BackupID == "" {
			return "", fmt.Errorf("backup_id is required")
		}
		path := fmt.Sprintf("user/backups/%s/run", url.PathEscape(in.BackupID))
		var resp any
		if err := c.PostJSON(ctx, path, nil, &resp); err != nil {
			return "", err
		}
		return prettyJSON(resp), nil
	}
}
