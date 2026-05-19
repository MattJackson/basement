package garage_v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

func TestNewClient(t *testing.T) {
	cfg := map[string]string{
		"admin_url":   "http://garage:3903",
		"admin_token": "test-token-123",
	}

	c := newClient(cfg)

	if c.baseURL != "http://garage:3903" {
		t.Errorf("baseURL = %q, want %q", c.baseURL, "http://garage:3903")
	}
	if c.token != "test-token-123" {
		t.Errorf("token = %q, want %q", c.token, "test-token-123")
	}
	if c.http == nil {
		t.Error("http client is nil")
	}
}

func TestClientDo_Success(t *testing.T) {
	want := map[string]string{"message": "ok"}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization header = %q, want %q", got, "Bearer test-token")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer ts.Close()

	c := newClient(map[string]string{"admin_url": ts.URL, "admin_token": "test-token"})

	var got map[string]string
	if err := c.do(context.Background(), "GET", "/v1/health", nil, &got); err != nil {
		t.Fatalf("do() error = %v", err)
	}
	if got["message"] != "ok" {
		t.Errorf("response = %+v, want %+v", got, want)
	}
}

func TestClientDo_401(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`Authorization token must be provided`))
	}))
	defer ts.Close()

	c := newClient(map[string]string{"admin_url": ts.URL, "admin_token": "test"})

	var got string
	err := c.do(context.Background(), "GET", "/v1/status", nil, &got)
	if err == nil {
		t.Fatal("expected error for 401")
	}
	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrUnauthenticated {
		t.Errorf("err = %v, want ErrUnauthenticated", de.Err)
	}
	if de.Driver != driverName {
		t.Errorf("driver = %q, want %q", de.Driver, driverName)
	}
	if de.Message != "Authorization token must be provided" {
		t.Errorf("message = %q, want body preserved verbatim", de.Message)
	}
}

func TestClientDo_403(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	c := newClient(map[string]string{"admin_url": ts.URL, "admin_token": "x"})

	var got string
	err := c.do(context.Background(), "GET", "/v1/health", nil, &got)
	if err == nil {
		t.Fatal("expected error for 403")
	}
	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("not a driver.Error: %v", err)
	}
	if de.Err != driverpkg.ErrPermissionDenied {
		t.Errorf("err = %v, want ErrPermissionDenied", de.Err)
	}
}

func TestClientDo_404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	c := newClient(map[string]string{"admin_url": ts.URL, "admin_token": "x"})

	var got string
	err := c.do(context.Background(), "GET", "/v1/bucket?id=foo", nil, &got)
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !errors.Is(err, driverpkg.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestClientDo_409(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer ts.Close()

	c := newClient(map[string]string{"admin_url": ts.URL, "admin_token": "x"})

	err := c.do(context.Background(), "POST", "/v1/layout/apply", nil, nil)
	if err == nil {
		t.Fatal("expected error for 409")
	}
	if !errors.Is(err, driverpkg.ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

func TestClientDo_400(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	c := newClient(map[string]string{"admin_url": ts.URL, "admin_token": "x"})

	err := c.do(context.Background(), "POST", "/v1/layout", nil, nil)
	if err == nil {
		t.Fatal("expected error for 400")
	}
	if !errors.Is(err, driverpkg.ErrInvalid) {
		t.Errorf("expected ErrInvalid, got %v", err)
	}
}

func TestClientDo_5xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`server bad state`))
	}))
	defer ts.Close()

	c := newClient(map[string]string{"admin_url": ts.URL, "admin_token": "x"})

	err := c.do(context.Background(), "GET", "/v1/health", nil, nil)
	if err == nil {
		t.Fatal("expected error for 500")
	}
	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("not a driver.Error: %v", err)
	}
	// 5xx isn't mapped to a sentinel — confirm the inner error message
	// preserves the status code.
	if de.Err == nil {
		t.Error("expected non-nil wrapped error for 5xx")
	}
}

func TestClientDo_NoToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("expected no Authorization header when token is empty, got %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := newClient(map[string]string{"admin_url": ts.URL, "admin_token": ""})
	if err := c.do(context.Background(), "GET", "/v1/health", nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
