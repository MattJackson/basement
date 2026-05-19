package garage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

func TestGetKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/GetKeyInfo" || r.Method != "GET" {
			t.Errorf("expected GET /v2/GetKeyInfo, got %s %s", r.Method, r.URL.Path)
		}

		secret := "test-secret-key-12345"
		response := getKeyInfoResponse{
			ID:     "access-key-id",
			Name:   "Test Key",
			SecretAccessKey: &secret,
			BucketsPermissions: []bucketPermissionResp{
				{BucketID: "bucket-1", Read: true, Write: false, Owner: false},
			},
		}

w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}))
defer server.Close()

d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

key, err := d.GetKey(context.Background(), "access-key-id")
	if err != nil {
		t.Fatalf("GetKey failed: %v", err)
	}

	if key.ID != "access-key-id" {
		t.Errorf("expected ID 'access-key-id', got '%s'", key.ID)
	}

	if key.Name != "Test Key" {
		t.Errorf("expected name 'Test Key', got '%s'", key.Name)
	}
}

func TestGetKeyNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "key not found"}`))
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	_, err := d.GetKey(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent key, got nil")
	}
}

func TestCreateKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/CreateKey" || r.Method != "POST" {
			t.Errorf("expected POST /v2/CreateKey, got %s %s", r.Method, r.URL.Path)
		}

		var req createKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Name != "my-new-key" {
			t.Errorf("expected Name 'my-new-key', got '%s'", req.Name)
		}

		secret := "generated-secret-67890"
		response := getKeyInfoResponse{
			ID:     "new-access-key",
			Name:   "my-new-key",
			SecretAccessKey: &secret,
			BucketsPermissions: []bucketPermissionResp{},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	key, err := d.CreateKey(context.Background(), driverpkg.KeySpec{Name: "my-new-key"})
	if err != nil {
		t.Fatalf("CreateKey failed: %v", err)
	}

	if key.ID != "new-access-key" {
		t.Errorf("expected ID 'new-access-key', got '%s'", key.ID)
	}

	if key.Name != "my-new-key" {
		t.Errorf("expected name 'my-new-key', got '%s'", key.Name)
	}
}

func TestCreateKeyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	_, err := d.CreateKey(context.Background(), driverpkg.KeySpec{Name: "my-new-key"})
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}

func TestUpdateKeyPermissions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/AllowBucketKey" || r.Method != "POST" {
			t.Errorf("expected POST /v2/AllowBucketKey, got %s %s", r.Method, r.URL.Path)
		}

		var req allowBucketKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.AccessKeyID != "test-key-id" {
			t.Errorf("expected AccessKeyID 'test-key-id', got '%s'", req.AccessKeyID)
		}

		if !req.Permissions.Read || !req.Permissions.Write || !req.Permissions.Owner {
			t.Errorf("expected all permissions true, got read=%v write=%v owner=%v", 
				req.Permissions.Read, req.Permissions.Write, req.Permissions.Owner)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	perms := []driverpkg.BucketPermission{
		{BucketID: "bucket-1", Read: true, Write: true, Owner: true},
	}

	err := d.UpdateKeyPermissions(context.Background(), "test-key-id", perms)
	if err != nil {
		t.Fatalf("UpdateKeyPermissions failed: %v", err)
	}
}

func TestUpdateKeyPermissionsMultipleBuckets(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/AllowBucketKey" || r.Method != "POST" {
			t.Errorf("expected POST /v2/AllowBucketKey, got %s %s", r.Method, r.URL.Path)
		}

		var req allowBucketKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		callCount++

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	perms := []driverpkg.BucketPermission{
		{BucketID: "bucket-1", Read: true, Write: false, Owner: false},
		{BucketID: "bucket-2", Read: false, Write: true, Owner: false},
	}

	err := d.UpdateKeyPermissions(context.Background(), "test-key-id", perms)
	if err != nil {
		t.Fatalf("UpdateKeyPermissions failed: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 API calls (one per bucket), got %d", callCount)
	}
}

func TestDeleteKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/DeleteKey" || r.Method != "POST" {
			t.Errorf("expected POST /v2/DeleteKey, got %s %s", r.Method, r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	err := d.DeleteKey(context.Background(), "key-to-delete")
	if err != nil {
		t.Fatalf("DeleteKey failed: %v", err)
	}
}

func TestDeleteKeyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "key not found"}`))
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	err := d.DeleteKey(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent key, got nil")
	}
}
