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

func TestGetBucket(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/GetBucketInfo" || r.Method != "GET" {
			t.Errorf("expected GET /v2/GetBucketInfo, got %s %s", r.Method, r.URL.Path)
		}

		response := getBucketInfoResponse{
			ID:              "test-bucket-123",
			Created:         time.Now(),
			GlobalAliases:   []string{"my-bucket"},
			WebsiteAccess:   false,
			Objects:         42,
			Bytes:           1024 * 1024,
			Quotas:          &apiBucketQuotas{MaxSize: int64Ptr(1000000), MaxObjects: int64Ptr(100)},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}
	d := &driver{client: client}

	bucket, err := d.GetBucket(context.Background(), "test-bucket-123")
	if err != nil {
		t.Fatalf("GetBucket failed: %v", err)
	}

	if bucket.ID != "test-bucket-123" {
		t.Errorf("expected ID 'test-bucket-123', got '%s'", bucket.ID)
	}

	if len(bucket.Aliases) != 1 || bucket.Aliases[0] != "my-bucket" {
		t.Errorf("expected aliases ['my-bucket'], got %v", bucket.Aliases)
	}

	if bucket.Quotas == nil {
		t.Fatal("expected non-nil Quotas")
	}

	if *bucket.Quotas.MaxSize != 1000000 || *bucket.Quotas.MaxObjects != 100 {
		t.Errorf("unexpected quotas: maxSize=%v, maxObjects=%v", bucket.Quotas.MaxSize, bucket.Quotas.MaxObjects)
	}
}

func TestGetBucketNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "bucket not found"}`))
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	_, err := d.GetBucket(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent bucket, got nil")
	}

	if !errors.Is(err, driverpkg.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestCreateBucket(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/CreateBucket" || r.Method != "POST" {
			t.Errorf("expected POST /v2/CreateBucket, got %s %s", r.Method, r.URL.Path)
		}

		var req createBucketRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.GlobalAlias == nil || *req.GlobalAlias != "new-bucket" {
			t.Errorf("expected GlobalAlias 'new-bucket', got %v", req.GlobalAlias)
		}

		response := getBucketInfoResponse{
			ID:              "created-bucket-456",
			Created:         time.Now(),
			GlobalAliases:   []string{"new-bucket"},
			WebsiteAccess:   false,
			Objects:         0,
			Bytes:           0,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	bucket, err := d.CreateBucket(context.Background(), driverpkg.BucketSpec{Alias: "new-bucket"})
	if err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}

	if bucket.ID != "created-bucket-456" {
		t.Errorf("expected ID 'created-bucket-456', got '%s'", bucket.ID)
	}

	if len(bucket.Aliases) != 1 || bucket.Aliases[0] != "new-bucket" {
		t.Errorf("expected aliases ['new-bucket'], got %v", bucket.Aliases)
	}
}

func TestCreateBucketError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	_, err := d.CreateBucket(context.Background(), driverpkg.BucketSpec{Alias: "new-bucket"})
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}

func TestUpdateBucket(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/UpdateBucket" || r.Method != "POST" {
			t.Errorf("expected POST /v2/UpdateBucket, got %s %s", r.Method, r.URL.Path)
		}

		var req updateBucketRequestBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		response := getBucketInfoResponse{
			ID:              "updated-bucket-789",
			Created:         time.Now(),
			GlobalAliases:   []string{"new-name"},
			WebsiteAccess:   true,
			Objects:         10,
			Bytes:           5000,
			Quotas:          &apiBucketQuotas{MaxSize: int64Ptr(2000000), MaxObjects: nil},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	newAliases := []string{"new-name"}
	update := driverpkg.BucketUpdate{
		Aliases: &newAliases,
		Quotas:  &driverpkg.Quotas{MaxSize: int64Ptr(2000000)},
	}

	bucket, err := d.UpdateBucket(context.Background(), "updated-bucket-789", update)
	if err != nil {
		t.Fatalf("UpdateBucket failed: %v", err)
	}

	if bucket.ID != "updated-bucket-789" {
		t.Errorf("expected ID 'updated-bucket-789', got '%s'", bucket.ID)
	}

	if len(bucket.Aliases) != 1 || bucket.Aliases[0] != "new-name" {
		t.Errorf("expected aliases ['new-name'], got %v", bucket.Aliases)
	}
}

func TestUpdateBucketNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "bucket not found"}`))
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	_, err := d.UpdateBucket(context.Background(), "nonexistent", driverpkg.BucketUpdate{})
	if err == nil {
		t.Fatal("expected error for nonexistent bucket, got nil")
	}
}

func TestDeleteBucket(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/DeleteBucket" || r.Method != "POST" {
			t.Errorf("expected POST /v2/DeleteBucket, got %s %s", r.Method, r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	err := d.DeleteBucket(context.Background(), "bucket-to-delete")
	if err != nil {
		t.Fatalf("DeleteBucket failed: %v", err)
	}
}

func TestDeleteBucketNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "bucket not found"}`))
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	err := d.DeleteBucket(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent bucket, got nil")
	}
}

func int64Ptr(i int64) *int64 {
	return &i
}
