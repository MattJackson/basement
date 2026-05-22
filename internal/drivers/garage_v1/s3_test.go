package garage_v1

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// makeGarageV1Driver constructs a real *driver pointed at the given test server.
func makeGarageV1Driver(t *testing.T, ts *httptest.Server, cfg map[string]string) *driver {
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

// TestGarageV1_PresignGet_EmptyEndpoint ensures PresignGet returns ErrUnsupported
// when no S3 endpoint is configured.
func TestGarageV1_PresignGet_EmptyEndpoint(t *testing.T) {
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

// TestGarageV1_PresignGet_ValidEndpoint ensures PresignGet returns a valid URL
// when S3 endpoint is configured. The presign happens locally; the test server
// just needs to be reachable for credential validation.
func TestGarageV1_PresignGet_ValidEndpoint(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Server doesn't need to respond; presign is local.
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := makeGarageV1Driver(t, ts, map[string]string{
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

// TestGarageV1_PresignPut_EmptyEndpoint ensures PresignPut returns ErrUnsupported
// when no S3 endpoint is configured.
func TestGarageV1_PresignPut_EmptyEndpoint(t *testing.T) {
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

// TestGarageV1_PresignPut_ValidEndpoint ensures PresignPut returns a valid URL
// when S3 endpoint is configured.
func TestGarageV1_PresignPut_ValidEndpoint(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := makeGarageV1Driver(t, ts, map[string]string{
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

// TestGarageV1_PresignPut_ValidEndpoint_NoContentType covers the contentType=="" branch.
func TestGarageV1_PresignPut_ValidEndpoint_NoContentType(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	d := makeGarageV1Driver(t, ts, map[string]string{
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

// TestGarageV1_CreateMultipart_EmptyEndpoint ensures CreateMultipart returns ErrUnsupported.
func TestGarageV1_CreateMultipart_EmptyEndpoint(t *testing.T) {
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

// TestGarageV1_PresignUploadPart_EmptyEndpoint ensures PresignUploadPart returns ErrUnsupported.
func TestGarageV1_PresignUploadPart_EmptyEndpoint(t *testing.T) {
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

// TestGarageV1_CompleteMultipart_EmptyEndpoint ensures CompleteMultipart returns ErrUnsupported.
func TestGarageV1_CompleteMultipart_EmptyEndpoint(t *testing.T) {
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

// TestGarageV1_AbortMultipart_EmptyEndpoint ensures AbortMultipart returns ErrUnsupported.
func TestGarageV1_AbortMultipart_EmptyEndpoint(t *testing.T) {
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

// TestGarageV1_ListObjects_EmptyEndpoint ensures ListObjects returns ErrUnsupported.
func TestGarageV1_ListObjects_EmptyEndpoint(t *testing.T) {
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

// TestGarageV1_StatObject_EmptyEndpoint ensures StatObject returns ErrUnsupported.
func TestGarageV1_StatObject_EmptyEndpoint(t *testing.T) {
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

// TestGarageV1_DeleteObject_EmptyEndpoint ensures DeleteObject returns ErrUnsupported.
func TestGarageV1_DeleteObject_EmptyEndpoint(t *testing.T) {
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

// TestGarageV1_ServerSideCopy_EmptyEndpoint ensures ServerSideCopy returns ErrUnsupported.
func TestGarageV1_ServerSideCopy_EmptyEndpoint(t *testing.T) {
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

// TestGarageV1_ServerSideCopy_SimpleHappyPath ensures ServerSideCopy succeeds with valid config.
func TestGarageV1_ServerSideCopy_SimpleHappyPath(t *testing.T) {
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

	d := makeGarageV1Driver(t, ts, map[string]string{
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

// TestGarageV1_ServerSideCopy_UsesCaps checks that Capabilities.ServerSideCopy is true when s3Client configured.
func TestGarageV1_ServerSideCopy_UsesCaps(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer ts.Close()

	d := makeGarageV1Driver(t, ts, map[string]string{
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
