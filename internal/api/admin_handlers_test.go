package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// testMockDriver is a mock driver for testing admin handlers.
type testMockDriver struct {
	listNodesFunc    func(ctx context.Context) ([]driver.Node, error)
	getLayoutFunc    func(ctx context.Context) (driver.Layout, error)
	listBucketsFunc  func(ctx context.Context) ([]driver.Bucket, error)
	listKeysFunc     func(ctx context.Context) ([]driver.Key, error)
}

func (m *testMockDriver) Capabilities(_ context.Context) (driver.Caps, error) { return driver.Caps{}, nil }
func (m *testMockDriver) HealthCheck(_ context.Context) (driver.HealthReport, error) { return driver.HealthReport{}, nil }
func (m *testMockDriver) ListNodes(ctx context.Context) ([]driver.Node, error) {
	if m.listNodesFunc != nil {
		return m.listNodesFunc(ctx)
	}
	return nil, nil
}
func (m *testMockDriver) GetLayout(ctx context.Context) (driver.Layout, error) {
	if m.getLayoutFunc != nil {
		return m.getLayoutFunc(ctx)
	}
	return driver.Layout{Nodes: []driver.Node{}}, nil
}
func (m *testMockDriver) StageLayout(_ context.Context, _ driver.LayoutChange) (driver.LayoutDiff, error) { return driver.LayoutDiff{}, nil }
func (m *testMockDriver) ApplyLayout(_ context.Context) error { return nil }
func (m *testMockDriver) RevertLayout(_ context.Context) error { return nil }
func (m *testMockDriver) ListBuckets(ctx context.Context) ([]driver.Bucket, error) {
	if m.listBucketsFunc != nil {
		return m.listBucketsFunc(ctx)
	}
	return nil, nil
}
func (m *testMockDriver) GetBucket(_ context.Context, _ string) (driver.Bucket, error) { return driver.Bucket{}, nil }
func (m *testMockDriver) CreateBucket(_ context.Context, _ driver.BucketSpec) (driver.Bucket, error) { return driver.Bucket{}, nil }
func (m *testMockDriver) UpdateBucket(_ context.Context, _ string, _ driver.BucketUpdate) (driver.Bucket, error) { return driver.Bucket{}, nil }
func (m *testMockDriver) DeleteBucket(_ context.Context, _ string) error { return nil }
func (m *testMockDriver) ListKeys(ctx context.Context) ([]driver.Key, error) {
	if m.listKeysFunc != nil {
		return m.listKeysFunc(ctx)
	}
	return nil, nil
}
func (m *testMockDriver) GetKey(_ context.Context, _ string) (driver.Key, error) { return driver.Key{}, nil }
func (m *testMockDriver) CreateKey(_ context.Context, _ driver.KeySpec) (driver.Key, error) { return driver.Key{}, nil }
func (m *testMockDriver) UpdateKeyPermissions(_ context.Context, _ string, _ []driver.BucketPermission) error { return nil }
func (m *testMockDriver) DeleteKey(_ context.Context, _ string) error { return nil }
func (m *testMockDriver) ListObjects(_ context.Context, _, _, _ string, _ int) (driver.ObjectPage, error) { return driver.ObjectPage{}, nil }
func (m *testMockDriver) StatObject(_ context.Context, _, _ string) (driver.ObjectInfo, error) { return driver.ObjectInfo{}, nil }
func (m *testMockDriver) PresignGet(_ context.Context, _, _ string, _ time.Duration) (driver.PresignedURL, error) { return driver.PresignedURL{}, nil }
func (m *testMockDriver) PresignPut(_ context.Context, _, _ string, _ time.Duration, _ string) (driver.PresignedURL, error) { return driver.PresignedURL{}, nil }
func (m *testMockDriver) DeleteObject(_ context.Context, _, _ string) error { return nil }
func (m *testMockDriver) CreateMultipart(_ context.Context, _, _, _ string) (driver.MultipartUpload, error) { return driver.MultipartUpload{}, nil }
func (m *testMockDriver) PresignUploadPart(_ context.Context, _ driver.MultipartUpload, _ int) (driver.PresignedURL, error) { return driver.PresignedURL{}, nil }
func (m *testMockDriver) CompleteMultipart(_ context.Context, _ driver.MultipartUpload, _ []driver.CompletedPart) error { return nil }
func (m *testMockDriver) AbortMultipart(_ context.Context, _ driver.MultipartUpload) error { return nil }

// testSecret is a 32-byte secret used for JWT token generation in tests.
var testSecret = func() []byte {
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i)
	}
	return secret
}()

// generateAdminToken creates a valid admin JWT token for testing.
func generateAdminToken() string {
	token, err := auth.IssueToken(testSecret, "admin", "admin", 24*time.Hour)
	if err != nil {
		panic(err)
	}
	return token
}

// generateUserToken creates a valid non-admin user JWT token for testing.
func generateUserToken() string {
	token, err := auth.IssueToken(testSecret, "user", "user", 24*time.Hour)
	if err != nil {
		panic(err)
	}
	return token
}

// newTestConfig returns a config with proper JWT secret for tests.
func newTestConfig() *config.Config {
	return &config.Config{Listen: ":8080", JWT: config.JWTConfig{Secret: testSecret}}
}

// createAuthRequest creates an HTTP request with a valid admin session cookie.
func createAuthRequest(method, url string) *http.Request {
	token := generateAdminToken()
	req := httptest.NewRequest(method, url, nil)
	req.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    token,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	return req
}

// createNonAdminRequest creates an HTTP request with a non-admin session cookie.
func createNonAdminRequest(method, url string) *http.Request {
	token := generateUserToken()
	req := httptest.NewRequest(method, url, nil)
	req.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    token,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	return req
}

// assertJSONResponse checks that the response has the expected status code and decodes as JSON.
func assertJSONResponse(t *testing.T, rr *httptest.ResponseRecorder, expectedStatus int) any {
	t.Helper()
	if rr.Code != expectedStatus {
		t.Errorf("expected status %d, got %d: body=%s", expectedStatus, rr.Code, rr.Body.String())
	}
	var resp any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil && expectedStatus == http.StatusOK {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	return resp
}

func TestListNodesHandler_HappyPath(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	nodes := []driver.Node{
		{ID: "node-1", Hostname: "host1", Address: "10.0.0.1", Zone: "zone-a", Role: "storage", Capacity: 1000, Tags: []string{"tag1"}, Status: "connected", Version: "1.0.0"},
	}

	drv := &testMockDriver{
		listNodesFunc: func(_ context.Context) ([]driver.Node, error) {
			return nodes, nil
		},
	}

	srv := New(cfg, st, drv)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/nodes")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	data := assertJSONResponse(t, rr, http.StatusOK).([]any)
	if len(data) != 1 {
		t.Errorf("expected 1 node, got %d", len(data))
	}
}

func TestListNodesHandler_EmptyList(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{
		listNodesFunc: func(_ context.Context) ([]driver.Node, error) {
			return []driver.Node{}, nil
		},
	}

	srv := New(cfg, st, drv)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/nodes")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	data := assertJSONResponse(t, rr, http.StatusOK).([]any)
	if len(data) != 0 {
		t.Errorf("expected empty list, got %d items", len(data))
	}
}

func TestListNodesHandler_DriverUnsupported(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{
		listNodesFunc: func(_ context.Context) ([]driver.Node, error) {
			return nil, &driver.Error{Op: "ListNodes", Driver: "test", Err: driver.ErrUnsupported, Message: "not supported"}
		},
	}

	srv := New(cfg, st, drv)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/nodes")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Errorf("expected status 501, got %d", rr.Code)
	}

	var resp map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	errorObj := resp["error"].(map[string]any)
	if errorObj["code"] != "DRIVER_UNSUPPORTED" {
		t.Errorf("expected code DRIVER_UNSUPPORTED, got %v", errorObj["code"])
	}
}

func TestListNodesHandler_DriverPermissionDenied(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{
		listNodesFunc: func(_ context.Context) ([]driver.Node, error) {
			return nil, &driver.Error{Op: "ListNodes", Driver: "test", Err: driver.ErrPermissionDenied, Message: "permission denied"}
		},
	}

	srv := New(cfg, st, drv)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/nodes")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rr.Code)
	}
}

func TestListNodesHandler_NoAuth(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{}
	srv := New(cfg, st, drv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/nodes", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}
}

func TestListNodesHandler_NonAdminRole(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{}
	srv := New(cfg, st, drv)

	req := createNonAdminRequest(http.MethodGet, "/api/v1/admin/nodes")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rr.Code)
	}
}

func TestListNodesHandler_MethodNotAllowed(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{}
	srv := New(cfg, st, drv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/nodes", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rr.Code)
	}
}

func TestGetLayoutHandler_HappyPath(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	layout := driver.Layout{
		Version: 1,
		Nodes: []driver.Node{
			{ID: "node-1", Hostname: "host1", Address: "10.0.0.1", Zone: "zone-a", Role: "storage", Capacity: 1000, Tags: []string{"tag1"}, Status: "connected", Version: "1.0.0"},
		},
	}

	drv := &testMockDriver{
		getLayoutFunc: func(_ context.Context) (driver.Layout, error) {
			return layout, nil
		},
	}

	srv := New(cfg, st, drv)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/layout")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	data := assertJSONResponse(t, rr, http.StatusOK).(map[string]any)
	nodesArr := data["Nodes"].([]any)
	if nodesArr == nil {
		t.Errorf("expected Nodes array to not be nil")
		return
	}
	if len(nodesArr) != 1 {
		t.Errorf("expected 1 node in layout, got %d", len(nodesArr))
	}
	if data["Version"] != float64(1) {
		t.Errorf("expected Version 1, got %v", data["Version"])
	}
}

func TestGetLayoutHandler_DriverUnsupported(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{
		getLayoutFunc: func(_ context.Context) (driver.Layout, error) {
			return driver.Layout{}, &driver.Error{Op: "GetLayout", Driver: "test", Err: driver.ErrUnsupported, Message: "not supported"}
		},
	}

	srv := New(cfg, st, drv)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/layout")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Errorf("expected status 501, got %d", rr.Code)
	}

	var resp map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	errorObj := resp["error"].(map[string]any)
	if errorObj["code"] != "DRIVER_UNSUPPORTED" {
		t.Errorf("expected code DRIVER_UNSUPPORTED, got %v", errorObj["code"])
	}
}

func TestGetLayoutHandler_NoAuth(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{}
	srv := New(cfg, st, drv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/layout", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}
}

func TestGetLayoutHandler_NonAdminRole(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{}
	srv := New(cfg, st, drv)

	req := createNonAdminRequest(http.MethodGet, "/api/v1/admin/layout")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rr.Code)
	}
}

func TestListBucketsHandler_HappyPath(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	buckets := []driver.Bucket{
		{ID: "bucket-1", Aliases: []string{"alias1"}, Quotas: nil, Created: time.Now()},
	}

	drv := &testMockDriver{
		listBucketsFunc: func(_ context.Context) ([]driver.Bucket, error) {
			return buckets, nil
		},
	}

	srv := New(cfg, st, drv)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/buckets")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	data := assertJSONResponse(t, rr, http.StatusOK).([]any)
	if len(data) != 1 {
		t.Errorf("expected 1 bucket, got %d", len(data))
	}
}

func TestListBucketsHandler_EmptyList(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{
		listBucketsFunc: func(_ context.Context) ([]driver.Bucket, error) {
			return []driver.Bucket{}, nil
		},
	}

	srv := New(cfg, st, drv)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/buckets")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	data := assertJSONResponse(t, rr, http.StatusOK).([]any)
	if len(data) != 0 {
		t.Errorf("expected empty list, got %d items", len(data))
	}
}

func TestListBucketsHandler_DriverUnsupported(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{
		listBucketsFunc: func(_ context.Context) ([]driver.Bucket, error) {
			return nil, &driver.Error{Op: "ListBuckets", Driver: "test", Err: driver.ErrUnsupported, Message: "not supported"}
		},
	}

	srv := New(cfg, st, drv)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/buckets")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Errorf("expected status 501, got %d", rr.Code)
	}

	var resp map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	errorObj := resp["error"].(map[string]any)
	if errorObj["code"] != "DRIVER_UNSUPPORTED" {
		t.Errorf("expected code DRIVER_UNSUPPORTED, got %v", errorObj["code"])
	}
}

func TestListBucketsHandler_DriverPermissionDenied(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{
		listBucketsFunc: func(_ context.Context) ([]driver.Bucket, error) {
			return nil, &driver.Error{Op: "ListBuckets", Driver: "test", Err: driver.ErrPermissionDenied, Message: "permission denied"}
		},
	}

	srv := New(cfg, st, drv)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/buckets")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rr.Code)
	}
}

func TestListBucketsHandler_NoAuth(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{}
	srv := New(cfg, st, drv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/buckets", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}
}

func TestListBucketsHandler_NonAdminRole(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{}
	srv := New(cfg, st, drv)

	req := createNonAdminRequest(http.MethodGet, "/api/v1/admin/buckets")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rr.Code)
	}
}

func TestListBucketsHandler_MethodNotAllowed(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{}
	srv := New(cfg, st, drv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/buckets", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rr.Code)
	}
}

func TestListKeysHandler_HappyPath(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	keys := []driver.Key{
		{ID: "key-1", Name: "test-key", AccessKeyID: "AKIAIOSFODNN7EXAMPLE", Created: time.Now(), AllowCreateBucket: true},
	}

	drv := &testMockDriver{
		listKeysFunc: func(_ context.Context) ([]driver.Key, error) {
			return keys, nil
		},
	}

	srv := New(cfg, st, drv)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/keys")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	data := assertJSONResponse(t, rr, http.StatusOK).([]any)
	if len(data) != 1 {
		t.Errorf("expected 1 key, got %d", len(data))
	}
}

func TestListKeysHandler_EmptyList(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{
		listKeysFunc: func(_ context.Context) ([]driver.Key, error) {
			return []driver.Key{}, nil
		},
	}

	srv := New(cfg, st, drv)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/keys")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	data := assertJSONResponse(t, rr, http.StatusOK).([]any)
	if len(data) != 0 {
		t.Errorf("expected empty list, got %d items", len(data))
	}
}

func TestListKeysHandler_DriverUnsupported(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{
		listKeysFunc: func(_ context.Context) ([]driver.Key, error) {
			return nil, &driver.Error{Op: "ListKeys", Driver: "test", Err: driver.ErrUnsupported, Message: "not supported"}
		},
	}

	srv := New(cfg, st, drv)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/keys")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Errorf("expected status 501, got %d", rr.Code)
	}

	var resp map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	errorObj := resp["error"].(map[string]any)
	if errorObj["code"] != "DRIVER_UNSUPPORTED" {
		t.Errorf("expected code DRIVER_UNSUPPORTED, got %v", errorObj["code"])
	}
}

func TestListKeysHandler_DriverPermissionDenied(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{
		listKeysFunc: func(_ context.Context) ([]driver.Key, error) {
			return nil, &driver.Error{Op: "ListKeys", Driver: "test", Err: driver.ErrPermissionDenied, Message: "permission denied"}
		},
	}

	srv := New(cfg, st, drv)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/keys")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rr.Code)
	}
}

func TestListKeysHandler_NoAuth(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{}
	srv := New(cfg, st, drv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/keys", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}
}

func TestListKeysHandler_NonAdminRole(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{}
	srv := New(cfg, st, drv)

	req := createNonAdminRequest(http.MethodGet, "/api/v1/admin/keys")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rr.Code)
	}
}

func TestListKeysHandler_MethodNotAllowed(t *testing.T) {
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{}
	srv := New(cfg, st, drv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/keys", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rr.Code)
	}
}
