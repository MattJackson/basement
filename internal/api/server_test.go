package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// mockDriver is a mock driver implementation for testing.
type mockDriver struct{}

func (m *mockDriver) Capabilities(_ context.Context) (driver.Caps, error) { return driver.Caps{}, nil }
func (m *mockDriver) HealthCheck(_ context.Context) (driver.HealthReport, error) { return driver.HealthReport{}, nil }
func (m *mockDriver) ListNodes(_ context.Context) ([]driver.Node, error) { return nil, nil }
func (m *mockDriver) GetLayout(_ context.Context) (driver.Layout, error) { return driver.Layout{}, nil }
func (m *mockDriver) StageLayout(_ context.Context, _ driver.LayoutChange) (driver.LayoutDiff, error) { return driver.LayoutDiff{}, nil }
func (m *mockDriver) ApplyLayout(_ context.Context) error { return nil }
func (m *mockDriver) RevertLayout(_ context.Context) error { return nil }
func (m *mockDriver) ListBuckets(_ context.Context) ([]driver.Bucket, error) { return nil, nil }
func (m *mockDriver) GetBucket(_ context.Context, _ string) (driver.Bucket, error) { return driver.Bucket{}, nil }
func (m *mockDriver) CreateBucket(_ context.Context, _ driver.BucketSpec) (driver.Bucket, error) { return driver.Bucket{}, nil }
func (m *mockDriver) UpdateBucket(_ context.Context, _ string, _ driver.BucketUpdate) (driver.Bucket, error) { return driver.Bucket{}, nil }
func (m *mockDriver) DeleteBucket(_ context.Context, _ string) error { return nil }
func (m *mockDriver) ListKeys(_ context.Context) ([]driver.Key, error) { return nil, nil }
func (m *mockDriver) GetKey(_ context.Context, _ string) (driver.Key, error) { return driver.Key{}, nil }
func (m *mockDriver) CreateKey(_ context.Context, _ driver.KeySpec) (driver.Key, error) { return driver.Key{}, nil }
func (m *mockDriver) UpdateKeyPermissions(_ context.Context, _ string, _ []driver.BucketPermission) error { return nil }
func (m *mockDriver) DeleteKey(_ context.Context, _ string) error { return nil }
func (m *mockDriver) ListObjects(_ context.Context, _, _, _, _ string, _ int) (driver.ObjectPage, error) { return driver.ObjectPage{}, nil }
func (m *mockDriver) StatObject(_ context.Context, _, _ string) (driver.ObjectInfo, error) { return driver.ObjectInfo{}, nil }
func (m *mockDriver) PresignGet(_ context.Context, _, _ string, _ time.Duration) (driver.PresignedURL, error) { return driver.PresignedURL{}, nil }
func (m *mockDriver) PresignPut(_ context.Context, _, _ string, _ time.Duration, _ string) (driver.PresignedURL, error) { return driver.PresignedURL{}, nil }
func (m *mockDriver) DeleteObject(_ context.Context, _, _ string) error { return nil }
func (m *mockDriver) CreateMultipart(_ context.Context, _, _, _ string) (driver.MultipartUpload, error) { return driver.MultipartUpload{}, nil }
func (m *mockDriver) PresignUploadPart(_ context.Context, _ driver.MultipartUpload, _ int) (driver.PresignedURL, error) { return driver.PresignedURL{}, nil }
func (m *mockDriver) CompleteMultipart(_ context.Context, _ driver.MultipartUpload, _ []driver.CompletedPart) error { return nil }
func (m *mockDriver) AbortMultipart(_ context.Context, _ driver.MultipartUpload) error { return nil }

func TestHealthHandler(t *testing.T) {
	cfg := &config.Config{Listen: ":8080"}
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)
	drv := &mockDriver{}

	srv := New(cfg, st, nil, drv, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var resp HealthResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("expected status 'ok', got '%s'", resp.Status)
	}

	if resp.Version != "dev" {
		t.Errorf("expected version 'dev', got '%s'", resp.Version)
	}
}

func TestHealthHandler404(t *testing.T) {
	cfg := &config.Config{Listen: ":8080"}
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)
	drv := &mockDriver{}

	srv := New(cfg, st, nil, drv, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nonexistent", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rr.Code)
	}
}

func TestHealthHandler405(t *testing.T) {
	cfg := &config.Config{Listen: ":8080"}
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)
	drv := &mockDriver{}

	srv := New(cfg, st, nil, drv, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/health", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rr.Code)
	}
}

func TestContentTypeMiddleware(t *testing.T) {
	cfg := &config.Config{Listen: ":8080"}
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)
	drv := &mockDriver{}

	srv := New(cfg, st, nil, drv, nil)

	body := bytes.NewBufferString(`{"test": "data"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/health", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405 (POST not allowed on health), got %d", rr.Code)
	}
}
