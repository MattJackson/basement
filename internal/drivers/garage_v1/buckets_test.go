package garage_v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

func TestListBuckets(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// httptest will split the path before "?". The full RequestURI is
		// what the client sends; check that ?list is present.
		if r.URL.Path != "/v1/bucket" || r.URL.RawQuery != "list" || r.Method != "GET" {
			t.Errorf("expected GET /v1/bucket?list, got %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode([]listBucketsItemV1{
			{ID: "bucket-a", GlobalAliases: []string{"docs"}},
			{ID: "bucket-b", GlobalAliases: []string{"site.com", "www.site.com"}},
		})
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	buckets, err := d.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets: %v", err)
	}
	if len(buckets) != 2 {
		t.Fatalf("got %d buckets, want 2", len(buckets))
	}
	if buckets[0].ID != "bucket-a" || buckets[0].Aliases[0] != "docs" {
		t.Errorf("buckets[0] = %+v", buckets[0])
	}
	if len(buckets[1].Aliases) != 2 {
		t.Errorf("buckets[1].Aliases = %v, want 2 entries", buckets[1].Aliases)
	}
}

func TestListBuckets_403(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	_, err := d.ListBuckets(context.Background())
	if !errors.Is(err, driverpkg.ErrPermissionDenied) {
		t.Errorf("err = %v, want ErrPermissionDenied", err)
	}
}

// TestListBuckets_S3Fallback_Garage404_ReturnsEmpty: Garage's S3
// data-plane endpoint does NOT implement ListBuckets and returns 404.
// We translate that specific case to an empty list so the user sees
// the friendly empty-state instead of a scary backend error — they
// can still navigate to a known bucket by URL on Garage.
func TestListBuckets_S3Fallback_Garage404_ReturnsEmpty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>NoSuchKey</Code></Error>`))
	}))
	defer ts.Close()

	d := &driver{
		client:     &client{baseURL: "", http: &http.Client{}},
		s3Endpoint: ts.URL,
	}
	s3c, err := newS3Client(map[string]string{
		"s3_endpoint":   ts.URL,
		"access_key_id": "AK",
		"secret_key":    "SK",
	})
	if err != nil {
		t.Fatalf("newS3Client: %v", err)
	}
	d.s3Client = s3c

	buckets, err := d.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets on Garage 404 should not error, got %v", err)
	}
	if len(buckets) != 0 {
		t.Errorf("expected empty bucket list on Garage 404, got %+v", buckets)
	}
}

// TestListBuckets_S3Fallback_ADR0002 verifies that when the driver is
// built without an admin URL (the region-tier path introduced by
// ADR-0002 / v1.1.0c), ListBuckets falls back to the S3 ListBuckets
// API. Without this fallback, every user-tier region call would 500.
func TestListBuckets_S3Fallback_ADR0002(t *testing.T) {
	// Minimal S3-compatible mock: respond to GET / (the AWS SDK's
	// ListBuckets path) with the canonical XML envelope listing two
	// buckets. The SDK accepts an empty Owner block.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Owner><ID>x</ID><DisplayName>x</DisplayName></Owner>
  <Buckets>
    <Bucket><Name>lsi</Name><CreationDate>2026-01-01T00:00:00.000Z</CreationDate></Bucket>
    <Bucket><Name>cheshire</Name><CreationDate>2026-01-01T00:00:00.000Z</CreationDate></Bucket>
  </Buckets>
</ListAllMyBucketsResult>`))
	}))
	defer ts.Close()

	// Build a driver with no admin client (mirrors the region-tier
	// path in registry_ext.go's ForUserRegion, which omits admin_url).
	d := &driver{
		client:     &client{baseURL: "", http: &http.Client{}},
		s3Endpoint: ts.URL,
	}
	s3c, err := newS3Client(map[string]string{
		"s3_endpoint":   ts.URL,
		"access_key_id": "AK",
		"secret_key":    "SK",
	})
	if err != nil {
		t.Fatalf("newS3Client: %v", err)
	}
	d.s3Client = s3c

	buckets, err := d.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets fallback: %v", err)
	}
	if len(buckets) != 2 {
		t.Fatalf("got %d buckets, want 2 (lsi, cheshire); buckets=%+v", len(buckets), buckets)
	}
	// At the region tier the S3 API only knows names — ID == Aliases[0] == name.
	for i, want := range []string{"lsi", "cheshire"} {
		if buckets[i].ID != want || len(buckets[i].Aliases) != 1 || buckets[i].Aliases[0] != want {
			t.Errorf("buckets[%d] = %+v, want id=%q alias=[%q]", i, buckets[i], want, want)
		}
	}
}

func TestGetBucket(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/bucket" || r.Method != "GET" {
			t.Errorf("expected GET /v1/bucket, got %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("id"); got != "b-123" {
			t.Errorf("id query = %q, want b-123", got)
		}
		_ = json.NewEncoder(w).Encode(bucketInfoV1{
			ID:                "b-123",
			GlobalAliases:     []string{"my-bucket"},
			Objects:           42,
			Bytes:             1024 * 1024,
			UnfinishedUploads: 3,
			Keys: []bucketKeyInfoV1{
				{
					AccessKeyID: "key-abc",
					Name:        "Test Key",
					Permissions: bucketKeyPermV1{Read: true, Write: false, Owner: false},
				},
			},
			Quotas: &bucketQuotasV1{MaxSize: int64Ptr(1000000), MaxObjects: int64Ptr(100)},
		})
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	b, err := d.GetBucket(context.Background(), "b-123")
	if err != nil {
		t.Fatalf("GetBucket: %v", err)
	}
	if b.ID != "b-123" {
		t.Errorf("ID = %q, want b-123", b.ID)
	}
	if b.Objects != 42 {
		t.Errorf("Objects = %d, want 42", b.Objects)
	}
	if b.Bytes != 1024*1024 {
		t.Errorf("Bytes = %d, want %d", b.Bytes, 1024*1024)
	}
	if b.UnfinishedUploads != 3 {
		t.Errorf("UnfinishedUploads = %d, want 3", b.UnfinishedUploads)
	}
if len(b.Keys) != 1 {
		t.Fatalf("Keys length = %d, want 1", len(b.Keys))
	}
	if b.Keys[0].KeyID != "key-abc" || b.Keys[0].Read != true || b.Keys[0].Write != false || b.Keys[0].Owner != false {
		t.Errorf("Keys[0] = %+v, want KeyID=key-abc Read=true Write=false Owner=false", b.Keys[0])
	}
	if b.Keys[0].Name != "Test Key" {
		t.Errorf("Keys[0].Name = %q, want Test Key", b.Keys[0].Name)
	}
	if b.Quotas == nil || *b.Quotas.MaxSize != 1000000 || *b.Quotas.MaxObjects != 100 {
		t.Errorf("Quotas = %+v", b.Quotas)
	}
}

func TestGetBucket_Fields(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/bucket" || r.Method != "GET" {
			t.Errorf("expected GET /v1/bucket, got %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(bucketInfoV1{
			ID:                "b-stats",
			GlobalAliases:     []string{"stats-bucket"},
			Objects:           12345,
			Bytes:             9876543210,
			UnfinishedUploads: 7,
			Keys: []bucketKeyInfoV1{
				{
					AccessKeyID: "k-1",
					Name:        "Admin Key",
					Permissions: bucketKeyPermV1{Read: true, Write: true, Owner: true},
				},
				{
					AccessKeyID: "k-2",
					Name:        "Reader Key",
					Permissions: bucketKeyPermV1{Read: true, Write: false, Owner: false},
				},
			},
		})
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	b, err := d.GetBucket(context.Background(), "b-stats")
	if err != nil {
		t.Fatalf("GetBucket: %v", err)
	}
	if b.Objects != 12345 {
		t.Errorf("Objects = %d, want 12345", b.Objects)
	}
	if b.Bytes != 9876543210 {
		t.Errorf("Bytes = %d, want 9876543210", b.Bytes)
	}
	if b.UnfinishedUploads != 7 {
		t.Errorf("UnfinishedUploads = %d, want 7", b.UnfinishedUploads)
	}
	if len(b.Keys) != 2 {
		t.Fatalf("Keys length = %d, want 2", len(b.Keys))
	}
	if b.Keys[0].KeyID != "k-1" || !b.Keys[0].Read || !b.Keys[0].Write || !b.Keys[0].Owner {
		t.Errorf("Keys[0] = %+v", b.Keys[0])
	}
	if b.Keys[1].KeyID != "k-2" || !b.Keys[1].Read || b.Keys[1].Write || b.Keys[1].Owner {
		t.Errorf("Keys[1] = %+v", b.Keys[1])
	}
}

func TestListBuckets_NoStatsOnList(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/bucket" || r.URL.RawQuery != "list" || r.Method != "GET" {
			t.Errorf("expected GET /v1/bucket?list, got %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode([]listBucketsItemV1{
			{ID: "bucket-a", GlobalAliases: []string{"docs"}},
			{ID: "bucket-b", GlobalAliases: []string{"site.com"}},
		})
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	buckets, err := d.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets: %v", err)
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

func TestGetBucket_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	_, err := d.GetBucket(context.Background(), "nope")
	if !errors.Is(err, driverpkg.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestCreateBucket(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/bucket" || r.Method != "POST" {
			t.Errorf("expected POST /v1/bucket, got %s %s", r.Method, r.URL.Path)
		}
		var body createBucketRequestV1
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.GlobalAlias != "new-bucket" {
			t.Errorf("globalAlias = %q, want new-bucket", body.GlobalAlias)
		}
		_ = json.NewEncoder(w).Encode(bucketInfoV1{
			ID:            "created-id",
			GlobalAliases: []string{"new-bucket"},
		})
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	b, err := d.CreateBucket(context.Background(), driverpkg.BucketSpec{Alias: "new-bucket"})
	if err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	if b.ID != "created-id" {
		t.Errorf("ID = %q, want created-id", b.ID)
	}
}

func TestCreateBucket_400(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("alias already exists"))
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	_, err := d.CreateBucket(context.Background(), driverpkg.BucketSpec{Alias: "dup"})
	if !errors.Is(err, driverpkg.ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
	var de *driverpkg.Error
	if !errors.As(err, &de) || !strings.Contains(de.Message, "alias already exists") {
		t.Errorf("expected body in Message, got %v", err)
	}
}

func TestUpdateBucket(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/bucket" || r.Method != "PUT" {
			t.Errorf("expected PUT /v1/bucket, got %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("id") != "b-1" {
			t.Errorf("id = %q, want b-1", r.URL.Query().Get("id"))
		}
		var body updateBucketRequestV1
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Quotas == nil {
			t.Fatal("expected quotas in body")
		}
		if body.Quotas.MaxSize == nil || *body.Quotas.MaxSize != 999 {
			t.Errorf("MaxSize = %v, want 999", body.Quotas.MaxSize)
		}
		_ = json.NewEncoder(w).Encode(bucketInfoV1{ID: "b-1", Quotas: &bucketQuotasV1{MaxSize: int64Ptr(999)}})
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	size := int64(999)
	_, err := d.UpdateBucket(context.Background(), "b-1", driverpkg.BucketUpdate{
		Quotas: &driverpkg.Quotas{MaxSize: &size},
	})
	if err != nil {
		t.Fatalf("UpdateBucket: %v", err)
	}
}

func TestUpdateBucket_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	_, err := d.UpdateBucket(context.Background(), "nope", driverpkg.BucketUpdate{})
	if !errors.Is(err, driverpkg.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestDeleteBucket(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/bucket" || r.Method != "DELETE" {
			t.Errorf("expected DELETE /v1/bucket, got %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("id") != "b-del" {
			t.Errorf("id = %q, want b-del", r.URL.Query().Get("id"))
		}
		// v1 returns 204 on delete (garage-admin-v1.yml:750-751).
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	if err := d.DeleteBucket(context.Background(), "b-del"); err != nil {
		t.Fatalf("DeleteBucket: %v", err)
	}
}

func TestDeleteBucket_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	err := d.DeleteBucket(context.Background(), "nope")
	if !errors.Is(err, driverpkg.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
