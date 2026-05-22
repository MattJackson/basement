// Package api: tests for OBS.USAGE /admin/usage/overview
// (cycle v0.9.0k).
//
// Coverage:
//
//  1. Happy path — multi-cluster fan-out aggregates totals, per-cluster
//     rows, and top-N tables in deterministic order.
//  2. Empty state — no connections returns zeroed totals and empty
//     slices (NOT null) so the frontend can map unconditionally.
//  3. Per-cluster failure isolation — one broken connection records
//     healthy=false in its row but doesn't blank the rest of the
//     dashboard.
//  4. Method gate — non-GET returns 405.
//  5. Capability gate — without host:manage_users returns 403.
//
// The fan-out stub driver from admin_clusters_fanout_test.go is reused;
// "ok" buckets in that stub carry zero Bytes/Objects, so we register a
// second variant that produces synthetic sized buckets for the top-N
// ordering tests.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// usageSizedDriver returns buckets with non-zero Bytes/Objects so the
// top-N ordering paths in the handler are actually exercised. behaviour
// "sized:{a,b,c}" mints three buckets with sizes derived from the conn
// id so test assertions can pin the ordering.
type usageSizedDriver struct {
	connID string
	sizes  []int64
}

func (d *usageSizedDriver) Capabilities(_ context.Context) (driver.Caps, error) {
	return driver.Caps{}, nil
}
func (d *usageSizedDriver) HealthCheck(_ context.Context) (driver.HealthReport, error) {
	return driver.HealthReport{Status: "healthy"}, nil
}
func (d *usageSizedDriver) ListNodes(_ context.Context) ([]driver.Node, error) { return nil, nil }
func (d *usageSizedDriver) GetLayout(_ context.Context) (driver.Layout, error) {
	return driver.Layout{}, nil
}
func (d *usageSizedDriver) StageLayout(_ context.Context, _ driver.LayoutChange) (driver.LayoutDiff, error) {
	return driver.LayoutDiff{}, nil
}
func (d *usageSizedDriver) ApplyLayout(_ context.Context) error  { return nil }
func (d *usageSizedDriver) RevertLayout(_ context.Context) error { return nil }

func (d *usageSizedDriver) ListBuckets(_ context.Context) ([]driver.Bucket, error) {
	out := make([]driver.Bucket, 0, len(d.sizes))
	for i, sz := range d.sizes {
		out = append(out, driver.Bucket{
			ID:      d.connID + "-b" + string(rune('0'+i)),
			Aliases: []string{d.connID + "-alias-" + string(rune('0'+i))},
			Bytes:   sz,
			Objects: sz / 100, // arbitrary deterministic relation
		})
	}
	return out, nil
}

func (d *usageSizedDriver) GetBucket(_ context.Context, id string) (driver.Bucket, error) {
	return driver.Bucket{ID: id}, nil
}
func (d *usageSizedDriver) CreateBucket(_ context.Context, _ driver.BucketSpec) (driver.Bucket, error) {
	return driver.Bucket{}, nil
}
func (d *usageSizedDriver) UpdateBucket(_ context.Context, _ string, _ driver.BucketUpdate) (driver.Bucket, error) {
	return driver.Bucket{}, nil
}
func (d *usageSizedDriver) DeleteBucket(_ context.Context, _ string) error { return nil }

func (d *usageSizedDriver) ListKeys(_ context.Context) ([]driver.Key, error) {
	// 2 keys per cluster so the per-cluster keys column has a non-trivial value.
	return []driver.Key{{ID: d.connID + "-k1"}, {ID: d.connID + "-k2"}}, nil
}

func (d *usageSizedDriver) GetKey(_ context.Context, _ string) (driver.Key, error) {
	return driver.Key{}, nil
}
func (d *usageSizedDriver) CreateKey(_ context.Context, _ driver.KeySpec) (driver.Key, error) {
	return driver.Key{}, nil
}
func (d *usageSizedDriver) UpdateKeyPermissions(_ context.Context, _ string, _ []driver.BucketPermission) error {
	return nil
}
func (d *usageSizedDriver) DeleteKey(_ context.Context, _ string) error { return nil }

func (d *usageSizedDriver) ListObjects(_ context.Context, _, _, _, _ string, _ int) (driver.ObjectPage, error) {
	return driver.ObjectPage{}, nil
}
func (d *usageSizedDriver) StatObject(_ context.Context, _, _ string) (driver.ObjectInfo, error) {
	return driver.ObjectInfo{}, nil
}
func (d *usageSizedDriver) PresignGet(_ context.Context, _, _ string, _ time.Duration) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, nil
}
func (d *usageSizedDriver) PresignPut(_ context.Context, _, _ string, _ time.Duration, _ string) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, nil
}
func (d *usageSizedDriver) DeleteObject(_ context.Context, _, _ string) error { return nil }
func (d *usageSizedDriver) CreateMultipart(_ context.Context, _, _, _ string) (driver.MultipartUpload, error) {
	return driver.MultipartUpload{}, nil
}
func (d *usageSizedDriver) PresignUploadPart(_ context.Context, _ driver.MultipartUpload, _ int) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, nil
}
func (d *usageSizedDriver) CompleteMultipart(_ context.Context, _ driver.MultipartUpload, _ []driver.CompletedPart) error {
	return nil
}
func (d *usageSizedDriver) AbortMultipart(_ context.Context, _ driver.MultipartUpload) error {
	return nil
}

func (d *usageSizedDriver) StreamObject(_ context.Context, _, _, _ string) (driver.StreamResult, error) {
	return driver.StreamResult{}, nil
}
func (d *usageSizedDriver) PutObjectStream(_ context.Context, _, _ string, _ io.Reader, _ string, _ int64) (driver.PutResult, error) {
	return driver.PutResult{}, nil
}
func (d *usageSizedDriver) ServerSideCopy(_ context.Context, _, _, _, _ string) error {
	return nil
}
func (d *usageSizedDriver) LifecycleSupport() driver.LifecycleCapabilities {
	return driver.LifecycleCapabilities{Supported: false}
}
func (d *usageSizedDriver) GetLifecycle(_ context.Context, _ string) ([]driver.LifecycleRule, error) {
	return nil, nil
}
func (d *usageSizedDriver) PutLifecycle(_ context.Context, _ string, _ []driver.LifecycleRule) error {
	return nil
}

const usageSizedDriverName = "stub-usage-sized"

var usageSizedRegisterOnce sync.Once

func registerUsageSizedDriver(t *testing.T) {
	t.Helper()
	usageSizedRegisterOnce.Do(func() {
		driver.Register(usageSizedDriverName, func(cfg driver.Config) (driver.Driver, error) {
			if cfg["force_build_error"] == "1" {
				return nil, errors.New("forced usage build error")
			}
			// Sizes are encoded into config as three comma-separated
			// numbers. Keep it simple — tests pass small, distinct
			// integers and read them back in ordering assertions.
			sizes := []int64{}
			if cfg["s0"] != "" {
				sizes = append(sizes, parseInt64OrZero(cfg["s0"]))
			}
			if cfg["s1"] != "" {
				sizes = append(sizes, parseInt64OrZero(cfg["s1"]))
			}
			if cfg["s2"] != "" {
				sizes = append(sizes, parseInt64OrZero(cfg["s2"]))
			}
			return &usageSizedDriver{connID: cfg["conn_id"], sizes: sizes}, nil
		})
		store.SupportedDrivers[usageSizedDriverName] = true
	})
}

func parseInt64OrZero(s string) int64 {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

// newUsageTestEnv builds a Server with: a real store at a tmp dir,
// a real policy enforcer, the sized fan-out driver registered, and
// (optionally) a host_admin assignment for the calling admin token.
// Returns the server plus a cleanup func.
func newUsageTestEnv(t *testing.T, conns []store.Connection, grantHostAdmin bool) (*Server, func()) {
	t.Helper()
	registerUsageSizedDriver(t)

	tmp, err := os.MkdirTemp("", "v090k-usage-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }

	cfg := newTestConfig()
	cfg.DataDir = tmp

	st, err := store.Open(tmp, 90*24*time.Hour)
	if err != nil {
		cleanup()
		t.Fatalf("store.Open: %v", err)
	}

	enf, err := policy.Open(filepath.Join(tmp, "policy"))
	if err != nil {
		cleanup()
		t.Fatalf("policy.Open: %v", err)
	}

	connsStore := &testMockConnectionStore{conns: conns}
	reg := driver.NewRegistry(connsStore)
	srv := New(cfg, st, connsStore, nil, reg)
	srv.SetPolicy(enf)

	if grantHostAdmin {
		if err := enf.AssignRole(policy.RoleAssignment{
			UserID: "admin", RoleID: "host_admin", Scope: "host:*",
		}); err != nil {
			cleanup()
			t.Fatalf("AssignRole: %v", err)
		}
	}

	return srv, cleanup
}

func usageReq(t *testing.T) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage/overview", nil)
	req.AddCookie(adminCookie())
	return req
}

func TestUsageOverview_HappyPath(t *testing.T) {
	// Two clusters, three buckets each, distinct sizes. Top buckets
	// by bytes should be the biggest from each cluster, ordered.
	conns := []store.Connection{
		{
			ID: "cb", Label: "betacluster", Driver: usageSizedDriverName,
			Config: map[string]string{
				"conn_id": "cb", "s0": "100", "s1": "500", "s2": "50",
			},
			Owner: "org",
		},
		{
			ID: "ca", Label: "alphacluster", Driver: usageSizedDriverName,
			Config: map[string]string{
				"conn_id": "ca", "s0": "2000", "s1": "10", "s2": "300",
			},
			Owner: "org",
		},
	}
	srv, cleanup := newUsageTestEnv(t, conns, true)
	defer cleanup()

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, usageReq(t))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp UsageOverviewResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Totals.
	if resp.Totals.Clusters != 2 {
		t.Errorf("Totals.Clusters=%d, want 2", resp.Totals.Clusters)
	}
	if resp.Totals.Buckets != 6 {
		t.Errorf("Totals.Buckets=%d, want 6", resp.Totals.Buckets)
	}
	if resp.Totals.Keys != 4 {
		t.Errorf("Totals.Keys=%d, want 4", resp.Totals.Keys)
	}
	wantBytes := int64(100 + 500 + 50 + 2000 + 10 + 300)
	if resp.Totals.Bytes != wantBytes {
		t.Errorf("Totals.Bytes=%d, want %d", resp.Totals.Bytes, wantBytes)
	}
	wantObjs := wantBytes / 100
	if resp.Totals.Objects != wantObjs {
		t.Errorf("Totals.Objects=%d, want %d", resp.Totals.Objects, wantObjs)
	}
	if resp.Totals.Grants != 0 {
		t.Errorf("Totals.Grants=%d, want 0 (no grants seeded)", resp.Totals.Grants)
	}

	// Per-cluster sorted by label alphabetically.
	if len(resp.PerCluster) != 2 {
		t.Fatalf("PerCluster len=%d, want 2", len(resp.PerCluster))
	}
	if resp.PerCluster[0].Label != "alphacluster" {
		t.Errorf("PerCluster[0].Label=%q, want alphacluster (alphabetical)", resp.PerCluster[0].Label)
	}
	if !resp.PerCluster[0].Healthy {
		t.Errorf("PerCluster[0].Healthy=false, want true")
	}
	if resp.PerCluster[0].Bytes != 2310 {
		t.Errorf("PerCluster[0].Bytes=%d, want 2310", resp.PerCluster[0].Bytes)
	}

	// Top buckets by bytes — largest first; biggest is ca's 2000-byte one.
	if len(resp.TopBucketsByBytes) != 6 {
		t.Errorf("TopBucketsByBytes len=%d, want 6 (six total buckets)", len(resp.TopBucketsByBytes))
	}
	if len(resp.TopBucketsByBytes) > 0 && resp.TopBucketsByBytes[0].Bytes != 2000 {
		t.Errorf("TopBucketsByBytes[0].Bytes=%d, want 2000", resp.TopBucketsByBytes[0].Bytes)
	}
	// Descending order across the slice.
	for i := 1; i < len(resp.TopBucketsByBytes); i++ {
		if resp.TopBucketsByBytes[i-1].Bytes < resp.TopBucketsByBytes[i].Bytes {
			t.Errorf("TopBucketsByBytes not sorted desc at index %d", i)
		}
	}
	// Same for objects.
	for i := 1; i < len(resp.TopBucketsByObjects); i++ {
		if resp.TopBucketsByObjects[i-1].Objects < resp.TopBucketsByObjects[i].Objects {
			t.Errorf("TopBucketsByObjects not sorted desc at index %d", i)
		}
	}
}

func TestUsageOverview_EmptyState(t *testing.T) {
	srv, cleanup := newUsageTestEnv(t, []store.Connection{}, true)
	defer cleanup()

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, usageReq(t))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	// Capture the raw body BEFORE decoding so the contains() checks
	// below have something to read (json.Decoder drains the buffer).
	bodyBytes := rr.Body.Bytes()
	body := string(bodyBytes)
	var resp UsageOverviewResponse
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Totals.Clusters != 0 || resp.Totals.Buckets != 0 || resp.Totals.Bytes != 0 {
		t.Errorf("expected zeroed totals, got %+v", resp.Totals)
	}
	// Slices must be empty arrays, not nil — the wire-shape promise to
	// the frontend is that .map() always works.
	for _, marker := range []string{`"perCluster":[]`, `"topBucketsByBytes":[]`, `"topBucketsByObjects":[]`} {
		if !contains(body, marker) {
			t.Errorf("body missing %q; got %s", marker, body)
		}
	}
}

func TestUsageOverview_PartialFailureIsolation(t *testing.T) {
	// Two clusters: one healthy + one with a forced build error. The
	// healthy one's data must still surface; the broken one shows up as
	// a perCluster row with healthy=false.
	conns := []store.Connection{
		{
			ID: "good", Label: "good", Driver: usageSizedDriverName,
			Config: map[string]string{
				"conn_id": "good", "s0": "100", "s1": "200",
			},
			Owner: "org",
		},
		{
			ID: "bad", Label: "bad", Driver: usageSizedDriverName,
			Config: map[string]string{
				"conn_id": "bad", "force_build_error": "1",
			},
			Owner: "org",
		},
	}
	srv, cleanup := newUsageTestEnv(t, conns, true)
	defer cleanup()

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, usageReq(t))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp UsageOverviewResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Healthy cluster's data survived.
	if resp.Totals.Buckets != 2 {
		t.Errorf("Totals.Buckets=%d, want 2 (only healthy cluster counted)", resp.Totals.Buckets)
	}
	if resp.Totals.Bytes != 300 {
		t.Errorf("Totals.Bytes=%d, want 300", resp.Totals.Bytes)
	}
	// Both clusters appear in perCluster; the bad one has healthy=false.
	if len(resp.PerCluster) != 2 {
		t.Fatalf("PerCluster len=%d, want 2 (both clusters listed even on partial failure)", len(resp.PerCluster))
	}
	var badRow *UsagePerCluster
	for i := range resp.PerCluster {
		if resp.PerCluster[i].ID == "bad" {
			badRow = &resp.PerCluster[i]
		}
	}
	if badRow == nil {
		t.Fatalf("bad cluster missing from PerCluster")
	}
	if badRow.Healthy {
		t.Errorf("badRow.Healthy=true, want false")
	}
	if badRow.Error == "" {
		t.Errorf("badRow.Error is empty; expected non-empty error description")
	}
}

func TestUsageOverview_MethodNotAllowed(t *testing.T) {
	srv, cleanup := newUsageTestEnv(t, []store.Connection{}, true)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/usage/overview", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	// chi returns 405 for a registered path matching a different method.
	if rr.Code != http.StatusMethodNotAllowed && rr.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 405 or 404", rr.Code)
	}
}

func TestUsageOverview_CapabilityGate(t *testing.T) {
	// grantHostAdmin=false — the calling admin has no host:manage_users
	// assignment so the per-handler gate returns 403.
	srv, cleanup := newUsageTestEnv(t, []store.Connection{}, false)
	defer cleanup()

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, usageReq(t))

	if rr.Code != http.StatusForbidden {
		t.Errorf("status=%d, want 403 (no host:manage_users)", rr.Code)
	}
}

// contains is a tiny helper to keep the empty-state body assertion
// readable without dragging strings.Contains into the import block
// (already implicit via other tests, but the test file should not
// reach beyond its own scope).
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
