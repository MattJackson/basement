package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/driver"
)

// TestObjectLock_GetUnsupportedSurfaces200WithSupportedFalse confirms
// the read endpoint returns 200 + supported=false on a driver that
// doesn't implement Object Lock. The FE uses this to decide whether
// to render the settings card without seeing a 501.
func TestObjectLock_GetUnsupportedSurfaces200WithSupportedFalse(t *testing.T) {
	mock := newRegionMockDriver()
	// objectLockSupportFunc default nil → false.

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodGet,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/object-lock", nil))
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get object-lock: expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	var resp objectLockConfigResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Supported {
		t.Errorf("expected supported=false, got true")
	}
	if resp.Enabled {
		t.Errorf("expected enabled=false on unsupported driver")
	}
}

// TestObjectLock_GetSupportedReturnsConfig exercises the supported
// branch with a default retention set on the bucket.
func TestObjectLock_GetSupportedReturnsConfig(t *testing.T) {
	mock := newRegionMockDriver()
	mock.objectLockSupportFunc = func() bool { return true }
	future := time.Now().Add(30 * 24 * time.Hour).UTC()
	mock.getObjectLockConfigFunc = func(_ context.Context, _ string) (*driver.ObjectLockConfig, error) {
		return &driver.ObjectLockConfig{
			Enabled: true,
			DefaultRetention: &driver.ObjectLockRetention{
				Mode:            driver.ObjectLockGovernance,
				RetainUntilDate: future,
			},
		}, nil
	}

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodGet,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/object-lock", nil))
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp objectLockConfigResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Supported || !resp.Enabled {
		t.Errorf("supported=%v enabled=%v want true/true", resp.Supported, resp.Enabled)
	}
	if resp.DefaultRetention == nil || resp.DefaultRetention.Mode != driver.ObjectLockGovernance {
		t.Errorf("DefaultRetention=%+v", resp.DefaultRetention)
	}
}

// TestObjectLock_PutEnabledAuditedAndCallsDriver exercises the
// happy-path write — capability gate, driver call, audit emission.
func TestObjectLock_PutEnabledAuditedAndCallsDriver(t *testing.T) {
	mock := newRegionMockDriver()
	mock.objectLockSupportFunc = func() bool { return true }
	called := false
	var got driver.ObjectLockConfig
	mock.putObjectLockConfigFunc = func(_ context.Context, bucket string, cfg driver.ObjectLockConfig) error {
		if bucket != "my-bucket" {
			t.Errorf("bucket=%q want my-bucket", bucket)
		}
		called = true
		got = cfg
		return nil
	}

	srv, auditLog, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	future := time.Now().Add(48 * time.Hour).UTC()
	body, _ := json.Marshal(objectLockConfigRequest{
		Enabled: true,
		DefaultRetention: &driver.ObjectLockRetention{
			Mode:            driver.ObjectLockCompliance,
			RetainUntilDate: future,
		},
	})
	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodPut,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/object-lock",
		bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("put object-lock: expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if !called {
		t.Error("PutObjectLockConfig was not called")
	}
	if !got.Enabled || got.DefaultRetention == nil ||
		got.DefaultRetention.Mode != driver.ObjectLockCompliance {
		t.Errorf("config passed to driver=%+v", got)
	}

	// Both audit events should fire — enabled + default_retention_set.
	wantActions := map[string]bool{
		"bucket:object_lock_enabled":               false,
		"bucket:object_lock_default_retention_set": false,
	}
	for _, e := range auditLog.snapshot() {
		if _, ok := wantActions[e.Action]; ok && e.Result == audit.ResultSuccess {
			wantActions[e.Action] = true
		}
	}
	for act, seen := range wantActions {
		if !seen {
			t.Errorf("expected audit action %q (success) — not found", act)
		}
	}
}

// TestObjectLock_PutUnsupportedReturns501 confirms the capability
// gate fires for the write path on Garage-style drivers.
func TestObjectLock_PutUnsupportedReturns501(t *testing.T) {
	mock := newRegionMockDriver()
	// Default support=false.

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	body, _ := json.Marshal(objectLockConfigRequest{Enabled: true})
	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodPut,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/object-lock",
		bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	var er ErrorResponse
	_ = json.NewDecoder(rr.Body).Decode(&er)
	if er.Error.Code != "NOT_SUPPORTED" {
		t.Errorf("code=%q want NOT_SUPPORTED", er.Error.Code)
	}
}

// TestObjectLock_PutRejectsDisable confirms enabled=false fails with 400.
func TestObjectLock_PutRejectsDisable(t *testing.T) {
	mock := newRegionMockDriver()
	mock.objectLockSupportFunc = func() bool { return true }

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	body, _ := json.Marshal(objectLockConfigRequest{Enabled: false})
	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodPut,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/object-lock",
		bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// TestObjectLock_PutRejectsInvalidRetentionShape covers the body
// validation branches that fire BEFORE driver dispatch — bad mode,
// missing date, past date.
func TestObjectLock_PutRejectsInvalidRetentionShape(t *testing.T) {
	mock := newRegionMockDriver()
	mock.objectLockSupportFunc = func() bool { return true }

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	cases := []struct {
		name string
		req  objectLockConfigRequest
	}{
		{"bad mode", objectLockConfigRequest{
			Enabled: true,
			DefaultRetention: &driver.ObjectLockRetention{
				Mode:            "BOGUS",
				RetainUntilDate: time.Now().Add(time.Hour),
			},
		}},
		{"missing date", objectLockConfigRequest{
			Enabled: true,
			DefaultRetention: &driver.ObjectLockRetention{
				Mode: driver.ObjectLockGovernance,
			},
		}},
		{"past date", objectLockConfigRequest{
			Enabled: true,
			DefaultRetention: &driver.ObjectLockRetention{
				Mode:            driver.ObjectLockGovernance,
				RetainUntilDate: time.Now().Add(-time.Hour),
			},
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.req)
			rr := httptest.NewRecorder()
			req := regionUserCookieReq(httptest.NewRequest(http.MethodPut,
				"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/object-lock",
				bytes.NewReader(body)))
			req.Header.Set("Content-Type", "application/json")
			srv.router.ServeHTTP(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d (body=%s)", rr.Code, rr.Body.String())
			}
		})
	}
}

// TestObjectLock_GetRetentionRequiresVersionID confirms the
// per-object surface won't accept a call without versionId.
func TestObjectLock_GetRetentionRequiresVersionID(t *testing.T) {
	mock := newRegionMockDriver()
	mock.objectLockSupportFunc = func() bool { return true }

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodGet,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/o/k1/retention", nil))
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 (missing versionId), got %d", rr.Code)
	}
}

// TestObjectLock_PutRetentionForwardsBypassFlag confirms the
// bypassGovernance query param is plumbed through to the driver.
func TestObjectLock_PutRetentionForwardsBypassFlag(t *testing.T) {
	mock := newRegionMockDriver()
	mock.objectLockSupportFunc = func() bool { return true }
	var gotBypass bool
	mock.putObjectRetentionFunc = func(_ context.Context, _, _, _ string, _ driver.ObjectLockRetention, bypass bool) error {
		gotBypass = bypass
		return nil
	}

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	body, _ := json.Marshal(driver.ObjectLockRetention{
		Mode:            driver.ObjectLockGovernance,
		RetainUntilDate: time.Now().Add(48 * time.Hour),
	})
	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodPut,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/o/k1/retention?versionId=v1&bypassGovernance=true",
		bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if !gotBypass {
		t.Error("driver did not receive bypassGovernance=true")
	}
}

// TestObjectLock_PutRetention_AuditAction_ExtendedVsReduced confirms
// the audit action varies based on whether the new retention date is
// after or before the existing one.
func TestObjectLock_PutRetention_AuditAction_ExtendedVsReduced(t *testing.T) {
	cases := []struct {
		name       string
		priorDelta time.Duration
		newDelta   time.Duration
		wantAction string
	}{
		{"extend", 24 * time.Hour, 72 * time.Hour, "object:retention_extended"},
		{"reduce", 72 * time.Hour, 24 * time.Hour, "object:retention_reduced"},
		{"set when no prior", 0, 24 * time.Hour, "object:retention_set"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			mock := newRegionMockDriver()
			mock.objectLockSupportFunc = func() bool { return true }
			if tc.priorDelta > 0 {
				priorDate := time.Now().Add(tc.priorDelta).UTC()
				mock.getObjectRetentionFunc = func(_ context.Context, _, _, _ string) (*driver.ObjectLockRetention, error) {
					return &driver.ObjectLockRetention{
						Mode:            driver.ObjectLockGovernance,
						RetainUntilDate: priorDate,
					}, nil
				}
			}
			mock.putObjectRetentionFunc = func(_ context.Context, _, _, _ string, _ driver.ObjectLockRetention, _ bool) error {
				return nil
			}

			srv, auditLog, cleanup := newRegionsTestEnv(t, mock)
			defer cleanup()

			regionID := createTestRegion(t, srv)

			body, _ := json.Marshal(driver.ObjectLockRetention{
				Mode:            driver.ObjectLockGovernance,
				RetainUntilDate: time.Now().Add(tc.newDelta).UTC(),
			})
			rr := httptest.NewRecorder()
			req := regionUserCookieReq(httptest.NewRequest(http.MethodPut,
				"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/o/k1/retention?versionId=v1",
				bytes.NewReader(body)))
			req.Header.Set("Content-Type", "application/json")
			srv.router.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rr.Code)
			}

			found := false
			for _, e := range auditLog.snapshot() {
				if e.Action == tc.wantAction && e.Result == audit.ResultSuccess {
					found = true
					if !strings.Contains(e.Detail, "versionId=v1") {
						t.Errorf("Detail missing versionId=v1: %q", e.Detail)
					}
					break
				}
			}
			if !found {
				t.Errorf("expected audit %q (success); events=%+v", tc.wantAction, auditLog.snapshot())
			}
		})
	}
}

// TestObjectLock_LegalHold_RoundTrip covers GET then PUT.
func TestObjectLock_LegalHold_RoundTrip(t *testing.T) {
	mock := newRegionMockDriver()
	mock.objectLockSupportFunc = func() bool { return true }
	current := false
	mock.getObjectLegalHoldFunc = func(_ context.Context, _, _, _ string) (bool, error) {
		return current, nil
	}
	mock.putObjectLegalHoldFunc = func(_ context.Context, _, _, _ string, on bool) error {
		current = on
		return nil
	}

	srv, auditLog, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	// GET initial state.
	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodGet,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/o/k1/legal-hold?versionId=v1", nil))
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get legal-hold: expected 200, got %d", rr.Code)
	}
	var got objectLegalHoldResponse
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.On {
		t.Errorf("expected on=false initially")
	}

	// PUT on.
	body, _ := json.Marshal(objectLegalHoldResponse{On: true})
	rr = httptest.NewRecorder()
	req = regionUserCookieReq(httptest.NewRequest(http.MethodPut,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/o/k1/legal-hold?versionId=v1",
		bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("put legal-hold on: expected 200, got %d", rr.Code)
	}

	// PUT off — should audit as released.
	body, _ = json.Marshal(objectLegalHoldResponse{On: false})
	rr = httptest.NewRecorder()
	req = regionUserCookieReq(httptest.NewRequest(http.MethodPut,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/o/k1/legal-hold?versionId=v1",
		bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("put legal-hold off: expected 200, got %d", rr.Code)
	}

	wantActions := map[string]bool{
		"object:legal_hold_set":      false,
		"object:legal_hold_released": false,
	}
	for _, e := range auditLog.snapshot() {
		if _, ok := wantActions[e.Action]; ok && e.Result == audit.ResultSuccess {
			wantActions[e.Action] = true
		}
	}
	for act, seen := range wantActions {
		if !seen {
			t.Errorf("expected audit %q (success) — not found", act)
		}
	}
}

// TestObjectLock_LegalHold_UnsupportedReturns501 — capability gate.
func TestObjectLock_LegalHold_UnsupportedReturns501(t *testing.T) {
	mock := newRegionMockDriver()
	// support=false default.

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodGet,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/o/k1/legal-hold?versionId=v1", nil))
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", rr.Code)
	}
}
