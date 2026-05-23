// Package webdav: unit + smoke tests for the v1.9.0a gateway.
//
// Coverage targets the contract documented in the cycle spec:
//   - OPTIONS responds with the DAV capability list without auth
//   - PROPFIND on /webdav/ lists the caller's regions
//   - PROPFIND on /webdav/{region}/ lists buckets (with admin bridge)
//   - PROPFIND on /webdav/{region}/{bucket}/ lists objects
//   - GET streams an object body
//   - PUT uploads + subsequent PROPFIND surfaces the new object
//   - DELETE removes an object
//   - Basic auth: wrong password → 401, correct → 207
//   - SA bearer-shaped Basic header authenticates
//   - LOCK / UNLOCK return 501
//
// The driver is a small in-memory stub that records calls so the
// tests can assert dispatch without spinning up Garage / S3. The
// stub satisfies the entire driver.Driver interface via embedded
// helper methods that return ErrUnsupported for the surface we
// don't exercise.

package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/serviceaccount"
	"github.com/mattjackson/basement/internal/store"
)

// -- test driver ------------------------------------------------------

// stubDriver implements driver.Driver with an in-memory bucket+object
// map. Only the methods the WebDAV verbs exercise are real; the rest
// return ErrUnsupported.
type stubDriver struct {
	mu      sync.Mutex
	buckets map[string]bool
	objects map[string]map[string][]byte // bucket -> key -> body
}

func newStubDriver() *stubDriver {
	return &stubDriver{
		buckets: map[string]bool{},
		objects: map[string]map[string][]byte{},
	}
}

func (s *stubDriver) addBucket(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buckets[name] = true
	if _, ok := s.objects[name]; !ok {
		s.objects[name] = map[string][]byte{}
	}
}

func (s *stubDriver) addObject(bucket, key string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.objects[bucket]; !ok {
		s.objects[bucket] = map[string][]byte{}
		s.buckets[bucket] = true
	}
	s.objects[bucket][key] = body
}

func (s *stubDriver) Capabilities(context.Context) (driver.Caps, error) {
	return driver.Caps{}, nil
}
func (s *stubDriver) HealthCheck(context.Context) (driver.HealthReport, error) {
	return driver.HealthReport{Status: "ok"}, nil
}
func (s *stubDriver) ListNodes(context.Context) ([]driver.Node, error) { return nil, nil }
func (s *stubDriver) GetLayout(context.Context) (driver.Layout, error) {
	return driver.Layout{}, nil
}
func (s *stubDriver) StageLayout(context.Context, driver.LayoutChange) (driver.LayoutDiff, error) {
	return driver.LayoutDiff{}, nil
}
func (s *stubDriver) ApplyLayout(context.Context) error  { return nil }
func (s *stubDriver) RevertLayout(context.Context) error { return nil }
func (s *stubDriver) ListBuckets(context.Context) ([]driver.Bucket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]driver.Bucket, 0, len(s.buckets))
	for name := range s.buckets {
		out = append(out, driver.Bucket{ID: name, Aliases: []string{name}})
	}
	return out, nil
}
func (s *stubDriver) GetBucket(_ context.Context, id string) (driver.Bucket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.buckets[id] {
		return driver.Bucket{}, &driver.Error{Err: driver.ErrNotFound}
	}
	return driver.Bucket{ID: id, Aliases: []string{id}}, nil
}
func (s *stubDriver) CreateBucket(_ context.Context, spec driver.BucketSpec) (driver.Bucket, error) {
	s.addBucket(spec.Alias)
	return driver.Bucket{ID: spec.Alias, Aliases: []string{spec.Alias}}, nil
}
func (s *stubDriver) UpdateBucket(context.Context, string, driver.BucketUpdate) (driver.Bucket, error) {
	return driver.Bucket{}, driver.ErrUnsupported
}
func (s *stubDriver) DeleteBucket(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.buckets, id)
	delete(s.objects, id)
	return nil
}
func (s *stubDriver) ListKeys(context.Context) ([]driver.Key, error)      { return nil, nil }
func (s *stubDriver) GetKey(context.Context, string) (driver.Key, error)  { return driver.Key{}, driver.ErrUnsupported }
func (s *stubDriver) CreateKey(context.Context, driver.KeySpec) (driver.Key, error) {
	return driver.Key{}, driver.ErrUnsupported
}
func (s *stubDriver) UpdateKeyPermissions(context.Context, string, []driver.BucketPermission) error {
	return driver.ErrUnsupported
}
func (s *stubDriver) DeleteKey(context.Context, string) error { return driver.ErrUnsupported }

func (s *stubDriver) ListObjects(_ context.Context, bucket, prefix, _ string, delimiter string, _ int) (driver.ObjectPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	page := driver.ObjectPage{Objects: []driver.ObjectInfo{}}
	objs, ok := s.objects[bucket]
	if !ok {
		return page, nil
	}
	seenPrefix := map[string]bool{}
	for key, body := range objs {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := strings.TrimPrefix(key, prefix)
		if delimiter == "/" {
			if idx := strings.Index(rest, "/"); idx >= 0 {
				sub := prefix + rest[:idx+1]
				if !seenPrefix[sub] {
					page.CommonPrefixes = append(page.CommonPrefixes, sub)
					seenPrefix[sub] = true
				}
				continue
			}
		}
		page.Objects = append(page.Objects, driver.ObjectInfo{
			Key:  key,
			Size: int64(len(body)),
		})
	}
	return page, nil
}

func (s *stubDriver) StatObject(_ context.Context, bucket, key string) (driver.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.objects[bucket][key]
	if !ok {
		return driver.ObjectInfo{}, &driver.Error{Err: driver.ErrNotFound}
	}
	return driver.ObjectInfo{Key: key, Size: int64(len(body))}, nil
}

func (s *stubDriver) PresignGet(context.Context, string, string, time.Duration) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, driver.ErrUnsupported
}
func (s *stubDriver) PresignPut(context.Context, string, string, time.Duration, string) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, driver.ErrUnsupported
}

func (s *stubDriver) DeleteObject(_ context.Context, bucket, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.objects[bucket]; !ok {
		return &driver.Error{Err: driver.ErrNotFound}
	}
	if _, ok := s.objects[bucket][key]; !ok {
		return &driver.Error{Err: driver.ErrNotFound}
	}
	delete(s.objects[bucket], key)
	return nil
}

func (s *stubDriver) CreateMultipart(context.Context, string, string, string) (driver.MultipartUpload, error) {
	return driver.MultipartUpload{}, driver.ErrUnsupported
}
func (s *stubDriver) PresignUploadPart(context.Context, driver.MultipartUpload, int) (driver.PresignedURL, error) {
	return driver.PresignedURL{}, driver.ErrUnsupported
}
func (s *stubDriver) CompleteMultipart(context.Context, driver.MultipartUpload, []driver.CompletedPart) error {
	return driver.ErrUnsupported
}
func (s *stubDriver) AbortMultipart(context.Context, driver.MultipartUpload) error {
	return driver.ErrUnsupported
}

func (s *stubDriver) StreamObject(_ context.Context, bucket, key, _ string) (driver.StreamResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.objects[bucket][key]
	if !ok {
		return driver.StreamResult{}, &driver.Error{Err: driver.ErrNotFound}
	}
	return driver.StreamResult{
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		ContentType:   "application/octet-stream",
	}, nil
}

func (s *stubDriver) PutObjectStream(_ context.Context, bucket, key string, reader io.Reader, _ string, _ int64) (driver.PutResult, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return driver.PutResult{}, err
	}
	s.addObject(bucket, key, body)
	return driver.PutResult{ETag: "etag"}, nil
}

func (s *stubDriver) ServerSideCopy(_ context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	s.mu.Lock()
	body, ok := s.objects[srcBucket][srcKey]
	s.mu.Unlock()
	if !ok {
		return &driver.Error{Err: driver.ErrNotFound}
	}
	s.addObject(dstBucket, dstKey, body)
	return nil
}

func (s *stubDriver) LifecycleSupport() driver.LifecycleCapabilities {
	return driver.LifecycleCapabilities{}
}
func (s *stubDriver) GetLifecycle(context.Context, string) ([]driver.LifecycleRule, error) {
	return nil, nil
}
func (s *stubDriver) PutLifecycle(context.Context, string, []driver.LifecycleRule) error {
	return driver.ErrUnsupported
}
func (s *stubDriver) PerBucketStatsAvailable() bool { return false }
func (s *stubDriver) ScrubSupport() driver.ScrubCapability {
	return driver.ScrubCapability{Supported: false}
}
func (s *stubDriver) ScrubState(context.Context) (driver.ScrubState, error) {
	return driver.ScrubState{}, driver.ErrUnsupported
}
func (s *stubDriver) StartScrub(context.Context) error { return driver.ErrUnsupported }

// -- stub helpers ----------------------------------------------------

type stubUsers struct{ password string }

func (s *stubUsers) UserByUsername(name string) (store.User, error) {
	if name != "alice" {
		return store.User{}, store.ErrUserNotFound
	}
	hash, _ := auth.HashPassword(s.password)
	return store.User{ID: "alice-id", Username: "alice", PasswordHash: hash}, nil
}

// -- test scaffold ---------------------------------------------------

// buildHandler constructs a Handler with a fixed pre-resolved test
// region, a stub driver, and a stub user store. The returned closure
// lets the test add buckets / objects to the driver between requests.
func buildHandler(t *testing.T) (*Handler, *stubDriver) {
	t.Helper()
	drv := newStubDriver()

	region := store.UserRegion{
		ID:          "region-1",
		UserID:      "alice-id",
		Alias:       "home",
		Endpoint:    "https://s3.example.test",
		AccessKeyID: "AKID",
		Region:      "us-east-1",
	}

	cfg := &config.Config{}
	cfg.Admin.User = "admin"
	hash, _ := auth.HashPassword("adminpw")
	cfg.Admin.PasswordHash = hash

	h := New(Deps{
		Cfg:   cfg,
		Users: &stubUsers{password: "alicepw"},
		Audit: audit.NewNoop(),
	})
	// Override the per-request fs construction so we don't need a
	// real driver.Registry. The Handler's ServeHTTP builds the fs
	// inside newFS(); we patch the regionLookup + driverFactory to
	// hard-code our stub.
	h.regionLookupOverride = func(ctx context.Context, userID string) ([]store.UserRegion, error) {
		// authenticate() returns claims.UserID = user's Username for the
		// password path (and the SA's OwnerUserID for the SA path).
		// The test always provisions for both shapes.
		if userID == "" {
			return nil, nil
		}
		return []store.UserRegion{region}, nil
	}
	h.driverFactoryOverride = func(ctx context.Context, _ store.UserRegion) (driver.Driver, error) {
		return drv, nil
	}
	return h, drv
}

func basic(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

func do(t *testing.T, h *Handler, method, target string, headers map[string]string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// -- tests -----------------------------------------------------------

func TestOptionsNoAuth(t *testing.T) {
	h, _ := buildHandler(t)
	w := do(t, h, http.MethodOptions, "/webdav/", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("OPTIONS: want 200 got %d", w.Code)
	}
	if got := w.Header().Get("DAV"); got != "1, 3" {
		t.Errorf("DAV header: want \"1, 3\" got %q", got)
	}
	if got := w.Header().Get("Allow"); !strings.Contains(got, "PROPFIND") {
		t.Errorf("Allow header missing PROPFIND: %q", got)
	}
}

func TestPropfindRequiresAuth(t *testing.T) {
	h, _ := buildHandler(t)
	w := do(t, h, "PROPFIND", "/webdav/", map[string]string{"Depth": "1"}, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("PROPFIND no-auth: want 401 got %d", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Basic ") {
		t.Errorf("WWW-Authenticate header missing/wrong: %q", got)
	}
}

func TestPropfindWrongPassword(t *testing.T) {
	h, _ := buildHandler(t)
	w := do(t, h, "PROPFIND", "/webdav/", map[string]string{
		"Authorization": basic("alice", "WRONG"),
		"Depth":         "1",
	}, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("PROPFIND wrong-pw: want 401 got %d", w.Code)
	}
}

func TestPropfindRootListsRegions(t *testing.T) {
	h, _ := buildHandler(t)
	w := do(t, h, "PROPFIND", "/webdav/", map[string]string{
		"Authorization": basic("alice", "alicepw"),
		"Depth":         "1",
	}, nil)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND /: want 207 got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "home") {
		t.Errorf("PROPFIND / body missing region alias: %s", w.Body.String())
	}
}

func TestPropfindRegionListsBuckets(t *testing.T) {
	h, drv := buildHandler(t)
	drv.addBucket("photos")
	drv.addBucket("docs")
	w := do(t, h, "PROPFIND", "/webdav/home/", map[string]string{
		"Authorization": basic("alice", "alicepw"),
		"Depth":         "1",
	}, nil)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND /home/: want 207 got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "photos") || !strings.Contains(body, "docs") {
		t.Errorf("PROPFIND /home/ body missing buckets: %s", body)
	}
}

func TestPropfindBucketListsObjects(t *testing.T) {
	h, drv := buildHandler(t)
	drv.addObject("photos", "vacation.jpg", []byte("img"))
	drv.addObject("photos", "subdir/extra.png", []byte("ext"))
	w := do(t, h, "PROPFIND", "/webdav/home/photos/", map[string]string{
		"Authorization": basic("alice", "alicepw"),
		"Depth":         "1",
	}, nil)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND /home/photos/: want 207 got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "vacation.jpg") {
		t.Errorf("body missing object: %s", body)
	}
	if !strings.Contains(body, "subdir") {
		t.Errorf("body missing common prefix folder: %s", body)
	}
}

func TestGetStreamsObject(t *testing.T) {
	h, drv := buildHandler(t)
	drv.addObject("photos", "hello.txt", []byte("hello world"))
	w := do(t, h, http.MethodGet, "/webdav/home/photos/hello.txt", map[string]string{
		"Authorization": basic("alice", "alicepw"),
	}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET: want 200 got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "hello world" {
		t.Errorf("GET body: want \"hello world\" got %q", got)
	}
}

func TestPutThenPropfindShowsObject(t *testing.T) {
	h, drv := buildHandler(t)
	drv.addBucket("uploads")
	w := do(t, h, http.MethodPut, "/webdav/home/uploads/note.txt", map[string]string{
		"Authorization": basic("alice", "alicepw"),
		"Content-Type":  "text/plain",
	}, []byte("from finder"))
	if w.Code != http.StatusCreated && w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Fatalf("PUT: unexpected status %d body=%s", w.Code, w.Body.String())
	}

	// Confirm the stub driver now holds the object.
	drv.mu.Lock()
	got, ok := drv.objects["uploads"]["note.txt"]
	drv.mu.Unlock()
	if !ok {
		t.Fatalf("PUT did not store object")
	}
	if string(got) != "from finder" {
		t.Errorf("PUT body: want \"from finder\" got %q", string(got))
	}

	// PROPFIND surfaces it.
	pf := do(t, h, "PROPFIND", "/webdav/home/uploads/", map[string]string{
		"Authorization": basic("alice", "alicepw"),
		"Depth":         "1",
	}, nil)
	if pf.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND post-PUT: want 207 got %d body=%s", pf.Code, pf.Body.String())
	}
	if !strings.Contains(pf.Body.String(), "note.txt") {
		t.Errorf("PROPFIND missing put object: %s", pf.Body.String())
	}
}

func TestDeleteRemovesObject(t *testing.T) {
	h, drv := buildHandler(t)
	drv.addObject("photos", "old.txt", []byte("bye"))
	w := do(t, h, http.MethodDelete, "/webdav/home/photos/old.txt", map[string]string{
		"Authorization": basic("alice", "alicepw"),
	}, nil)
	if w.Code != http.StatusNoContent && w.Code != http.StatusOK {
		t.Fatalf("DELETE: unexpected status %d body=%s", w.Code, w.Body.String())
	}
	drv.mu.Lock()
	_, ok := drv.objects["photos"]["old.txt"]
	drv.mu.Unlock()
	if ok {
		t.Fatalf("DELETE did not remove object")
	}
}

func TestLockReturns501(t *testing.T) {
	h, _ := buildHandler(t)
	w := do(t, h, "LOCK", "/webdav/home/", map[string]string{
		"Authorization": basic("alice", "alicepw"),
	}, nil)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("LOCK: want 501 got %d", w.Code)
	}
}

// stubOrgCaps satisfies OrgCapsLookup for the v1.9.0b gating tests
// without dragging in a real *OrgCapabilitiesStore + filesystem path.
type stubOrgCaps struct{ caps store.OrgCapabilities }

func (s *stubOrgCaps) Get() store.OrgCapabilities { return s.caps }

// TestWebDAVDisabled_Returns403: when the operator has flipped
// Gateways.WebDAV.Enabled to false in /admin/system, every WebDAV
// verb — including OPTIONS — short-circuits with a typed 403
// GATEWAY_DISABLED before auth runs. This is the v1.9.0b core
// contract: the kill switch works without re-deploying.
func TestWebDAVDisabled_Returns403(t *testing.T) {
	h, _ := buildHandler(t)
	h.deps.OrgCaps = &stubOrgCaps{caps: store.OrgCapabilities{
		Gateways: store.GatewaySettings{
			WebDAV: store.WebDAVSettings{Enabled: false},
		},
	}}

	// Authenticated PROPFIND is rejected with 403, not 207 + listing.
	w := do(t, h, "PROPFIND", "/webdav/", map[string]string{
		"Authorization": basic("alice", "alicepw"),
		"Depth":         "1",
	}, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("PROPFIND disabled: want 403 got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); !strings.Contains(got, "GATEWAY_DISABLED") {
		t.Errorf("body missing GATEWAY_DISABLED code: %s", got)
	}

	// OPTIONS also blocked — no DAV discovery should leak through on
	// a disabled gateway, otherwise Finder will keep retrying creds.
	wo := do(t, h, http.MethodOptions, "/webdav/", nil, nil)
	if wo.Code != http.StatusForbidden {
		t.Errorf("OPTIONS disabled: want 403 got %d", wo.Code)
	}
}

// TestWebDAVEnabled_AllowsRequests: the same Get() returning
// Enabled=true keeps the gateway operational. Guards against a
// regression where a typo in the toggle check (e.g. inverted
// boolean) would silently disable WebDAV on every install.
func TestWebDAVEnabled_AllowsRequests(t *testing.T) {
	h, _ := buildHandler(t)
	h.deps.OrgCaps = &stubOrgCaps{caps: store.OrgCapabilities{
		Gateways: store.GatewaySettings{
			WebDAV: store.WebDAVSettings{Enabled: true},
		},
	}}

	w := do(t, h, "PROPFIND", "/webdav/", map[string]string{
		"Authorization": basic("alice", "alicepw"),
		"Depth":         "1",
	}, nil)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND enabled: want 207 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSAAuthAccepted(t *testing.T) {
	// Wire a stub SA store with one registered key.
	dir := t.TempDir()
	sas, err := serviceaccount.Open(dir)
	if err != nil {
		t.Fatalf("open sa store: %v", err)
	}
	sa, secret, err := sas.Create(context.Background(), serviceaccount.ServiceAccount{
		OwnerUserID: "alice-id",
		Name:        "test-sa",
	})
	if err != nil {
		t.Fatalf("create sa: %v", err)
	}

	h, _ := buildHandler(t)
	// Re-attach the SA store onto the auth resolver after construction.
	h.auth.sas = sas

	w := do(t, h, "PROPFIND", "/webdav/", map[string]string{
		"Authorization": basic(sa.AccessKeyID, secret),
		"Depth":         "1",
	}, nil)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND SA: want 207 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestDecodeBasic(t *testing.T) {
	user, pass, err := decodeBasic(base64.StdEncoding.EncodeToString([]byte("u:p:more")))
	if err != nil {
		t.Fatalf("decodeBasic: %v", err)
	}
	if user != "u" || pass != "p:more" {
		t.Errorf("decodeBasic: got %q / %q", user, pass)
	}
	if _, _, err := decodeBasic("not-base64!!"); err == nil {
		t.Errorf("decodeBasic: want error on garbage")
	}
}
