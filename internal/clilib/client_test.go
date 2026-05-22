package clilib

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGetJSONHappyPath verifies the basic GET round-trip: bearer
// header attached, /api/v1 prefix applied, JSON body decoded.
func TestGetJSONHappyPath(t *testing.T) {
	var sawAuth string
	var sawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"hello": "world"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "BMNT00000000ffff", "topsecret")
	var out struct {
		Hello string `json:"hello"`
	}
	if err := c.GetJSON(context.Background(), "user/regions", &out); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if out.Hello != "world" {
		t.Errorf("body decoded wrong: %+v", out)
	}
	if sawAuth != "Bearer BMNT00000000ffff:topsecret" {
		t.Errorf("auth header = %q", sawAuth)
	}
	if sawPath != "/api/v1/user/regions" {
		t.Errorf("path = %q, want /api/v1/user/regions", sawPath)
	}
}

// TestPostJSONBody covers the POST path including JSON request-body
// encoding + Content-Type header.
func TestPostJSONBody(t *testing.T) {
	type req struct {
		Name string `json:"name"`
	}
	type resp struct {
		ID string `json:"id"`
	}

	var sawBody req
	var sawCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&sawBody)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(resp{ID: "abc"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "s")
	var got resp
	if err := c.PostJSON(context.Background(), "user/shares", req{Name: "demo"}, &got); err != nil {
		t.Fatalf("PostJSON: %v", err)
	}
	if got.ID != "abc" {
		t.Errorf("ID = %q", got.ID)
	}
	if sawBody.Name != "demo" {
		t.Errorf("body not delivered: %+v", sawBody)
	}
	if sawCT != "application/json" {
		t.Errorf("Content-Type = %q", sawCT)
	}
}

// TestErrorEnvelope decodes the basement-server error shape so
// callers see a typed code, not a raw HTTP status line.
func TestErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"SERVICE_ACCOUNT_REVOKED","message":"This service account has been revoked"}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "s")
	err := c.GetJSON(context.Background(), "user/regions", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("not APIError: %v", err)
	}
	if apiErr.Code != "SERVICE_ACCOUNT_REVOKED" {
		t.Errorf("code = %q", apiErr.Code)
	}
	if apiErr.Status != http.StatusUnauthorized {
		t.Errorf("status = %d", apiErr.Status)
	}
}

// TestNonJSONErrorBody covers the "reverse-proxy 502 with an HTML
// body" case — we still surface a useful error, just without the
// typed code.
func TestNonJSONErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>502 Bad Gateway</html>"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "s")
	err := c.GetJSON(context.Background(), "user/regions", nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %v", err)
	}
	if apiErr.Status != http.StatusBadGateway {
		t.Errorf("status = %d", apiErr.Status)
	}
}

// TestEmptyEndpointFails locks the "configure a profile first"
// safety net — without an endpoint we can't even build a URL.
func TestEmptyEndpointFails(t *testing.T) {
	c := NewClient("", "k", "s")
	err := c.GetJSON(context.Background(), "user/regions", nil)
	if err == nil {
		t.Fatal("expected error for empty endpoint")
	}
}
