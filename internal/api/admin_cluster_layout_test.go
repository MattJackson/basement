package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// layoutDriverName is registered once per process for layout tests.
const layoutDriverName = "stub-layout"

var layoutRegisterOnce sync.Once

// layoutDriver implements driver.Driver with configurable layout behaviour.
type layoutDriver struct {
	nodes  []driver.Node
	layout driver.Layout
}

func (d *layoutDriver) Capabilities(_ context.Context) (driver.Caps, error) {
	return driver.Caps{Layout: driver.LayoutApplyRevert}, nil
}
func (d *layoutDriver) HealthCheck(_ context.Context) (driver.HealthReport, error) {
	return driver.HealthReport{Status: "healthy"}, nil
}
func (d *layoutDriver) ListNodes(_ context.Context) ([]driver.Node, error) {
	return d.nodes, nil
}
func (d *layoutDriver) GetLayout(_ context.Context) (driver.Layout, error) {
	return d.layout, nil
}
func (d *layoutDriver) StageLayout(_ context.Context, change driver.LayoutChange) (driver.LayoutDiff, error) {
	return driver.LayoutDiff{}, nil
}
func (d *layoutDriver) ApplyLayout(_ context.Context) error  { return nil }
func (d *layoutDriver) RevertLayout(_ context.Context) error { return nil }
func (d *layoutDriver) ListBuckets(_ context.Context) ([]driver.Bucket, error) {
	return nil, nil
}
func (d *layoutDriver) GetBucket(_ context.Context, _ string) (driver.Bucket, error) {
	return driver.Bucket{}, nil
}
func (d *layoutDriver) CreateBucket(_ context.Context, _ driver.BucketSpec) (driver.Bucket, error) {
	return driver.Bucket{}, nil
}
func (d *layoutDriver) UpdateBucket(_ context.Context, _ string, _ driver.BucketUpdate) (driver.Bucket, error) {
	return driver.Bucket{}, nil
}
func (d *layoutDriver) DeleteBucket(_ context.Context, _ string) error { return nil }
func (d *layoutDriver) ListKeys(_ context.Context) ([]driver.Key, error) {
	return nil, nil
}
func (d *layoutDriver) GetKey(_ context.Context, _ string) (driver.Key, error) {
	return driver.Key{}, nil
}
func (d *layoutDriver) CreateKey(_ context.Context, _ driver.KeySpec) (driver.Key, error) {
	return driver.Key{}, nil
}
func (d *layoutDriver) UpdateKeyPermissions(_ context.Context, _ string, _ []driver.BucketPermission) error {
	return nil
}
func (d *layoutDriver) DeleteKey(_ context.Context, _ string) error { return nil }
func (d *layoutDriver) ListObjects(_ context.Context, _, _, _, _ string, _ int) (driver.ObjectPage, error) {
	return driver.ObjectPage{}, nil
}
func (d *layoutDriver) StatObject(_ context.Context, _, _ string) (driver.ObjectInfo, error) {
	return driver.ObjectInfo{}, nil
}
func (d *layoutDriver) PresignGet(_ context.Context, _, _ string, _ time.Duration) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, nil
}
func (d *layoutDriver) PresignPut(_ context.Context, _, _ string, _ time.Duration, _ string) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, nil
}
func (d *layoutDriver) DeleteObject(_ context.Context, _, _ string) error { return nil }
func (d *layoutDriver) CreateMultipart(_ context.Context, _, _, _ string) (driver.MultipartUpload, error) {
	return driver.MultipartUpload{}, nil
}
func (d *layoutDriver) PresignUploadPart(_ context.Context, _ driver.MultipartUpload, _ int) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, nil
}
func (d *layoutDriver) CompleteMultipart(_ context.Context, _ driver.MultipartUpload, _ []driver.CompletedPart) error {
	return nil
}
func (d *layoutDriver) AbortMultipart(_ context.Context, _ driver.MultipartUpload) error {
	return nil
}

func registerLayoutDriver(t *testing.T) {
	t.Helper()
	layoutRegisterOnce.Do(func() {
		driver.Register(layoutDriverName, func(_ driver.Config) (driver.Driver, error) {
			return &layoutDriver{
				nodes: []driver.Node{
					{ID: "n1", Hostname: "h1", Zone: "z1"},
					{ID: "n2", Hostname: "h2", Zone: "z2"},
				},
				layout: driver.Layout{Version: 7, Nodes: []driver.Node{{ID: "n1"}}},
			}, nil
		})
		store.SupportedDrivers[layoutDriverName] = true
	})
}

func layoutTestServer(t *testing.T) *Server {
	t.Helper()
	registerLayoutDriver(t)

	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "cl", Label: "cl", Driver: layoutDriverName, Config: map[string]string{}, Owner: "org"},
		},
	}
	reg := driver.NewRegistry(connsStore)
	return New(newTestConfig(), nil, connsStore, nil, reg)
}

// TestListNodesHandler_HappyPathFromRegistry covers the listNodes
// path via /admin/clusters/{cid}/nodes (the new routing).
func TestListNodesHandler_HappyPathFromRegistry(t *testing.T) {
	srv := layoutTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters/cl/nodes", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var nodes []driver.Node
	if err := json.NewDecoder(rr.Body).Decode(&nodes); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("nodes=%d, want 2", len(nodes))
	}
}

// TestListNodesHandler_NotFound covers the unknown-cluster path.
func TestListNodesHandler_NotFound(t *testing.T) {
	registerLayoutDriver(t)
	connsStore := &testMockConnectionStore{}
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters/missing/nodes", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rr.Code)
	}
}

// TestGetLayoutHandler_HappyPathFromRegistry covers GET /admin/clusters/{cid}/layout.
func TestGetLayoutHandler_HappyPathFromRegistry(t *testing.T) {
	srv := layoutTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters/cl/layout", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var layout driver.Layout
	if err := json.NewDecoder(rr.Body).Decode(&layout); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if layout.Version != 7 {
		t.Errorf("Version=%d, want 7", layout.Version)
	}
}

// TestGetLayoutHandler_NotFound covers the unknown-cluster path.
func TestGetLayoutHandler_NotFound(t *testing.T) {
	registerLayoutDriver(t)
	connsStore := &testMockConnectionStore{}
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters/missing/layout", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rr.Code)
	}
}

// TestStageLayoutHandler_HappyPath covers POST .../layout/stage.
func TestStageLayoutHandler_HappyPath(t *testing.T) {
	srv := layoutTestServer(t)

	body := `{"nodeId":"n1","capacity":1000}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters/cl/layout/stage", strings.NewReader(body))
	req.AddCookie(adminCookie())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

// TestStageLayoutHandler_BadJSON covers the decode-error branch.
func TestStageLayoutHandler_BadJSON(t *testing.T) {
	srv := layoutTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters/cl/layout/stage", strings.NewReader("{not-json"))
	req.AddCookie(adminCookie())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rr.Code)
	}
}

// TestApplyLayoutHandler_HappyPath covers POST .../layout/apply.
func TestApplyLayoutHandler_HappyPath(t *testing.T) {
	srv := layoutTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters/cl/layout/apply", nil)
	req.AddCookie(adminCookie())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("status=%d, want 204", rr.Code)
	}
}

// TestRevertLayoutHandler_HappyPath covers POST .../layout/revert.
func TestRevertLayoutHandler_HappyPath(t *testing.T) {
	srv := layoutTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters/cl/layout/revert", nil)
	req.AddCookie(adminCookie())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("status=%d, want 204", rr.Code)
	}
}

// TestApplyLayoutHandler_ClusterNotFound covers the unknown-cluster path
// (exercises resolveClusterDriver's error branch through Apply).
func TestApplyLayoutHandler_ClusterNotFound(t *testing.T) {
	registerLayoutDriver(t)
	connsStore := &testMockConnectionStore{}
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters/missing/layout/apply", nil)
	req.AddCookie(adminCookie())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rr.Code)
	}
}
