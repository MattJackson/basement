package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// memAuditLogger is a minimal in-memory audit.Logger so tests can
// assert which events fired without disk IO.
type memAuditLogger struct {
	mu     sync.Mutex
	events []audit.Event
}

func (m *memAuditLogger) Log(e audit.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
}

func (m *memAuditLogger) Query(_, _ time.Time, _ audit.QueryFilter) ([]audit.Event, error) {
	return nil, nil
}

// v1.4.0a: pagination requires a separate read variant that surfaces
// the pre-pagination total. The in-memory tests don't exercise the
// audit query handler, so a quiet stub is fine here.
func (m *memAuditLogger) QueryWithTotal(_, _ time.Time, _ audit.QueryFilter) ([]audit.Event, int, error) {
	return nil, 0, nil
}

func (m *memAuditLogger) snapshot() []audit.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]audit.Event, len(m.events))
	copy(out, m.events)
	return out
}

// regionMockDriver is a per-test mock that records the calls the
// region handlers make. Embeds testMockDriver so we don't reimplement
// every method.
type regionMockDriver struct {
	*testMockDriver
}

func newRegionMockDriver() *regionMockDriver {
	return &regionMockDriver{testMockDriver: &testMockDriver{}}
}

// newRegionsTestEnv builds a Server with a real Store (with
// UserRegions wired), an in-memory audit logger, and a Registry whose
// per-region driver builder hands back the supplied mock driver.
func newRegionsTestEnv(t *testing.T, mock *regionMockDriver) (*Server, *memAuditLogger, func()) {
	t.Helper()

	tmp, err := os.MkdirTemp("", "user-regions-")
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
	if err := st.WireUserRegions(cfg.JWT.Secret); err != nil {
		cleanup()
		t.Fatalf("WireUserRegions: %v", err)
	}

	conns := &testMockConnectionStore{}
	reg := driver.NewRegistry(conns)
	reg.SetUserRegionsStore(st.UserRegions())
	reg.SetRegionDriverBuilder(func(_, _, _, _, _ string) (driver.Driver, error) {
		return mock, nil
	})

	auditLog := &memAuditLogger{}

	srv := New(cfg, st, conns, nil, reg)
	srv.SetAuditLogger(auditLog)

	return srv, auditLog, func() {
		cleanup()
	}
}

// regionUserCookieReq adds the standard "user" session cookie used by
// the rest of the user-tier API tests.
func regionUserCookieReq(req *http.Request) *http.Request {
	req.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateUserToken(),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	return req
}

// regionCookieReqFor builds a request with a session cookie for the
// supplied (username, role) pair. Used for the ownership test where
// two distinct users have to see each other's regions.
func regionCookieReqFor(t *testing.T, method, url, username string, body interface{}) *http.Request {
	t.Helper()
	var req *http.Request
	if body != nil {
		data, _ := json.Marshal(body)
		req = httptest.NewRequest(method, url, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, url, nil)
		req.Header.Set("Content-Type", "application/json")
	}
	token, err := auth.IssueToken(testSecret, username, "user", false, 24*time.Hour)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}
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

// TestUserRegions_HappyPath_CreateListGetUseDelete walks the full
// lifecycle: create a region, list it, get it, sign ListBuckets, and
// delete. All against the in-memory store + a mock driver.
func TestUserRegions_HappyPath_CreateListGetUseDelete(t *testing.T) {
	mock := newRegionMockDriver()
	mock.listBucketsFunc = func(_ context.Context) ([]driver.Bucket, error) {
		return []driver.Bucket{{ID: "lsi", Aliases: []string{"lsi"}}, {ID: "cheshire", Aliases: []string{"cheshire"}}}, nil
	}

	srv, auditLog, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	// 1. Create
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
		t.Fatalf("create: expected 201, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	var created userRegionResponse
	if err := json.NewDecoder(rr.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID == "" {
		t.Errorf("expected non-empty region ID")
	}
	if created.UserID != "user" {
		t.Errorf("expected userId=user, got %q", created.UserID)
	}
	if created.AccessKeyID != "GK_user_key" {
		t.Errorf("expected accessKeyId echoed back, got %q", created.AccessKeyID)
	}
	// Endpoint normalized
	if created.Endpoint != "https://s3.basement.pq.io" {
		t.Errorf("expected normalized endpoint, got %q", created.Endpoint)
	}
	// Secret never leaked
	rawBody := rr.Body.String()
	if bytes.Contains([]byte(rawBody), []byte("user-secret-do-not-log")) {
		t.Errorf("create response leaked plaintext secret: %s", rawBody)
	}

	// 2. List
	listReq := regionUserCookieReq(httptest.NewRequest(http.MethodGet, "/api/v1/user/regions", nil))
	listReq.Header.Set("Content-Type", "application/json")
	rrList := httptest.NewRecorder()
	srv.router.ServeHTTP(rrList, listReq)
	if rrList.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d (body=%s)", rrList.Code, rrList.Body.String())
	}
	var list []userRegionResponse
	if err := json.NewDecoder(rrList.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Errorf("expected single region matching create, got %#v", list)
	}

	// 3. Get
	getReq := regionUserCookieReq(httptest.NewRequest(http.MethodGet, "/api/v1/user/regions/"+created.ID, nil))
	getReq.Header.Set("Content-Type", "application/json")
	rrGet := httptest.NewRecorder()
	srv.router.ServeHTTP(rrGet, getReq)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d (body=%s)", rrGet.Code, rrGet.Body.String())
	}

	// 4. Use for ListBuckets — also verifies LastUsedAt bump
	bucketsReq := regionUserCookieReq(httptest.NewRequest(http.MethodGet, "/api/v1/user/regions/"+created.ID+"/buckets", nil))
	bucketsReq.Header.Set("Content-Type", "application/json")
	rrBuckets := httptest.NewRecorder()
	srv.router.ServeHTTP(rrBuckets, bucketsReq)
	if rrBuckets.Code != http.StatusOK {
		t.Fatalf("buckets: expected 200, got %d (body=%s)", rrBuckets.Code, rrBuckets.Body.String())
	}
	// v1.4.0a: response wraps the bucket list in
	// {buckets, perBucketStatsAvailable} so the FE can gate column
	// visibility per driver.
	var bucketsResp struct {
		Buckets                 []driver.Bucket `json:"buckets"`
		PerBucketStatsAvailable bool            `json:"perBucketStatsAvailable"`
	}
	if err := json.NewDecoder(rrBuckets.Body).Decode(&bucketsResp); err != nil {
		t.Fatalf("decode buckets: %v", err)
	}
	if len(bucketsResp.Buckets) != 2 {
		t.Errorf("expected 2 buckets from mock, got %d", len(bucketsResp.Buckets))
	}
	// Default mock returns false; the capability flag should mirror it.
	if bucketsResp.PerBucketStatsAvailable {
		t.Errorf("expected PerBucketStatsAvailable=false from default mock, got true")
	}

	// LastUsedAt was bumped
	stored, err := srv.store.UserRegions().Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Get post-buckets: %v", err)
	}
	if stored.LastUsedAt.IsZero() {
		t.Errorf("expected LastUsedAt to be set after /buckets call")
	}

	// 5. Delete
	delReq := regionUserCookieReq(httptest.NewRequest(http.MethodDelete, "/api/v1/user/regions/"+created.ID, nil))
	delReq.Header.Set("Content-Type", "application/json")
	rrDel := httptest.NewRecorder()
	srv.router.ServeHTTP(rrDel, delReq)
	if rrDel.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d (body=%s)", rrDel.Code, rrDel.Body.String())
	}

	// Subsequent GET on deleted region → 404
	rrGetAfter := httptest.NewRecorder()
	getAfter := regionUserCookieReq(httptest.NewRequest(http.MethodGet, "/api/v1/user/regions/"+created.ID, nil))
	getAfter.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rrGetAfter, getAfter)
	if rrGetAfter.Code != http.StatusNotFound {
		t.Errorf("get after delete: expected 404, got %d", rrGetAfter.Code)
	}

	// 6. Audit log: create + delete events
	evs := auditLog.snapshot()
	foundCreate, foundDelete := false, false
	for _, e := range evs {
		if e.Action == "region:create" && e.Result == audit.ResultSuccess && e.Resource == "region:"+created.ID {
			foundCreate = true
		}
		if e.Action == "region:delete" && e.Result == audit.ResultSuccess && e.Resource == "region:"+created.ID {
			foundDelete = true
		}
	}
	if !foundCreate {
		t.Errorf("expected region:create audit success, got %#v", evs)
	}
	if !foundDelete {
		t.Errorf("expected region:delete audit success, got %#v", evs)
	}
}

// TestUserRegions_OwnershipReturns404 — user B asking for user A's
// region must see 404 (not 403, to avoid existence leak).
func TestUserRegions_OwnershipReturns404(t *testing.T) {
	mock := newRegionMockDriver()
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	// User A (the standard "user" token) creates a region.
	create := map[string]string{
		"alias":       "alice-home",
		"endpoint":    "https://s3.alice.example.com",
		"accessKeyId": "GKalice",
		"secretKey":   "alice-secret",
	}
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", create)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create as A: expected 201, got %d", rr.Code)
	}
	var aRegion userRegionResponse
	_ = json.NewDecoder(rr.Body).Decode(&aRegion)

	// User B (different cookie) asks for A's region by ID.
	rrB := httptest.NewRecorder()
	srv.router.ServeHTTP(rrB, regionCookieReqFor(t, http.MethodGet, "/api/v1/user/regions/"+aRegion.ID, "bob", nil))
	if rrB.Code != http.StatusNotFound {
		t.Errorf("user B reading A's region: expected 404, got %d (body=%s)", rrB.Code, rrB.Body.String())
	}

	// And B's list is empty (A's region not visible).
	rrList := httptest.NewRecorder()
	srv.router.ServeHTTP(rrList, regionCookieReqFor(t, http.MethodGet, "/api/v1/user/regions", "bob", nil))
	if rrList.Code != http.StatusOK {
		t.Fatalf("list as B: expected 200, got %d", rrList.Code)
	}
	var bList []userRegionResponse
	_ = json.NewDecoder(rrList.Body).Decode(&bList)
	if len(bList) != 0 {
		t.Errorf("expected B's list to be empty, got %#v", bList)
	}
}

// TestUserRegions_DuplicateAlias409 — v1.2.0d: same user + same
// endpoint + SAME alias returns 409 DUPLICATE_REGION. Same endpoint
// with a DIFFERENT alias is allowed (covered by sibling test).
func TestUserRegions_DuplicateAlias409(t *testing.T) {
	mock := newRegionMockDriver()
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	body := map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.basement.pq.io",
		"accessKeyId": "AK1",
		"secretKey":   "S1",
	}
	rr1 := httptest.NewRecorder()
	srv.router.ServeHTTP(rr1, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", body)))
	if rr1.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d (body=%s)", rr1.Code, rr1.Body.String())
	}

	// Same endpoint + same alias → 409 (alias collides).
	dup := map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.basement.pq.io",
		"accessKeyId": "AK2",
		"secretKey":   "S2",
	}
	rr2 := httptest.NewRecorder()
	srv.router.ServeHTTP(rr2, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", dup)))
	if rr2.Code != http.StatusConflict {
		t.Fatalf("duplicate alias: expected 409, got %d (body=%s)", rr2.Code, rr2.Body.String())
	}
	var errResp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rr2.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if errResp.Error.Code != "DUPLICATE_REGION" {
		t.Errorf("expected code DUPLICATE_REGION, got %q", errResp.Error.Code)
	}
}

// TestUserRegions_SameEndpointDifferentAlias201 — v1.2.0d: same user +
// same endpoint + DIFFERENT alias succeeds. Each access key is the
// primary user noun, so "Work S3" + "Personal S3" against one service
// are first-class.
func TestUserRegions_SameEndpointDifferentAlias201(t *testing.T) {
	mock := newRegionMockDriver()
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	first := map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.basement.pq.io",
		"accessKeyId": "AK1",
		"secretKey":   "S1",
	}
	rr1 := httptest.NewRecorder()
	srv.router.ServeHTTP(rr1, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", first)))
	if rr1.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d (body=%s)", rr1.Code, rr1.Body.String())
	}

	second := map[string]string{
		"alias":       "work",
		"endpoint":    "https://s3.basement.pq.io",
		"accessKeyId": "AK2",
		"secretKey":   "S2",
	}
	rr2 := httptest.NewRecorder()
	srv.router.ServeHTTP(rr2, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", second)))
	if rr2.Code != http.StatusCreated {
		t.Fatalf("second create (different alias): expected 201, got %d (body=%s)", rr2.Code, rr2.Body.String())
	}

	// List should now return 2 rows.
	rrList := httptest.NewRecorder()
	srv.router.ServeHTTP(rrList, regionUserCookieReq(httptest.NewRequest(http.MethodGet, "/api/v1/user/regions", nil)))
	if rrList.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rrList.Code)
	}
	var list []userRegionResponse
	if err := json.NewDecoder(rrList.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 regions after multi-key add, got %d", len(list))
	}
}

// TestUserRegions_InvalidEndpoint400 — malformed endpoint → 400
// INVALID_ENDPOINT.
func TestUserRegions_InvalidEndpoint400(t *testing.T) {
	mock := newRegionMockDriver()
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	body := map[string]string{
		"alias":       "home",
		"endpoint":    "not-a-url",
		"accessKeyId": "AK",
		"secretKey":   "SK",
	}
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", body)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	var errResp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&errResp)
	if errResp.Error.Code != "INVALID_ENDPOINT" {
		t.Errorf("expected code INVALID_ENDPOINT, got %q", errResp.Error.Code)
	}
}

// TestUserRegions_MissingFields_400 — each required field empty →
// 400 INVALID_REQUEST.
func TestUserRegions_MissingFields_400(t *testing.T) {
	mock := newRegionMockDriver()
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	cases := []struct {
		name string
		body map[string]string
	}{
		{"missing alias", map[string]string{"endpoint": "https://x", "accessKeyId": "k", "secretKey": "s"}},
		{"missing endpoint", map[string]string{"alias": "h", "accessKeyId": "k", "secretKey": "s"}},
		{"missing accessKeyId", map[string]string{"alias": "h", "endpoint": "https://x", "secretKey": "s"}},
		{"missing secretKey", map[string]string{"alias": "h", "endpoint": "https://x", "accessKeyId": "k"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			srv.router.ServeHTTP(rr, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", tc.body)))
			if rr.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d (body=%s)", rr.Code, rr.Body.String())
			}
		})
	}
}

// TestUserRegions_NoAuth — every endpoint requires a session cookie.
func TestUserRegions_NoAuth(t *testing.T) {
	mock := newRegionMockDriver()
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	cases := []struct {
		method, path string
	}{
		{http.MethodPost, "/api/v1/user/regions"},
		{http.MethodGet, "/api/v1/user/regions"},
		{http.MethodGet, "/api/v1/user/regions/some-id"},
		{http.MethodDelete, "/api/v1/user/regions/some-id"},
		{http.MethodGet, "/api/v1/user/regions/some-id/buckets"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var req *http.Request
			if tc.method == http.MethodPost {
				req = newJSONRequest(tc.path, map[string]string{})
				req.Method = tc.method
			} else {
				req = httptest.NewRequest(tc.method, tc.path, nil)
				req.Header.Set("Content-Type", "application/json")
			}
			rr := httptest.NewRecorder()
			srv.router.ServeHTTP(rr, req)
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", rr.Code)
			}
		})
	}
}

// TestUserRegions_DeleteInvalidatesDriverCache — after delete, the
// registry's per-region cache no longer has the old entry; verified
// by checking the cache via a second build that hits the (mock) builder.
func TestUserRegions_DeleteInvalidatesDriverCache(t *testing.T) {
	mock := newRegionMockDriver()
	mock.listBucketsFunc = func(_ context.Context) ([]driver.Bucket, error) { return nil, nil }

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	// We need to count builder invocations to assert eviction.
	built := 0
	srv.reg.SetRegionDriverBuilder(func(_, _, _, _, _ string) (driver.Driver, error) {
		built++
		return mock, nil
	})

	create := map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.basement.pq.io",
		"accessKeyId": "AK",
		"secretKey":   "SK",
	}
	rrC := httptest.NewRecorder()
	srv.router.ServeHTTP(rrC, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", create)))
	if rrC.Code != http.StatusCreated {
		t.Fatalf("create: %d", rrC.Code)
	}
	var region userRegionResponse
	_ = json.NewDecoder(rrC.Body).Decode(&region)

	// First buckets call: builder fires once.
	rrB1 := httptest.NewRecorder()
	getB := regionUserCookieReq(httptest.NewRequest(http.MethodGet, "/api/v1/user/regions/"+region.ID+"/buckets", nil))
	getB.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rrB1, getB)
	if rrB1.Code != http.StatusOK {
		t.Fatalf("buckets1: %d (%s)", rrB1.Code, rrB1.Body.String())
	}
	if built != 1 {
		t.Errorf("expected 1 build after first list, got %d", built)
	}

	// Delete — should invalidate the cache.
	rrD := httptest.NewRecorder()
	delReq := regionUserCookieReq(httptest.NewRequest(http.MethodDelete, "/api/v1/user/regions/"+region.ID, nil))
	delReq.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rrD, delReq)
	if rrD.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rrD.Code)
	}

	// Re-create the same endpoint — should rebuild.
	rrC2 := httptest.NewRecorder()
	srv.router.ServeHTTP(rrC2, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", create)))
	if rrC2.Code != http.StatusCreated {
		t.Fatalf("re-create: %d (%s)", rrC2.Code, rrC2.Body.String())
	}
	var region2 userRegionResponse
	_ = json.NewDecoder(rrC2.Body).Decode(&region2)

	rrB2 := httptest.NewRecorder()
	getB2 := regionUserCookieReq(httptest.NewRequest(http.MethodGet, "/api/v1/user/regions/"+region2.ID+"/buckets", nil))
	getB2.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rrB2, getB2)
	if rrB2.Code != http.StatusOK {
		t.Fatalf("buckets2: %d", rrB2.Code)
	}
	if built != 2 {
		t.Errorf("expected 2 builds after delete + re-create, got %d", built)
	}
}

// TestUserRegions_UnwiredRegionsStore_503 — when the keychain hasn't
// been wired the API surfaces a 503 REGIONS_NOT_WIRED.
func TestUserRegions_UnwiredRegionsStore_503(t *testing.T) {
	tmp, err := os.MkdirTemp("", "user-regions-unwired-")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer os.RemoveAll(tmp)

	cfg := newTestConfig()
	cfg.DataDir = tmp
	// Open store WITHOUT WireUserRegions.
	st, err := store.Open(tmp, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	conns := &testMockConnectionStore{}
	reg := driver.NewRegistry(conns)
	srv := New(cfg, st, conns, nil, reg)

	body := map[string]string{
		"alias": "h", "endpoint": "https://x.example.com", "accessKeyId": "k", "secretKey": "s",
	}
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", body)))
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 on unwired store, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	var errResp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&errResp)
	if errResp.Error.Code != "REGIONS_NOT_WIRED" {
		t.Errorf("expected REGIONS_NOT_WIRED, got %q", errResp.Error.Code)
	}
}

// TestUserRegions_PresignUploadPart_HappyPath verifies the v1.1.0c
// per-part presign endpoint signs the request via the region driver
// and surfaces the presigned URL to the caller.
func TestUserRegions_PresignUploadPart_HappyPath(t *testing.T) {
	mock := newRegionMockDriver()
	mock.presignUploadPartFunc = func(_ context.Context, upload driver.MultipartUpload, partNum int) (driver.PresignedURL, error) {
		if upload.UploadID != "U-1" || upload.Bucket != "lsi" {
			t.Errorf("presign got upload=%+v", upload)
		}
		if partNum != 3 {
			t.Errorf("presign got partNum=%d, want 3", partNum)
		}
		return driver.PresignedURL{URL: "https://signed.example.com/part-3"}, nil
	}

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	// Create a region to use.
	create := map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.example.com",
		"accessKeyId": "AK",
		"secretKey":   "SK",
	}
	rrCreate := httptest.NewRecorder()
	srv.router.ServeHTTP(rrCreate, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", create)))
	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (body=%s)", rrCreate.Code, rrCreate.Body.String())
	}
	var region userRegionResponse
	_ = json.NewDecoder(rrCreate.Body).Decode(&region)

	// Presign part 3.
	url := "/api/v1/user/regions/" + region.ID + "/buckets/lsi/multipart/U-1/part/3/presign"
	req := regionUserCookieReq(httptest.NewRequest(http.MethodPost, url, nil))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("presign-part: expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	var presign driver.PresignedURL
	if err := json.NewDecoder(rr.Body).Decode(&presign); err != nil {
		t.Fatalf("decode presign: %v", err)
	}
	if presign.URL != "https://signed.example.com/part-3" {
		t.Errorf("expected signed URL echoed, got %q", presign.URL)
	}
}

// TestUserRegions_PresignUploadPart_BadPartNumber rejects part numbers
// outside the [1, 10000] S3 range with a 400.
func TestUserRegions_PresignUploadPart_BadPartNumber(t *testing.T) {
	mock := newRegionMockDriver()
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	rrCreate := httptest.NewRecorder()
	srv.router.ServeHTTP(rrCreate, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.example.com",
		"accessKeyId": "AK",
		"secretKey":   "SK",
	})))
	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("create: %d", rrCreate.Code)
	}
	var region userRegionResponse
	_ = json.NewDecoder(rrCreate.Body).Decode(&region)

	for _, bad := range []string{"0", "10001", "abc"} {
		url := "/api/v1/user/regions/" + region.ID + "/buckets/lsi/multipart/U-1/part/" + bad + "/presign"
		req := regionUserCookieReq(httptest.NewRequest(http.MethodPost, url, nil))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("partNum=%s: expected 400, got %d", bad, rr.Code)
		}
	}
}

// TestUserRegions_DeleteObject_HappyPath verifies the v1.1.0c object
// delete endpoint hits the region driver and returns 204.
func TestUserRegions_DeleteObject_HappyPath(t *testing.T) {
	mock := newRegionMockDriver()
	deleted := struct {
		bucket string
		key    string
	}{}
	mock.deleteObjectFunc = func(_ context.Context, bucket, key string) error {
		deleted.bucket = bucket
		deleted.key = key
		return nil
	}

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	rrCreate := httptest.NewRecorder()
	srv.router.ServeHTTP(rrCreate, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.example.com",
		"accessKeyId": "AK",
		"secretKey":   "SK",
	})))
	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("create: %d", rrCreate.Code)
	}
	var region userRegionResponse
	_ = json.NewDecoder(rrCreate.Body).Decode(&region)

	url := "/api/v1/user/regions/" + region.ID + "/buckets/lsi/objects/notes.txt"
	req := regionUserCookieReq(httptest.NewRequest(http.MethodDelete, url, nil))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete-object: expected 204, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if deleted.bucket != "lsi" || deleted.key != "notes.txt" {
		t.Errorf("expected driver.DeleteObject(lsi, notes.txt), got (%q, %q)", deleted.bucket, deleted.key)
	}
}

// TestUserRegions_ListObjects_AuditEvent verifies the v1.1.0h audit
// hook on the object-tier ListObjects handler. The other object-tier
// handlers (presign_get/put, multipart_init/part/complete/abort,
// delete_object) follow the IDENTICAL template — auditEmit on success
// with regionObjectResource + regionAuditDetail, auditFailure on each
// error path — so one assertion across the shared shape covers them
// all. Asserts: action=region:list_objects, actor=userID,
// resource=region:{id}:{bucketID}, detail=accessKey=<id>, result=success.
func TestUserRegions_ListObjects_AuditEvent(t *testing.T) {
	mock := newRegionMockDriver()
	mock.listObjectsFunc = func(_ context.Context, bucket, _, _, _ string, _ int) (driver.ObjectPage, error) {
		if bucket != "lsi" {
			t.Errorf("ListObjects got bucket=%q, want lsi", bucket)
		}
		return driver.ObjectPage{Objects: []driver.ObjectInfo{{Key: "notes.txt"}}}, nil
	}

	srv, auditLog, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	// Seed a region — same shape as the audit test for ListBuckets so
	// the resource + detail assertions stay readable.
	create := map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.pq.io",
		"accessKeyId": "GK434abc",
		"secretKey":   "shh",
	}
	rrC := httptest.NewRecorder()
	srv.router.ServeHTTP(rrC, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", create)))
	if rrC.Code != http.StatusCreated {
		t.Fatalf("create region: %d (%s)", rrC.Code, rrC.Body.String())
	}
	var region userRegionResponse
	_ = json.NewDecoder(rrC.Body).Decode(&region)

	// Fire the handler.
	url := "/api/v1/user/regions/" + region.ID + "/buckets/lsi/objects"
	rrL := httptest.NewRecorder()
	srv.router.ServeHTTP(rrL, regionUserCookieReq(httptest.NewRequest(http.MethodGet, url, nil)))
	if rrL.Code != http.StatusOK {
		t.Fatalf("list-objects: %d (%s)", rrL.Code, rrL.Body.String())
	}

	// Find the region:list_objects event.
	var ev *audit.Event
	for i, e := range auditLog.snapshot() {
		if e.Action == "region:list_objects" {
			snap := auditLog.snapshot()
			ev = &snap[i]
			break
		}
	}
	if ev == nil {
		t.Fatalf("expected region:list_objects audit event, none recorded")
	}
	if ev.Actor != "user" {
		t.Errorf("expected actor=user, got %q", ev.Actor)
	}
	if ev.Result != audit.ResultSuccess {
		t.Errorf("expected ResultSuccess, got %q", ev.Result)
	}
	want := "region:" + region.ID + ":lsi"
	if ev.Resource != want {
		t.Errorf("expected resource %q, got %q", want, ev.Resource)
	}
	if ev.Detail != "accessKey=GK434abc" {
		t.Errorf("expected detail accessKey=GK434abc, got %q", ev.Detail)
	}
}

// TestUserRegions_ListObjects_DelimiterDefault (v1.3.0c.1): the
// object-tier list handler defaults delimiter="/" so the bucket browser
// gets folder-tier results (commonPrefixes + only direct files), and
// faithfully returns whatever CommonPrefixes the driver yields.
func TestUserRegions_ListObjects_DelimiterDefault(t *testing.T) {
	mock := newRegionMockDriver()
	var gotDelim string
	mock.listObjectsFunc = func(_ context.Context, _, _, _, delim string, _ int) (driver.ObjectPage, error) {
		gotDelim = delim
		return driver.ObjectPage{
			Objects:        []driver.ObjectInfo{{Key: "raw/notes.txt"}},
			CommonPrefixes: []string{"index/", "raw/"},
		}, nil
	}

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	create := map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.pq.io",
		"accessKeyId": "GK_DELIM",
		"secretKey":   "shh",
	}
	rrC := httptest.NewRecorder()
	srv.router.ServeHTTP(rrC, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", create)))
	if rrC.Code != http.StatusCreated {
		t.Fatalf("create region: %d (%s)", rrC.Code, rrC.Body.String())
	}
	var region userRegionResponse
	_ = json.NewDecoder(rrC.Body).Decode(&region)

	// Default invocation — no ?delimiter param, handler picks "/".
	url := "/api/v1/user/regions/" + region.ID + "/buckets/lsi/objects"
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(httptest.NewRequest(http.MethodGet, url, nil)))
	if rr.Code != http.StatusOK {
		t.Fatalf("list-objects: %d (%s)", rr.Code, rr.Body.String())
	}
	if gotDelim != "/" {
		t.Errorf("driver called with delimiter=%q, want %q", gotDelim, "/")
	}
	var page driver.ObjectPage
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(page.CommonPrefixes) != 2 || page.CommonPrefixes[0] != "index/" || page.CommonPrefixes[1] != "raw/" {
		t.Errorf("commonPrefixes=%v, want [index/ raw/]", page.CommonPrefixes)
	}
	// Wire-name sanity check: the JSON tag is commonPrefixes (not
	// prefixes) so the FE binds to the right field.
	if !strings.Contains(rr.Body.String(), "\"commonPrefixes\":") {
		t.Errorf("expected response to contain \"commonPrefixes\":, got %s", rr.Body.String())
	}
}

// TestUserRegions_ListObjects_DelimiterEmpty (v1.3.0c.1): callers
// (sync engine preview, scripts) opt out of folder-tier listing by
// passing ?delimiter= (empty). The handler honors that and asks the
// driver for a flat recursive listing.
func TestUserRegions_ListObjects_DelimiterEmpty(t *testing.T) {
	mock := newRegionMockDriver()
	var gotDelim = "<unset>"
	mock.listObjectsFunc = func(_ context.Context, _, _, _, delim string, _ int) (driver.ObjectPage, error) {
		gotDelim = delim
		return driver.ObjectPage{
			Objects: []driver.ObjectInfo{
				{Key: "raw/notes.txt"},
				{Key: "raw/sub/deep.txt"},
			},
		}, nil
	}

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	create := map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.pq.io",
		"accessKeyId": "GK_FLAT",
		"secretKey":   "shh",
	}
	rrC := httptest.NewRecorder()
	srv.router.ServeHTTP(rrC, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", create)))
	if rrC.Code != http.StatusCreated {
		t.Fatalf("create region: %d", rrC.Code)
	}
	var region userRegionResponse
	_ = json.NewDecoder(rrC.Body).Decode(&region)

	url := "/api/v1/user/regions/" + region.ID + "/buckets/lsi/objects?delimiter="
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(httptest.NewRequest(http.MethodGet, url, nil)))
	if rr.Code != http.StatusOK {
		t.Fatalf("list-objects: %d (%s)", rr.Code, rr.Body.String())
	}
	if gotDelim != "" {
		t.Errorf("driver called with delimiter=%q, want empty", gotDelim)
	}
}

// TestUserRegions_AddressingStyle_DefaultsToPathOnCreate (v1.3.0c):
// the create handler echoes back AddressingStyle="path" when the
// request omits the field — backwards-compat guarantee for every key
// added before the toggle existed.
func TestUserRegions_AddressingStyle_DefaultsToPathOnCreate(t *testing.T) {
	mock := newRegionMockDriver()
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	body := map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.pq.io",
		"accessKeyId": "GK_NOPE",
		"secretKey":   "shh",
	}
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", body)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: %d (%s)", rr.Code, rr.Body.String())
	}
	var created userRegionResponse
	_ = json.NewDecoder(rr.Body).Decode(&created)
	if created.AddressingStyle != "path" {
		t.Errorf("AddressingStyle = %q, want \"path\" by default", created.AddressingStyle)
	}
}

// TestUserRegions_AddressingStyle_VirtualHostOnCreate (v1.3.0c): an
// explicit "virtual_host" survives the create round-trip and is
// reflected in the wire response for the FE's "via virtual-host"
// subtitle.
func TestUserRegions_AddressingStyle_VirtualHostOnCreate(t *testing.T) {
	mock := newRegionMockDriver()
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	body := map[string]string{
		"alias":           "home",
		"endpoint":        "https://s3.pq.io",
		"accessKeyId":     "GK_NOPE",
		"secretKey":       "shh",
		"addressingStyle": "virtual_host",
	}
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", body)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: %d (%s)", rr.Code, rr.Body.String())
	}
	var created userRegionResponse
	_ = json.NewDecoder(rr.Body).Decode(&created)
	if created.AddressingStyle != "virtual_host" {
		t.Errorf("AddressingStyle = %q, want \"virtual_host\"", created.AddressingStyle)
	}
}

// TestUserRegions_Rotate_HappyPath (v1.3.0c): POST /rotate replaces
// accessKey + secret in place, preserves alias/endpoint/region, and
// emits a region:rotate audit success row.
func TestUserRegions_Rotate_HappyPath(t *testing.T) {
	mock := newRegionMockDriver()
	srv, auditLog, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	// 1. Create the original region.
	body := map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.pq.io",
		"accessKeyId": "GK_OLD",
		"secretKey":   "old-secret",
	}
	rrC := httptest.NewRecorder()
	srv.router.ServeHTTP(rrC, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", body)))
	if rrC.Code != http.StatusCreated {
		t.Fatalf("create: %d (%s)", rrC.Code, rrC.Body.String())
	}
	var created userRegionResponse
	_ = json.NewDecoder(rrC.Body).Decode(&created)

	// 2. Rotate.
	rotate := map[string]string{
		"accessKeyId": "GK_NEW",
		"secretKey":   "fresh-secret",
	}
	rrR := httptest.NewRecorder()
	srv.router.ServeHTTP(rrR, regionUserCookieReq(newJSONRequest("/api/v1/user/regions/"+created.ID+"/rotate", rotate)))
	if rrR.Code != http.StatusOK {
		t.Fatalf("rotate: %d (%s)", rrR.Code, rrR.Body.String())
	}
	var rotated userRegionResponse
	_ = json.NewDecoder(rrR.Body).Decode(&rotated)

	if rotated.ID != created.ID {
		t.Errorf("rotate must return same region id; got %q want %q", rotated.ID, created.ID)
	}
	if rotated.AccessKeyID != "GK_NEW" {
		t.Errorf("AccessKeyID not rotated: got %q want GK_NEW", rotated.AccessKeyID)
	}
	if rotated.Alias != created.Alias {
		t.Errorf("Alias must survive rotation: got %q want %q", rotated.Alias, created.Alias)
	}
	if rotated.Endpoint != created.Endpoint {
		t.Errorf("Endpoint must survive rotation: got %q want %q", rotated.Endpoint, created.Endpoint)
	}
	// Secret never leaked back on the wire.
	if bytes.Contains(rrR.Body.Bytes(), []byte("fresh-secret")) {
		t.Errorf("rotate response leaked plaintext secret: %s", rrR.Body.String())
	}

	// 3. Audit row.
	var found bool
	for _, e := range auditLog.snapshot() {
		if e.Action == "region:rotate" && e.Result == audit.ResultSuccess {
			found = true
			wantPrefix := "region:" + created.ID + ":"
			if !bytes.HasPrefix([]byte(e.Resource), []byte(wantPrefix)) {
				t.Errorf("audit resource = %q, want prefix %q", e.Resource, wantPrefix)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected region:rotate success audit row; got events: %+v", auditLog.snapshot())
	}
}

// TestUserRegions_Rotate_OtherUser404 (v1.3.0c): user B cannot rotate
// user A's region; the owner check returns 404 (never 403, per the
// region API security model — don't leak existence of other users'
// regions).
func TestUserRegions_Rotate_OtherUser404(t *testing.T) {
	mock := newRegionMockDriver()
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	rrCreate := httptest.NewRecorder()
	srv.router.ServeHTTP(rrCreate, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.example.com",
		"accessKeyId": "AK_USER",
		"secretKey":   "SK_USER",
	})))
	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("create: %d", rrCreate.Code)
	}
	var region userRegionResponse
	_ = json.NewDecoder(rrCreate.Body).Decode(&region)

	// Bob (other user) tries to rotate matthew's region.
	url := "/api/v1/user/regions/" + region.ID + "/rotate"
	req := regionCookieReqFor(t, http.MethodPost, url, "bob", map[string]string{
		"accessKeyId": "GK_BOB_TRIED",
		"secretKey":   "bobs-secret",
	})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-owner rotate, got %d (body=%s)", rr.Code, rr.Body.String())
	}
}

// TestUserRegions_Rotate_MissingFields_400 (v1.3.0c): omitted
// accessKeyId or secretKey on the rotate body surfaces a 400 — neither
// silently coerces to a wipe.
func TestUserRegions_Rotate_MissingFields_400(t *testing.T) {
	mock := newRegionMockDriver()
	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	rrCreate := httptest.NewRecorder()
	srv.router.ServeHTTP(rrCreate, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.example.com",
		"accessKeyId": "AK_OLD",
		"secretKey":   "SK_OLD",
	})))
	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("create: %d", rrCreate.Code)
	}
	var region userRegionResponse
	_ = json.NewDecoder(rrCreate.Body).Decode(&region)

	cases := []struct {
		name string
		body map[string]string
	}{
		{"missing access key", map[string]string{"secretKey": "x"}},
		{"missing secret", map[string]string{"accessKeyId": "GK_X"}},
		{"empty body", map[string]string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			srv.router.ServeHTTP(rr, regionUserCookieReq(newJSONRequest("/api/v1/user/regions/"+region.ID+"/rotate", tc.body)))
			if rr.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d (body=%s)", rr.Code, rr.Body.String())
			}
		})
	}
}

// TestUserRegions_Rotate_InvalidatesDriverCache (v1.3.0c): after a
// rotate, the next ForUserRegion rebuild MUST hit the builder rather
// than serve a cached driver carrying the stale secret.
func TestUserRegions_Rotate_InvalidatesDriverCache(t *testing.T) {
	mock := newRegionMockDriver()
	mock.listBucketsFunc = func(_ context.Context) ([]driver.Bucket, error) { return nil, nil }

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	built := 0
	srv.reg.SetRegionDriverBuilder(func(_, _, _, _, _ string) (driver.Driver, error) {
		built++
		return mock, nil
	})

	rrC := httptest.NewRecorder()
	srv.router.ServeHTTP(rrC, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.pq.io",
		"accessKeyId": "AK_OLD",
		"secretKey":   "SK_OLD",
	})))
	if rrC.Code != http.StatusCreated {
		t.Fatalf("create: %d", rrC.Code)
	}
	var region userRegionResponse
	_ = json.NewDecoder(rrC.Body).Decode(&region)

	// First buckets call seeds the cache.
	rrB1 := httptest.NewRecorder()
	srv.router.ServeHTTP(rrB1, regionUserCookieReq(httptest.NewRequest(http.MethodGet, "/api/v1/user/regions/"+region.ID+"/buckets", nil)))
	if rrB1.Code != http.StatusOK {
		t.Fatalf("buckets1: %d", rrB1.Code)
	}
	if built != 1 {
		t.Fatalf("expected 1 build after first list, got %d", built)
	}

	// Rotate the key.
	rrR := httptest.NewRecorder()
	srv.router.ServeHTTP(rrR, regionUserCookieReq(newJSONRequest("/api/v1/user/regions/"+region.ID+"/rotate", map[string]string{
		"accessKeyId": "AK_NEW",
		"secretKey":   "SK_NEW",
	})))
	if rrR.Code != http.StatusOK {
		t.Fatalf("rotate: %d (%s)", rrR.Code, rrR.Body.String())
	}

	// Next buckets call must rebuild — old cache key (endpoint, AK_OLD)
	// is evicted; the new driver is built fresh with AK_NEW.
	rrB2 := httptest.NewRecorder()
	srv.router.ServeHTTP(rrB2, regionUserCookieReq(httptest.NewRequest(http.MethodGet, "/api/v1/user/regions/"+region.ID+"/buckets", nil)))
	if rrB2.Code != http.StatusOK {
		t.Fatalf("buckets2: %d", rrB2.Code)
	}
	if built != 2 {
		t.Errorf("expected 2 builds after rotate, got %d (cache was not invalidated)", built)
	}
}

// TestUserRegions_DeleteObject_OtherUser404 — user B cannot delete an
// object via user A's region; the owner check returns 404.
func TestUserRegions_DeleteObject_OtherUser404(t *testing.T) {
	mock := newRegionMockDriver()
	mock.deleteObjectFunc = func(_ context.Context, _, _ string) error {
		t.Errorf("driver.DeleteObject must not be called for non-owner")
		return nil
	}

	srv, _, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	rrCreate := httptest.NewRecorder()
	srv.router.ServeHTTP(rrCreate, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.example.com",
		"accessKeyId": "AK",
		"secretKey":   "SK",
	})))
	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("create: %d", rrCreate.Code)
	}
	var region userRegionResponse
	_ = json.NewDecoder(rrCreate.Body).Decode(&region)

	url := "/api/v1/user/regions/" + region.ID + "/buckets/lsi/objects/notes.txt"
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, regionCookieReqFor(t, http.MethodDelete, url, "bob", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-owner delete, got %d (body=%s)", rr.Code, rr.Body.String())
	}
}

