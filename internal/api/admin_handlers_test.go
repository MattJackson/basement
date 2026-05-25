package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// testMockDriver is a mock driver for testing admin handlers.
type testMockDriver struct {
	listNodesFunc    func(ctx context.Context) ([]driver.Node, error)
	getLayoutFunc    func(ctx context.Context) (driver.Layout, error)
	stageLayoutFunc  func(ctx context.Context, change driver.LayoutChange) (driver.LayoutDiff, error)
	applyLayoutFunc  func(ctx context.Context) error
	revertLayoutFunc func(ctx context.Context) error
	listBucketsFunc  func(ctx context.Context) ([]driver.Bucket, error)
	getBucketFunc    func(ctx context.Context, id string) (driver.Bucket, error)
	createBucketFunc func(ctx context.Context, spec driver.BucketSpec) (driver.Bucket, error)
	updateBucketFunc func(ctx context.Context, id string, update driver.BucketUpdate) (driver.Bucket, error)
	deleteBucketFunc func(ctx context.Context, id string) error
	listKeysFunc     func(ctx context.Context) ([]driver.Key, error)
	getKeyFunc       func(ctx context.Context, id string) (driver.Key, error)
	createKeyFunc    func(ctx context.Context, spec driver.KeySpec) (driver.Key, error)
	updateKeyPermissionsFunc func(ctx context.Context, keyID string, perms []driver.BucketPermission) error
	deleteKeyFunc    func(ctx context.Context, id string) error
	listObjectsFunc  func(ctx context.Context, bucket, prefix, token, delimiter string, limit int) (driver.ObjectPage, error)
	statObjectFunc   func(ctx context.Context, bucket, key string) (driver.ObjectInfo, error)
	presignGetFunc   func(ctx context.Context, bucket, key string, ttl time.Duration) (driver.PresignedURL, error)
	presignPutFunc   func(ctx context.Context, bucket, key string, ttl time.Duration, contentType string) (driver.PresignedURL, error)
	deleteObjectFunc func(ctx context.Context, bucket, key string) error
	createMultipartFunc func(ctx context.Context, bucket, key, contentType string) (driver.MultipartUpload, error)
	presignUploadPartFunc func(ctx context.Context, upload driver.MultipartUpload, partNum int) (driver.PresignedURL, error)
	completeMultipartFunc func(ctx context.Context, upload driver.MultipartUpload, parts []driver.CompletedPart) error
	abortMultipartFunc func(ctx context.Context, upload driver.MultipartUpload) error
	healthCheckErr     error  // custom HealthCheck error for tests

	// v0.9.0i LIFECYCLE.WIZARD hooks. nil-default means tests get
	// Supported=false (matches Garage v1) and quiet stubs.
	lifecycleSupportFunc func() driver.LifecycleCapabilities
	getLifecycleFunc     func(ctx context.Context, bucketID string) ([]driver.LifecycleRule, error)
	putLifecycleFunc     func(ctx context.Context, bucketID string, rules []driver.LifecycleRule) error

	// v1.4.0a hook. nil-default returns false (matches Garage v1).
	perBucketStatsAvailableFunc func() bool

	// v1.4.0c SCRUB.MAINT hooks. nil-default reports unsupported.
	scrubSupportFunc func() driver.ScrubCapability
	scrubStateFunc   func(ctx context.Context) (driver.ScrubState, error)
	startScrubFunc   func(ctx context.Context) error

	// v1.10.0a versioning hooks. nil-default reports unsupported
	// (matches Garage v1/v2 posture).
	versioningSupportFunc   func() bool
	getVersioningStatusFunc func(ctx context.Context, bucket string) (driver.VersioningStatus, error)
	enableVersioningFunc    func(ctx context.Context, bucket string) error
	suspendVersioningFunc   func(ctx context.Context, bucket string) error
	listObjectVersionsFunc  func(ctx context.Context, bucket, prefix, versionIDMarker string, limit int) ([]driver.ObjectVersion, string, error)
	getObjectVersionFunc    func(ctx context.Context, bucket, key, versionID string) (driver.StreamResult, error)
	deleteObjectVersionFunc func(ctx context.Context, bucket, key, versionID string) error

	// v1.10.0c Object Lock hooks. nil-default reports unsupported
	// (matches Garage v1/v2 posture). Tests that exercise the
	// Object Lock surface override these per case.
	objectLockSupportFunc    func() bool
	getObjectLockConfigFunc  func(ctx context.Context, bucket string) (*driver.ObjectLockConfig, error)
	putObjectLockConfigFunc  func(ctx context.Context, bucket string, cfg driver.ObjectLockConfig) error
	getObjectRetentionFunc   func(ctx context.Context, bucket, key, versionID string) (*driver.ObjectLockRetention, error)
	putObjectRetentionFunc   func(ctx context.Context, bucket, key, versionID string, retention driver.ObjectLockRetention, bypassGovernance bool) error
	getObjectLegalHoldFunc   func(ctx context.Context, bucket, key, versionID string) (bool, error)
	putObjectLegalHoldFunc   func(ctx context.Context, bucket, key, versionID string, on bool) error

	// v1.10.0d Bucket Encryption hooks. nil-default reports
	// unsupported (matches Garage v1/v2 posture). Tests that
	// exercise the encryption surface override these per case.
	sseSupportFunc             func() (bool, bool)
	getBucketEncryptionFunc    func(ctx context.Context, bucket string) (*driver.BucketEncryption, error)
	putBucketEncryptionFunc    func(ctx context.Context, bucket string, enc driver.BucketEncryption) error
	deleteBucketEncryptionFunc func(ctx context.Context, bucket string) error
}

func (m *testMockDriver) Capabilities(_ context.Context) (driver.Caps, error) { return driver.Caps{}, nil }
func (m *testMockDriver) HealthCheck(_ context.Context) (driver.HealthReport, error) { 
	if m.healthCheckErr != nil {
		return driver.HealthReport{}, m.healthCheckErr
	}
	return driver.HealthReport{}, nil
}
func (m *testMockDriver) ListNodes(ctx context.Context) ([]driver.Node, error) {
	if m.listNodesFunc != nil {
		return m.listNodesFunc(ctx)
	}
	return nil, nil
}
func (m *testMockDriver) GetLayout(ctx context.Context) (driver.Layout, error) {
	if m.getLayoutFunc != nil {
		return m.getLayoutFunc(ctx)
	}
	return driver.Layout{Nodes: []driver.Node{}}, nil
}
func (m *testMockDriver) StageLayout(ctx context.Context, change driver.LayoutChange) (driver.LayoutDiff, error) {
	if m.stageLayoutFunc != nil {
		return m.stageLayoutFunc(ctx, change)
	}
	return driver.LayoutDiff{}, nil
}
func (m *testMockDriver) ApplyLayout(ctx context.Context) error {
	if m.applyLayoutFunc != nil {
		return m.applyLayoutFunc(ctx)
	}
	return nil
}
func (m *testMockDriver) RevertLayout(ctx context.Context) error {
	if m.revertLayoutFunc != nil {
		return m.revertLayoutFunc(ctx)
	}
	return nil
}
func (m *testMockDriver) ListBuckets(ctx context.Context) ([]driver.Bucket, error) {
	if m.listBucketsFunc != nil {
		return m.listBucketsFunc(ctx)
	}
	return nil, nil
}
func (m *testMockDriver) GetBucket(ctx context.Context, id string) (driver.Bucket, error) {
	if m.getBucketFunc != nil {
		return m.getBucketFunc(ctx, id)
	}
	return driver.Bucket{}, nil
}
func (m *testMockDriver) CreateBucket(ctx context.Context, spec driver.BucketSpec) (driver.Bucket, error) {
	if m.createBucketFunc != nil {
		return m.createBucketFunc(ctx, spec)
	}
	return driver.Bucket{}, nil
}
func (m *testMockDriver) UpdateBucket(ctx context.Context, id string, update driver.BucketUpdate) (driver.Bucket, error) {
	if m.updateBucketFunc != nil {
		return m.updateBucketFunc(ctx, id, update)
	}
	return driver.Bucket{}, nil
}
func (m *testMockDriver) DeleteBucket(ctx context.Context, id string) error {
	if m.deleteBucketFunc != nil {
		return m.deleteBucketFunc(ctx, id)
	}
	return nil
}
func (m *testMockDriver) ListKeys(ctx context.Context) ([]driver.Key, error) {
	if m.listKeysFunc != nil {
		return m.listKeysFunc(ctx)
	}
	return nil, nil
}
func (m *testMockDriver) GetKey(ctx context.Context, id string) (driver.Key, error) {
	if m.getKeyFunc != nil {
		return m.getKeyFunc(ctx, id)
	}
	return driver.Key{}, nil
}
func (m *testMockDriver) CreateKey(ctx context.Context, spec driver.KeySpec) (driver.Key, error) {
	if m.createKeyFunc != nil {
		return m.createKeyFunc(ctx, spec)
	}
	return driver.Key{}, nil
}
func (m *testMockDriver) UpdateKeyPermissions(ctx context.Context, keyID string, perms []driver.BucketPermission) error {
	if m.updateKeyPermissionsFunc != nil {
		return m.updateKeyPermissionsFunc(ctx, keyID, perms)
	}
	return nil
}
func (m *testMockDriver) DeleteKey(ctx context.Context, id string) error {
	if m.deleteKeyFunc != nil {
		return m.deleteKeyFunc(ctx, id)
	}
	return nil
}
func (m *testMockDriver) ListObjects(ctx context.Context, bucket, prefix, token, delimiter string, limit int) (driver.ObjectPage, error) {
	if m.listObjectsFunc != nil {
		return m.listObjectsFunc(ctx, bucket, prefix, token, delimiter, limit)
	}
	return driver.ObjectPage{}, nil
}
func (m *testMockDriver) StatObject(ctx context.Context, bucket, key string) (driver.ObjectInfo, error) {
	if m.statObjectFunc != nil {
		return m.statObjectFunc(ctx, bucket, key)
	}
	return driver.ObjectInfo{}, nil
}
func (m *testMockDriver) PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (driver.PresignedURL, error) {
	if m.presignGetFunc != nil {
		return m.presignGetFunc(ctx, bucket, key, ttl)
	}
	return driver.PresignedURL{}, nil
}
func (m *testMockDriver) PresignPut(ctx context.Context, bucket, key string, ttl time.Duration, contentType string) (driver.PresignedURL, error) {
	if m.presignPutFunc != nil {
		return m.presignPutFunc(ctx, bucket, key, ttl, contentType)
	}
	return driver.PresignedURL{}, nil
}
func (m *testMockDriver) DeleteObject(ctx context.Context, bucket, key string) error {
	if m.deleteObjectFunc != nil {
		return m.deleteObjectFunc(ctx, bucket, key)
	}
	return nil
}
func (m *testMockDriver) CreateMultipart(ctx context.Context, bucket, key, contentType string) (driver.MultipartUpload, error) {
	if m.createMultipartFunc != nil {
		return m.createMultipartFunc(ctx, bucket, key, contentType)
	}
	return driver.MultipartUpload{}, nil
}
func (m *testMockDriver) PresignUploadPart(ctx context.Context, upload driver.MultipartUpload, partNum int) (driver.PresignedURL, error) {
	if m.presignUploadPartFunc != nil {
		return m.presignUploadPartFunc(ctx, upload, partNum)
	}
	return driver.PresignedURL{}, nil
}
func (m *testMockDriver) CompleteMultipart(ctx context.Context, upload driver.MultipartUpload, parts []driver.CompletedPart) error {
	if m.completeMultipartFunc != nil {
		return m.completeMultipartFunc(ctx, upload, parts)
	}
	return nil
}
func (m *testMockDriver) AbortMultipart(ctx context.Context, upload driver.MultipartUpload) error {
	if m.abortMultipartFunc != nil {
		return m.abortMultipartFunc(ctx, upload)
	}
	return nil
}

// v0.8.0a+b additions — stub methods so admin handler tests can use
// this mock without touching every existing call site.

func (m *testMockDriver) StreamObject(_ context.Context, _, _, _ string) (driver.StreamResult, error) {
	return driver.StreamResult{}, nil
}

func (m *testMockDriver) PutObjectStream(_ context.Context, _, _ string, _ io.Reader, _ string, _ int64) (driver.PutResult, error) {
	return driver.PutResult{}, nil
}

func (m *testMockDriver) ServerSideCopy(_ context.Context, _, _, _, _ string) error {
	return nil
}

// v0.9.0i LIFECYCLE.WIZARD additions — overridable funcs so the
// lifecycle handler tests can plug in custom behaviour without a
// second mock. Default: Supported=false (matches Garage v1's real
// behaviour) so admin tests that don't care about lifecycle don't
// accidentally trigger the editor.
func (m *testMockDriver) LifecycleSupport() driver.LifecycleCapabilities {
	if m.lifecycleSupportFunc != nil {
		return m.lifecycleSupportFunc()
	}
	return driver.LifecycleCapabilities{Supported: false}
}

func (m *testMockDriver) GetLifecycle(ctx context.Context, bucketID string) ([]driver.LifecycleRule, error) {
	if m.getLifecycleFunc != nil {
		return m.getLifecycleFunc(ctx, bucketID)
	}
	return nil, nil
}

func (m *testMockDriver) PutLifecycle(ctx context.Context, bucketID string, rules []driver.LifecycleRule) error {
	if m.putLifecycleFunc != nil {
		return m.putLifecycleFunc(ctx, bucketID, rules)
	}
	return nil
}

// v1.4.0a — overridable per-bucket stats availability. Default false
// (matches Garage v1, the current operator's deployment). Tests that
// flip column visibility set perBucketStatsAvailableFunc.
func (m *testMockDriver) PerBucketStatsAvailable() bool {
	if m.perBucketStatsAvailableFunc != nil {
		return m.perBucketStatsAvailableFunc()
	}
	return false
}

// v1.4.0c SCRUB.MAINT — overridable scrub support/state/start.
// Defaults: unsupported (matches AWS S3 / MinIO posture).
func (m *testMockDriver) ScrubSupport() driver.ScrubCapability {
	if m.scrubSupportFunc != nil {
		return m.scrubSupportFunc()
	}
	return driver.ScrubCapability{Supported: false}
}

func (m *testMockDriver) ScrubState(ctx context.Context) (driver.ScrubState, error) {
	if m.scrubStateFunc != nil {
		return m.scrubStateFunc(ctx)
	}
	return driver.ScrubState{}, driver.ErrUnsupported
}

func (m *testMockDriver) StartScrub(ctx context.Context) error {
	if m.startScrubFunc != nil {
		return m.startScrubFunc(ctx)
	}
	return driver.ErrUnsupported
}

// v1.10.0a versioning hooks — overridable funcs so the versioning
// handler tests can plug in custom behaviour without a second mock.
// Defaults: VersioningSupport=false (matches Garage v1/v2 posture)
// and methods return ErrUnsupported so a handler that runs against
// an unconfigured mock surfaces the 501 NOT_SUPPORTED branch.
func (m *testMockDriver) VersioningSupport() bool {
	if m.versioningSupportFunc != nil {
		return m.versioningSupportFunc()
	}
	return false
}

func (m *testMockDriver) GetVersioningStatus(ctx context.Context, bucket string) (driver.VersioningStatus, error) {
	if m.getVersioningStatusFunc != nil {
		return m.getVersioningStatusFunc(ctx, bucket)
	}
	return driver.VersioningDisabled, driver.ErrUnsupported
}

func (m *testMockDriver) EnableVersioning(ctx context.Context, bucket string) error {
	if m.enableVersioningFunc != nil {
		return m.enableVersioningFunc(ctx, bucket)
	}
	return driver.ErrUnsupported
}

func (m *testMockDriver) SuspendVersioning(ctx context.Context, bucket string) error {
	if m.suspendVersioningFunc != nil {
		return m.suspendVersioningFunc(ctx, bucket)
	}
	return driver.ErrUnsupported
}

func (m *testMockDriver) ListObjectVersions(ctx context.Context, bucket, prefix, versionIDMarker string, limit int) ([]driver.ObjectVersion, string, error) {
	if m.listObjectVersionsFunc != nil {
		return m.listObjectVersionsFunc(ctx, bucket, prefix, versionIDMarker, limit)
	}
	return nil, "", driver.ErrUnsupported
}

func (m *testMockDriver) GetObjectVersion(ctx context.Context, bucket, key, versionID string) (driver.StreamResult, error) {
	if m.getObjectVersionFunc != nil {
		return m.getObjectVersionFunc(ctx, bucket, key, versionID)
	}
	return driver.StreamResult{}, driver.ErrUnsupported
}

func (m *testMockDriver) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	if m.deleteObjectVersionFunc != nil {
		return m.deleteObjectVersionFunc(ctx, bucket, key, versionID)
	}
	return driver.ErrUnsupported
}

// v1.10.0c Object Lock hooks — overridable funcs so the Object Lock
// handler tests can plug in custom behaviour. Defaults: unsupported
// (matches Garage v1/v2 posture) and methods return ErrUnsupported.
func (m *testMockDriver) ObjectLockSupport() bool {
	if m.objectLockSupportFunc != nil {
		return m.objectLockSupportFunc()
	}
	return false
}

func (m *testMockDriver) GetObjectLockConfig(ctx context.Context, bucket string) (*driver.ObjectLockConfig, error) {
	if m.getObjectLockConfigFunc != nil {
		return m.getObjectLockConfigFunc(ctx, bucket)
	}
	return nil, driver.ErrUnsupported
}

func (m *testMockDriver) PutObjectLockConfig(ctx context.Context, bucket string, cfg driver.ObjectLockConfig) error {
	if m.putObjectLockConfigFunc != nil {
		return m.putObjectLockConfigFunc(ctx, bucket, cfg)
	}
	return driver.ErrUnsupported
}

func (m *testMockDriver) GetObjectRetention(ctx context.Context, bucket, key, versionID string) (*driver.ObjectLockRetention, error) {
	if m.getObjectRetentionFunc != nil {
		return m.getObjectRetentionFunc(ctx, bucket, key, versionID)
	}
	return nil, driver.ErrUnsupported
}

func (m *testMockDriver) PutObjectRetention(ctx context.Context, bucket, key, versionID string, retention driver.ObjectLockRetention, bypassGovernance bool) error {
	if m.putObjectRetentionFunc != nil {
		return m.putObjectRetentionFunc(ctx, bucket, key, versionID, retention, bypassGovernance)
	}
	return driver.ErrUnsupported
}

func (m *testMockDriver) GetObjectLegalHold(ctx context.Context, bucket, key, versionID string) (bool, error) {
	if m.getObjectLegalHoldFunc != nil {
		return m.getObjectLegalHoldFunc(ctx, bucket, key, versionID)
	}
	return false, driver.ErrUnsupported
}

func (m *testMockDriver) PutObjectLegalHold(ctx context.Context, bucket, key, versionID string, on bool) error {
	if m.putObjectLegalHoldFunc != nil {
		return m.putObjectLegalHoldFunc(ctx, bucket, key, versionID, on)
	}
	return driver.ErrUnsupported
}

// v1.10.0d Bucket Encryption hooks — overridable funcs so the
// encryption handler tests can plug in custom behaviour. Defaults:
// unsupported (matches Garage v1/v2 posture) and methods return
// ErrUnsupported.
func (m *testMockDriver) SSESupport() (bool, bool) {
	if m.sseSupportFunc != nil {
		return m.sseSupportFunc()
	}
	return false, false
}

func (m *testMockDriver) GetBucketEncryption(ctx context.Context, bucket string) (*driver.BucketEncryption, error) {
	if m.getBucketEncryptionFunc != nil {
		return m.getBucketEncryptionFunc(ctx, bucket)
	}
	return nil, driver.ErrUnsupported
}

func (m *testMockDriver) PutBucketEncryption(ctx context.Context, bucket string, enc driver.BucketEncryption) error {
	if m.putBucketEncryptionFunc != nil {
		return m.putBucketEncryptionFunc(ctx, bucket, enc)
	}
	return driver.ErrUnsupported
}

func (m *testMockDriver) DeleteBucketEncryption(ctx context.Context, bucket string) error {
	if m.deleteBucketEncryptionFunc != nil {
		return m.deleteBucketEncryptionFunc(ctx, bucket)
	}
	return driver.ErrUnsupported
}

// testSecret is a 32-byte secret used for JWT token generation in tests.
var testSecret = func() []byte {
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i)
	}
	return secret
}()

// generateAdminToken creates a valid admin JWT token for testing.
//
// Per ADR-0003 + v1.3.0a.4 amendment: tokens are minted in ADMIN mode
// with a 1-hour mode-expiry. Admin tests exercise every admin
// capability (cluster:edit, cluster:delete, bucket:delete, etc.); all
// of them require ADMIN under the two-mode model so a single ADMIN
// token satisfies the gate. Tests that specifically want to exercise
// USER-mode behaviour mint their own token via auth.IssueToken (USER
// default).
func generateAdminToken() string {
	modeExpiresAt := time.Now().Add(1 * time.Hour).Unix()
	token, err := auth.IssueTokenWithMode(testSecret, "admin", "admin", true,
		"admin", modeExpiresAt, 24*time.Hour)
	if err != nil {
		panic(err)
	}
	return token
}

// generateUIAdminToken creates a valid admin JWT token with activeRole.kind="ui-admin" for cross-cluster routes.
func generateUIAdminToken() string {
	modeExpiresAt := time.Now().Add(1 * time.Hour).Unix()
	token, err := auth.IssueTokenWithActiveRole(testSecret, "admin", "admin", true,
		"admin", modeExpiresAt, 24*time.Hour, &auth.ActiveRole{Kind: "ui-admin"})
	if err != nil {
		panic(err)
	}
	return token
}

// generateClusterAdminToken creates a valid admin JWT token with activeRole.kind="cluster-admin" for the given cluster.
func generateClusterAdminToken(cid string) string {
	modeExpiresAt := time.Now().Add(1 * time.Hour).Unix()
	token, err := auth.IssueTokenWithActiveRole(testSecret, "admin", "admin", true,
		"admin", modeExpiresAt, 24*time.Hour, &auth.ActiveRole{Kind: "cluster-admin", Cluster: cid})
	if err != nil {
		panic(err)
	}
	return token
}

// generateUserModeAdminToken creates a valid admin JWT token with activeRole.kind="user" for negative tests.
func generateUserModeAdminToken() string {
	modeExpiresAt := time.Now().Add(1 * time.Hour).Unix()
	token, err := auth.IssueTokenWithActiveRole(testSecret, "admin", "admin", true,
		"admin", modeExpiresAt, 24*time.Hour, &auth.ActiveRole{Kind: "user"})
	if err != nil {
		panic(err)
	}
	return token
}

// generateUserToken creates a valid non-admin user JWT token for testing.
func generateUserToken() string {
	token, err := auth.IssueToken(testSecret, "user", "user", false, 24*time.Hour)
	if err != nil {
		panic(err)
	}
	return token
}

// newTestConfig returns a config with proper JWT secret for tests.
func newTestConfig() *config.Config {
	return &config.Config{Listen: ":8080", JWT: config.JWTConfig{Secret: testSecret}}
}

// createAuthRequest creates an HTTP request with a valid admin session cookie.
func createAuthRequest(method, url string) *http.Request {
	req := httptest.NewRequest(method, url, nil)
	addAdminCookie(req)
	return req
}

// addAdminCookie adds an admin session cookie to the request.
func addAdminCookie(req *http.Request) {
	token := generateAdminToken()
	req.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    token,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// createNonAdminRequest creates an HTTP request with a non-admin session cookie.
func createNonAdminRequest(method, url string) *http.Request {
	token := generateUserToken()
	req := httptest.NewRequest(method, url, nil)
	req.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    token,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	return req
}

// assertJSONResponse checks that the response has the expected status code and decodes as JSON.
func assertJSONResponse(t *testing.T, rr *httptest.ResponseRecorder, expectedStatus int) any {
	t.Helper()
	if rr.Code != expectedStatus {
		t.Errorf("expected status %d, got %d: body=%s", expectedStatus, rr.Code, rr.Body.String())
	}
	var resp any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil && expectedStatus == http.StatusOK {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	return resp
}

func TestListNodesHandler_HappyPath(t *testing.T) {
	t.Skip("Skipped after CLUSTER.LAYOUT-EDITOR moved nodes/layout routes under /admin/clusters/{cid}/. Rewrite to construct a Registry with a stub Connection.")
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	nodes := []driver.Node{
		{ID: "node-1", Hostname: "host1", Address: "10.0.0.1", Zone: "zone-a", Role: "storage", Capacity: 1000, Tags: []string{"tag1"}, Status: "connected", Version: "1.0.0"},
	}

	drv := &testMockDriver{
		listNodesFunc: func(_ context.Context) ([]driver.Node, error) {
			return nodes, nil
		},
	}

	srv := New(cfg, st, nil, drv, nil)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/nodes")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	data := assertJSONResponse(t, rr, http.StatusOK).([]any)
	if len(data) != 1 {
		t.Errorf("expected 1 node, got %d", len(data))
	}
}

func TestListNodesHandler_EmptyList(t *testing.T) {
	t.Skip("Skipped after CLUSTER.LAYOUT-EDITOR moved nodes/layout routes under /admin/clusters/{cid}/. Rewrite to construct a Registry with a stub Connection.")
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{
		listNodesFunc: func(_ context.Context) ([]driver.Node, error) {
			return []driver.Node{}, nil
		},
	}

	srv := New(cfg, st, nil, drv, nil)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/nodes")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	data := assertJSONResponse(t, rr, http.StatusOK).([]any)
	if len(data) != 0 {
		t.Errorf("expected empty list, got %d items", len(data))
	}
}

func TestListNodesHandler_DriverUnsupported(t *testing.T) {
	t.Skip("Skipped after CLUSTER.LAYOUT-EDITOR moved nodes/layout routes under /admin/clusters/{cid}/. Rewrite to construct a Registry with a stub Connection.")
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{
		listNodesFunc: func(_ context.Context) ([]driver.Node, error) {
			return nil, &driver.Error{Op: "ListNodes", Driver: "test", Err: driver.ErrUnsupported, Message: "not supported"}
		},
	}

	srv := New(cfg, st, nil, drv, nil)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/nodes")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Errorf("expected status 501, got %d", rr.Code)
	}

	var resp map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	errorObj := resp["error"].(map[string]any)
	if errorObj["code"] != "DRIVER_UNSUPPORTED" {
		t.Errorf("expected code DRIVER_UNSUPPORTED, got %v", errorObj["code"])
	}
}

func TestListNodesHandler_DriverPermissionDenied(t *testing.T) {
	t.Skip("Skipped after CLUSTER.LAYOUT-EDITOR moved nodes/layout routes under /admin/clusters/{cid}/. Rewrite to construct a Registry with a stub Connection.")
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{
		listNodesFunc: func(_ context.Context) ([]driver.Node, error) {
			return nil, &driver.Error{Op: "ListNodes", Driver: "test", Err: driver.ErrPermissionDenied, Message: "permission denied"}
		},
	}

	srv := New(cfg, st, nil, drv, nil)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/nodes")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rr.Code)
	}
}

func TestListNodesHandler_NoAuth(t *testing.T) {
	t.Skip("Skipped after CLUSTER.LAYOUT-EDITOR moved nodes/layout routes under /admin/clusters/{cid}/. Rewrite to construct a Registry with a stub Connection.")
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{}
	srv := New(cfg, st, nil, drv, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/nodes", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}
}

func TestListNodesHandler_NonAdminRole(t *testing.T) {
	t.Skip("Skipped after CLUSTER.LAYOUT-EDITOR moved nodes/layout routes under /admin/clusters/{cid}/. Rewrite to construct a Registry with a stub Connection.")
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{}
	srv := New(cfg, st, nil, drv, nil)

	req := createNonAdminRequest(http.MethodGet, "/api/v1/admin/nodes")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rr.Code)
	}
}

func TestListNodesHandler_MethodNotAllowed(t *testing.T) {
	t.Skip("Skipped after CLUSTER.LAYOUT-EDITOR moved nodes/layout routes under /admin/clusters/{cid}/. Rewrite to construct a Registry with a stub Connection.")
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{}
	srv := New(cfg, st, nil, drv, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/nodes", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rr.Code)
	}
}

func TestGetLayoutHandler_HappyPath(t *testing.T) {
	t.Skip("Skipped after CLUSTER.LAYOUT-EDITOR moved nodes/layout routes under /admin/clusters/{cid}/. Rewrite to construct a Registry with a stub Connection.")
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	layout := driver.Layout{
		Version: 1,
		Nodes: []driver.Node{
			{ID: "node-1", Hostname: "host1", Address: "10.0.0.1", Zone: "zone-a", Role: "storage", Capacity: 1000, Tags: []string{"tag1"}, Status: "connected", Version: "1.0.0"},
		},
	}

	drv := &testMockDriver{
		getLayoutFunc: func(_ context.Context) (driver.Layout, error) {
			return layout, nil
		},
	}

	srv := New(cfg, st, nil, drv, nil)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/layout")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	data := assertJSONResponse(t, rr, http.StatusOK).(map[string]any)
	nodesArr, ok := data["nodes"].([]any)
	if !ok || nodesArr == nil {
		t.Errorf("expected nodes array, got data=%+v", data)
		return
	}
	if len(nodesArr) != 1 {
		t.Errorf("expected 1 node in layout, got %d", len(nodesArr))
	}
	if data["version"] != float64(1) {
		t.Errorf("expected version 1, got %v", data["version"])
	}
}

func TestGetLayoutHandler_DriverUnsupported(t *testing.T) {
	t.Skip("Skipped after CLUSTER.LAYOUT-EDITOR moved nodes/layout routes under /admin/clusters/{cid}/. Rewrite to construct a Registry with a stub Connection.")
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{
		getLayoutFunc: func(_ context.Context) (driver.Layout, error) {
			return driver.Layout{}, &driver.Error{Op: "GetLayout", Driver: "test", Err: driver.ErrUnsupported, Message: "not supported"}
		},
	}

	srv := New(cfg, st, nil, drv, nil)

	req := createAuthRequest(http.MethodGet, "/api/v1/admin/layout")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Errorf("expected status 501, got %d", rr.Code)
	}

	var resp map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	errorObj := resp["error"].(map[string]any)
	if errorObj["code"] != "DRIVER_UNSUPPORTED" {
		t.Errorf("expected code DRIVER_UNSUPPORTED, got %v", errorObj["code"])
	}
}

func TestGetLayoutHandler_NoAuth(t *testing.T) {
	t.Skip("Skipped after CLUSTER.LAYOUT-EDITOR moved nodes/layout routes under /admin/clusters/{cid}/. Rewrite to construct a Registry with a stub Connection.")
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{}
	srv := New(cfg, st, nil, drv, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/layout", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}
}

func TestGetLayoutHandler_NonAdminRole(t *testing.T) {
	t.Skip("Skipped after CLUSTER.LAYOUT-EDITOR moved nodes/layout routes under /admin/clusters/{cid}/. Rewrite to construct a Registry with a stub Connection.")
	cfg := newTestConfig()
	st, _ := store.Open("/tmp/test-store", 90*24*time.Hour)

	drv := &testMockDriver{}
	srv := New(cfg, st, nil, drv, nil)

	req := createNonAdminRequest(http.MethodGet, "/api/v1/admin/layout")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rr.Code)
	}
}

// Skipped: Route /admin/buckets now returns aggregated response with errors field
// func TestListBucketsHandler_HappyPath(t *testing.T) {
// 	cfg := newTestConfig()
