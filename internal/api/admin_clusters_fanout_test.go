package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// ----- fan-out test stub driver -----
//
// We register a real driver factory so driver.Registry can build instances
// from store.Connection records. Each connection's Config["behavior"]
// selects a per-call strategy: "ok", "stall", "error".

// fanoutDriver is the in-memory driver returned by the stub factory.
type fanoutDriver struct {
	behavior string // "ok", "stall", "error"
	connID   string // for distinguishing buckets/keys per cluster
}

func (d *fanoutDriver) Capabilities(_ context.Context) (driver.Caps, error) {
	return driver.Caps{}, nil
}
func (d *fanoutDriver) HealthCheck(_ context.Context) (driver.HealthReport, error) {
	switch d.behavior {
	case "error":
		return driver.HealthReport{Status: "unhealthy"}, errors.New("health failed")
	default:
		return driver.HealthReport{Status: "healthy"}, nil
	}
}
func (d *fanoutDriver) ListNodes(_ context.Context) ([]driver.Node, error) {
	return nil, nil
}
func (d *fanoutDriver) GetLayout(_ context.Context) (driver.Layout, error) {
	return driver.Layout{}, nil
}
func (d *fanoutDriver) StageLayout(_ context.Context, _ driver.LayoutChange) (driver.LayoutDiff, error) {
	return driver.LayoutDiff{}, nil
}
func (d *fanoutDriver) ApplyLayout(_ context.Context) error { return nil }
func (d *fanoutDriver) RevertLayout(_ context.Context) error { return nil }

func (d *fanoutDriver) ListBuckets(ctx context.Context) ([]driver.Bucket, error) {
	switch d.behavior {
	case "error":
		return nil, errors.New("ListBuckets error from " + d.connID)
	case "stall":
		// Block until the per-cluster 3s deadline trips.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Second):
			return nil, errors.New("unreachable")
		}
	default:
		return []driver.Bucket{
			{ID: "b-" + d.connID + "-1", Aliases: []string{"bucket-" + d.connID}},
		}, nil
	}
}

func (d *fanoutDriver) GetBucket(_ context.Context, id string) (driver.Bucket, error) {
	return driver.Bucket{ID: id, Aliases: []string{id}}, nil
}
func (d *fanoutDriver) CreateBucket(_ context.Context, _ driver.BucketSpec) (driver.Bucket, error) {
	return driver.Bucket{}, nil
}
func (d *fanoutDriver) UpdateBucket(_ context.Context, _ string, _ driver.BucketUpdate) (driver.Bucket, error) {
	return driver.Bucket{}, nil
}
func (d *fanoutDriver) DeleteBucket(_ context.Context, _ string) error { return nil }

func (d *fanoutDriver) ListKeys(ctx context.Context) ([]driver.Key, error) {
	switch d.behavior {
	case "error":
		return nil, errors.New("ListKeys error from " + d.connID)
	case "stall":
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Second):
			return nil, errors.New("unreachable")
		}
	default:
		return []driver.Key{
			{ID: "k-" + d.connID + "-1", Name: "key-" + d.connID},
		}, nil
	}
}

func (d *fanoutDriver) GetKey(_ context.Context, _ string) (driver.Key, error) {
	return driver.Key{}, nil
}
// CreateKey on the fanout stub returns a populated key including a
// SecretAccessKey so v0.9.0m's "secret shown once" handler test can
// assert the response surface. behavior="error" still errors so the
// existing error-path tests keep their meaning.
func (d *fanoutDriver) CreateKey(_ context.Context, spec driver.KeySpec) (driver.Key, error) {
	if d.behavior == "error" {
		return driver.Key{}, errors.New("CreateKey error from " + d.connID)
	}
	secret := "S" + d.connID + "-secret-" + spec.Name
	return driver.Key{
		ID:          "k-" + d.connID + "-new",
		Name:        spec.Name,
		AccessKeyID: "GK" + d.connID + "ACCESS",
		// Pointer to a string — driver.Key models the secret as *string
		// because it's only populated on create. Get/List paths leave
		// it nil so callers can't accidentally leak a stale value.
		SecretAccessKey: &secret,
	}, nil
}
func (d *fanoutDriver) UpdateKeyPermissions(_ context.Context, _ string, _ []driver.BucketPermission) error {
	return nil
}
func (d *fanoutDriver) DeleteKey(_ context.Context, _ string) error { return nil }

func (d *fanoutDriver) ListObjects(_ context.Context, _, _, _, _ string, _ int) (driver.ObjectPage, error) {
	return driver.ObjectPage{}, nil
}
func (d *fanoutDriver) StatObject(_ context.Context, _, _ string) (driver.ObjectInfo, error) {
	return driver.ObjectInfo{}, nil
}
func (d *fanoutDriver) PresignGet(_ context.Context, _, _ string, _ time.Duration) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, nil
}
func (d *fanoutDriver) PresignPut(_ context.Context, _, _ string, _ time.Duration, _ string) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, nil
}
func (d *fanoutDriver) DeleteObject(_ context.Context, _, _ string) error { return nil }
func (d *fanoutDriver) CreateMultipart(_ context.Context, _, _, _ string) (driver.MultipartUpload, error) {
	return driver.MultipartUpload{}, nil
}
func (d *fanoutDriver) PresignUploadPart(_ context.Context, _ driver.MultipartUpload, _ int) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, nil
}
func (d *fanoutDriver) CompleteMultipart(_ context.Context, _ driver.MultipartUpload, _ []driver.CompletedPart) error {
	return nil
}
func (d *fanoutDriver) AbortMultipart(_ context.Context, _ driver.MultipartUpload) error { return nil }

// fanoutDriverName is registered once via init() and never duplicated.
const fanoutDriverName = "stub-fanout-driver"

var fanoutRegisterOnce sync.Once

func registerFanoutDriver(t *testing.T) {
	t.Helper()
	fanoutRegisterOnce.Do(func() {
		driver.Register(fanoutDriverName, func(cfg driver.Config) (driver.Driver, error) {
			if cfg["force_build_error"] == "1" {
				return nil, errors.New("forced build error")
			}
			return &fanoutDriver{
				behavior: cfg["behavior"],
				connID:   cfg["conn_id"],
			}, nil
		})
		// Allow the connection store to accept our stub driver. SupportedDrivers
		// is a process-global; once we add the key it persists for all tests.
		store.SupportedDrivers[fanoutDriverName] = true
	})
}

// makeFanoutConnsStore builds a mock conns store seeded with connections
// pointing at fanoutDriverName, one per per-cluster behavior in `behaviors`.
func makeFanoutConnsStore(behaviors map[string]string) *testMockConnectionStore {
	conns := make([]store.Connection, 0, len(behaviors))
	for id, behavior := range behaviors {
		conns = append(conns, store.Connection{
			ID:     id,
			Label:  "conn-" + id,
			Driver: fanoutDriverName,
			Config: map[string]string{
				"behavior": behavior,
				"conn_id":  id,
			},
			Owner: "org",
		})
	}
	return &testMockConnectionStore{conns: conns}
}

// TestListAllBucketsHandler_HappyPath covers the all-clusters-OK code path.
func TestListAllBucketsHandler_HappyPath(t *testing.T) {
	registerFanoutDriver(t)

	connsStore := makeFanoutConnsStore(map[string]string{
		"c1": "ok",
		"c2": "ok",
	})
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/buckets", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var resp AggregatedBucketsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Buckets) != 2 {
		t.Errorf("Buckets count=%d, want 2", len(resp.Buckets))
	}
	if len(resp.Errors) != 0 {
		t.Errorf("Errors count=%d, want 0; got %+v", len(resp.Errors), resp.Errors)
	}
}

// TestListAllBucketsHandler_OneStalledOneHealthy verifies the per-cluster
// 3s deadline kicks in and the healthy cluster's result is returned alongside
// the stalled cluster's error.
func TestListAllBucketsHandler_OneStalledOneHealthy(t *testing.T) {
	registerFanoutDriver(t)

	connsStore := makeFanoutConnsStore(map[string]string{
		"healthy": "ok",
		"stalled": "stall",
	})
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/buckets", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()

	start := time.Now()
	srv.router.ServeHTTP(rr, req)
	elapsed := time.Since(start)

	// Must complete well under 10s (the unreachable timeout the stub would hit
	// if the 3s deadline didn't fire).
	if elapsed > 6*time.Second {
		t.Errorf("handler took %v; expected <6s with per-cluster 3s deadline", elapsed)
	}

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var resp AggregatedBucketsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Buckets) != 1 {
		t.Errorf("Buckets count=%d, want 1; got %+v", len(resp.Buckets), resp.Buckets)
	}
	if resp.Buckets[0].ConnectionID != "healthy" {
		t.Errorf("expected healthy bucket, got connID=%q", resp.Buckets[0].ConnectionID)
	}
	if len(resp.Errors) != 1 {
		t.Errorf("Errors count=%d, want 1; got %+v", len(resp.Errors), resp.Errors)
	} else if resp.Errors[0].ConnectionID != "stalled" {
		t.Errorf("Errors[0].ConnectionID=%q, want stalled", resp.Errors[0].ConnectionID)
	}
}

// TestListAllBucketsHandler_AllErrors covers the case where every cluster's
// ListBuckets fails (driver-level error, not deadline).
func TestListAllBucketsHandler_AllErrors(t *testing.T) {
	registerFanoutDriver(t)

	connsStore := makeFanoutConnsStore(map[string]string{
		"e1": "error",
		"e2": "error",
	})
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/buckets", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var resp AggregatedBucketsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Buckets) != 0 {
		t.Errorf("Buckets count=%d, want 0", len(resp.Buckets))
	}
	if len(resp.Errors) != 2 {
		t.Errorf("Errors count=%d, want 2; got %+v", len(resp.Errors), resp.Errors)
	}
}

// TestListAllBucketsHandler_BuildError covers the driver-construction failure
// path: registry.For() returns an error before ListBuckets is called.
func TestListAllBucketsHandler_BuildError(t *testing.T) {
	registerFanoutDriver(t)

	connsStore := &testMockConnectionStore{conns: []store.Connection{
		{
			ID:     "broken",
			Label:  "broken-conn",
			Driver: fanoutDriverName,
			Config: map[string]string{"force_build_error": "1", "conn_id": "broken"},
			Owner:  "org",
		},
	}}
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/buckets", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp AggregatedBucketsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Errors) != 1 {
		t.Fatalf("Errors count=%d, want 1", len(resp.Errors))
	}
	if resp.Errors[0].ConnectionID != "broken" {
		t.Errorf("ConnectionID=%q, want broken", resp.Errors[0].ConnectionID)
	}
}

// TestListAllBucketsHandler_EmptyConns covers the no-connections fast path.
func TestListAllBucketsHandler_EmptyConns(t *testing.T) {
	registerFanoutDriver(t)

	connsStore := &testMockConnectionStore{conns: []store.Connection{}}
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/buckets", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	var resp AggregatedBucketsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Buckets) != 0 || len(resp.Errors) != 0 {
		t.Errorf("expected empty buckets+errors, got %+v / %+v", resp.Buckets, resp.Errors)
	}
}

// TestListAllBucketsHandler_StoreError covers the conns.List() failure path.
func TestListAllBucketsHandler_StoreError(t *testing.T) {
	registerFanoutDriver(t)

	connsStore := &testMockConnectionStore{
		listFunc: func(_ context.Context) ([]store.Connection, error) {
			return nil, errors.New("backend disk error")
		},
	}
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/buckets", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", rr.Code)
	}
}

// TestListAllBucketsHandler_MethodNotAllowed covers the GET-only gate.
func TestListAllBucketsHandler_MethodNotAllowed(t *testing.T) {
	registerFanoutDriver(t)
	connsStore := &testMockConnectionStore{}
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	// chi's router responds with 405 to a registered path matching another method.
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/buckets", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d, want 405", rr.Code)
	}
}

// v1.11.0.15: TestListAllKeys* tests removed alongside the
// /admin/keys route. Keys are inherently per-cluster (Garage admin
// model); the per-cluster /admin/clusters/{cid}/keys handler is the
// canonical path and has its own dispatch tests in
// admin_per_cluster_dispatch_test.go.

// TestListBucketsByClusterHandler_HappyPath covers the connection-scoped path.
func TestListBucketsByClusterHandler_HappyPath(t *testing.T) {
	registerFanoutDriver(t)
	connsStore := makeFanoutConnsStore(map[string]string{"single": "ok"})
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters/single/buckets", nil)
	req.AddCookie(generateClusterAdminCookie("single"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var buckets []driver.Bucket
	if err := json.NewDecoder(rr.Body).Decode(&buckets); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(buckets) != 1 {
		t.Errorf("buckets=%d, want 1", len(buckets))
	}
}

// TestListBucketsByClusterHandler_NotFound covers the unknown-cluster path.
func TestListBucketsByClusterHandler_NotFound(t *testing.T) {
	registerFanoutDriver(t)
	connsStore := &testMockConnectionStore{}
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters/missing/buckets", nil)
	req.AddCookie(generateClusterAdminCookie("missing"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rr.Code)
	}
}

// TestListBucketsByClusterHandler_DriverError covers the driver-error path
// (writeDriverError branch for non-driver.Error errors → 500 INTERNAL).
func TestListBucketsByClusterHandler_DriverError(t *testing.T) {
	registerFanoutDriver(t)
	connsStore := makeFanoutConnsStore(map[string]string{"err": "error"})
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters/err/buckets", nil)
	req.AddCookie(generateClusterAdminCookie("err"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", rr.Code)
	}
}

// TestListBucketsByClusterHandler_EmptyNotNil covers nil→[] conversion.
func TestListBucketsByClusterHandler_EmptyNotNil(t *testing.T) {
	registerFanoutDriver(t)

	// Register a driver factory whose ListBuckets returns nil (not an error).
	const nilName = "stub-nil-buckets"
	var nilOnce sync.Once
	nilOnce.Do(func() {
		driver.Register(nilName, func(_ driver.Config) (driver.Driver, error) {
			return &fanoutEmptyDriver{}, nil
		})
		store.SupportedDrivers[nilName] = true
	})

	connsStore := &testMockConnectionStore{conns: []store.Connection{
		{ID: "empty", Label: "empty", Driver: nilName, Config: map[string]string{}, Owner: "org"},
	}}
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters/empty/buckets", nil)
	req.AddCookie(generateClusterAdminCookie("empty"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	// Body should be `[]\n`, not `null\n` — the handler swaps nil for [].
	got := rr.Body.String()
	if got != "[]\n" {
		t.Errorf("body=%q, want \"[]\\n\"", got)
	}
}

// TestListKeysByClusterHandler_HappyPath covers the connection-scoped keys path.
func TestListKeysByClusterHandler_HappyPath(t *testing.T) {
	registerFanoutDriver(t)
	connsStore := makeFanoutConnsStore(map[string]string{"kc": "ok"})
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters/kc/keys", nil)
	req.AddCookie(generateClusterAdminCookie("kc"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var keys []driver.Key
	if err := json.NewDecoder(rr.Body).Decode(&keys); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("keys=%d, want 1", len(keys))
	}
}

// TestListKeysByClusterHandler_NotFound covers the unknown-cluster path.
func TestListKeysByClusterHandler_NotFound(t *testing.T) {
	registerFanoutDriver(t)
	connsStore := &testMockConnectionStore{}
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters/missing/keys", nil)
	req.AddCookie(generateClusterAdminCookie("missing"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rr.Code)
	}
}

// TestListKeysByClusterHandler_DriverError covers the driver-error path.
func TestListKeysByClusterHandler_DriverError(t *testing.T) {
	registerFanoutDriver(t)
	connsStore := makeFanoutConnsStore(map[string]string{"e": "error"})
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters/e/keys", nil)
	req.AddCookie(generateClusterAdminCookie("e"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", rr.Code)
	}
}

// TestCreateKeyHandler_ReturnsSecretAccessKey covers the v0.9.0m
// shown-once contract: POST /admin/clusters/{cid}/keys MUST return both
// accessKeyId AND secretAccessKey in the response body. Without the
// secret in the response, basement can't bootstrap an operator's
// Connect-a-bucket flow — Garage hands the secret out exactly once at
// creation, and the only path off the wire is this handler's reply.
//
// The test additionally asserts the registry routed to the cid-specific
// driver (the returned accessKeyId carries the connID we hit), so a
// future regression that flips back to s.drv would be caught.
func TestCreateKeyHandler_ReturnsSecretAccessKey(t *testing.T) {
	registerFanoutDriver(t)
	connsStore := makeFanoutConnsStore(map[string]string{"cc": "ok"})
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

body := bytes.NewBufferString(`{"name":"bootstrap"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters/cc/keys", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(generateClusterAdminCookie("cc"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	var got driver.Key
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Both fields MUST be present. The secret in particular — the
	// frontend's shown-once dialog reads it straight off this response;
	// strip it on the server side and the dialog shows "(no secret
	// returned by backend)" and the operator is blocked.
	if got.AccessKeyID == "" {
		t.Errorf("accessKeyId empty, want non-empty")
	}
	if got.SecretAccessKey == nil {
		t.Fatalf("secretAccessKey nil — handler dropped the create-only secret")
	}
	if *got.SecretAccessKey == "" {
		t.Errorf("secretAccessKey empty string, want non-empty")
	}

	// Registry-routing sanity: the stub embeds connID in both fields,
	// so a cross-wired registry would surface a different cid here.
	if !bytes.Contains([]byte(got.AccessKeyID), []byte("cc")) {
		t.Errorf("accessKeyId=%q does not reference cid=cc — handler used wrong driver", got.AccessKeyID)
	}
	if !bytes.Contains([]byte(*got.SecretAccessKey), []byte("cc")) {
		t.Errorf("secretAccessKey=%q does not reference cid=cc — handler used wrong driver", *got.SecretAccessKey)
	}
}

// TestCreateKeyHandler_UnknownCluster: POST to a cid the conn store
// doesn't know about must 404 via the registry, not silently fall
// through to s.drv. Catches a regression where the handler skips the
// registry lookup when reg returns ErrConnectionNotFound.
func TestCreateKeyHandler_UnknownCluster(t *testing.T) {
	registerFanoutDriver(t)
	connsStore := &testMockConnectionStore{}
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	body := bytes.NewBufferString(`{"name":"k"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters/missing/keys", body)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(generateClusterAdminCookie("missing"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}

// TestTestClusterHandler_HappyPath covers POST /_test on a healthy cluster.
func TestTestClusterHandler_HappyPath(t *testing.T) {
	registerFanoutDriver(t)
	connsStore := makeFanoutConnsStore(map[string]string{"hc": "ok"})
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters/hc/_test", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var result TestClusterResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !result.Ok {
		t.Errorf("Ok=false, want true; message=%q", result.Message)
	}
	if result.Message != "healthy" {
		t.Errorf("Message=%q, want \"healthy\"", result.Message)
	}
}

// TestTestClusterHandler_HealthCheckFails covers the HealthCheck error branch.
func TestTestClusterHandler_HealthCheckFails(t *testing.T) {
	registerFanoutDriver(t)
	connsStore := makeFanoutConnsStore(map[string]string{"sad": "error"})
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters/sad/_test", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	// Note: handler returns 200 + Ok=false on health check failure
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	var result TestClusterResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Ok {
		t.Errorf("Ok=true, want false")
	}
	if result.Message == "" {
		t.Errorf("Message is empty")
	}
}

// TestTestClusterHandler_NotFound covers the connection-not-found path.
func TestTestClusterHandler_NotFound(t *testing.T) {
	registerFanoutDriver(t)
	connsStore := &testMockConnectionStore{}
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters/nope/_test", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rr.Code)
	}
}

// fanoutEmptyDriver returns nil for ListBuckets/ListKeys to exercise the
// nil→[] conversion paths.
type fanoutEmptyDriver struct{ fanoutDriver }

func (d *fanoutEmptyDriver) ListBuckets(_ context.Context) ([]driver.Bucket, error) {
	return nil, nil
}
func (d *fanoutEmptyDriver) ListKeys(_ context.Context) ([]driver.Key, error) {
	return nil, nil
}

// ----- Additional cluster-CRUD edge cases -----

// TestCreateClusterHandler_BadJSON covers the json.Decode error branch.
func TestCreateClusterHandler_BadJSON(t *testing.T) {
	connsStore := &testMockConnectionStore{}
	srv := New(newTestConfig(), nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters", stringsReader("{not-json"))
	req.AddCookie(adminCookie())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rr.Code)
	}
}

// TestCreateClusterHandler_EmptyLabel covers the label-required branch.
func TestCreateClusterHandler_EmptyLabel(t *testing.T) {
	connsStore := &testMockConnectionStore{}
	srv := New(newTestConfig(), nil, connsStore, nil, nil)

	body := `{"label":"","driver":"garage","config":{"x":"y"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters", stringsReader(body))
	req.AddCookie(adminCookie())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rr.Code)
	}
}

// TestCreateClusterHandler_EmptyConfig covers the CONFIG_REQUIRED branch.
func TestCreateClusterHandler_EmptyConfig(t *testing.T) {
	connsStore := &testMockConnectionStore{}
	srv := New(newTestConfig(), nil, connsStore, nil, nil)

	body := `{"label":"x","driver":"garage","config":{}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters", stringsReader(body))
	req.AddCookie(adminCookie())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rr.Code)
	}
	var resp ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != "CONFIG_REQUIRED" {
		t.Errorf("code=%q, want CONFIG_REQUIRED", resp.Error.Code)
	}
}

// TestCreateClusterHandler_ListError covers the conns.List() failure path
// during the duplicate-label check.
func TestCreateClusterHandler_ListError(t *testing.T) {
	connsStore := &testMockConnectionStore{
		listFunc: func(_ context.Context) ([]store.Connection, error) {
			return nil, errors.New("list failed")
		},
	}
	srv := New(newTestConfig(), nil, connsStore, nil, nil)

	body := `{"label":"x","driver":"garage","config":{"k":"v"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters", stringsReader(body))
	req.AddCookie(adminCookie())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", rr.Code)
	}
}

// TestCreateClusterHandler_CreateFails covers store.Create() failure.
func TestCreateClusterHandler_CreateFails(t *testing.T) {
	connsStore := &testMockConnectionStore{
		createFunc: func(_ context.Context, _ store.Connection) (store.Connection, error) {
			return store.Connection{}, errors.New("disk full")
		},
	}
	srv := New(newTestConfig(), nil, connsStore, nil, nil)

	body := `{"label":"x","driver":"garage","config":{"k":"v"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters", stringsReader(body))
	req.AddCookie(adminCookie())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rr.Code)
	}
}

// TestListClustersHandler_StoreError covers the conns.List() failure path.
func TestListClustersHandler_StoreError(t *testing.T) {
	connsStore := &testMockConnectionStore{
		listFunc: func(_ context.Context) ([]store.Connection, error) {
			return nil, errors.New("boom")
		},
	}
	srv := New(newTestConfig(), nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", rr.Code)
	}
}

// TestUpdateClusterHandler_BadJSON covers the decode-error path.
func TestUpdateClusterHandler_BadJSON(t *testing.T) {
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{{ID: "x", Label: "x", Driver: "garage", Config: map[string]string{}, Owner: "org"}},
	}
	srv := New(newTestConfig(), nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/clusters/x", stringsReader("{bad"))
	req.AddCookie(adminCookie())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rr.Code)
	}
}

// TestUpdateClusterHandler_BadDriver covers the unsupported-driver branch.
func TestUpdateClusterHandler_BadDriver(t *testing.T) {
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{{ID: "x", Label: "x", Driver: "garage", Config: map[string]string{}, Owner: "org"}},
	}
	srv := New(newTestConfig(), nil, connsStore, nil, nil)

	body := `{"driver":"not-supported"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/clusters/x", stringsReader(body))
	req.AddCookie(adminCookie())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rr.Code)
	}
}

// TestUpdateClusterHandler_DuplicateLabel covers the dup-check in PATCH.
func TestUpdateClusterHandler_DuplicateLabel(t *testing.T) {
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "a", Label: "alpha", Driver: "garage", Config: map[string]string{}, Owner: "org"},
			{ID: "b", Label: "beta", Driver: "garage", Config: map[string]string{}, Owner: "org"},
		},
	}
	srv := New(newTestConfig(), nil, connsStore, nil, nil)

	body := `{"label":"beta"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/clusters/a", stringsReader(body))
	req.AddCookie(adminCookie())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("status=%d, want 409", rr.Code)
	}
}

// TestUpdateClusterHandler_ListErrorOnRename covers conns.List() failure during the dup-check.
func TestUpdateClusterHandler_ListErrorOnRename(t *testing.T) {
	connsStore := &testMockConnectionStore{
		listFunc: func(_ context.Context) ([]store.Connection, error) {
			return nil, errors.New("list err")
		},
	}
	srv := New(newTestConfig(), nil, connsStore, nil, nil)

	body := `{"label":"renamed"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/clusters/x", stringsReader(body))
	req.AddCookie(adminCookie())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status=%d, want 500", rr.Code)
	}
}

// TestUpdateClusterHandler_EmptyLabelSkipsValidation ensures an empty label
// in the patch is treated as "no change" (no validation error).
func TestUpdateClusterHandler_EmptyLabelSkipsValidation(t *testing.T) {
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{{ID: "x", Label: "orig", Driver: "garage", Config: map[string]string{}, Owner: "org"}},
	}
	srv := New(newTestConfig(), nil, connsStore, nil, nil)

	body := `{"color":"#ff0000"}` // label not set
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/clusters/x", stringsReader(body))
	req.AddCookie(adminCookie())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status=%d, want 200; body=%s", rr.Code, rr.Body.String())
	}
}

// TestUpdateClusterHandler_NotFound covers the Update-not-found branch.
func TestUpdateClusterHandler_NotFound(t *testing.T) {
	connsStore := &testMockConnectionStore{}
	srv := New(newTestConfig(), nil, connsStore, nil, nil)

	body := `{"color":"#ff0000"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/clusters/missing", stringsReader(body))
	req.AddCookie(adminCookie())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rr.Code)
	}
}

// TestArmDeleteClusterHandler_NotFound covers the connection-not-found path.
func TestArmDeleteClusterHandler_NotFound(t *testing.T) {
	connsStore := &testMockConnectionStore{}
	srv := New(newTestConfig(), nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters/missing/_arm-delete", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 404", rr.Code)
	}
}

// TestDeleteClusterHandler_MismatchToken covers the ErrConfirmMismatch path:
// the operator arms delete for cluster A, then tries it on cluster B.
func TestDeleteClusterHandler_MismatchToken(t *testing.T) {
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "a", Label: "a", Driver: "garage", Config: map[string]string{}, Owner: "org"},
			{ID: "b", Label: "b", Driver: "garage", Config: map[string]string{}, Owner: "org"},
		},
	}
	srv := New(newTestConfig(), nil, connsStore, nil, nil)

	// Arm delete on a
	armReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters/a/_arm-delete", nil)
	armReq.AddCookie(adminCookie())
	armRR := httptest.NewRecorder()
	srv.router.ServeHTTP(armRR, armReq)

	var armResp map[string]any
	_ = json.NewDecoder(armRR.Body).Decode(&armResp)
	token, _ := armResp["token"].(string)
	if token == "" {
		t.Fatalf("arm did not return a token; body=%s", armRR.Body.String())
	}

	// Try to delete b with a's token
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/clusters/b", nil)
	delReq.AddCookie(adminCookie())
	delReq.Header.Set("X-Confirm-Delete", token)
	delRR := httptest.NewRecorder()
	srv.router.ServeHTTP(delRR, delReq)

	if delRR.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", delRR.Code)
	}
	var er ErrorResponse
	_ = json.NewDecoder(delRR.Body).Decode(&er)
	if er.Error.Code != "CONFIRMATION_MISMATCH" {
		t.Errorf("code=%q, want CONFIRMATION_MISMATCH", er.Error.Code)
	}
}

// TestDeleteClusterHandler_RegistryInvalidate ensures Invalidate is called
// on successful delete (no observable assertion, but exercises the branch).
func TestDeleteClusterHandler_RegistryInvalidate(t *testing.T) {
	registerFanoutDriver(t)
	connsStore := makeFanoutConnsStore(map[string]string{"to-delete": "ok"})
	reg := driver.NewRegistry(connsStore)

	// Force the registry to cache a driver before delete.
	if _, err := reg.For(context.Background(), "to-delete"); err != nil {
		t.Fatalf("priming registry: %v", err)
	}

	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	armReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters/to-delete/_arm-delete", nil)
	armReq.AddCookie(adminCookie())
	armRR := httptest.NewRecorder()
	srv.router.ServeHTTP(armRR, armReq)

	var armResp map[string]any
	_ = json.NewDecoder(armRR.Body).Decode(&armResp)
	token := armResp["token"].(string)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/clusters/to-delete", nil)
	delReq.AddCookie(adminCookie())
	delReq.Header.Set("X-Confirm-Delete", token)
	delRR := httptest.NewRecorder()
	srv.router.ServeHTTP(delRR, delReq)

	if delRR.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", delRR.Code, delRR.Body.String())
	}
}

// TestGetClusterHandler_EmptyCID covers the chi-URL-empty branch (cannot
// actually hit through the router since `/clusters/` would 404, but it's
// reachable via a different mount). Skip for now and rely on TestGetClusterHandler_NotFound.

// ----- helpers -----

// stringsReader wraps a string for httptest.NewRequest bodies.
func stringsReader(s string) *stringReader { return &stringReader{s: s} }

type stringReader struct {
	s string
	o int
}

func (r *stringReader) Read(p []byte) (int, error) {
	if r.o >= len(r.s) {
		return 0, fmt.Errorf("EOF")
	}
	n := copy(p, r.s[r.o:])
	r.o += n
	return n, nil
}

func adminCookie() *http.Cookie {
	return &http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateUIAdminToken(),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
}

// generateClusterAdminCookie creates a cookie with activeRole.kind="cluster-admin" for the given cluster.
func generateClusterAdminCookie(cid string) *http.Cookie {
	return &http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateClusterAdminToken(cid),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
}
