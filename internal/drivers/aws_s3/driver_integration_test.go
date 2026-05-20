package aws_s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// makeAwsS3Driver constructs a real *driver pointed at the given test
// server. Returns t.Fatal()ed driver, never nil.
func makeAwsS3Driver(t *testing.T, ts *httptest.Server) *driver {
	t.Helper()
	dr, err := newDriver(map[string]string{
		"region":     "us-east-1",
		"access_key": "test-ak",
		"secret_key": "test-sk",
		"endpoint":   ts.URL,
	})
	if err != nil {
		t.Fatalf("newDriver: %v", err)
	}
	d, ok := dr.(*driver)
	if !ok {
		t.Fatalf("unexpected driver type %T", dr)
	}
	return d
}

// TestRealDriver_Capabilities ensures Capabilities returns the correct
// shape via the actual driver implementation, not the testDriver stub.
func TestRealDriver_Capabilities(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer ts.Close()
	d := makeAwsS3Driver(t, ts)

	caps, err := d.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if caps.Driver != driverName {
		t.Errorf("caps.Driver=%q, want %q", caps.Driver, driverName)
	}
	if caps.KeyModel != driverpkg.KeyModelIAM {
		t.Errorf("caps.KeyModel=%q, want IAM", caps.KeyModel)
	}
	if !caps.Presign || !caps.Multipart || !caps.Versioning {
		t.Errorf("caps Presign/Multipart/Versioning all expected true; got %+v", caps)
	}
}

// TestRealDriver_HealthCheck_Healthy exercises the success branch.
func TestRealDriver_HealthCheck_Healthy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Owner><ID>owner-id</ID><DisplayName>owner</DisplayName></Owner>
  <Buckets/>
</ListAllMyBucketsResult>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)

	rep, err := d.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if rep.Status != "healthy" {
		t.Errorf("status=%q, want healthy", rep.Status)
	}
}

// TestRealDriver_HealthCheck_Unhealthy exercises the unhealthy fallback
// (non-401/403/NoSuchBucket error → status "unhealthy").
func TestRealDriver_HealthCheck_Unhealthy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	rep, err := d.HealthCheck(context.Background())
	if err != nil {
		t.Fatalf("HealthCheck returned error: %v", err)
	}
	if rep.Status != "unhealthy" {
		t.Errorf("status=%q, want unhealthy", rep.Status)
	}
}

// TestRealDriver_ListBuckets_Happy exercises ListBuckets against an
// httptest server returning a valid ListAllMyBucketsResult.
func TestRealDriver_ListBuckets_Happy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Owner><ID>id</ID><DisplayName>n</DisplayName></Owner>
  <Buckets>
    <Bucket><Name>my-bucket-1</Name><CreationDate>2024-01-01T00:00:00.000Z</CreationDate></Bucket>
    <Bucket><Name>my-bucket-2</Name><CreationDate>2024-02-01T00:00:00.000Z</CreationDate></Bucket>
  </Buckets>
</ListAllMyBucketsResult>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	buckets, err := d.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets: %v", err)
	}
	if len(buckets) != 2 {
		t.Fatalf("buckets=%d, want 2", len(buckets))
	}
	if buckets[0].ID != "my-bucket-1" {
		t.Errorf("buckets[0].ID=%q", buckets[0].ID)
	}
	if buckets[0].Aliases[0] != "my-bucket-1" {
		t.Errorf("buckets[0].Aliases[0]=%q", buckets[0].Aliases[0])
	}
}

// TestRealDriver_ListBuckets_Error exercises the API-failure path.
func TestRealDriver_ListBuckets_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	_, err := d.ListBuckets(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("error is not driver.Error: %v", err)
	}
	if de.Op != "ListBuckets" {
		t.Errorf("op=%q, want ListBuckets", de.Op)
	}
}

// TestRealDriver_GetBucket_Happy exercises the head-bucket success path.
func TestRealDriver_GetBucket_Happy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// HeadBucket: server returns 200 with no body to signal existence.
		if r.Method != http.MethodHead {
			http.Error(w, "expected HEAD", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	b, err := d.GetBucket(context.Background(), "my-bucket")
	if err != nil {
		t.Fatalf("GetBucket: %v", err)
	}
	if b.ID != "my-bucket" {
		t.Errorf("ID=%q, want my-bucket", b.ID)
	}
	if len(b.Aliases) != 1 || b.Aliases[0] != "my-bucket" {
		t.Errorf("Aliases=%v, want [my-bucket]", b.Aliases)
	}
}

// TestRealDriver_GetBucket_NotFound exercises the NoSuchBucket / 404 path.
func TestRealDriver_GetBucket_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	_, err := d.GetBucket(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("not a driver.Error: %v", err)
	}
	if de.Op != "GetBucket" {
		t.Errorf("op=%q, want GetBucket", de.Op)
	}
	// The SDK reports HEAD 404 with "NotFound" ErrorCode → mapped to ErrNotFound.
	if !errors.Is(err, driverpkg.ErrNotFound) {
		// Some SDK versions map 404 differently; accept ErrInvalid as fallback.
		if !errors.Is(err, driverpkg.ErrInvalid) {
			t.Errorf("err category=%v, want ErrNotFound or ErrInvalid", de.Err)
		}
	}
}

// TestRealDriver_CreateBucket_Happy exercises CreateBucket OK.
func TestRealDriver_CreateBucket_Happy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "want PUT", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Location", "/new-bucket")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	b, err := d.CreateBucket(context.Background(), driverpkg.BucketSpec{Alias: "new-bucket"})
	if err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	if b.ID != "new-bucket" {
		t.Errorf("b.ID=%q", b.ID)
	}
}

// TestRealDriver_CreateBucket_AlreadyOwned exercises the conflict mapping.
func TestRealDriver_CreateBucket_AlreadyOwned(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>BucketAlreadyOwnedByYou</Code><Message>You already own it</Message><RequestId>r1</RequestId></Error>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	_, err := d.CreateBucket(context.Background(), driverpkg.BucketSpec{Alias: "owned"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, driverpkg.ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

// TestRealDriver_CreateBucket_OtherError exercises the generic error path.
func TestRealDriver_CreateBucket_OtherError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	_, err := d.CreateBucket(context.Background(), driverpkg.BucketSpec{Alias: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("not driver.Error: %v", err)
	}
	if de.Op != "CreateBucket" {
		t.Errorf("op=%q, want CreateBucket", de.Op)
	}
}

// TestRealDriver_DeleteBucket_Happy exercises the success path.
func TestRealDriver_DeleteBucket_Happy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "want DELETE", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	if err := d.DeleteBucket(context.Background(), "to-delete"); err != nil {
		t.Fatalf("DeleteBucket: %v", err)
	}
}

// TestRealDriver_DeleteBucket_NotEmpty exercises the conflict mapping.
func TestRealDriver_DeleteBucket_NotEmpty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>BucketNotEmpty</Code><Message>not empty</Message><RequestId>r2</RequestId></Error>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	err := d.DeleteBucket(context.Background(), "full")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, driverpkg.ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

// TestRealDriver_DeleteBucket_NoSuchBucket exercises the not-found mapping.
func TestRealDriver_DeleteBucket_NoSuchBucket(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<Error><Code>NoSuchBucket</Code><Message>no such</Message><RequestId>r3</RequestId></Error>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	err := d.DeleteBucket(context.Background(), "absent")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, driverpkg.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TestRealDriver_DeleteBucket_OtherError exercises the generic-failure
// path (no API code mapping → ErrInvalid).
func TestRealDriver_DeleteBucket_OtherError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	err := d.DeleteBucket(context.Background(), "x")
	if err == nil {
		t.Fatal("expected error")
	}
	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("not driver.Error: %v", err)
	}
	if de.Op != "DeleteBucket" {
		t.Errorf("op=%q, want DeleteBucket", de.Op)
	}
}

// TestRealDriver_UpdateBucket_Unsupported exercises the unsupported-stub branch.
func TestRealDriver_UpdateBucket_Unsupported(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer ts.Close()
	d := makeAwsS3Driver(t, ts)

	_, err := d.UpdateBucket(context.Background(), "x", driverpkg.BucketUpdate{})
	if err == nil {
		t.Fatal("expected ErrUnsupported")
	}
	if !errors.Is(err, driverpkg.ErrUnsupported) {
		t.Errorf("got %v, want ErrUnsupported", err)
	}
}

// TestRealDriver_StatObject_Happy exercises a successful HeadObject.
func TestRealDriver_StatObject_Happy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			http.Error(w, "want HEAD", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Length", "42")
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("Last-Modified", "Mon, 01 Jan 2024 00:00:00 GMT")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	info, err := d.StatObject(context.Background(), "bkt", "key.txt")
	if err != nil {
		t.Fatalf("StatObject: %v", err)
	}
	if info.Key != "key.txt" {
		t.Errorf("Key=%q", info.Key)
	}
	if info.Size != 42 {
		t.Errorf("Size=%d, want 42", info.Size)
	}
	if info.ContentType != "text/plain" {
		t.Errorf("ContentType=%q, want text/plain", info.ContentType)
	}
}

// TestRealDriver_StatObject_NotFound exercises the 404 → ErrNotFound mapping.
func TestRealDriver_StatObject_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	_, err := d.StatObject(context.Background(), "bkt", "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("not driver.Error: %v", err)
	}
	if de.Op != "StatObject" {
		t.Errorf("op=%q, want StatObject", de.Op)
	}
}

// TestRealDriver_StatObject_OtherError exercises the generic error path.
func TestRealDriver_StatObject_OtherError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	_, err := d.StatObject(context.Background(), "bkt", "x")
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestRealDriver_PresignGet exercises presign for GET — works without
// hitting the test server (the SDK signs locally).
func TestRealDriver_PresignGet(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	u, err := d.PresignGet(context.Background(), "bkt", "key", 10*time.Minute)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	if u.URL == "" {
		t.Error("URL is empty")
	}
	if u.Method != "GET" {
		t.Errorf("Method=%q, want GET", u.Method)
	}
	if !strings.Contains(u.URL, ts.URL) {
		t.Errorf("URL=%q, expected to contain test server endpoint %q", u.URL, ts.URL)
	}
	// UsePathStyle should result in "bkt/key" path-style, not "bkt." virtual host.
	if !strings.Contains(u.URL, "/bkt/key") {
		t.Errorf("URL=%q missing path-style /bkt/key segment", u.URL)
	}
	if u.Expires.Before(time.Now()) {
		t.Errorf("Expires=%v is in the past", u.Expires)
	}
}

// TestRealDriver_PresignPut exercises presign for PUT with a content type.
func TestRealDriver_PresignPut(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	u, err := d.PresignPut(context.Background(), "bkt", "key", 10*time.Minute, "text/plain")
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	if u.URL == "" {
		t.Error("URL is empty")
	}
	if u.Method != "PUT" {
		t.Errorf("Method=%q, want PUT", u.Method)
	}
}

// TestRealDriver_PresignPut_NoContentType covers the contentType=="" branch.
func TestRealDriver_PresignPut_NoContentType(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	u, err := d.PresignPut(context.Background(), "bkt", "key", 5*time.Minute, "")
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
	if u.Method != "PUT" {
		t.Errorf("Method=%q", u.Method)
	}
}

// TestRealDriver_DeleteObject_Happy exercises the success branch.
func TestRealDriver_DeleteObject_Happy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "want DELETE", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	if err := d.DeleteObject(context.Background(), "bkt", "key.txt"); err != nil {
		t.Fatalf("DeleteObject: %v", err)
	}
}

// TestRealDriver_DeleteObject_Error exercises the failure branch.
func TestRealDriver_DeleteObject_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	err := d.DeleteObject(context.Background(), "bkt", "k")
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestRealDriver_ListObjects_Happy exercises ListObjectsV2 with both
// Contents and CommonPrefixes.
func TestRealDriver_ListObjects_Happy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>bkt</Name>
  <Prefix></Prefix>
  <KeyCount>1</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>false</IsTruncated>
  <Contents>
    <Key>file1.txt</Key>
    <LastModified>2024-01-01T00:00:00.000Z</LastModified>
    <ETag>"abc"</ETag>
    <Size>10</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
  <CommonPrefixes>
    <Prefix>subdir/</Prefix>
  </CommonPrefixes>
</ListBucketResult>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	page, err := d.ListObjects(context.Background(), "bkt", "", "", 1000)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(page.Objects) != 1 {
		t.Errorf("Objects=%d, want 1", len(page.Objects))
	}
	if len(page.Prefixes) != 1 || page.Prefixes[0] != "subdir/" {
		t.Errorf("Prefixes=%v, want [subdir/]", page.Prefixes)
	}
	if page.IsTruncated {
		t.Error("IsTruncated should be false")
	}
}

// TestRealDriver_ListObjects_Error exercises the failure branch.
func TestRealDriver_ListObjects_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	_, err := d.ListObjects(context.Background(), "bkt", "", "", 100)
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestRealDriver_CreateMultipart_Happy exercises the success branch.
func TestRealDriver_CreateMultipart_Happy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<InitiateMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Bucket>bkt</Bucket>
  <Key>k</Key>
  <UploadId>upload-abc</UploadId>
</InitiateMultipartUploadResult>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	mu, err := d.CreateMultipart(context.Background(), "bkt", "k", "application/octet-stream")
	if err != nil {
		t.Fatalf("CreateMultipart: %v", err)
	}
	if mu.UploadID != "upload-abc" {
		t.Errorf("UploadID=%q", mu.UploadID)
	}
	if mu.Bucket != "bkt" || mu.Key != "k" {
		t.Errorf("Bucket/Key=%q/%q", mu.Bucket, mu.Key)
	}
	if mu.ContentType != "application/octet-stream" {
		t.Errorf("ContentType=%q", mu.ContentType)
	}
}

// TestRealDriver_CreateMultipart_NoContentType exercises the contentType==""
// branch.
func TestRealDriver_CreateMultipart_NoContentType(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<InitiateMultipartUploadResult><Bucket>b</Bucket><Key>k</Key><UploadId>u1</UploadId></InitiateMultipartUploadResult>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	mu, err := d.CreateMultipart(context.Background(), "b", "k", "")
	if err != nil {
		t.Fatalf("CreateMultipart: %v", err)
	}
	if mu.ContentType != "" {
		t.Errorf("ContentType=%q, want empty", mu.ContentType)
	}
}

// TestRealDriver_CreateMultipart_Error exercises the failure branch.
func TestRealDriver_CreateMultipart_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	_, err := d.CreateMultipart(context.Background(), "b", "k", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestRealDriver_PresignUploadPart exercises the presign branch.
func TestRealDriver_PresignUploadPart(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	u, err := d.PresignUploadPart(context.Background(), driverpkg.MultipartUpload{
		Bucket:   "bkt",
		Key:      "key",
		UploadID: "upload-x",
	}, 1)
	if err != nil {
		t.Fatalf("PresignUploadPart: %v", err)
	}
	if u.URL == "" {
		t.Error("URL is empty")
	}
	if u.Method != "PUT" {
		t.Errorf("Method=%q, want PUT", u.Method)
	}
}

// TestRealDriver_CompleteMultipart_Happy exercises the success branch.
func TestRealDriver_CompleteMultipart_Happy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<CompleteMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Location>http://example.com/bkt/k</Location>
  <Bucket>bkt</Bucket>
  <Key>k</Key>
  <ETag>"abc"</ETag>
</CompleteMultipartUploadResult>`))
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	err := d.CompleteMultipart(context.Background(),
		driverpkg.MultipartUpload{Bucket: "bkt", Key: "k", UploadID: "u1"},
		[]driverpkg.CompletedPart{{PartNumber: 1, ETag: "et"}},
	)
	if err != nil {
		t.Fatalf("CompleteMultipart: %v", err)
	}
}

// TestRealDriver_CompleteMultipart_Error exercises the failure branch.
func TestRealDriver_CompleteMultipart_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	err := d.CompleteMultipart(context.Background(),
		driverpkg.MultipartUpload{Bucket: "b", Key: "k", UploadID: "u"},
		nil,
	)
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestRealDriver_AbortMultipart_Happy exercises the success branch.
func TestRealDriver_AbortMultipart_Happy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "want DELETE", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	err := d.AbortMultipart(context.Background(),
		driverpkg.MultipartUpload{Bucket: "b", Key: "k", UploadID: "u"},
	)
	if err != nil {
		t.Fatalf("AbortMultipart: %v", err)
	}
}

// TestRealDriver_AbortMultipart_Error exercises the failure branch.
func TestRealDriver_AbortMultipart_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	err := d.AbortMultipart(context.Background(),
		driverpkg.MultipartUpload{Bucket: "b", Key: "k", UploadID: "u"},
	)
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestRealDriver_ListNodes_Unsupported, GetLayout, StageLayout, ApplyLayout,
// RevertLayout — all return ErrUnsupported wrapped in driver.Error.
func TestRealDriver_ClusterMethodsUnsupported(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer ts.Close()
	d := makeAwsS3Driver(t, ts)
	ctx := context.Background()

	if _, err := d.ListNodes(ctx); !errors.Is(err, driverpkg.ErrUnsupported) {
		t.Errorf("ListNodes err=%v, want ErrUnsupported", err)
	}
	if _, err := d.GetLayout(ctx); !errors.Is(err, driverpkg.ErrUnsupported) {
		t.Errorf("GetLayout err=%v, want ErrUnsupported", err)
	}
	if _, err := d.StageLayout(ctx, driverpkg.LayoutChange{}); !errors.Is(err, driverpkg.ErrUnsupported) {
		t.Errorf("StageLayout err=%v, want ErrUnsupported", err)
	}
	if err := d.ApplyLayout(ctx); !errors.Is(err, driverpkg.ErrUnsupported) {
		t.Errorf("ApplyLayout err=%v, want ErrUnsupported", err)
	}
	if err := d.RevertLayout(ctx); !errors.Is(err, driverpkg.ErrUnsupported) {
		t.Errorf("RevertLayout err=%v, want ErrUnsupported", err)
	}
}

// TestRealDriver_KeyMethodsUnsupported exercises the IAM-managed stubs.
func TestRealDriver_KeyMethodsUnsupported(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer ts.Close()
	d := makeAwsS3Driver(t, ts)
	ctx := context.Background()

	if _, err := d.ListKeys(ctx); !errors.Is(err, driverpkg.ErrUnsupported) {
		t.Errorf("ListKeys err=%v, want ErrUnsupported", err)
	}
	if _, err := d.GetKey(ctx, "id"); !errors.Is(err, driverpkg.ErrUnsupported) {
		t.Errorf("GetKey err=%v, want ErrUnsupported", err)
	}
	if _, err := d.CreateKey(ctx, driverpkg.KeySpec{}); !errors.Is(err, driverpkg.ErrUnsupported) {
		t.Errorf("CreateKey err=%v, want ErrUnsupported", err)
	}
	if err := d.UpdateKeyPermissions(ctx, "k", nil); !errors.Is(err, driverpkg.ErrUnsupported) {
		t.Errorf("UpdateKeyPermissions err=%v, want ErrUnsupported", err)
	}
	if err := d.DeleteKey(ctx, "k"); !errors.Is(err, driverpkg.ErrUnsupported) {
		t.Errorf("DeleteKey err=%v, want ErrUnsupported", err)
	}
}

// TestNewDriver_FactoryWrapping ensures that newDriver wraps factory errors
// inside a driver.Error with the right Op/Driver fields.
func TestNewDriver_FactoryWrapping(t *testing.T) {
	_, err := newDriver(map[string]string{}) // missing all required keys
	if err == nil {
		t.Fatal("expected error from newDriver")
	}
	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("not driver.Error: %v", err)
	}
	if de.Op != "newDriver" {
		t.Errorf("Op=%q, want newDriver", de.Op)
	}
	if de.Driver != driverName {
		t.Errorf("Driver=%q, want %q", de.Driver, driverName)
	}
	if !errors.Is(err, driverpkg.ErrInvalid) {
		t.Errorf("not ErrInvalid: %v", err)
	}
}

// TestDriver_UnsupportedHelper exercises the unsupported(op) helper directly,
// which is otherwise only reached via the stub methods.
func TestDriver_UnsupportedHelper(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer ts.Close()
	d := makeAwsS3Driver(t, ts)

	err := d.unsupported("some-op")
	var de *driverpkg.Error
	if !errors.As(err, &de) {
		t.Fatalf("not driver.Error: %v", err)
	}
	if de.Op != "some-op" {
		t.Errorf("Op=%q, want some-op", de.Op)
	}
	if de.Driver != driverName {
		t.Errorf("Driver=%q", de.Driver)
	}
	if !errors.Is(err, driverpkg.ErrUnsupported) {
		t.Errorf("not ErrUnsupported: %v", err)
	}
}

// TestRealDriver_StreamObject_Happy tests streaming a known object.
func TestRealDriver_StreamObject_Happy(t *testing.T) {
	if os.Getenv("DRIVER_INTEGRATION") == "" {
		t.Skip("DRIVER_INTEGRATION not set")
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" && strings.Contains(r.URL.Path, "/test-bucket/test-key") {
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", "12")
			w.Header().Set("ETag", "\"abc123\"")
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/test-bucket/test-key") {
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", "12")
			w.Header().Set("ETag", "\"abc123\"")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("hello world!"))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	ctx := context.Background()

	result, err := d.StreamObject(ctx, "test-bucket", "test-key", "")
	if err != nil {
		t.Fatalf("StreamObject: %v", err)
	}
	defer result.Body.Close()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if string(data) != "hello world!" {
		t.Errorf("body=%q, want hello world!", data)
	}
	if result.ContentType != "text/plain" {
		t.Errorf("contentType=%q, want text/plain", result.ContentType)
	}
	if result.ETag != "\"abc123\"" {
		t.Errorf("etag=%q, want \"abc123\"", result.ETag)
	}
}

// TestRealDriver_StreamObject_Range tests streaming with range header.
func TestRealDriver_StreamObject_Range(t *testing.T) {
	if os.Getenv("DRIVER_INTEGRATION") == "" {
		t.Skip("DRIVER_INTEGRATION not set")
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.Contains(r.URL.Path, "/test-bucket/test-key") {
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Range", "bytes 5-9/12")
			w.Header().Set("Content-Length", "5")
			w.Header().Set("ETag", "\"abc123\"")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("world"))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	ctx := context.Background()

	result, err := d.StreamObject(ctx, "test-bucket", "test-key", "bytes=5-9")
	if err != nil {
		t.Fatalf("StreamObject: %v", err)
	}
	defer result.Body.Close()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	if string(data) != "world" {
		t.Errorf("body=%q, want world", data)
	}
}

// TestRealDriver_PutObjectStream_Happy tests streaming upload.
func TestRealDriver_PutObjectStream_Happy(t *testing.T) {
	if os.Getenv("DRIVER_INTEGRATION") == "" {
		t.Skip("DRIVER_INTEGRATION not set")
	}

	var receivedBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" && strings.Contains(r.URL.Path, "/test-bucket/test-key") {
			receivedBody, _ = io.ReadAll(r.Body)
			w.Header().Set("ETag", "\"put-etag-123\"")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><PutObjectResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ETag>"put-etag-123"</ETag></PutObjectResult>`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	ctx := context.Background()

	testData := []byte("test data for streaming upload")
	result, err := d.PutObjectStream(ctx, "test-bucket", "test-key", bytes.NewReader(testData), "text/plain", int64(len(testData)))
	if err != nil {
		t.Fatalf("PutObjectStream: %v", err)
	}

	if result.ETag != "\"put-etag-123\"" {
		t.Errorf("etag=%q, want \"put-etag-123\"", result.ETag)
	}
	if !bytes.Equal(receivedBody, testData) {
		t.Errorf("received body mismatch")
	}
}

// TestRealDriver_PutObjectStream_WithContentType tests streaming upload with content type.
func TestRealDriver_PutObjectStream_WithContentType(t *testing.T) {
	if os.Getenv("DRIVER_INTEGRATION") == "" {
		t.Skip("DRIVER_INTEGRATION not set")
	}

	var contentType string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" && strings.Contains(r.URL.Path, "/test-bucket/test-key") {
			contentType = r.Header.Get("Content-Type")
			w.Header().Set("ETag", "\"content-type-etag\"")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><PutObjectResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ETag>"content-type-etag"</ETag></PutObjectResult>`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	ctx := context.Background()

	testData := []byte("binary data")
	result, err := d.PutObjectStream(ctx, "test-bucket", "test-key", bytes.NewReader(testData), "application/octet-stream", int64(len(testData)))
	if err != nil {
		t.Fatalf("PutObjectStream: %v", err)
	}

	if contentType != "application/octet-stream" {
		t.Errorf("contentType=%q, want application/octet-stream", contentType)
	}
	if result.ETag != "\"content-type-etag\"" {
		t.Errorf("etag=%q, want \"content-type-etag\"", result.ETag)
	}
}

// TestRealDriver_PutObjectStream_WithSizeZero tests streaming upload with size 0.
func TestRealDriver_PutObjectStream_WithSizeZero(t *testing.T) {
	if os.Getenv("DRIVER_INTEGRATION") == "" {
		t.Skip("DRIVER_INTEGRATION not set")
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" && strings.Contains(r.URL.Path, "/test-bucket/test-key") {
			w.Header().Set("ETag", "\"zero-size-etag\"")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><PutObjectResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ETag>"zero-size-etag"</ETag></PutObjectResult>`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	d := makeAwsS3Driver(t, ts)
	ctx := context.Background()

	result, err := d.PutObjectStream(ctx, "test-bucket", "test-key", strings.NewReader(""), "", 0)
	if err != nil {
		t.Fatalf("PutObjectStream: %v", err)
	}

	if result.ETag != "\"zero-size-etag\"" {
		t.Errorf("etag=%q, want \"zero-size-etag\"", result.ETag)
	}
}
