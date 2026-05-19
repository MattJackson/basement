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

func TestListKeys(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/key" || r.URL.RawQuery != "list" || r.Method != "GET" {
			t.Errorf("expected GET /v1/key?list, got %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode([]listKeysItemV1{
			{ID: "GK1", Name: "test-key"},
			{ID: "GK2", Name: ""},
		})
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	keys, err := d.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("got %d keys, want 2", len(keys))
	}
	if keys[0].ID != "GK1" || keys[0].Name != "test-key" || keys[0].AccessKeyID != "GK1" {
		t.Errorf("keys[0] = %+v", keys[0])
	}
}

func TestListKeys_401(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	_, err := d.ListKeys(context.Background())
	if !errors.Is(err, driverpkg.ErrUnauthenticated) {
		t.Errorf("err = %v, want ErrUnauthenticated", err)
	}
}

func TestGetKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/key" || r.Method != "GET" {
			t.Errorf("expected GET /v1/key, got %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("id") != "GK1" {
			t.Errorf("id = %q, want GK1", r.URL.Query().Get("id"))
		}
		_ = json.NewEncoder(w).Encode(keyInfoV1{
			Name:        "test-key",
			AccessKeyID: "GK1",
			Permissions: keyInfoPermsV1{CreateBucket: true},
		})
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	k, err := d.GetKey(context.Background(), "GK1")
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if k.ID != "GK1" || k.Name != "test-key" || !k.AllowCreateBucket {
		t.Errorf("Key = %+v", k)
	}
}

func TestGetKey_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	_, err := d.GetKey(context.Background(), "missing")
	if !errors.Is(err, driverpkg.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestCreateKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/key" || r.Method != "POST" {
			t.Errorf("expected POST /v1/key, got %s %s", r.Method, r.URL.Path)
		}
		var body createKeyRequestV1
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Name != "new-key" {
			t.Errorf("name = %q, want new-key", body.Name)
		}
		secret := "secret-bytes"
		_ = json.NewEncoder(w).Encode(keyInfoV1{
			Name:            "new-key",
			AccessKeyID:     "GKnew",
			SecretAccessKey: &secret,
		})
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	k, err := d.CreateKey(context.Background(), driverpkg.KeySpec{Name: "new-key"})
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	if k.ID != "GKnew" || k.Name != "new-key" {
		t.Errorf("Key = %+v", k)
	}
}

func TestCreateKey_500(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	_, err := d.CreateKey(context.Background(), driverpkg.KeySpec{Name: "x"})
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestUpdateKeyPermissions(t *testing.T) {
	var allowCalls, denyCalls int
	var lastAllow, lastDeny bucketPermChangeV1

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body bucketPermChangeV1
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		switch r.URL.Path {
		case "/v1/bucket/allow":
			allowCalls++
			lastAllow = body
		case "/v1/bucket/deny":
			denyCalls++
			lastDeny = body
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	err := d.UpdateKeyPermissions(context.Background(), "GK1", []driverpkg.BucketPermission{
		{BucketID: "b-1", Read: true, Write: true, Owner: false},
	})
	if err != nil {
		t.Fatalf("UpdateKeyPermissions: %v", err)
	}
	if allowCalls != 1 {
		t.Errorf("allow calls = %d, want 1", allowCalls)
	}
	if denyCalls != 1 {
		t.Errorf("deny calls = %d, want 1", denyCalls)
	}
	// allow body: read=true, write=true, owner=false
	if !lastAllow.Permissions.Read || !lastAllow.Permissions.Write || lastAllow.Permissions.Owner {
		t.Errorf("allow perms = %+v, want read/write true, owner false", lastAllow.Permissions)
	}
	// deny body: complementary -> read=false, write=false, owner=true
	if lastDeny.Permissions.Read || lastDeny.Permissions.Write || !lastDeny.Permissions.Owner {
		t.Errorf("deny perms = %+v, want owner only true", lastDeny.Permissions)
	}
	if lastAllow.BucketID != "b-1" || lastAllow.AccessKeyID != "GK1" {
		t.Errorf("allow target = %+v, want b-1/GK1", lastAllow)
	}
}

func TestUpdateKeyPermissions_MultipleBuckets(t *testing.T) {
	var allowCalls, denyCalls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/bucket/allow":
			allowCalls++
		case "/v1/bucket/deny":
			denyCalls++
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	err := d.UpdateKeyPermissions(context.Background(), "GK", []driverpkg.BucketPermission{
		{BucketID: "b-1", Read: true},
		{BucketID: "b-2", Write: true},
		{BucketID: "b-3", Owner: true},
	})
	if err != nil {
		t.Fatalf("UpdateKeyPermissions: %v", err)
	}
	if allowCalls != 3 || denyCalls != 3 {
		t.Errorf("want 3 allow + 3 deny, got %d allow + %d deny", allowCalls, denyCalls)
	}
}

func TestUpdateKeyPermissions_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	err := d.UpdateKeyPermissions(context.Background(), "GK", []driverpkg.BucketPermission{
		{BucketID: "missing", Read: true},
	})
	if !errors.Is(err, driverpkg.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestDeleteKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/key" || r.Method != "DELETE" {
			t.Errorf("expected DELETE /v1/key, got %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("id") != "GK-rm" {
			t.Errorf("id = %q, want GK-rm", r.URL.Query().Get("id"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	if err := d.DeleteKey(context.Background(), "GK-rm"); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
}

func TestDeleteKey_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	d := newTestDriver(ts.URL)
	err := d.DeleteKey(context.Background(), "missing")
	if !errors.Is(err, driverpkg.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
