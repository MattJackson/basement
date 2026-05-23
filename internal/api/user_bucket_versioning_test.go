package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/driver"
)

// createTestRegion drives the create endpoint to register a region
// for the user, then returns the resulting region ID. Shared helper
// used by the versioning tests so each test starts from the same
// canonical "user has one region" state.
func createTestRegion(t *testing.T, srv *Server) string {
	t.Helper()
	body := map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.basement.pq.io",
		"accessKeyId": "GK_user_key",
		"secretKey":   "user-secret-do-not-log",
		"region":      "garage",
	}
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", body)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create region: expected 201, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	var created userRegionResponse
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("decode created region: %v", err)
	}
	return created.ID
}

// TestVersioning_GetUnsupportedSurfaces200WithSupportedFalse
// confirms the read endpoint returns 200 + supported=false on a
// driver that doesn't implement versioning. The FE uses this to
// decide whether to render the toggle without seeing a 501.
func TestVersioning_GetUnsupportedSurfaces200WithSupportedFalse(t *testing.T) {
	mock := newRegionMockDriver()
	// versioningSupportFunc defaults nil → VersioningSupport()=false.

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodGet,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/versioning", nil))
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get versioning: expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	var resp versioningStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Supported {
		t.Errorf("expected supported=false, got true")
	}
	if resp.Status != driver.VersioningDisabled {
		t.Errorf("expected status=disabled, got %q", resp.Status)
	}
}

// TestVersioning_GetSupportedSurfacesStatus exercises the success
// path against a driver that advertises support.
func TestVersioning_GetSupportedSurfacesStatus(t *testing.T) {
	mock := newRegionMockDriver()
	mock.versioningSupportFunc = func() bool { return true }
	mock.getVersioningStatusFunc = func(_ context.Context, _ string) (driver.VersioningStatus, error) {
		return driver.VersioningEnabled, nil
	}

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodGet,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/versioning", nil))
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get versioning: expected 200, got %d", rr.Code)
	}
	var resp versioningStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Supported {
		t.Errorf("expected supported=true")
	}
	if resp.Status != driver.VersioningEnabled {
		t.Errorf("status=%q, want enabled", resp.Status)
	}
}

// TestVersioning_PutEnabledAuditedAndCallsDriver exercises the
// write path's full chain: capability gate, driver call, audit
// emission. Asserts the right audit event fired with success.
func TestVersioning_PutEnabledAuditedAndCallsDriver(t *testing.T) {
	mock := newRegionMockDriver()
	mock.versioningSupportFunc = func() bool { return true }
	called := false
	mock.enableVersioningFunc = func(_ context.Context, bucket string) error {
		if bucket != "my-bucket" {
			t.Errorf("driver called with bucket=%q, want my-bucket", bucket)
		}
		called = true
		return nil
	}

	srv, auditLog, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	body, _ := json.Marshal(versioningStatusRequest{Status: "enabled"})
	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodPut,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/versioning",
		bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("put versioning: expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if !called {
		t.Error("EnableVersioning was not called on the driver")
	}

	found := false
	for _, e := range auditLog.snapshot() {
		if e.Action == "bucket:versioning_enabled" && e.Result == audit.ResultSuccess {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected bucket:versioning_enabled success audit event")
	}
}

// TestVersioning_PutUnsupportedReturns501 confirms the capability
// gate fires for the write path. Garage drivers + any future
// non-versioning backend should see this 501 with a typed code.
func TestVersioning_PutUnsupportedReturns501(t *testing.T) {
	mock := newRegionMockDriver()
	// Default versioningSupportFunc = nil → false.

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	body, _ := json.Marshal(versioningStatusRequest{Status: "enabled"})
	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodPut,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/versioning",
		bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("put versioning: expected 501, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	var er ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&er); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if er.Error.Code != "NOT_SUPPORTED" {
		t.Errorf("error code=%q, want NOT_SUPPORTED", er.Error.Code)
	}
}

// TestVersioning_PutInvalidStatusReturns400 confirms only the two
// settable states (enabled, suspended) are accepted.
func TestVersioning_PutInvalidStatusReturns400(t *testing.T) {
	mock := newRegionMockDriver()
	mock.versioningSupportFunc = func() bool { return true }

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	for _, badStatus := range []string{"", "disabled", "Enabled", "true"} {
		body, _ := json.Marshal(versioningStatusRequest{Status: badStatus})
		rr := httptest.NewRecorder()
		req := regionUserCookieReq(httptest.NewRequest(http.MethodPut,
			"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/versioning",
			bytes.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		srv.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("status=%q: expected 400, got %d", badStatus, rr.Code)
		}
	}
}

// TestVersioning_ListObjectVersionsPaginates exercises the success
// path: driver returns versions + a next marker, the handler emits
// them on the wire shape with the marker preserved.
func TestVersioning_ListObjectVersionsPaginates(t *testing.T) {
	mock := newRegionMockDriver()
	mock.versioningSupportFunc = func() bool { return true }
	mock.listObjectVersionsFunc = func(_ context.Context, bucket, prefix, marker string, _ int) ([]driver.ObjectVersion, string, error) {
		if bucket != "my-bucket" || prefix != "k1" {
			t.Errorf("driver call: bucket=%q prefix=%q", bucket, prefix)
		}
		return []driver.ObjectVersion{
			// Adjacent-prefix noise that the handler MUST filter out.
			{Key: "k1-other", VersionID: "noise", IsLatest: true},
			{Key: "k1", VersionID: "v2", IsLatest: true, Size: 200},
			{Key: "k1", VersionID: "v1", Size: 100},
		}, "next-marker-blob", nil
	}

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodGet,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/o/k1/versions", nil))
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list versions: expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	var resp objectVersionsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.NextVersionIDMarker != "next-marker-blob" {
		t.Errorf("nextMarker=%q, want next-marker-blob", resp.NextVersionIDMarker)
	}
	if len(resp.Versions) != 2 {
		t.Fatalf("versions=%d, want 2 (k1-other should be filtered)", len(resp.Versions))
	}
	if resp.Versions[0].VersionID != "v2" || resp.Versions[1].VersionID != "v1" {
		t.Errorf("ordering wrong: %+v", resp.Versions)
	}
}

// TestVersioning_ListObjectVersionsUnsupported501 confirms the
// capability gate fires for list-versions too.
func TestVersioning_ListObjectVersionsUnsupported501(t *testing.T) {
	mock := newRegionMockDriver()

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodGet,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/o/k/versions", nil))
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", rr.Code)
	}
}

// TestVersioning_GetObjectVersionStreamsBody confirms the handler
// forwards Content-Type + body bytes back to the caller.
func TestVersioning_GetObjectVersionStreamsBody(t *testing.T) {
	mock := newRegionMockDriver()
	mock.versioningSupportFunc = func() bool { return true }
	mock.getObjectVersionFunc = func(_ context.Context, _, _, version string) (driver.StreamResult, error) {
		if version != "v1" {
			t.Errorf("versionId=%q, want v1", version)
		}
		return driver.StreamResult{
			Body:        io.NopCloser(bytes.NewReader([]byte("hello v1"))),
			ContentType: "text/plain",
			ETag:        `"abc"`,
		}, nil
	}

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodGet,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/o/k1/versions/v1", nil))
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("get version: expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "text/plain" {
		t.Errorf("Content-Type=%q", got)
	}
	if got := rr.Body.String(); got != "hello v1" {
		t.Errorf("body=%q", got)
	}
}

// TestVersioning_DeleteObjectVersionAuditedAndForwarded asserts the
// destructive path: driver receives the right key+versionId pair,
// audit emits object:version_delete with the versionId in Detail.
func TestVersioning_DeleteObjectVersionAuditedAndForwarded(t *testing.T) {
	mock := newRegionMockDriver()
	mock.versioningSupportFunc = func() bool { return true }
	called := false
	mock.deleteObjectVersionFunc = func(_ context.Context, bucket, key, versionID string) error {
		if bucket != "my-bucket" || key != "k1" || versionID != "v1" {
			t.Errorf("driver: bucket=%q key=%q version=%q", bucket, key, versionID)
		}
		called = true
		return nil
	}

	srv, auditLog, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodDelete,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/o/k1/versions/v1", nil))
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete version: expected 204, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if !called {
		t.Error("DeleteObjectVersion was not called on the driver")
	}

	foundDelete := false
	for _, e := range auditLog.snapshot() {
		if e.Action == "object:version_delete" && e.Result == audit.ResultSuccess {
			foundDelete = true
			if !bytes.Contains([]byte(e.Detail), []byte("versionId=v1")) {
				t.Errorf("audit Detail missing versionId=v1: %q", e.Detail)
			}
			break
		}
	}
	if !foundDelete {
		t.Error("expected object:version_delete success audit event")
	}
}

// TestVersioning_DeleteObjectVersionUnsupported501 — destructive
// path's capability gate.
func TestVersioning_DeleteObjectVersionUnsupported501(t *testing.T) {
	mock := newRegionMockDriver()

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodDelete,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/o/k1/versions/v1", nil))
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", rr.Code)
	}
}
