package garage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// makeGarageDriver constructs a real *driver pointed at the given test server.
func makeGarageDriver(t *testing.T, ts *httptest.Server, cfg map[string]string) *driver {
	t.Helper()
	dr, err := newDriver(cfg)
	if err != nil {
		t.Fatalf("newDriver: %v", err)
	}
	d, ok := dr.(*driver)
	if !ok {
		t.Fatalf("unexpected driver type %T", dr)
	}
	return d
}

// TestGarage_PresignGet_EmptyEndpoint ensures PresignGet returns ErrUnsupported
// when no S3 endpoint is configured.
func TestGarage_PresignGet_EmptyEndpoint(t *testing.T) {
	d := &driver{
		client:     newClient(map[string]string{"admin_url": "http://example.com"}),
		s3Endpoint: "",
	}

	_, err := d.PresignGet(context.Background(), "bucket", "key", 10*time.Minute)
	if err == nil {
		t.Fatal("expected error when s3_endpoint is empty")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("expected *driver.Error, got %T", err)
	}
	if driverErr.Err != driverpkg.ErrUnsupported {
		t.Errorf("err=%v (Err=%v), want ErrUnsupported", err, driverErr.Err)
	}
	if !strings.Contains(driverErr.Message, "S3 endpoint not configured") {
		t.Errorf("unexpected error message: %q", driverErr.Message)
	}
}

// TestGarage_PresignGet_ValidEndpoint ensures PresignGet returns a valid URL
// when S3 endpoint is configured. The presign happens locally; the test server
// just needs to be reachable for credential validation.
func TestGarage_PresignGet_ValidEndpoint(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := makeGarageDriver(t, ts, map[string]string{
		"admin_url":     "http://example.com",
		"s3_endpoint":   ts.URL,
		"access_key_id": "test-access-key",
		"secret_key":    "test-secret-key",
	})

	u, err := d.PresignGet(context.Background(), "mybucket", "mykey.txt", 10*time.Minute)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}

	if u.URL == "" {
		t.Error("URL is empty")
	}
	if !strings.Contains(u.URL, ts.URL) {
		t.Errorf("URL=%q does not contain test server endpoint %q", u.URL, ts.URL)
	}
	if !strings.Contains(u.URL, "/mybucket/mykey.txt") {
		t.Errorf("URL=%q missing path-style /mybucket/mykey.txt segment", u.URL)
	}
	if !strings.Contains(u.URL, "X-Amz-Signature=") && !strings.Contains(u.URL, "X-Amz-SignedHeaders=") {
		t.Error("URL missing signature parameters")
	}
	if u.Method != "GET" {
		t.Errorf("Method=%q, want GET", u.Method)
	}
	if u.Expires.Before(time.Now()) || u.Expires.After(time.Now().Add(11*time.Minute)) {
		t.Errorf("Expires=%v not within expected range", u.Expires)
	}
}

// TestGarage_PresignPut_EmptyEndpoint ensures PresignPut returns ErrUnsupported
// when no S3 endpoint is configured.
func TestGarage_PresignPut_EmptyEndpoint(t *testing.T) {
	d := &driver{
		client:     newClient(map[string]string{"admin_url": "http://example.com"}),
		s3Endpoint: "",
	}

	_, err := d.PresignPut(context.Background(), "bucket", "key", 10*time.Minute, "text/plain")
	if err == nil {
		t.Fatal("expected error when s3_endpoint is empty")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("expected *driver.Error, got %T", err)
	}
	if driverErr.Err != driverpkg.ErrUnsupported {
		t.Errorf("err=%v (Err=%v), want ErrUnsupported", err, driverErr.Err)
	}
	if !strings.Contains(driverErr.Message, "S3 endpoint not configured") {
		t.Errorf("unexpected error message: %q", driverErr.Message)
	}
}

// TestGarage_PresignPut_ValidEndpoint ensures PresignPut returns a valid URL
// when S3 endpoint is configured.
func TestGarage_PresignPut_ValidEndpoint(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := makeGarageDriver(t, ts, map[string]string{
		"admin_url":     "http://example.com",
		"s3_endpoint":   ts.URL,
		"access_key_id": "test-access-key",
		"secret_key":    "test-secret-key",
	})

	u, err := d.PresignPut(context.Background(), "mybucket", "mykey.txt", 10*time.Minute, "text/plain")
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}

	if u.URL == "" {
		t.Error("URL is empty")
	}
	if !strings.Contains(u.URL, ts.URL) {
		t.Errorf("URL=%q does not contain test server endpoint %q", u.URL, ts.URL)
	}
	if !strings.Contains(u.URL, "/mybucket/mykey.txt") {
		t.Errorf("URL=%q missing path-style /mybucket/mykey.txt segment", u.URL)
	}
	if u.Method != "PUT" {
		t.Errorf("Method=%q, want PUT", u.Method)
	}
	if u.Expires.Before(time.Now()) || u.Expires.After(time.Now().Add(11*time.Minute)) {
		t.Errorf("Expires=%v not within expected range", u.Expires)
	}
}

// TestGarage_PresignPut_ValidEndpoint_NoContentType covers the contentType=="" branch.
func TestGarage_PresignPut_ValidEndpoint_NoContentType(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := makeGarageDriver(t, ts, map[string]string{
		"admin_url":     "http://example.com",
		"s3_endpoint":   ts.URL,
		"access_key_id": "test-access-key",
		"secret_key":    "test-secret-key",
	})

	u, err := d.PresignPut(context.Background(), "mybucket", "mykey.txt", 5*time.Minute, "")
	if err != nil {
		t.Fatalf("PresignPut: %v", err)
	}

	if u.Method != "PUT" {
		t.Errorf("Method=%q, want PUT", u.Method)
	}
	if !strings.Contains(u.URL, ts.URL) {
		t.Errorf("URL does not contain test server endpoint")
	}
}

// TestGarage_CreateMultipart_EmptyEndpoint ensures CreateMultipart returns ErrUnsupported.
func TestGarage_CreateMultipart_EmptyEndpoint(t *testing.T) {
	d := &driver{
		client:     newClient(map[string]string{"admin_url": "http://example.com"}),
		s3Endpoint: "",
	}

	_, err := d.CreateMultipart(context.Background(), "bucket", "key", "text/plain")
	if err == nil {
		t.Fatal("expected error when s3_endpoint is empty")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("expected *driver.Error, got %T", err)
	}
	if driverErr.Err != driverpkg.ErrUnsupported {
		t.Errorf("err=%v (Err=%v), want ErrUnsupported", err, driverErr.Err)
	}
	if !strings.Contains(driverErr.Message, "S3 endpoint not configured") {
		t.Errorf("unexpected error message: %q", driverErr.Message)
	}
}

// TestGarage_PresignUploadPart_EmptyEndpoint ensures PresignUploadPart returns ErrUnsupported.
func TestGarage_PresignUploadPart_EmptyEndpoint(t *testing.T) {
	d := &driver{
		client:     newClient(map[string]string{"admin_url": "http://example.com"}),
		s3Endpoint: "",
	}

	_, err := d.PresignUploadPart(context.Background(), driverpkg.MultipartUpload{UploadID: "test-id"}, 1)
	if err == nil {
		t.Fatal("expected error when s3_endpoint is empty")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("expected *driver.Error, got %T", err)
	}
	if driverErr.Err != driverpkg.ErrUnsupported {
		t.Errorf("err=%v (Err=%v), want ErrUnsupported", err, driverErr.Err)
	}
}

// TestGarage_CompleteMultipart_EmptyEndpoint ensures CompleteMultipart returns ErrUnsupported.
func TestGarage_CompleteMultipart_EmptyEndpoint(t *testing.T) {
	d := &driver{
		client:     newClient(map[string]string{"admin_url": "http://example.com"}),
		s3Endpoint: "",
	}

	err := d.CompleteMultipart(context.Background(), driverpkg.MultipartUpload{UploadID: "test-id"}, nil)
	if err == nil {
		t.Fatal("expected error when s3_endpoint is empty")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("expected *driver.Error, got %T", err)
	}
	if driverErr.Err != driverpkg.ErrUnsupported {
		t.Errorf("err=%v (Err=%v), want ErrUnsupported", err, driverErr.Err)
	}
}

// TestGarage_AbortMultipart_EmptyEndpoint ensures AbortMultipart returns ErrUnsupported.
func TestGarage_AbortMultipart_EmptyEndpoint(t *testing.T) {
	d := &driver{
		client:     newClient(map[string]string{"admin_url": "http://example.com"}),
		s3Endpoint: "",
	}

	err := d.AbortMultipart(context.Background(), driverpkg.MultipartUpload{UploadID: "test-id"})
	if err == nil {
		t.Fatal("expected error when s3_endpoint is empty")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("expected *driver.Error, got %T", err)
	}
	if driverErr.Err != driverpkg.ErrUnsupported {
		t.Errorf("err=%v (Err=%v), want ErrUnsupported", err, driverErr.Err)
	}
}

// TestGarage_ListObjects_EmptyEndpoint ensures ListObjects returns ErrUnsupported.
func TestGarage_ListObjects_EmptyEndpoint(t *testing.T) {
	d := &driver{
		client:     newClient(map[string]string{"admin_url": "http://example.com"}),
		s3Endpoint: "",
	}

	_, err := d.ListObjects(context.Background(), "bucket", "", "", "/", 100)
	if err == nil {
		t.Fatal("expected error when s3_endpoint is empty")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("expected *driver.Error, got %T", err)
	}
	if driverErr.Err != driverpkg.ErrUnsupported {
		t.Errorf("err=%v (Err=%v), want ErrUnsupported", err, driverErr.Err)
	}
	if !strings.Contains(driverErr.Message, "S3 endpoint not configured") {
		t.Errorf("unexpected error message: %q", driverErr.Message)
	}
}

// TestGarage_StatObject_EmptyEndpoint ensures StatObject returns ErrUnsupported.
func TestGarage_StatObject_EmptyEndpoint(t *testing.T) {
	d := &driver{
		client:     newClient(map[string]string{"admin_url": "http://example.com"}),
		s3Endpoint: "",
	}

	_, err := d.StatObject(context.Background(), "bucket", "key")
	if err == nil {
		t.Fatal("expected error when s3_endpoint is empty")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("expected *driver.Error, got %T", err)
	}
	if driverErr.Err != driverpkg.ErrUnsupported {
		t.Errorf("err=%v (Err=%v), want ErrUnsupported", err, driverErr.Err)
	}
	if !strings.Contains(driverErr.Message, "S3 endpoint not configured") {
		t.Errorf("unexpected error message: %q", driverErr.Message)
	}
}

// TestGarage_DeleteObject_EmptyEndpoint ensures DeleteObject returns ErrUnsupported.
func TestGarage_DeleteObject_EmptyEndpoint(t *testing.T) {
	d := &driver{
		client:     newClient(map[string]string{"admin_url": "http://example.com"}),
		s3Endpoint: "",
	}

	err := d.DeleteObject(context.Background(), "bucket", "key")
	if err == nil {
		t.Fatal("expected error when s3_endpoint is empty")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("expected *driver.Error, got %T", err)
	}
	if driverErr.Err != driverpkg.ErrUnsupported {
		t.Errorf("err=%v (Err=%v), want ErrUnsupported", err, driverErr.Err)
	}
	if !strings.Contains(driverErr.Message, "S3 endpoint not configured") {
		t.Errorf("unexpected error message: %q", driverErr.Message)
	}
}

// TestGarage_ServerSideCopy_EmptyEndpoint ensures ServerSideCopy returns ErrUnsupported.
func TestGarage_ServerSideCopy_EmptyEndpoint(t *testing.T) {
	d := &driver{
		client:     newClient(map[string]string{"admin_url": "http://example.com"}),
		s3Endpoint: "",
	}

	err := d.ServerSideCopy(context.Background(), "src-bucket", "src-key", "dst-bucket", "dst-key")
	if err == nil {
		t.Fatal("expected error when s3_endpoint is empty")
	}

	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("expected *driver.Error, got %T", err)
	}
	if driverErr.Err != driverpkg.ErrUnsupported {
		t.Errorf("err=%v (Err=%v), want ErrUnsupported", err, driverErr.Err)
	}
	if !strings.Contains(driverErr.Message, "S3 endpoint not configured") {
		t.Errorf("unexpected error message: %q", driverErr.Message)
	}
}

// TestGarage_ServerSideCopy_SimpleHappyPath ensures ServerSideCopy succeeds with valid config.
func TestGarage_ServerSideCopy_SimpleHappyPath(t *testing.T) {
	var copyCalled bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == "PUT" && strings.Contains(r.URL.RawQuery, "x-id=CopyObject") {
			copyCalled = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><CopyObjectResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ETag>"abc123"</ETag></CopyObjectResult>`))
			return
		}

		http.NotFound(w, r)
	}))
	defer ts.Close()

	d := makeGarageDriver(t, ts, map[string]string{
		"admin_url":     "http://example.com",
		"s3_endpoint":   ts.URL,
		"access_key_id": "test-access-key",
		"secret_key":    "test-secret-key",
	})

	err := d.ServerSideCopy(context.Background(), "src-bucket", "src-key", "dst-bucket", "dst-key")
	if err != nil {
		t.Fatalf("ServerSideCopy: %v", err)
	}
	if !copyCalled {
		t.Fatal("COPY request not sent to test server")
	}
}

// TestGarage_ServerSideCopy_UsesCaps checks that Capabilities.ServerSideCopy is true when s3Client configured.
func TestGarage_ServerSideCopy_UsesCaps(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer ts.Close()

	d := makeGarageDriver(t, ts, map[string]string{
		"admin_url":     "http://example.com",
		"s3_endpoint":   ts.URL,
		"access_key_id": "test-access-key",
		"secret_key":    "test-secret-key",
	})

	caps, err := d.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if !caps.ServerSideCopy {
		t.Errorf("ServerSideCopy capability not set; got %+v", caps)
	}
}

// TestGarage_NewDriver_AdminOnly_NoS3Creds verifies that the v2 driver
// constructs successfully when only admin_token is configured (operator-tier
// admin connection per ADR-0001). Without this, adding a Garage v2 cluster
// with admin_token only would fail with "missing required config key:
// access_key_id". This mirrors the gate in the v1 driver
// (internal/drivers/garage_v1/garage.go).
func TestGarage_NewDriver_AdminOnly_NoS3Creds(t *testing.T) {
	// admin_token only, no s3_endpoint / access_key_id / secret_key.
	dr, err := newDriver(map[string]string{
		"admin_url":   "http://example.com",
		"admin_token": "test-token",
	})
	if err != nil {
		t.Fatalf("newDriver should succeed with admin-only config, got: %v", err)
	}
	d, ok := dr.(*driver)
	if !ok {
		t.Fatalf("unexpected driver type %T", dr)
	}
	if d.s3Client != nil {
		t.Errorf("s3Client should be nil for admin-only config, got non-nil")
	}
}

// TestGarage_NewDriver_AdminOnly_S3EndpointButNoCreds covers the case where
// the operator configures s3_endpoint (so the v1.1.0d region bridge can find
// the driver by endpoint) but omits access_key_id and secret_key. The S3
// client must still be skipped — user-region keys take over via the bridge.
func TestGarage_NewDriver_AdminOnly_S3EndpointButNoCreds(t *testing.T) {
	dr, err := newDriver(map[string]string{
		"admin_url":   "http://example.com",
		"admin_token": "test-token",
		"s3_endpoint": "http://example.com:3902",
	})
	if err != nil {
		t.Fatalf("newDriver should succeed with s3_endpoint but no creds, got: %v", err)
	}
	d, ok := dr.(*driver)
	if !ok {
		t.Fatalf("unexpected driver type %T", dr)
	}
	if d.s3Client != nil {
		t.Errorf("s3Client should be nil when access_key_id and secret_key absent, got non-nil")
	}
	if d.s3Endpoint != "http://example.com:3902" {
		t.Errorf("s3Endpoint = %q, want %q (must still be carried for region-bridge lookup)", d.s3Endpoint, "http://example.com:3902")
	}
}

// TestGarage_NewDriver_AdminOnly_ListBucketsWorks verifies that the admin-API
// path (ListBuckets) works on an admin-only driver constructed without S3
// creds. This is the regression test for v1.11.0.1: previously, newDriver
// failed with "missing required config key: access_key_id" before any admin
// call could be issued.
func TestGarage_NewDriver_AdminOnly_ListBucketsWorks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/ListBuckets" || r.Method != "GET" {
			t.Errorf("expected GET /v2/ListBuckets, got %s %s", r.Method, r.URL.Path)
		}
		response := []listBucketsResponseItem{
			{ID: "bucket-a", Created: time.Now(), GlobalAliases: []string{"docs"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	dr, err := newDriver(map[string]string{
		"admin_url":   server.URL,
		"admin_token": "test-token",
	})
	if err != nil {
		t.Fatalf("newDriver: %v", err)
	}

	buckets, err := dr.ListBuckets(context.Background())
	if err != nil {
		t.Fatalf("ListBuckets failed on admin-only driver: %v", err)
	}
	if len(buckets) != 1 {
		t.Fatalf("got %d buckets, want 1", len(buckets))
	}
}

// TestGarage_NewDriver_AdminOnly_S3OpsReturnUnsupported verifies that S3-data-plane
// operations on an admin-only driver return ErrUnsupported (not a panic, not a
// nil-pointer dereference). The region bridge should pick up S3 work via
// user-region keys; admin-only callers get a graceful error.
func TestGarage_NewDriver_AdminOnly_S3OpsReturnUnsupported(t *testing.T) {
	dr, err := newDriver(map[string]string{
		"admin_url":   "http://example.com",
		"admin_token": "test-token",
	})
	if err != nil {
		t.Fatalf("newDriver: %v", err)
	}

	_, err = dr.ListObjects(context.Background(), "bucket", "", "", "/", 100)
	if err == nil {
		t.Fatal("expected error on admin-only ListObjects")
	}
	var driverErr *driverpkg.Error
	if !errors.As(err, &driverErr) {
		t.Fatalf("expected *driver.Error, got %T", err)
	}
	if driverErr.Err != driverpkg.ErrUnsupported {
		t.Errorf("err=%v (Err=%v), want ErrUnsupported", err, driverErr.Err)
	}
}

// TestGarage_NewDriver_FullCreds verifies the full-creds path remains
// unchanged: when all three S3 config keys are present, the S3 client is
// built and S3 ops are reachable.
func TestGarage_NewDriver_FullCreds(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	dr, err := newDriver(map[string]string{
		"admin_url":     "http://example.com",
		"admin_token":   "test-token",
		"s3_endpoint":   ts.URL,
		"access_key_id": "test-access-key",
		"secret_key":    "test-secret-key",
	})
	if err != nil {
		t.Fatalf("newDriver with full creds: %v", err)
	}
	d, ok := dr.(*driver)
	if !ok {
		t.Fatalf("unexpected driver type %T", dr)
	}
	if d.s3Client == nil {
		t.Error("s3Client should be non-nil when full creds present")
	}
}
