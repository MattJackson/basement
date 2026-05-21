// Package api: tests for OBS.USAGE.SERIES /admin/usage/series
// (v1.0.0d).
//
// Coverage:
//
//  1. Happy path — seed the metrics recorder with snapshots, request
//     the series for one bucket, assert ordering + range envelope.
//  2. Capability gate — no host:manage_users assignment returns 403.
//  3. Missing-param — cid or bid omitted returns 400.
//  4. Range clamp — a from/to spanning a year is silently clamped to
//     90d (no 400; the chart still renders).
//  5. Empty state — recorder returns no rows; response wraps
//     snapshots:[] (NOT null) so the frontend can map unconditionally.
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/metrics"
	"github.com/mattjackson/basement/internal/store"
)

// newUsageSeriesTestEnv builds a Server with the metrics recorder
// wired to a FileRecorder rooted at a tmp dir, plus the standard
// policy + admin grant fixture.
func newUsageSeriesTestEnv(t *testing.T, grantHostAdmin bool) (*Server, *metrics.FileRecorder, func()) {
	t.Helper()

	tmp := t.TempDir()

	cfg := newTestConfig()
	cfg.DataDir = tmp

	st, err := store.Open(tmp, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := st.WireBucketGrants(testSecret); err != nil {
		t.Fatalf("WireBucketGrants: %v", err)
	}

	enf, err := policy.Open(filepath.Join(tmp, "policy"))
	if err != nil {
		t.Fatalf("policy.Open: %v", err)
	}

	connsStore := &testMockConnectionStore{}
	srv := New(cfg, st, connsStore, nil, nil)
	srv.SetPolicy(enf)

	rec := metrics.NewFileRecorder(tmp)
	srv.SetMetricsRecorder(rec)

	if grantHostAdmin {
		if err := enf.AssignRole(policy.RoleAssignment{
			UserID: "admin", RoleID: "host_admin", Scope: "host:*",
		}); err != nil {
			t.Fatalf("AssignRole: %v", err)
		}
	}

	cleanup := func() {
		_ = rec.Close()
	}

	return srv, rec, cleanup
}

func usageSeriesReq(t *testing.T, query string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage/series?"+query, nil)
	req.AddCookie(adminCookie())
	return req
}

func TestUsageSeries_HappyPath(t *testing.T) {
	srv, rec, cleanup := newUsageSeriesTestEnv(t, true)
	defer cleanup()

	// Seed three snapshots across the last hour for the target
	// bucket plus one snapshot for an OTHER bucket that must not
	// appear in the response.
	now := time.Now().UTC()
	for i, off := range []time.Duration{-3 * time.Hour, -2 * time.Hour, -1 * time.Hour} {
		if err := rec.Snapshot(metrics.Snapshot{
			Time:         now.Add(off),
			ConnectionID: "ca",
			BucketID:     "b1",
			BucketAlias:  "photos",
			Bytes:        int64((i + 1) * 1000),
			Objects:      int64((i + 1) * 10),
		}); err != nil {
			t.Fatalf("seed snapshot: %v", err)
		}
	}
	if err := rec.Snapshot(metrics.Snapshot{
		Time:         now.Add(-30 * time.Minute),
		ConnectionID: "ca",
		BucketID:     "OTHER",
		Bytes:        9999,
	}); err != nil {
		t.Fatalf("seed snapshot OTHER: %v", err)
	}

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, usageSeriesReq(t, "cid=ca&bid=b1"))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp usageSeriesResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Snapshots) != 3 {
		t.Errorf("expected 3 snapshots, got %d", len(resp.Snapshots))
	}
	if resp.BucketAlias != "photos" {
		t.Errorf("BucketAlias=%q, want photos", resp.BucketAlias)
	}
	if resp.Range != "7d" {
		t.Errorf("Range=%q, want 7d", resp.Range)
	}
	// First snap should be the oldest (Bytes=1000).
	if len(resp.Snapshots) > 0 && resp.Snapshots[0].Bytes != 1000 {
		t.Errorf("Snapshots[0].Bytes=%d, want 1000 (chart-friendly oldest-first)", resp.Snapshots[0].Bytes)
	}
}

func TestUsageSeries_CapabilityGate(t *testing.T) {
	srv, _, cleanup := newUsageSeriesTestEnv(t, false)
	defer cleanup()

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, usageSeriesReq(t, "cid=ca&bid=b1"))

	if rr.Code != http.StatusForbidden {
		t.Errorf("status=%d, want 403 (no host:manage_users)", rr.Code)
	}
}

func TestUsageSeries_MissingCid(t *testing.T) {
	srv, _, cleanup := newUsageSeriesTestEnv(t, true)
	defer cleanup()

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, usageSeriesReq(t, "bid=b1"))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 (missing cid)", rr.Code)
	}
}

func TestUsageSeries_MissingBid(t *testing.T) {
	srv, _, cleanup := newUsageSeriesTestEnv(t, true)
	defer cleanup()

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, usageSeriesReq(t, "cid=ca"))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 (missing bid)", rr.Code)
	}
}

func TestUsageSeries_RangeClamping(t *testing.T) {
	srv, rec, cleanup := newUsageSeriesTestEnv(t, true)
	defer cleanup()

	now := time.Now().UTC()
	// One snap inside the 90-day window, one well outside.
	if err := rec.Snapshot(metrics.Snapshot{
		Time:         now.Add(-30 * 24 * time.Hour),
		ConnectionID: "ca",
		BucketID:     "b1",
		Bytes:        100,
	}); err != nil {
		t.Fatalf("seed inside: %v", err)
	}
	if err := rec.Snapshot(metrics.Snapshot{
		Time:         now.Add(-365 * 24 * time.Hour),
		ConnectionID: "ca",
		BucketID:     "b1",
		Bytes:        99,
	}); err != nil {
		t.Fatalf("seed outside: %v", err)
	}

	// Ask for two years of data — must be clamped to 90d.
	from := now.Add(-2 * 365 * 24 * time.Hour).Format(time.RFC3339)
	to := now.Format(time.RFC3339)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, usageSeriesReq(t, "cid=ca&bid=b1&from="+from+"&to="+to))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp usageSeriesResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Range != "90d" {
		t.Errorf("Range=%q, want 90d (clamp)", resp.Range)
	}
	// Only the in-window snap should come back.
	if len(resp.Snapshots) != 1 {
		t.Errorf("expected 1 snapshot after clamp, got %d", len(resp.Snapshots))
	}
}

func TestUsageSeries_EmptyState(t *testing.T) {
	srv, _, cleanup := newUsageSeriesTestEnv(t, true)
	defer cleanup()

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, usageSeriesReq(t, "cid=ca&bid=b1"))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	// Capture raw body so the marker check has something to read.
	body := rr.Body.String()
	if !contains(body, `"snapshots":[]`) {
		t.Errorf("body missing snapshots:[]; got %s", body)
	}
}

func TestUsageSeries_MethodNotAllowed(t *testing.T) {
	srv, _, cleanup := newUsageSeriesTestEnv(t, true)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/usage/series?cid=ca&bid=b1", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed && rr.Code != http.StatusNotFound {
		t.Errorf("status=%d, want 405 or 404", rr.Code)
	}
}

func TestUsageSeries_InvalidFrom(t *testing.T) {
	srv, _, cleanup := newUsageSeriesTestEnv(t, true)
	defer cleanup()

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, usageSeriesReq(t, "cid=ca&bid=b1&from=not-a-time"))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 (invalid from)", rr.Code)
	}
}
