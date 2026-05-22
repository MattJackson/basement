// Package api: tests for the v1.3.0a.1 "backend rejected the key"
// error path. When a UserRegion is bound to an access key the backend
// has since revoked / rotated / never knew about, the region handlers
// must surface a typed 401 USER_KEY_REJECTED with the region context
// the FE needs to render an actionable "delete this key + add a fresh
// one" call-to-action — NOT the bare 500 INTERNAL the un-fixed code
// path produced.

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/driver"
)

// createRevokedKeyRegion stands up a region for the standard test user
// + returns its ID. Used by every test below so each test stays
// focused on the error-path assertion rather than fixture setup.
func createRevokedKeyRegion(t *testing.T, srv *Server) string {
	t.Helper()
	body := map[string]string{
		"alias":       "lsi",
		"endpoint":    "https://s3.basement.pq.io",
		"accessKeyId": "GK6f4403ea8f6168544d035f4d", // matches the live repro key
		"secretKey":   "secret-for-revoked-key-test",
	}
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", body)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("setup create: expected 201, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	var created userRegionResponse
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("decode created region: %v", err)
	}
	return created.ID
}

// assertUserKeyRejected verifies a response is the 401 USER_KEY_REJECTED
// shape with the region detail payload the FE relies on. Centralised so
// each handler-level test stays a one-liner.
func assertUserKeyRejected(t *testing.T, rr *httptest.ResponseRecorder, regionID string) {
	t.Helper()
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	var parsed struct {
		Error struct {
			Code    string                 `json:"code"`
			Message string                 `json:"message"`
			Details map[string]interface{} `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if parsed.Error.Code != "USER_KEY_REJECTED" {
		t.Errorf("expected code USER_KEY_REJECTED, got %q (body=%s)", parsed.Error.Code, rr.Body.String())
	}
	if parsed.Error.Message == "" {
		t.Errorf("expected non-empty message, got empty")
	}
	if !strings.Contains(parsed.Error.Message, "rejected") {
		t.Errorf("expected message to mention 'rejected', got %q", parsed.Error.Message)
	}
	if got, _ := parsed.Error.Details["regionId"].(string); got != regionID {
		t.Errorf("expected details.regionId=%q, got %q", regionID, got)
	}
	if got, _ := parsed.Error.Details["alias"].(string); got != "lsi" {
		t.Errorf("expected details.alias=lsi, got %q", got)
	}
	if got, _ := parsed.Error.Details["endpoint"].(string); got != "https://s3.basement.pq.io" {
		t.Errorf("expected details.endpoint, got %q", got)
	}
}

// revokedKeyErr is the wrapped driver.Error that mocks throw to
// simulate a backend rejecting the access key. Matches the shape the
// real aws_s3 / garage / minio drivers produce — Err=ErrInvalid plus
// the underlying "api error <Code>: …" text in Message.
func revokedKeyErr(op, awsCode string) error {
	return &driver.Error{
		Op:      op,
		Driver:  "aws-s3",
		Err:     driver.ErrInvalid,
		Message: "operation error S3: " + op + ", https response error StatusCode: 403, RequestID: abc, HostID: def, api error " + awsCode + ": The AWS Access Key Id you provided does not exist in our records.",
	}
}

// TestUserRegions_RevokedKey_ListBuckets verifies a revoked key bubbles
// up as 401 USER_KEY_REJECTED on the canonical reproduction path (the
// `/buckets` endpoint the FE hits first when navigating to a region).
func TestUserRegions_RevokedKey_ListBuckets(t *testing.T) {
	cases := []struct {
		name    string
		awsCode string
	}{
		{"InvalidAccessKeyId", "InvalidAccessKeyId"},
		{"SignatureDoesNotMatch", "SignatureDoesNotMatch"},
		{"AccessDenied", "AccessDenied"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := newRegionMockDriver()
			mock.listBucketsFunc = func(_ context.Context) ([]driver.Bucket, error) {
				return nil, revokedKeyErr("ListBuckets", tc.awsCode)
			}
			srv, _, cleanup := newRegionsTestEnv(t, mock)
			defer cleanup()

			regionID := createRevokedKeyRegion(t, srv)

			rr := httptest.NewRecorder()
			srv.router.ServeHTTP(rr, regionUserCookieReq(httptest.NewRequest(
				http.MethodGet, "/api/v1/user/regions/"+regionID+"/buckets", nil)))

			assertUserKeyRejected(t, rr, regionID)
		})
	}
}

// TestUserRegions_RevokedKey_ListObjects verifies the same conversion
// happens on the object-tier list endpoint — the FE drills into a
// bucket and the key is no longer valid; the user should see the
// actionable alert, not a generic 500.
func TestUserRegions_RevokedKey_ListObjects(t *testing.T) {
	mock := newRegionMockDriver()
	mock.listObjectsFunc = func(_ context.Context, _, _, _ string, _ int) (driver.ObjectPage, error) {
		return driver.ObjectPage{}, revokedKeyErr("ListObjectsV2", "InvalidAccessKeyId")
	}
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createRevokedKeyRegion(t, srv)

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(httptest.NewRequest(
		http.MethodGet, "/api/v1/user/regions/"+regionID+"/buckets/foo/objects", nil)))

	assertUserKeyRejected(t, rr, regionID)
}

// TestUserRegions_RevokedKey_PresignGet covers the read-presign path —
// even though the URL is meant to be ephemeral, a revoked key fails at
// signing time too on some SDK versions, so the same UX matters.
func TestUserRegions_RevokedKey_PresignGet(t *testing.T) {
	mock := newRegionMockDriver()
	mock.presignGetFunc = func(_ context.Context, _, _ string, _ time.Duration) (driver.PresignedURL, error) {
		return driver.PresignedURL{}, revokedKeyErr("GetObject", "AccessDenied")
	}
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createRevokedKeyRegion(t, srv)

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(httptest.NewRequest(
		http.MethodGet, "/api/v1/user/regions/"+regionID+"/buckets/foo/objects/bar.txt/presign-get", nil)))

	assertUserKeyRejected(t, rr, regionID)
}

// TestUserRegions_RevokedKey_DeleteObject — destructive op, same
// conversion: the operator sees "your key is bad" rather than a 500
// that looks like a server problem.
func TestUserRegions_RevokedKey_DeleteObject(t *testing.T) {
	mock := newRegionMockDriver()
	mock.deleteObjectFunc = func(_ context.Context, _, _ string) error {
		return revokedKeyErr("DeleteObject", "AccessDenied")
	}
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createRevokedKeyRegion(t, srv)

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(httptest.NewRequest(
		http.MethodDelete, "/api/v1/user/regions/"+regionID+"/buckets/foo/objects/bar.txt", nil)))

	assertUserKeyRejected(t, rr, regionID)
}

// TestUserRegions_RevokedKey_OtherErrorsPassThrough — non-auth errors
// (network timeout, NotFound, etc.) must NOT be misclassified as
// USER_KEY_REJECTED. They should surface via the existing
// writeDriverError path so the FE shows the right cause.
func TestUserRegions_RevokedKey_OtherErrorsPassThrough(t *testing.T) {
	mock := newRegionMockDriver()
	// driver.ErrNotFound wrapped — must remain a 404 from writeDriverError.
	mock.listBucketsFunc = func(_ context.Context) ([]driver.Bucket, error) {
		return nil, &driver.Error{
			Op:      "ListBuckets",
			Driver:  "aws-s3",
			Err:     driver.ErrNotFound,
			Message: "bucket not found",
		}
	}
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createRevokedKeyRegion(t, srv)

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(httptest.NewRequest(
		http.MethodGet, "/api/v1/user/regions/"+regionID+"/buckets", nil)))

	if rr.Code != http.StatusNotFound {
		t.Errorf("ErrNotFound: expected 404 (NOT 401), got %d (body=%s)", rr.Code, rr.Body.String())
	}

	// Plain (non-driver.Error) errors fall through to the generic 500
	// INTERNAL path — they're network / transport problems, not auth.
	mock2 := newRegionMockDriver()
	mock2.listBucketsFunc = func(_ context.Context) ([]driver.Bucket, error) {
		return nil, errors.New("connect: connection timed out")
	}
	srv2, _, cleanup2 := newRegionsTestEnv(t, mock2)
	defer cleanup2()

	regionID2 := createRevokedKeyRegion(t, srv2)

	rr2 := httptest.NewRecorder()
	srv2.router.ServeHTTP(rr2, regionUserCookieReq(httptest.NewRequest(
		http.MethodGet, "/api/v1/user/regions/"+regionID2+"/buckets", nil)))

	if rr2.Code != http.StatusInternalServerError {
		t.Errorf("plain error: expected 500, got %d (body=%s)", rr2.Code, rr2.Body.String())
	}
}

// TestIsUserKeyRejected_UnitMatrix locks the helper down at the unit
// level so future test churn at the handler layer doesn't accidentally
// drop coverage of an AWS code. Keeps the test cheap (no Server).
func TestIsUserKeyRejected_UnitMatrix(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("connect: timed out"), false},
		{"driver.Error wrapping ErrNotFound", &driver.Error{Err: driver.ErrNotFound, Message: "not found"}, false},
		{"driver.Error wrapping ErrPermissionDenied (no code in msg)", &driver.Error{Err: driver.ErrPermissionDenied, Message: "perm denied"}, false},
		{"InvalidAccessKeyId", revokedKeyErr("ListBuckets", "InvalidAccessKeyId"), true},
		{"SignatureDoesNotMatch", revokedKeyErr("ListBuckets", "SignatureDoesNotMatch"), true},
		{"AccessDenied", revokedKeyErr("ListBuckets", "AccessDenied"), true},
		{"Forbidden", revokedKeyErr("ListBuckets", "Forbidden"), true},
		{"InvalidSignature (Garage variant)", revokedKeyErr("ListBuckets", "InvalidSignature"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isUserKeyRejected(tc.err)
			if got != tc.want {
				t.Errorf("isUserKeyRejected(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
