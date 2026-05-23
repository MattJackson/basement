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
			ID:                "test-bucket-123",
			Created:           time.Now(),
			GlobalAliases:     []string{"my-bucket"},
			WebsiteAccess:     false,
			Objects:           42,
			Bytes:             1024 * 1024,
			UnfinishedUploads: 3,
			Keys: []getBucketInfoKey{
				{
					AccessKeyID: "key-abc",
					Name:        "Test Key",
					Permissions: apiBucketKeyPerm{Read: true, Write: false, Owner: false},
				},
			},
			Quotas: &apiBucketQuotas{MaxSize: int64Ptr(1000000), MaxObjects: int64Ptr(100)},
		}

w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
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

	if bucket.Objects != 42 {
		t.Errorf("Objects = %d, want 42", bucket.Objects)
	}

	if bucket.Bytes != 1024*1024 {
		t.Errorf("Bytes = %d, want %d", bucket.Bytes, 1024*1024)
	}

	if bucket.UnfinishedUploads != 3 {
		t.Errorf("UnfinishedUploads = %d, want 3", bucket.UnfinishedUploads)
	}

	if len(bucket.Keys) != 1 {
		t.Fatalf("Keys length = %d, want 1", len(bucket.Keys))
	}

	if bucket.Keys[0].KeyID != "key-abc" || bucket.Keys[0].Read != true || bucket.Keys[0].Write != false || bucket.Keys[0].Owner != false {
		t.Errorf("Keys[0] = %+v, want KeyID=key-abc Read=true Write=false Owner=false", bucket.Keys[0])
	}

	if bucket.Quotas == nil {
		t.Fatal("expected non-nil Quotas")
	}

	if *bucket.Quotas.MaxSize != 1000000 || *bucket.Quotas.MaxObjects != 100 {
		t.Errorf("unexpected quotas: maxSize=%v, maxObjects=%v", bucket.Quotas.MaxSize, bucket.Quotas.MaxObjects)
	}
}

func TestGetBucket_Fields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/GetBucketInfo" || r.Method != "GET" {
			t.Errorf("expected GET /v2/GetBucketInfo, got %s %s", r.Method, r.URL.Path)
		}

		response := getBucketInfoResponse{
			ID:                "bucket-stats-456",
			Created:           time.Now(),
			GlobalAliases:     []string{"stats-bucket"},
			WebsiteAccess:     false,
			Objects:           12345,
			Bytes:             9876543210,
			UnfinishedUploads: 7,
			Keys: []getBucketInfoKey{
				{
					AccessKeyID: "k-1",
					Name:        "Admin Key",
					Permissions: apiBucketKeyPerm{Read: true, Write: true, Owner: true},
				},
				{
					AccessKeyID: "k-2",
					Name:        "Reader Key",
					Permissions: apiBucketKeyPerm{Read: true, Write: false, Owner: false},
				},
			},
		}

w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	bucket, err := d.GetBucket(context.Background(), "bucket-stats-456")
	if err != nil {
		t.Fatalf("GetBucket failed: %v", err)
	}

	if bucket.Objects != 12345 {
		t.Errorf("Objects = %d, want 12345", bucket.Objects)
	}

	if bucket.Bytes != 9876543210 {
		t.Errorf("Bytes = %d, want 9876543210", bucket.Bytes)
	}

	if bucket.UnfinishedUploads != 7 {
		t.Errorf("UnfinishedUploads = %d, want 7", bucket.UnfinishedUploads)
	}

	if len(bucket.Keys) != 2 {
		t.Fatalf("Keys length = %d, want 2", len(bucket.Keys))
	}

	if bucket.Keys[0].KeyID != "k-1" || !bucket.Keys[0].Read || !bucket.Keys[0].Write || !bucket.Keys[0].Owner {
		t.Errorf("Keys[0] = %+v", bucket.Keys[0])
	}

	if bucket.Keys[1].KeyID != "k-2" || !bucket.Keys[1].Read || bucket.Keys[1].Write || bucket.Keys[1].Owner {
		t.Errorf("Keys[1] = %+v", bucket.Keys[1])
	}
}

func TestListBuckets_NoStatsOnList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/ListBuckets" || r.Method != "GET" {
			t.Errorf("expected GET /v2/ListBuckets, got %s %s", r.Method, r.URL.Path)
		}

		response := []listBucketsResponseItem{
			{ID: "bucket-a", Created: time.Now(), GlobalAliases: []string{"docs"}},
			{ID: "bucket-b", Created: time.Now(), GlobalAliases: []string{"site.com"}},
		}

w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	buckets, err := d.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets failed: %v", err)
	}

	if len(buckets) != 2 {
		t.Fatalf("got %d buckets, want 2", len(buckets))
	}

	for i, b := range buckets {
		if b.Objects != 0 {
			t.Errorf("buckets[%d].Objects = %d, want 0 (list endpoint omits stats)", i, b.Objects)
		}
		if b.Bytes != 0 {
			t.Errorf("buckets[%d].Bytes = %d, want 0 (list endpoint omits stats)", i, b.Bytes)
		}
		if b.UnfinishedUploads != 0 {
			t.Errorf("buckets[%d].UnfinishedUploads = %d, want 0", i, b.UnfinishedUploads)
		}
		if len(b.Keys) != 0 {
			t.Errorf("buckets[%d].Keys length = %d, want 0 (list endpoint omits keys)", i, len(b.Keys))
		}
	}
}

func TestGetBucketNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "bucket not found"}`))
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
	_ = json.NewEncoder(w).Encode(response)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	_, err := d.CreateBucket(context.Background(), driverpkg.BucketSpec{Alias: "new-bucket"})
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}

// TestUpdateBucket verifies the combined-update path: quotas via
// POST /v2/UpdateBucket, aliases via Add/Remove diff, and a final GET
// re-fetch that returns the canonical post-update bucket info.
// v1.11.0.6: BUG01 fix — aliases were silently dropped before this.
func TestUpdateBucket(t *testing.T) {
	var (
		quotaPostHit  bool
		addAliasHit   string
		removeAliasHit string
		getCount      int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/UpdateBucket" && r.Method == "POST":
			quotaPostHit = true
			var req updateBucketRequestBody
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode UpdateBucket request: %v", err)
			}
			if req.Quotas == nil || req.Quotas.MaxSize == nil || *req.Quotas.MaxSize != 2000000 {
				t.Errorf("UpdateBucket body quotas = %+v, want MaxSize=2000000", req.Quotas)
			}
			// Echo back something — driver ignores this on combined
			// updates and trusts the final GET.
			_ = json.NewEncoder(w).Encode(getBucketInfoResponse{ID: "updated-bucket-789"})

		case r.URL.Path == "/v2/AddBucketAlias" && r.Method == "POST":
			var body bucketAliasEnum
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode AddBucketAlias request: %v", err)
			}
			if body.BucketID != "updated-bucket-789" {
				t.Errorf("AddBucketAlias bucketId = %q, want updated-bucket-789", body.BucketID)
			}
			addAliasHit = body.GlobalAlias
			_, _ = w.Write([]byte("{}"))

		case r.URL.Path == "/v2/RemoveBucketAlias" && r.Method == "POST":
			var body bucketAliasEnum
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode RemoveBucketAlias request: %v", err)
			}
			if body.BucketID != "updated-bucket-789" {
				t.Errorf("RemoveBucketAlias bucketId = %q, want updated-bucket-789", body.BucketID)
			}
			removeAliasHit = body.GlobalAlias
			_, _ = w.Write([]byte("{}"))

		case r.URL.Path == "/v2/GetBucketInfo" && r.Method == "GET":
			getCount++
			// First GET (pre-diff) returns the OLD alias so the
			// driver computes a rename. Second GET (final re-fetch)
			// returns the NEW alias.
			aliases := []string{"old-name"}
			if getCount == 2 {
				aliases = []string{"new-name"}
			}
			_ = json.NewEncoder(w).Encode(getBucketInfoResponse{
				ID:            "updated-bucket-789",
				Created:       time.Now(),
				GlobalAliases: aliases,
				Objects:       10,
				Bytes:         5000,
				Quotas:        &apiBucketQuotas{MaxSize: int64Ptr(2000000)},
			})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
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

	if !quotaPostHit {
		t.Error("expected POST /v2/UpdateBucket to be called for quotas")
	}
	if addAliasHit != "new-name" {
		t.Errorf("AddBucketAlias = %q, want new-name", addAliasHit)
	}
	if removeAliasHit != "old-name" {
		t.Errorf("RemoveBucketAlias = %q, want old-name", removeAliasHit)
	}
	if bucket.ID != "updated-bucket-789" {
		t.Errorf("expected ID 'updated-bucket-789', got '%s'", bucket.ID)
	}
	if len(bucket.Aliases) != 1 || bucket.Aliases[0] != "new-name" {
		t.Errorf("expected aliases ['new-name'], got %v", bucket.Aliases)
	}
}

// TestUpdateBucket_QuotasOnly verifies that an update with only quotas
// (Aliases == nil) doesn't touch the alias endpoints — the previous
// behaviour for callers that never set Aliases.
func TestUpdateBucket_QuotasOnly(t *testing.T) {
	var addAliasCalls, removeAliasCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/UpdateBucket" && r.Method == "POST":
			_ = json.NewEncoder(w).Encode(getBucketInfoResponse{ID: "b-q"})
		case r.URL.Path == "/v2/AddBucketAlias":
			addAliasCalls++
		case r.URL.Path == "/v2/RemoveBucketAlias":
			removeAliasCalls++
		case r.URL.Path == "/v2/GetBucketInfo" && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(getBucketInfoResponse{
				ID:            "b-q",
				GlobalAliases: []string{"keep-me"},
				Quotas:        &apiBucketQuotas{MaxSize: int64Ptr(123)},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}
	_, err := d.UpdateBucket(context.Background(), "b-q", driverpkg.BucketUpdate{
		Quotas: &driverpkg.Quotas{MaxSize: int64Ptr(123)},
	})
	if err != nil {
		t.Fatalf("UpdateBucket: %v", err)
	}
	if addAliasCalls != 0 || removeAliasCalls != 0 {
		t.Errorf("quotas-only update touched alias endpoints (add=%d remove=%d)",
			addAliasCalls, removeAliasCalls)
	}
}

// TestDiffAliases covers the add-then-remove ordering invariant and
// the no-op case. The diff helper is the load-bearing piece of the
// BUG01 fix; a regression here would silently mis-apply rename calls.
func TestDiffAliases(t *testing.T) {
	cases := []struct {
		name              string
		current, desired  []string
		wantAdd, wantRemove []string
	}{
		{
			name:    "rename",
			current: []string{"old"}, desired: []string{"new"},
			wantAdd: []string{"new"}, wantRemove: []string{"old"},
		},
		{
			name:    "noop",
			current: []string{"a", "b"}, desired: []string{"b", "a"},
			wantAdd: nil, wantRemove: nil,
		},
		{
			name:    "add-only",
			current: []string{"a"}, desired: []string{"a", "b"},
			wantAdd: []string{"b"}, wantRemove: nil,
		},
		{
			name:    "remove-only",
			current: []string{"a", "b"}, desired: []string{"a"},
			wantAdd: nil, wantRemove: []string{"b"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, r := diffAliases(tc.current, tc.desired)
			if !stringSliceEqual(a, tc.wantAdd) {
				t.Errorf("toAdd = %v, want %v", a, tc.wantAdd)
			}
			if !stringSliceEqual(r, tc.wantRemove) {
				t.Errorf("toRemove = %v, want %v", r, tc.wantRemove)
			}
		})
	}
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestUpdateBucketNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "bucket not found"}`))
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
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	d := &driver{client: &client{baseURL: server.URL, token: "test-token", http: &http.Client{}}}

	err := d.DeleteBucket(context.Background(), "bucket-to-delete")
	if err != nil {
		t.Fatalf("DeleteBucket failed: %v", err)
	}
}

func TestDeleteBucketNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error": "bucket not found"}`))
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
