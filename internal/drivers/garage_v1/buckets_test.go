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

func TestGetBucket(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/bucket" || r.Method != "GET" {
			t.Errorf("expected GET /v1/bucket, got %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("id"); got != "b-123" {
			t.Errorf("id query = %q, want b-123", got)
		}
		_ = json.NewEncoder(w).Encode(bucketInfoV1{
			ID:            "b-123",
			GlobalAliases: []string{"my-bucket"},
			Objects:       42,
			Bytes:         1024,
			Quotas:        &bucketQuotasV1{MaxSize: int64Ptr(1000000), MaxObjects: int64Ptr(100)},
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
	if b.Quotas == nil || *b.Quotas.MaxSize != 1000000 || *b.Quotas.MaxObjects != 100 {
		t.Errorf("Quotas = %+v", b.Quotas)
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
