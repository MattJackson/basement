package garage

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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(want)
	}))
	defer ts.Close()

	cfg := map[string]string{
		"admin_url":   ts.URL,
		"admin_token": "test-token",
	}

	c := newClient(cfg)

	var got map[string]string
	err := c.do(context.Background(), "TestOp", "/", nil, &got)
	if err != nil {
		t.Fatalf("do() error = %v", err)
	}

	if len(got) == 0 || got["message"] != "ok" {
		t.Errorf("response = %+v, want %+v", got, want)
	}
}

func TestClientDo_401(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	cfg := map[string]string{
		"admin_url":   ts.URL,
		"admin_token": "test-token",
	}

	c := newClient(cfg)

	var got string
	err := c.do(context.Background(), "TestOp", "/", nil, &got)

	if err == nil {
		t.Fatal("expected error for 401")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}

	if driverErr.Err != driverpkg.ErrUnauthenticated {
		t.Errorf("err = %v, want ErrUnauthenticated", driverErr.Err)
	}
}

func TestClientDo_403(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	cfg := map[string]string{
		"admin_url":   ts.URL,
		"admin_token": "test-token",
	}

	c := newClient(cfg)

	var got string
	err := c.do(context.Background(), "TestOp", "/", nil, &got)

	if err == nil {
		t.Fatal("expected error for 403")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}

	if driverErr.Err != driverpkg.ErrPermissionDenied {
		t.Errorf("err = %v, want ErrPermissionDenied", driverErr.Err)
	}
}

func TestClientDo_404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	cfg := map[string]string{
		"admin_url":   ts.URL,
		"admin_token": "test-token",
	}

	c := newClient(cfg)

	var got string
	err := c.do(context.Background(), "TestOp", "/", nil, &got)

	if err == nil {
		t.Fatal("expected error for 404")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}

	if driverErr.Err != driverpkg.ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", driverErr.Err)
	}
}

func TestClientDo_5xx(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	cfg := map[string]string{
		"admin_url":   ts.URL,
		"admin_token": "test-token",
	}

	c := newClient(cfg)

	var got string
	err := c.do(context.Background(), "TestOp", "/", nil, &got)

	if err == nil {
		t.Fatal("expected error for 500")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}
}
