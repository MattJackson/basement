// Package webdav: unit + smoke tests for the v1.9.0c refactored
// WebDAV gateway.
//
// Coverage mirrors the v1.9.0a/b internal/webdav test surface
// (OPTIONS, PROPFIND, GET, PUT, DELETE, LOCK, auth) plus
// gateway.Gateway interface assertions (Name, DisplayName, Caps,
// Status, Implemented).
//
// The fake Backend stays small + in-memory so the tests assert the
// FileSystem refactor (path parsing → Backend call) without
// dragging in driver / store / SA wiring. The PRODUCTION backend
// is exercised separately by backend_impl_test.go.

package webdav

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/gateway"
)

// -- fake backend ---------------------------------------------------

// fakeBackend implements gateway.Backend with an in-memory map of
// regions + buckets + objects. Records calls minimally; the value is
// in being able to flip behaviour cheaply per test.
type fakeBackend struct {
	mu       sync.Mutex
	users    map[string]string // user → password
	saAKID   string
	saSecret string
	saOwner  string

	regions map[string][]gateway.Region                // userID → regions
	objects map[string]map[string]map[string][]byte    // regionID → bucket → key → body
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		users:   map[string]string{},
		regions: map[string][]gateway.Region{},
		objects: map[string]map[string]map[string][]byte{},
	}
}

func (b *fakeBackend) addUser(name, pw string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.users[name] = pw
}

func (b *fakeBackend) addSA(akid, secret, owner string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.saAKID = akid
	b.saSecret = secret
	b.saOwner = owner
}

func (b *fakeBackend) addRegion(userID string, r gateway.Region) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.regions[userID] = append(b.regions[userID], r)
	if _, ok := b.objects[r.ID]; !ok {
		b.objects[r.ID] = map[string]map[string][]byte{}
	}
}

func (b *fakeBackend) addBucket(regionID, bucket string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.objects[regionID]; !ok {
		b.objects[regionID] = map[string]map[string][]byte{}
	}
	if _, ok := b.objects[regionID][bucket]; !ok {
		b.objects[regionID][bucket] = map[string][]byte{}
	}
}

func (b *fakeBackend) addObject(regionID, bucket, key string, body []byte) {
	b.addBucket(regionID, bucket)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objects[regionID][bucket][key] = body
}

func (b *fakeBackend) AuthBasic(_ context.Context, user, pass string) (*gateway.UserContext, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if want, ok := b.users[user]; ok && want == pass {
		return &gateway.UserContext{UserID: user}, nil
	}
	return nil, gateway.ErrUnauthenticated
}

func (b *fakeBackend) AuthBearer(_ context.Context, payload string) (*gateway.UserContext, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	idx := strings.IndexByte(payload, ':')
	if idx < 0 {
		return nil, gateway.ErrUnauthenticated
	}
	if payload[:idx] != b.saAKID || payload[idx+1:] != b.saSecret {
		return nil, gateway.ErrUnauthenticated
	}
	return &gateway.UserContext{UserID: b.saOwner, ServiceAccountID: "sa-1"}, nil
}

func (b *fakeBackend) AuthSigV4(_ context.Context, _ *http.Request) (*gateway.UserContext, error) {
	return nil, gateway.ErrUnsupported
}

func (b *fakeBackend) ListRegions(_ context.Context, uctx *gateway.UserContext) ([]gateway.Region, error) {
	if uctx == nil {
		return nil, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := append([]gateway.Region(nil), b.regions[uctx.UserID]...)
	return out, nil
}

func (b *fakeBackend) ListBuckets(_ context.Context, _ *gateway.UserContext, regionID string) ([]gateway.Bucket, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	bkts := b.objects[regionID]
	out := make([]gateway.Bucket, 0, len(bkts))
	for name := range bkts {
		out = append(out, gateway.Bucket{ID: name, Aliases: []string{name}})
	}
	return out, nil
}

func (b *fakeBackend) ListObjects(_ context.Context, _ *gateway.UserContext, regionID, bucket, prefix, delimiter, _ string, _ int) (gateway.ObjectPage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	page := gateway.ObjectPage{Objects: []gateway.ObjectMeta{}}
	objs, ok := b.objects[regionID][bucket]
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
		page.Objects = append(page.Objects, gateway.ObjectMeta{
			Key:  key,
			Size: int64(len(body)),
		})
	}
	return page, nil
}

func (b *fakeBackend) HeadObject(_ context.Context, _ *gateway.UserContext, regionID, bucket, key string) (gateway.ObjectMeta, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	body, ok := b.objects[regionID][bucket][key]
	if !ok {
		return gateway.ObjectMeta{}, gateway.ErrNotFound
	}
	return gateway.ObjectMeta{Key: key, Size: int64(len(body))}, nil
}

func (b *fakeBackend) GetObject(_ context.Context, _ *gateway.UserContext, regionID, bucket, key string) (io.ReadCloser, gateway.ObjectMeta, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	body, ok := b.objects[regionID][bucket][key]
	if !ok {
		return nil, gateway.ObjectMeta{}, gateway.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(body)), gateway.ObjectMeta{
		Key:         key,
		Size:        int64(len(body)),
		ContentType: "application/octet-stream",
	}, nil
}

func (b *fakeBackend) PutObject(_ context.Context, _ *gateway.UserContext, regionID, bucket, key string, body io.Reader, _ int64, _ string) error {
	bs, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	b.addObject(regionID, bucket, key, bs)
	return nil
}

func (b *fakeBackend) DeleteObject(_ context.Context, _ *gateway.UserContext, regionID, bucket, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.objects[regionID][bucket]; !ok {
		return gateway.ErrNotFound
	}
	if _, ok := b.objects[regionID][bucket][key]; !ok {
		return gateway.ErrNotFound
	}
	delete(b.objects[regionID][bucket], key)
	return nil
}

func (b *fakeBackend) CopyObject(_ context.Context, _ *gateway.UserContext, srcRegionID, srcBucket, srcKey, dstRegionID, dstBucket, dstKey string) error {
	b.mu.Lock()
	body, ok := b.objects[srcRegionID][srcBucket][srcKey]
	b.mu.Unlock()
	if !ok {
		return gateway.ErrNotFound
	}
	b.addObject(dstRegionID, dstBucket, dstKey, body)
	return nil
}

func (b *fakeBackend) CreateBucket(_ context.Context, _ *gateway.UserContext, regionID, bucket string) error {
	b.addBucket(regionID, bucket)
	return nil
}

func (b *fakeBackend) DeleteBucket(_ context.Context, _ *gateway.UserContext, regionID, bucket string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.objects[regionID], bucket)
	return nil
}

// stubOrgCaps satisfies OrgCapsLookup for the kill-switch tests.
type stubOrgCaps struct{ enabled bool }

func (s *stubOrgCaps) IsEnabled() bool { return s.enabled }

// -- test scaffold --------------------------------------------------

// buildGateway constructs a Gateway with a fake Backend pre-loaded
// for the standard alice/alicepw + /home region case.
func buildGateway(t *testing.T) (*Gateway, *fakeBackend) {
	t.Helper()
	be := newFakeBackend()
	be.addUser("alice", "alicepw")
	be.addRegion("alice", gateway.Region{
		ID:          "region-1",
		Alias:       "home",
		Endpoint:    "https://s3.example.test",
		AccessKeyID: "AKID",
		Region:      "us-east-1",
	})
	g := New(Deps{
		Backend: be,
		Audit:   audit.NewNoop(),
	})
	return g, be
}

func basic(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

func do(t *testing.T, h http.Handler, method, target string, headers map[string]string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// -- gateway interface assertions ------------------------------------

// TestImplementsGatewayInterface is a compile-time + runtime check
// that *Gateway satisfies gateway.Gateway. If a signature drifts
// this fails at compile time; if a method returns a surprising
// default we catch it here too.
func TestImplementsGatewayInterface(t *testing.T) {
	var _ gateway.Gateway = (*Gateway)(nil)

	g, _ := buildGateway(t)
	if g.Name() != "webdav" {
		t.Errorf("Name: want webdav, got %q", g.Name())
	}
	if g.DisplayName() != "WebDAV" {
		t.Errorf("DisplayName: want WebDAV, got %q", g.DisplayName())
	}
	if g.Description() == "" {
		t.Errorf("Description: want non-empty")
	}
	if !g.Implemented() {
		t.Errorf("Implemented: want true for the production WebDAV impl")
	}
	caps := g.Capabilities()
	if !caps.Read || !caps.Write || !caps.Delete || !caps.Move {
		t.Errorf("Capabilities: want read+write+delete+move; got %+v", caps)
	}
	if caps.Lock {
		t.Errorf("Capabilities: want Lock=false (LOCK/UNLOCK return 501)")
	}
	if !caps.BasicAuth || !caps.BearerAuth {
		t.Errorf("Capabilities: want BasicAuth+BearerAuth; got %+v", caps)
	}
	if g.ListenAddress() != "" {
		t.Errorf("ListenAddress: want \"\" for HTTP-mounted, got %q", g.ListenAddress())
	}
	if g.HTTPHandler() == nil {
		t.Errorf("HTTPHandler: want non-nil")
	}
}

func TestStartStop_FlipsRunning(t *testing.T) {
	g, _ := buildGateway(t)
	if g.Status().Running {
		t.Errorf("pre-Start: want Running=false")
	}
	if err := g.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !g.Status().Running {
		t.Errorf("post-Start: want Running=true")
	}
	if err := g.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if g.Status().Running {
		t.Errorf("post-Stop: want Running=false")
	}
}

// -- HTTP verb tests (migrated from v1.9.0a/b) -----------------------

func TestOptionsNoAuth(t *testing.T) {
	g, _ := buildGateway(t)
	w := do(t, g, http.MethodOptions, "/webdav/", nil, nil)
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
	g, _ := buildGateway(t)
	w := do(t, g, "PROPFIND", "/webdav/", map[string]string{"Depth": "1"}, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("PROPFIND no-auth: want 401 got %d", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Basic ") {
		t.Errorf("WWW-Authenticate header missing/wrong: %q", got)
	}
}

func TestPropfindWrongPassword(t *testing.T) {
	g, _ := buildGateway(t)
	w := do(t, g, "PROPFIND", "/webdav/", map[string]string{
		"Authorization": basic("alice", "WRONG"),
		"Depth":         "1",
	}, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("PROPFIND wrong-pw: want 401 got %d", w.Code)
	}
}

func TestPropfindRootListsRegions(t *testing.T) {
	g, _ := buildGateway(t)
	w := do(t, g, "PROPFIND", "/webdav/", map[string]string{
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
	g, be := buildGateway(t)
	be.addBucket("region-1", "photos")
	be.addBucket("region-1", "docs")
	w := do(t, g, "PROPFIND", "/webdav/home/", map[string]string{
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
	g, be := buildGateway(t)
	be.addObject("region-1", "photos", "vacation.jpg", []byte("img"))
	be.addObject("region-1", "photos", "subdir/extra.png", []byte("ext"))
	w := do(t, g, "PROPFIND", "/webdav/home/photos/", map[string]string{
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
	g, be := buildGateway(t)
	be.addObject("region-1", "photos", "hello.txt", []byte("hello world"))
	w := do(t, g, http.MethodGet, "/webdav/home/photos/hello.txt", map[string]string{
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
	g, be := buildGateway(t)
	be.addBucket("region-1", "uploads")
	w := do(t, g, http.MethodPut, "/webdav/home/uploads/note.txt", map[string]string{
		"Authorization": basic("alice", "alicepw"),
		"Content-Type":  "text/plain",
	}, []byte("from finder"))
	if w.Code != http.StatusCreated && w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Fatalf("PUT: unexpected status %d body=%s", w.Code, w.Body.String())
	}

	be.mu.Lock()
	got, ok := be.objects["region-1"]["uploads"]["note.txt"]
	be.mu.Unlock()
	if !ok {
		t.Fatalf("PUT did not store object in fake backend")
	}
	if string(got) != "from finder" {
		t.Errorf("PUT body: want \"from finder\" got %q", string(got))
	}

	pf := do(t, g, "PROPFIND", "/webdav/home/uploads/", map[string]string{
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
	g, be := buildGateway(t)
	be.addObject("region-1", "photos", "old.txt", []byte("bye"))
	w := do(t, g, http.MethodDelete, "/webdav/home/photos/old.txt", map[string]string{
		"Authorization": basic("alice", "alicepw"),
	}, nil)
	if w.Code != http.StatusNoContent && w.Code != http.StatusOK {
		t.Fatalf("DELETE: unexpected status %d body=%s", w.Code, w.Body.String())
	}
	be.mu.Lock()
	_, ok := be.objects["region-1"]["photos"]["old.txt"]
	be.mu.Unlock()
	if ok {
		t.Fatalf("DELETE did not remove object")
	}
}

func TestLockReturns501(t *testing.T) {
	g, _ := buildGateway(t)
	w := do(t, g, "LOCK", "/webdav/home/", map[string]string{
		"Authorization": basic("alice", "alicepw"),
	}, nil)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("LOCK: want 501 got %d", w.Code)
	}
}

// TestWebDAVDisabled_Returns403: when OrgCaps reports IsEnabled=false
// every WebDAV verb — including OPTIONS — short-circuits with a
// typed 403 GATEWAY_DISABLED before auth runs.
func TestWebDAVDisabled_Returns403(t *testing.T) {
	g, _ := buildGateway(t)
	g.orgCaps = &stubOrgCaps{enabled: false}

	w := do(t, g, "PROPFIND", "/webdav/", map[string]string{
		"Authorization": basic("alice", "alicepw"),
		"Depth":         "1",
	}, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("PROPFIND disabled: want 403 got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); !strings.Contains(got, "GATEWAY_DISABLED") {
		t.Errorf("body missing GATEWAY_DISABLED code: %s", got)
	}

	wo := do(t, g, http.MethodOptions, "/webdav/", nil, nil)
	if wo.Code != http.StatusForbidden {
		t.Errorf("OPTIONS disabled: want 403 got %d", wo.Code)
	}
}

func TestWebDAVEnabled_AllowsRequests(t *testing.T) {
	g, _ := buildGateway(t)
	g.orgCaps = &stubOrgCaps{enabled: true}

	w := do(t, g, "PROPFIND", "/webdav/", map[string]string{
		"Authorization": basic("alice", "alicepw"),
		"Depth":         "1",
	}, nil)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND enabled: want 207 got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSAAuthAccepted(t *testing.T) {
	g, be := buildGateway(t)
	be.addSA("BMNTabcdef0123456789", "secretvalue", "alice")

	w := do(t, g, "PROPFIND", "/webdav/", map[string]string{
		"Authorization": basic("BMNTabcdef0123456789", "secretvalue"),
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

// TestServeBumpsStats checks the runtime counter wires through to
// Status() — the /admin/gateways API surfaces these per-gateway, so a
// regression that fails to bump TotalRequests would silently flatten
// the operator dashboard.
func TestServeBumpsStats(t *testing.T) {
	g, _ := buildGateway(t)
	if got := g.Status().TotalRequests; got != 0 {
		t.Fatalf("pre-request TotalRequests: want 0 got %d", got)
	}
	_ = do(t, g, http.MethodOptions, "/webdav/", nil, nil)
	_ = do(t, g, http.MethodOptions, "/webdav/", nil, nil)
	if got := g.Status().TotalRequests; got != 2 {
		t.Errorf("post-request TotalRequests: want 2 got %d", got)
	}
	if g.Status().LastActivity == nil {
		t.Errorf("LastActivity: want non-nil after a request")
	}
}

// Compile-time: assert the gateway.Backend interface didn't grow a
// method we forgot to implement on fakeBackend.
var _ gateway.Backend = (*fakeBackend)(nil)

// Make the unused import warnings go away — errors used only in
// gateway sentinels in the fake. Keeps go-vet happy on a strict
// install.
var _ = errors.New
