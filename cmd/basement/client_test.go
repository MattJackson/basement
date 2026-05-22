// Package main — client_test.go covers the HTTP transport: bearer
// header injection, JSON encode/decode, and APIError unwrapping.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientSendsBearerHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "BMNTtestakid", "topsecret")
	var out struct {
		OK bool `json:"ok"`
	}
	if err := client.GetJSON(context.Background(), "/anything", &out); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	want := "Bearer BMNTtestakid:topsecret"
	if gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	if !out.OK {
		t.Error("decode didn't fill OK")
	}
}

func TestClientPostsJSON(t *testing.T) {
	var gotBody, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "k", "s")
	var out struct {
		ID string `json:"id"`
	}
	if err := client.PostJSON(context.Background(), "/foo", map[string]string{"a": "b"}, &out); err != nil {
		t.Fatalf("PostJSON: %v", err)
	}
	if out.ID != "abc" {
		t.Errorf("decoded id = %q", out.ID)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if !strings.Contains(gotBody, `"a":"b"`) {
		t.Errorf("body = %q", gotBody)
	}
}

func TestClientAPIErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "INVALID_SECRET",
				"message": "Secret did not match",
			},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "k", "s")
	err := client.GetJSON(context.Background(), "/foo", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T (%v)", err, err)
	}
	if apiErr.Code != "INVALID_SECRET" || apiErr.Status != http.StatusUnauthorized {
		t.Errorf("APIError = %+v", apiErr)
	}
	if !strings.Contains(apiErr.Error(), "Secret did not match") {
		t.Errorf("Error() = %q", apiErr.Error())
	}
}

func TestClientDeleteJSON(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "k", "s")
	if err := client.DeleteJSON(context.Background(), "/foo/1", nil); err != nil {
		t.Fatalf("DeleteJSON: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q", gotMethod)
	}
}

func TestClientEmptyEndpoint(t *testing.T) {
	client := NewClient("", "k", "s")
	if err := client.GetJSON(context.Background(), "/foo", nil); err == nil {
		t.Fatal("expected error on empty endpoint")
	}
}

func TestClientURLFor(t *testing.T) {
	c := NewClient("https://example.test/", "k", "s")
	got := c.urlFor("/admin/things")
	want := "https://example.test/api/v1/admin/things"
	if got != want {
		t.Errorf("urlFor = %q, want %q", got, want)
	}
	// Trailing-slash robustness: endpoint trims; leading-slash on
	// path is optional.
	if got := c.urlFor("admin/things"); got != want {
		t.Errorf("urlFor without leading slash = %q, want %q", got, want)
	}
}
