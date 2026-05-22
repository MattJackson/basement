// Package main: the `basement` CLI binary. client.go is the HTTP
// transport for every subcommand — it speaks to basement's JSON
// API via bearer-auth (service-account AKID + secret) that ships in
// v1.7.0b (see internal/auth/bearer.go).
//
// Three primitives — GetJSON / PostJSON / DeleteJSON — are enough to
// cover every endpoint the CLI calls in v1.8.0a. Subcommands keep
// their wire shapes private to their own file; the client only knows
// how to dispatch a verb at a path with a JSON body and decode the
// response.
//
// Auth: every request carries "Authorization: Bearer {AKID}:{Secret}"
// per the bearer middleware's grammar. No cookie path — the CLI is a
// scripted client, not a browser session.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a thin HTTP client for the basement JSON API. Endpoint
// is the base URL (e.g. https://basement.pq.io); the API prefix
// (/api/v1) is concatenated by the caller on each request path.
//
// The HTTP client carries a sane default timeout — CLI calls should
// never hang the operator's terminal — but presigned downloads /
// uploads bypass it via their own contexts (a 1GB upload can legitimately
// run longer than 30s).
type Client struct {
	endpoint string
	akid     string
	secret   string
	http     *http.Client
}

// NewClient builds a Client pointing at the given endpoint with the
// supplied bearer creds. Endpoint is trimmed of trailing slashes so
// path concatenation is unambiguous.
func NewClient(endpoint, akid, secret string) *Client {
	return &Client{
		endpoint: strings.TrimRight(endpoint, "/"),
		akid:     akid,
		secret:   secret,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// APIError is returned from request when the server speaks the
// basement error envelope. Subcommands print these as
// "{code}: {message}" — the wire envelope is internal/api/errors.go's
// ErrorResponse shape.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Message)
}

// errorEnvelope mirrors api.ErrorResponse so we can decode + unwrap.
type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// urlFor joins the client endpoint with /api/v1 + the supplied path.
// Path may start with "/" or not — either way one slash separates.
func (c *Client) urlFor(path string) string {
	p := strings.TrimLeft(path, "/")
	return c.endpoint + "/api/v1/" + p
}

// do is the single request fan-in. method+path build the URL, body is
// pre-encoded JSON (or nil), out (if non-nil) is JSON-decoded from the
// 2xx response body. Non-2xx responses are decoded into an APIError so
// callers get the typed code + message without re-parsing.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader, out any) error {
	if c.endpoint == "" {
		return errors.New("client endpoint is empty — run `basement login` first")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.urlFor(path), body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if c.akid != "" && c.secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.akid+":"+c.secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, c.urlFor(path), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || resp.StatusCode == http.StatusNoContent {
			// Drain so the connection can be reused.
			_, _ = io.Copy(io.Discard, resp.Body)
			return nil
		}
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(out); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("decode response: %w", err)
		}
		return nil
	}

	// Try to decode the envelope; fall back to raw body if it isn't JSON.
	var env errorEnvelope
	buf, _ := io.ReadAll(resp.Body)
	if json.Unmarshal(buf, &env) == nil && env.Error.Code != "" {
		return &APIError{Status: resp.StatusCode, Code: env.Error.Code, Message: env.Error.Message}
	}
	msg := strings.TrimSpace(string(buf))
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	return &APIError{Status: resp.StatusCode, Message: msg}
}

// GetJSON sends a GET and decodes the JSON response into out. out may
// be nil for endpoints whose response body is uninteresting.
func (c *Client) GetJSON(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

// PostJSON sends a POST whose body is the JSON encoding of body (nil =
// no body), decoding the response into out. body may be nil for
// "action" endpoints that take no payload (rotate, etc.).
func (c *Client) PostJSON(ctx context.Context, path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode body: %w", err)
		}
		r = bytes.NewReader(buf)
	}
	return c.do(ctx, http.MethodPost, path, r, out)
}

// DeleteJSON sends a DELETE. Out is decoded from the response on 2xx
// (e.g. /admin/clusters/{id} returns {"message": "..."}).
func (c *Client) DeleteJSON(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodDelete, path, nil, out)
}

// pathEscape is a small helper for splat-style segments where the
// segment itself may contain slashes (object keys mainly). It encodes
// each "/" as %2F so chi sees a single path parameter rather than
// nested folders. Used by object commands.
func pathEscape(s string) string {
	// We want %2F for slashes so the chi /{key} param captures the
	// whole key; url.PathEscape leaves "/" alone.
	return strings.ReplaceAll(url.PathEscape(s), "%2F", "%2F")
}
