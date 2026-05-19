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

func (m *mockDriver) Capabilities(ctx context.Context) (driver.Caps, error) { return driver.Caps{}, nil }
func (m *mockDriver) HealthCheck(ctx context.Context) (driver.HealthReport, error) { return driver.HealthReport{}, nil }
func (m *mockDriver) ListNodes(ctx context.Context) ([]driver.Node, error) { return nil, nil }
func (m *mockDriver) GetLayout(ctx context.Context) (driver.Layout, error) { return driver.Layout{}, nil }
func (m *mockDriver) StageLayout(ctx context.Context, change driver.LayoutChange) (driver.LayoutDiff, error) { return driver.LayoutDiff{}, nil }
func (m *mockDriver) ApplyLayout(ctx context.Context) error { return nil }
func (m *mockDriver) RevertLayout(ctx context.Context) error { return nil }
func (m *mockDriver) ListBuckets(ctx context.Context) ([]driver.Bucket, error) { return nil, nil }
func (m *mockDriver) GetBucket(ctx context.Context, id string) (driver.Bucket, error) { return driver.Bucket{}, nil }
func (m *mockDriver) CreateBucket(ctx context.Context, spec driver.BucketSpec) (driver.Bucket, error) { return driver.Bucket{}, nil }
func (m *mockDriver) UpdateBucket(ctx context.Context, id string, update driver.BucketUpdate) (driver.Bucket, error) { return driver.Bucket{}, nil }
func (m *mockDriver) DeleteBucket(ctx context.Context, id string) error { return nil }
func (m *mockDriver) ListKeys(ctx context.Context) ([]driver.Key, error) { return nil, nil }
func (m *mockDriver) GetKey(ctx context.Context, id string) (driver.Key, error) { return driver.Key{}, nil }
func (m *mockDriver) CreateKey(ctx context.Context, spec driver.KeySpec) (driver.Key, error) { return driver.Key{}, nil }
func (m *mockDriver) UpdateKeyPermissions(ctx context.Context, keyID string, perms []driver.BucketPermission) error { return nil }
func (m *mockDriver) DeleteKey(ctx context.Context, id string) error { return nil }
func (m *mockDriver) ListObjects(ctx context.Context, bucket, prefix, continuation string, limit int) (driver.ObjectPage, error) { return driver.ObjectPage{}, nil }
func (m *mockDriver) StatObject(ctx context.Context, bucket, key string) (driver.ObjectInfo, error) { return driver.ObjectInfo{}, nil }
func (m *mockDriver) PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (driver.PresignedURL, error) { return driver.PresignedURL{}, nil }
func (m *mockDriver) PresignPut(ctx context.Context, bucket, key string, ttl time.Duration, contentType string) (driver.PresignedURL, error) { return driver.PresignedURL{}, nil }
func (m *mockDriver) DeleteObject(ctx context.Context, bucket, key string) error { return nil }
func (m *mockDriver) CreateMultipart(ctx context.Context, bucket, key, contentType string) (driver.MultipartUpload, error) { return driver.MultipartUpload{}, nil }
func (m *mockDriver) PresignUploadPart(ctx context.Context, upload driver.MultipartUpload, partNum int) (driver.PresignedURL, error) { return driver.PresignedURL{}, nil }
func (m *mockDriver) CompleteMultipart(ctx context.Context, upload driver.MultipartUpload, parts []driver.CompletedPart) error { return nil }
func (m *mockDriver) AbortMultipart(ctx context.Context, upload driver.MultipartUpload) error { return nil }

func TestHealthHandler(t *testing.T) {
	cfg := &config.Config{Listen: ":8080"}
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)
	drv := &mockDriver{}

	srv := New(cfg, st, drv)

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

	srv := New(cfg, st, drv)

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

	srv := New(cfg, st, drv)

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

	srv := New(cfg, st, drv)

	body := bytes.NewBufferString(`{"test": "data"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/health", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405 (POST not allowed on health), got %d", rr.Code)
	}
}
