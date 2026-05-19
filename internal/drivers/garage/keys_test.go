package garage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

func TestListKeys_HappyPath(t *testing.T) {
	wantResp := []listKeysResponseItem{
		{
			ID:         "key-abc123",
			Name:       "admin-key",
			Created:    "2024-01-10T08:00:00Z",
			Expiration: "",
			Expired:    false,
		},
		{
			ID:         "key-def456",
			Name:       "app-key",
			Created:    "2024-03-15T12:30:00Z",
			Expiration: "2025-03-15T12:30:00Z",
			Expired:    false,
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(wantResp)
	}))
	defer ts.Close()

	cfg := map[string]string{
		"admin_url":   ts.URL,
		"admin_token": "test-token",
	}

	d := &driver{client: newClient(cfg)}

	keys, err := d.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys() error = %v", err)
	}

	if len(keys) != 2 {
		t.Errorf("len(keys) = %d, want 2", len(keys))
	}

	if keys[0].ID != "key-abc123" {
		t.Errorf("keys[0].ID = %q, want \"key-abc123\"", keys[0].ID)
	}

	if keys[0].Name != "admin-key" {
		t.Errorf("keys[0].Name = %q, want \"admin-key\"", keys[0].Name)
	}

	expectedTime := time.Date(2024, 1, 10, 8, 0, 0, 0, time.UTC)
	if !keys[0].Created.Equal(expectedTime) {
		t.Errorf("keys[0].Created = %v, want %v", keys[0].Created, expectedTime)
	}

	if keys[1].AllowCreateBucket != false {
		t.Errorf("keys[1].AllowCreateBucket = %v, want false", keys[1].AllowCreateBucket)
	}
}

func TestListKeys_403(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	cfg := map[string]string{
		"admin_url":   ts.URL,
		"admin_token": "test-token",
	}

	d := &driver{client: newClient(cfg)}

	keys, err := d.ListKeys(context.Background())
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

	if keys != nil {
		t.Errorf("keys = %+v, want nil on error", keys)
	}
}

func TestListKeys_500(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	cfg := map[string]string{
		"admin_url":   ts.URL,
		"admin_token": "test-token",
	}

	d := &driver{client: newClient(cfg)}

	keys, err := d.ListKeys(context.Background())
	if err == nil {
		t.Fatal("expected error for 500")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("error is not a driver.Error: %v", err)
	}

	if keys != nil {
		t.Errorf("keys = %+v, want nil on error", keys)
	}
}
