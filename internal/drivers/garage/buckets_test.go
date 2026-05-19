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

func TestListBuckets_HappyPath(t *testing.T) {
	wantResp := []listBucketsResponseItem{
		{
			ID:            "bucket-123",
			Created:       "2024-01-15T10:30:00Z",
			GlobalAliases: []string{"my-bucket", "data-prod"},
			LocalAliases:  []string{},
		},
		{
			ID:            "bucket-456",
			Created:       "2024-02-20T14:45:00Z",
			GlobalAliases: []string{"logs"},
			LocalAliases:  []string{},
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

	buckets, err := d.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets() error = %v", err)
	}

	if len(buckets) != 2 {
		t.Errorf("len(buckets) = %d, want 2", len(buckets))
	}

	if buckets[0].ID != "bucket-123" {
		t.Errorf("buckets[0].ID = %q, want \"bucket-123\"", buckets[0].ID)
	}

	if len(buckets[0].Aliases) != 2 {
		t.Errorf("len(buckets[0].Aliases) = %d, want 2", len(buckets[0].Aliases))
	}

	expectedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	if !buckets[0].Created.Equal(expectedTime) {
		t.Errorf("buckets[0].Created = %v, want %v", buckets[0].Created, expectedTime)
	}
}

func TestListBuckets_401(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	cfg := map[string]string{
		"admin_url":   ts.URL,
		"admin_token": "test-token",
	}

	d := &driver{client: newClient(cfg)}

	buckets, err := d.ListBuckets(context.Background())
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

	if buckets != nil {
		t.Errorf("buckets = %+v, want nil on error", buckets)
	}
}

func TestListBuckets_404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	cfg := map[string]string{
		"admin_url":   ts.URL,
		"admin_token": "test-token",
	}

	d := &driver{client: newClient(cfg)}

	_, err := d.ListBuckets(context.Background())
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
