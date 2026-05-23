package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/driver"
)

// TestEncryption_GetUnsupportedSurfaces200WithSupportedFalse confirms
// the read endpoint returns 200 + supportedS3=false + supportedKms=false
// on a driver that doesn't implement SSE. The FE uses this to decide
// whether to render the settings card without seeing a 501.
func TestEncryption_GetUnsupportedSurfaces200WithSupportedFalse(t *testing.T) {
	mock := newRegionMockDriver()
	// sseSupportFunc default nil → (false, false).

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodGet,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/encryption", nil))
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get encryption: expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	var resp bucketEncryptionResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SupportedS3 || resp.SupportedKMS {
		t.Errorf("expected both supported flags false, got s3=%v kms=%v",
			resp.SupportedS3, resp.SupportedKMS)
	}
	if resp.Enabled {
		t.Errorf("expected enabled=false on unsupported driver")
	}
}

// TestEncryption_GetSupportedReturnsConfig exercises the supported
// branch with a real SSE-KMS configuration.
func TestEncryption_GetSupportedReturnsConfig(t *testing.T) {
	mock := newRegionMockDriver()
	mock.sseSupportFunc = func() (bool, bool) { return true, true }
	mock.getBucketEncryptionFunc = func(_ context.Context, _ string) (*driver.BucketEncryption, error) {
		return &driver.BucketEncryption{
			Enabled:   true,
			Algorithm: driver.SSEAlgorithmKMS,
			KMSKeyID:  "arn:aws:kms:us-east-1:111122223333:key/abcd-1234",
			BucketKey: true,
		}, nil
	}

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodGet,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/encryption", nil))
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp bucketEncryptionResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.SupportedS3 || !resp.SupportedKMS || !resp.Enabled {
		t.Errorf("supportedS3=%v supportedKms=%v enabled=%v want all true",
			resp.SupportedS3, resp.SupportedKMS, resp.Enabled)
	}
	if resp.Algorithm != driver.SSEAlgorithmKMS {
		t.Errorf("Algorithm=%q, want aws:kms", resp.Algorithm)
	}
	if !strings.Contains(resp.KMSKeyID, "arn:aws:kms") {
		t.Errorf("KMSKeyID=%q, want a KMS ARN", resp.KMSKeyID)
	}
	if !resp.BucketKey {
		t.Errorf("BucketKey=false, want true")
	}
}

// TestEncryption_PutEnabledAuditedAndCallsDriver exercises the happy-path
// write — capability gate, driver call, audit emission for a fresh enable.
func TestEncryption_PutEnabledAuditedAndCallsDriver(t *testing.T) {
	mock := newRegionMockDriver()
	mock.sseSupportFunc = func() (bool, bool) { return true, true }
	// Pre-fetch returns "never configured".
	mock.getBucketEncryptionFunc = func(_ context.Context, _ string) (*driver.BucketEncryption, error) {
		return &driver.BucketEncryption{Enabled: false}, nil
	}
	called := false
	var got driver.BucketEncryption
	mock.putBucketEncryptionFunc = func(_ context.Context, bucket string, enc driver.BucketEncryption) error {
		if bucket != "my-bucket" {
			t.Errorf("bucket=%q want my-bucket", bucket)
		}
		called = true
		got = enc
		return nil
	}

	srv, auditLog, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	body, _ := json.Marshal(bucketEncryptionRequest{
		Algorithm: driver.SSEAlgorithmAES256,
	})
	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodPut,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/encryption",
		bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("put encryption: expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if !called {
		t.Error("PutBucketEncryption was not called")
	}
	if !got.Enabled || got.Algorithm != driver.SSEAlgorithmAES256 {
		t.Errorf("config passed to driver=%+v", got)
	}

	// Expect bucket:encryption_enabled to fire (was disabled → enabled).
	sawEnabled := false
	for _, e := range auditLog.snapshot() {
		if e.Action == "bucket:encryption_enabled" && e.Result == audit.ResultSuccess {
			sawEnabled = true
		}
	}
	if !sawEnabled {
		t.Error("expected audit action bucket:encryption_enabled (success) — not found")
	}
}

// TestEncryption_PutAlgorithmChangeAudited covers the
// algorithm-changed audit branch — was enabled with AES256, now KMS.
func TestEncryption_PutAlgorithmChangeAudited(t *testing.T) {
	mock := newRegionMockDriver()
	mock.sseSupportFunc = func() (bool, bool) { return true, true }
	mock.getBucketEncryptionFunc = func(_ context.Context, _ string) (*driver.BucketEncryption, error) {
		return &driver.BucketEncryption{
			Enabled:   true,
			Algorithm: driver.SSEAlgorithmAES256,
		}, nil
	}
	mock.putBucketEncryptionFunc = func(_ context.Context, _ string, _ driver.BucketEncryption) error {
		return nil
	}

	srv, auditLog, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	body, _ := json.Marshal(bucketEncryptionRequest{
		Algorithm: driver.SSEAlgorithmKMS,
		KMSKeyID:  "arn:aws:kms:us-east-1:111122223333:key/abcd-1234",
	})
	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodPut,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/encryption",
		bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	sawChange := false
	for _, e := range auditLog.snapshot() {
		if e.Action == "bucket:encryption_algorithm_changed" && e.Result == audit.ResultSuccess {
			sawChange = true
		}
	}
	if !sawChange {
		t.Error("expected audit action bucket:encryption_algorithm_changed (success) — not found")
	}
}

// TestEncryption_PutKMSKeyChangeAudited covers the kms_key_changed
// audit branch — was enabled with KMS key A, now KMS key B.
func TestEncryption_PutKMSKeyChangeAudited(t *testing.T) {
	mock := newRegionMockDriver()
	mock.sseSupportFunc = func() (bool, bool) { return true, true }
	mock.getBucketEncryptionFunc = func(_ context.Context, _ string) (*driver.BucketEncryption, error) {
		return &driver.BucketEncryption{
			Enabled:   true,
			Algorithm: driver.SSEAlgorithmKMS,
			KMSKeyID:  "arn:aws:kms:us-east-1:111122223333:key/OLD",
		}, nil
	}
	mock.putBucketEncryptionFunc = func(_ context.Context, _ string, _ driver.BucketEncryption) error {
		return nil
	}

	srv, auditLog, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	body, _ := json.Marshal(bucketEncryptionRequest{
		Algorithm: driver.SSEAlgorithmKMS,
		KMSKeyID:  "arn:aws:kms:us-east-1:111122223333:key/NEW",
	})
	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodPut,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/encryption",
		bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	sawKey := false
	for _, e := range auditLog.snapshot() {
		if e.Action == "bucket:encryption_kms_key_changed" && e.Result == audit.ResultSuccess {
			sawKey = true
		}
	}
	if !sawKey {
		t.Error("expected audit action bucket:encryption_kms_key_changed (success) — not found")
	}
}

// TestEncryption_PutUnsupportedReturns501 confirms the capability gate
// fires for the write path on Garage-style drivers.
func TestEncryption_PutUnsupportedReturns501(t *testing.T) {
	mock := newRegionMockDriver()
	// Default sseSupport=(false,false).

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	body, _ := json.Marshal(bucketEncryptionRequest{
		Algorithm: driver.SSEAlgorithmAES256,
	})
	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodPut,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/encryption",
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

// TestEncryption_PutKMSWhenOnlyS3SupportedReturns501 exercises the
// per-axis capability gate — a driver advertising (true, false)
// accepts AES256 but rejects aws:kms with a capability hint.
func TestEncryption_PutKMSWhenOnlyS3SupportedReturns501(t *testing.T) {
	mock := newRegionMockDriver()
	mock.sseSupportFunc = func() (bool, bool) { return true, false }

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	body, _ := json.Marshal(bucketEncryptionRequest{
		Algorithm: driver.SSEAlgorithmKMS,
		KMSKeyID:  "arn:aws:kms:us-east-1:111122223333:key/abcd",
	})
	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodPut,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/encryption",
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
	if cap, _ := er.Error.Details["capability"].(string); cap != "sseKms" {
		t.Errorf("capability=%q want sseKms", cap)
	}
}

// TestEncryption_PutRejectsInvalidShape covers the body validation
// branches that fire BEFORE driver dispatch — bad algorithm, missing
// KMS key for SSE-KMS.
func TestEncryption_PutRejectsInvalidShape(t *testing.T) {
	mock := newRegionMockDriver()
	mock.sseSupportFunc = func() (bool, bool) { return true, true }

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	cases := []struct {
		name string
		req  bucketEncryptionRequest
	}{
		{"bad algorithm", bucketEncryptionRequest{Algorithm: "ROT13"}},
		{"missing algorithm", bucketEncryptionRequest{}},
		{"kms without key id", bucketEncryptionRequest{Algorithm: driver.SSEAlgorithmKMS}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.req)
			rr := httptest.NewRecorder()
			req := regionUserCookieReq(httptest.NewRequest(http.MethodPut,
				"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/encryption",
				bytes.NewReader(body)))
			req.Header.Set("Content-Type", "application/json")
			srv.router.ServeHTTP(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d (body=%s)", rr.Code, rr.Body.String())
			}
		})
	}
}

// TestEncryption_DeleteAuditedAndCallsDriver exercises the happy-path
// disable — capability gate, driver call, audit emission.
func TestEncryption_DeleteAuditedAndCallsDriver(t *testing.T) {
	mock := newRegionMockDriver()
	mock.sseSupportFunc = func() (bool, bool) { return true, true }
	called := false
	mock.deleteBucketEncryptionFunc = func(_ context.Context, bucket string) error {
		if bucket != "my-bucket" {
			t.Errorf("bucket=%q want my-bucket", bucket)
		}
		called = true
		return nil
	}

	srv, auditLog, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodDelete,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/encryption", nil))
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("delete encryption: expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if !called {
		t.Error("DeleteBucketEncryption was not called")
	}

	sawDisabled := false
	for _, e := range auditLog.snapshot() {
		if e.Action == "bucket:encryption_disabled" && e.Result == audit.ResultSuccess {
			sawDisabled = true
		}
	}
	if !sawDisabled {
		t.Error("expected audit action bucket:encryption_disabled (success) — not found")
	}
}

// TestEncryption_DeleteUnsupportedReturns501 confirms the capability
// gate fires for DELETE on Garage-style drivers.
func TestEncryption_DeleteUnsupportedReturns501(t *testing.T) {
	mock := newRegionMockDriver()
	// Default unsupported.

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	regionID := createTestRegion(t, srv)

	rr := httptest.NewRecorder()
	req := regionUserCookieReq(httptest.NewRequest(http.MethodDelete,
		"/api/v1/user/regions/"+regionID+"/buckets/my-bucket/encryption", nil))
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", rr.Code)
	}
}
