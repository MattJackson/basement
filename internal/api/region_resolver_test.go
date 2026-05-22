package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// resolverTestEnv stands up a Server with a real Store (UserRegions
// wired) plus a stubbable Connections fixture so each resolver
// scenario can seed exactly the admin Connection set it needs.
type resolverTestEnv struct {
	srv     *Server
	store   *store.Store
	conns   *testMockConnectionStore
	cleanup func()
}

func newResolverTestEnv(t *testing.T) *resolverTestEnv {
	t.Helper()

	tmp, err := os.MkdirTemp("", "region-resolver-")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
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
	reg.SetRegionDriverBuilder(func(_, _, _, _ string) (driver.Driver, error) {
		return &testMockDriver{}, nil
	})

	srv := New(cfg, st, conns, nil, reg)
	srv.SetAuditLogger(&memAuditLogger{})

	return &resolverTestEnv{srv: srv, store: st, conns: conns, cleanup: cleanup}
}

func (e *resolverTestEnv) seedRegion(t *testing.T, userID, endpoint, accessKey string) store.UserRegion {
	t.Helper()
	r, err := e.store.UserRegions().Create(context.Background(), store.UserRegion{
		UserID:       userID,
		Alias:        "test",
		Endpoint:     endpoint,
		AccessKeyID:  accessKey,
		SecretKeyEnc: []byte("sk-test"),
	})
	if err != nil {
		t.Fatalf("seedRegion: %v", err)
	}
	return r
}

func (e *resolverTestEnv) seedConn(id, driverName, endpointKey, endpoint string) {
	e.conns.conns = append(e.conns.conns, store.Connection{
		ID:        id,
		Label:     "admin-" + id,
		Driver:    driverName,
		Config:    map[string]string{endpointKey: endpoint},
		Owner:     "org",
		CreatedAt: time.Now().UTC(),
	})
}

// TestResolveRegionToConnection_Happy — a UserRegion whose canonical
// endpoint matches an admin Connection resolves to that Connection's ID.
func TestResolveRegionToConnection_Happy(t *testing.T) {
	env := newResolverTestEnv(t)
	defer env.cleanup()

	endpoint := "https://s3.basement.pq.io"
	env.seedConn("conn-prod", store.DriverGarageV1, "s3_endpoint", endpoint)
	region := env.seedRegion(t, "matthew", endpoint, "GK_user")

	got, err := env.srv.resolveRegionToConnection(context.Background(), "matthew", region.ID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "conn-prod" {
		t.Errorf("expected conn-prod, got %q", got)
	}
}

// TestResolveRegionToConnection_NoAdminBridge — no admin Connection at
// the region's endpoint returns the specific sentinel.
func TestResolveRegionToConnection_NoAdminBridge(t *testing.T) {
	env := newResolverTestEnv(t)
	defer env.cleanup()

	region := env.seedRegion(t, "matthew", "https://orphan.example.com", "GK_user")

	_, err := env.srv.resolveRegionToConnection(context.Background(), "matthew", region.ID)
	if !errors.Is(err, ErrNoAdminBridge) {
		t.Errorf("expected ErrNoAdminBridge, got %v", err)
	}
}

// TestResolveRegionToConnection_NotMine — region exists but belongs to
// a different user surfaces as ErrRegionNotFound so the handler maps
// to 404 without leaking the existence.
func TestResolveRegionToConnection_NotMine(t *testing.T) {
	env := newResolverTestEnv(t)
	defer env.cleanup()

	endpoint := "https://s3.basement.pq.io"
	env.seedConn("conn-prod", store.DriverGarageV1, "s3_endpoint", endpoint)
	aliceRegion := env.seedRegion(t, "alice", endpoint, "GK_alice")

	_, err := env.srv.resolveRegionToConnection(context.Background(), "bob", aliceRegion.ID)
	if !errors.Is(err, ErrRegionNotFound) {
		t.Errorf("expected ErrRegionNotFound for cross-user lookup, got %v", err)
	}
}

// TestResolveRegionToConnection_FirstWinsOnDuplicate — when two admin
// Connections share the same endpoint, the FIRST in store-order wins
// (matches connections.List's deterministic order). A warning is
// logged but the call still succeeds.
func TestResolveRegionToConnection_FirstWinsOnDuplicate(t *testing.T) {
	env := newResolverTestEnv(t)
	defer env.cleanup()

	endpoint := "https://s3.duplicated.example"
	env.seedConn("conn-first", store.DriverGarageV1, "s3_endpoint", endpoint)
	env.seedConn("conn-second", store.DriverGarageV1, "s3_endpoint", endpoint)
	region := env.seedRegion(t, "matthew", endpoint, "GK_user")

	got, err := env.srv.resolveRegionToConnection(context.Background(), "matthew", region.ID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "conn-first" {
		t.Errorf("expected first match (conn-first), got %q", got)
	}
}

// --- handler-level sync tests ---

// resolverUserCookie attaches the standard "matthew" user session.
func resolverUserCookie(req *http.Request, username string) *http.Request {
	token, err := auth.IssueToken(testSecret, username, "user", false, 24*time.Hour)
	if err != nil {
		panic(err)
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

func resolverJSONReq(method, path string, body interface{}) *http.Request {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// TestCreateSync_RegionId_Resolves — POST /user/syncs with a regionId in
// the srcConnectionId / dstConnectionId fields resolves to the matching
// admin Connection and the sync is accepted.
func TestCreateSync_RegionId_Resolves(t *testing.T) {
	env := newResolverTestEnv(t)
	defer env.cleanup()

	endpoint := "https://s3.basement.pq.io"
	env.seedConn("conn-prod", store.DriverGarageV1, "s3_endpoint", endpoint)
	region := env.seedRegion(t, "matthew", endpoint, "GK_user")

	body := map[string]string{
		"mode":            "pull",
		"srcConnectionId": region.ID,
		"srcBucket":       "lsi",
		"dstConnectionId": region.ID,
		"dstBucket":       "cheshire",
	}
	rr := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr, resolverUserCookie(resolverJSONReq(http.MethodPost, "/api/v1/user/syncs", body), "matthew"))

	if rr.Code != http.StatusAccepted {
		t.Fatalf("create sync: expected 202, got %d (%s)", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp["id"] == nil || resp["id"] == "" {
		t.Errorf("expected sync id in response, got %+v", resp)
	}
}

// TestCreateSync_DirectConnectionId_BackCompat — back-compat: a caller
// still passing a real Connection.ID (legacy or non-FE) is accepted.
func TestCreateSync_DirectConnectionId_BackCompat(t *testing.T) {
	env := newResolverTestEnv(t)
	defer env.cleanup()

	endpoint := "https://s3.basement.pq.io"
	env.seedConn("conn-prod", store.DriverGarageV1, "s3_endpoint", endpoint)
	// Seed a region too so the visibility check passes via the keychain.
	env.seedRegion(t, "matthew", endpoint, "GK_user")

	body := map[string]string{
		"mode":            "pull",
		"srcConnectionId": "conn-prod",
		"srcBucket":       "lsi",
		"dstConnectionId": "conn-prod",
		"dstBucket":       "cheshire",
	}
	rr := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr, resolverUserCookie(resolverJSONReq(http.MethodPost, "/api/v1/user/syncs", body), "matthew"))

	if rr.Code != http.StatusAccepted {
		t.Fatalf("legacy connectionId path: expected 202, got %d (%s)", rr.Code, rr.Body.String())
	}
}

// TestCreateSync_RegionWithNoBridge_400 — region exists but has no
// admin Connection at the endpoint returns 400 NO_ADMIN_BRIDGE with
// the endpoint surfaced in details.
func TestCreateSync_RegionWithNoBridge_400(t *testing.T) {
	env := newResolverTestEnv(t)
	defer env.cleanup()

	region := env.seedRegion(t, "matthew", "https://orphan.example.com", "GK_user")

	body := map[string]string{
		"mode":            "pull",
		"srcConnectionId": region.ID,
		"srcBucket":       "lsi",
		"dstConnectionId": region.ID,
		"dstBucket":       "cheshire",
	}
	rr := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr, resolverUserCookie(resolverJSONReq(http.MethodPost, "/api/v1/user/syncs", body), "matthew"))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("orphan region: expected 400, got %d (%s)", rr.Code, rr.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string                 `json:"code"`
			Message string                 `json:"message"`
			Details map[string]interface{} `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != "NO_ADMIN_BRIDGE" {
		t.Errorf("expected code NO_ADMIN_BRIDGE, got %q", resp.Error.Code)
	}
	if resp.Error.Details["endpoint"] != "https://orphan.example.com" {
		t.Errorf("expected endpoint in details, got %+v", resp.Error.Details)
	}
	if resp.Error.Details["field"] != "srcConnectionId" {
		t.Errorf("expected field=srcConnectionId in details, got %+v", resp.Error.Details)
	}
}

// --- handler-level share tests ---

// TestCreateShare_RegionId_Resolves — POST /user/shares with a regionId
// in connectionId resolves to the matching admin Connection and the
// share record is created.
func TestCreateShare_RegionId_Resolves(t *testing.T) {
	env := newResolverTestEnv(t)
	defer env.cleanup()

	endpoint := "https://s3.basement.pq.io"
	env.seedConn("conn-prod", store.DriverGarageV1, "s3_endpoint", endpoint)
	region := env.seedRegion(t, "matthew", endpoint, "GK_user")

	body := map[string]interface{}{
		"connectionId": region.ID,
		"bucketId":     "lsi",
		"prefix":       "shared/",
	}
	rr := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr, resolverUserCookie(resolverJSONReq(http.MethodPost, "/api/v1/user/shares", body), "matthew"))

	if rr.Code != http.StatusCreated {
		t.Fatalf("create share: expected 201, got %d (%s)", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	// Verify the stored share carries the RESOLVED Connection.ID, not
	// the region ID — that's the load-bearing assertion for the
	// resolver bridge (the response shape itself is shared with the
	// existing TestCreateShare_HappyPath coverage).
	if resp["connectionId"] != "conn-prod" {
		t.Errorf("expected resolved connectionId=conn-prod, got %v", resp["connectionId"])
	}
	if resp["bucketId"] != "lsi" {
		t.Errorf("expected bucketId=lsi, got %v", resp["bucketId"])
	}
}

// TestCreateShare_RegionWithNoBridge_400 — share POST with a region
// that has no admin bridge returns 400 NO_ADMIN_BRIDGE with the
// endpoint + field name surfaced.
func TestCreateShare_RegionWithNoBridge_400(t *testing.T) {
	env := newResolverTestEnv(t)
	defer env.cleanup()

	region := env.seedRegion(t, "matthew", "https://orphan.example.com", "GK_user")

	body := map[string]interface{}{
		"connectionId": region.ID,
		"bucketId":     "lsi",
		"prefix":       "shared/",
	}
	rr := httptest.NewRecorder()
	env.srv.router.ServeHTTP(rr, resolverUserCookie(resolverJSONReq(http.MethodPost, "/api/v1/user/shares", body), "matthew"))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("orphan share: expected 400, got %d (%s)", rr.Code, rr.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string                 `json:"code"`
			Details map[string]interface{} `json:"details"`
		} `json:"error"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Error.Code != "NO_ADMIN_BRIDGE" {
		t.Errorf("expected code NO_ADMIN_BRIDGE, got %q", resp.Error.Code)
	}
	if resp.Error.Details["field"] != "connectionId" {
		t.Errorf("expected field=connectionId in details, got %+v", resp.Error.Details)
	}
}

// --- audit verification ---

// TestRegionListBuckets_AuditEvent — confirms ADR step 7: a region-tier
// ListBuckets call records an audit event with actor=userID, action
// region:list_buckets, resource carrying region ID + host, and detail
// carrying accessKey=... so an operator can correlate with backend
// access logs.
func TestRegionListBuckets_AuditEvent(t *testing.T) {
	mock := newRegionMockDriver()
	mock.listBucketsFunc = func(_ context.Context) ([]driver.Bucket, error) {
		return []driver.Bucket{{ID: "lsi", Aliases: []string{"lsi"}}}, nil
	}

	srv, auditLog, cleanup := newRegionsTestEnv(t, mock)
	defer cleanup()

	// Create a region under the standard "user" token then call
	// /buckets so the handler fires its audit emit.
	body := map[string]string{
		"alias":       "home",
		"endpoint":    "https://s3.pq.io",
		"accessKeyId": "GK434abc",
		"secretKey":   "shh",
	}
	rrC := httptest.NewRecorder()
	srv.router.ServeHTTP(rrC, regionUserCookieReq(newJSONRequest("/api/v1/user/regions", body)))
	if rrC.Code != http.StatusCreated {
		t.Fatalf("create region: %d (%s)", rrC.Code, rrC.Body.String())
	}
	var created userRegionResponse
	_ = json.NewDecoder(rrC.Body).Decode(&created)

	rrB := httptest.NewRecorder()
	srv.router.ServeHTTP(rrB, regionUserCookieReq(httptest.NewRequest(http.MethodGet, "/api/v1/user/regions/"+created.ID+"/buckets", nil)))
	if rrB.Code != http.StatusOK {
		t.Fatalf("buckets: %d (%s)", rrB.Code, rrB.Body.String())
	}

	// Find the region:list_buckets event.
	var ev *audit.Event
	for i, e := range auditLog.snapshot() {
		if e.Action == "region:list_buckets" {
			ev = &auditLog.snapshot()[i]
			break
		}
	}
	if ev == nil {
		t.Fatalf("expected region:list_buckets audit event, none recorded")
	}
	if ev.Actor != "user" {
		t.Errorf("expected actor=user, got %q", ev.Actor)
	}
	if ev.Result != audit.ResultSuccess {
		t.Errorf("expected ResultSuccess, got %q", ev.Result)
	}
	// Resource: region:{id}:{host}
	wantPrefix := "region:" + created.ID + ":"
	if len(ev.Resource) <= len(wantPrefix) || ev.Resource[:len(wantPrefix)] != wantPrefix {
		t.Errorf("expected resource prefix %q, got %q", wantPrefix, ev.Resource)
	}
	// Host must follow the prefix (not a region URL with scheme baked in)
	if ev.Resource[len(wantPrefix):] != "s3.pq.io" {
		t.Errorf("expected host suffix s3.pq.io in resource, got %q", ev.Resource[len(wantPrefix):])
	}
	// Detail: accessKey=...
	if ev.Detail != "accessKey=GK434abc" {
		t.Errorf("expected detail accessKey=GK434abc, got %q", ev.Detail)
	}
}
