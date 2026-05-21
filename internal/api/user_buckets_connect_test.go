package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/store"
)

// newConnectTestEnv builds a Server with a real Store + real policy
// enforcer wired against a unique temp dir, plus an in-memory mock
// Connections store. Returns a cleanup the caller defers.
func newConnectTestEnv(t *testing.T) (*Server, *testMockConnectionStore, func()) {
	t.Helper()

	tmp, err := os.MkdirTemp("", "user-buckets-connect-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(tmp)
	}

	cfg := newTestConfig()
	cfg.DataDir = tmp

	st, err := store.Open(tmp, 90*24*time.Hour)
	if err != nil {
		cleanup()
		t.Fatalf("store.Open: %v", err)
	}
	if err := st.WireBucketGrants(cfg.JWT.Secret); err != nil {
		cleanup()
		t.Fatalf("WireBucketGrants: %v", err)
	}

	enf, err := policy.Open(filepath.Join(tmp, "policy"))
	if err != nil {
		cleanup()
		t.Fatalf("policy.Open: %v", err)
	}

	conns := &testMockConnectionStore{}
	srv := New(cfg, st, conns, nil, nil)
	srv.SetPolicy(enf)
	return srv, conns, cleanup
}

// makeUserCookieReq builds a POST request authenticated as "user" via
// the standard generateUserToken helper. Body is JSON-encoded.
func makeUserCookieReq(url string, body interface{}) *http.Request {
	req := newJSONRequest(url, body)
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

// TestUserBucketsConnect_NoAuth: missing session cookie → 401.
func TestUserBucketsConnect_NoAuth(t *testing.T) {
	srv, _, cleanup := newConnectTestEnv(t)
	defer cleanup()

	req := newJSONRequest("/api/v1/user/buckets/connect", map[string]string{
		"alias":       "lsi",
		"s3Endpoint":  "https://s3.example.com",
		"accessKeyId": "GK1234",
		"secretKey":   "secretshhh",
	})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d (body=%s)", rr.Code, rr.Body.String())
	}
}

// TestUserBucketsConnect_HappyPath: with auth + a fresh policy +
// grants store, a complete body should yield 201, a created
// Connection, a BucketGrant, and a bucket_user RoleAssignment.
func TestUserBucketsConnect_HappyPath(t *testing.T) {
	srv, conns, cleanup := newConnectTestEnv(t)
	defer cleanup()

	body := map[string]string{
		"alias":       "lsi",
		"s3Endpoint":  "https://s3.basement.pq.io",
		"accessKeyId": "GK_user_key",
		"secretKey":   "user-secret-do-not-log",
		"region":      "garage",
	}
	req := makeUserCookieReq("/api/v1/user/buckets/connect", body)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body=%s)", rr.Code, rr.Body.String())
	}

	var resp userBucketsConnectResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ConnectionID == "" {
		t.Errorf("expected non-empty connectionId, got %q", resp.ConnectionID)
	}
	if resp.BucketID != "lsi" {
		t.Errorf("expected bucketId=lsi, got %q", resp.BucketID)
	}
	if resp.Alias != "lsi" {
		t.Errorf("expected alias=lsi, got %q", resp.Alias)
	}

	// Connection was created with user as owner.
	list, _ := conns.List(context.Background())
	if len(list) != 1 {
		t.Fatalf("expected 1 connection after connect, got %d", len(list))
	}
	if list[0].Owner != "user" {
		t.Errorf("expected owner=user, got %q", list[0].Owner)
	}
	if list[0].Driver != store.DriverGarageV1 {
		t.Errorf("expected driver=garage-v1, got %q", list[0].Driver)
	}
	if list[0].Config["s3_endpoint"] != "https://s3.basement.pq.io" {
		t.Errorf("expected s3_endpoint in config, got %v", list[0].Config)
	}

	// Grant was persisted with the encrypted secret (never the plaintext).
	grants, _ := srv.store.CredGrants().ListForUser(context.Background(), "user")
	if len(grants) != 1 {
		t.Fatalf("expected 1 bucket grant, got %d", len(grants))
	}
	if grants[0].AccessKeyID != "GK_user_key" {
		t.Errorf("expected accessKeyId=GK_user_key, got %q", grants[0].AccessKeyID)
	}
	if len(grants[0].SecretKeyEnc) == 0 {
		t.Errorf("expected encrypted secret bytes, got empty")
	}
	plain, err := srv.store.CredGrants().Decrypt(grants[0])
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if plain != "user-secret-do-not-log" {
		t.Errorf("decrypted secret round-trip mismatch: %q", plain)
	}

	// RoleAssignment exists at the bucket scope.
	assignments := srv.policy.AssignmentsFor("user")
	wantScope := "bucket:" + resp.ConnectionID + ":lsi"
	found := false
	for _, a := range assignments {
		if a.RoleID == "bucket_user" && a.Scope == wantScope {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected bucket_user assignment at scope %q, got %#v", wantScope, assignments)
	}

	// And the enforcer says yes for objects:list at that scope.
	if !srv.policy.Can("user", "objects:list", wantScope) {
		t.Errorf("expected enforcer to grant objects:list at %q", wantScope)
	}
}

// TestUserBucketsConnect_MissingFields: each required field empty →
// 400 INVALID_REQUEST.
func TestUserBucketsConnect_MissingFields(t *testing.T) {
	cases := []struct {
		name string
		body map[string]string
	}{
		{
			name: "missing alias",
			body: map[string]string{"s3Endpoint": "https://s3.example.com", "accessKeyId": "k", "secretKey": "s"},
		},
		{
			name: "missing endpoint",
			body: map[string]string{"alias": "lsi", "accessKeyId": "k", "secretKey": "s"},
		},
		{
			name: "missing accessKeyId",
			body: map[string]string{"alias": "lsi", "s3Endpoint": "https://s3.example.com", "secretKey": "s"},
		},
		{
			name: "missing secretKey",
			body: map[string]string{"alias": "lsi", "s3Endpoint": "https://s3.example.com", "accessKeyId": "k"},
		},
		{
			name: "endpoint is not a URL",
			body: map[string]string{"alias": "lsi", "s3Endpoint": "not-a-url", "accessKeyId": "k", "secretKey": "s"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _, cleanup := newConnectTestEnv(t)
			defer cleanup()

			req := makeUserCookieReq("/api/v1/user/buckets/connect", tc.body)
			rr := httptest.NewRecorder()
			srv.router.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("%s: expected 400, got %d (body=%s)", tc.name, rr.Code, rr.Body.String())
			}
		})
	}
}

// TestUserBucketsConnect_DuplicateGrant: a second connect for the
// same alias on the same endpoint → 409 GRANT_DUPLICATE.
func TestUserBucketsConnect_DuplicateGrant(t *testing.T) {
	srv, _, cleanup := newConnectTestEnv(t)
	defer cleanup()

	body := map[string]string{
		"alias":       "lsi",
		"s3Endpoint":  "https://s3.basement.pq.io",
		"accessKeyId": "GK_user_key",
		"secretKey":   "user-secret",
	}

	// First call: 201.
	rr1 := httptest.NewRecorder()
	srv.router.ServeHTTP(rr1, makeUserCookieReq("/api/v1/user/buckets/connect", body))
	if rr1.Code != http.StatusCreated {
		t.Fatalf("first connect: expected 201, got %d (body=%s)", rr1.Code, rr1.Body.String())
	}

	// Second call with the same alias + endpoint: should find the
	// existing Connection, then trip the unique-constraint on
	// (userID, connectionID, bucketID) → 409.
	rr2 := httptest.NewRecorder()
	srv.router.ServeHTTP(rr2, makeUserCookieReq("/api/v1/user/buckets/connect", body))
	if rr2.Code != http.StatusConflict {
		t.Fatalf("duplicate connect: expected 409, got %d (body=%s)", rr2.Code, rr2.Body.String())
	}

	var errResp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rr2.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if errResp.Error.Code != "GRANT_DUPLICATE" {
		t.Errorf("expected code GRANT_DUPLICATE, got %q", errResp.Error.Code)
	}
}

// TestUserBucketsConnect_ReusesExistingConnection: a second connect
// with a NEW alias on an endpoint that already has a Connection should
// re-use it rather than creating a second one.
func TestUserBucketsConnect_ReusesExistingConnection(t *testing.T) {
	srv, conns, cleanup := newConnectTestEnv(t)
	defer cleanup()

	first := map[string]string{
		"alias":       "lsi",
		"s3Endpoint":  "https://s3.basement.pq.io",
		"accessKeyId": "k1",
		"secretKey":   "s1",
	}
	rr1 := httptest.NewRecorder()
	srv.router.ServeHTTP(rr1, makeUserCookieReq("/api/v1/user/buckets/connect", first))
	if rr1.Code != http.StatusCreated {
		t.Fatalf("first connect: expected 201, got %d (body=%s)", rr1.Code, rr1.Body.String())
	}

	second := map[string]string{
		"alias":       "family-photos",
		"s3Endpoint":  "https://s3.basement.pq.io",
		"accessKeyId": "k2",
		"secretKey":   "s2",
	}
	rr2 := httptest.NewRecorder()
	srv.router.ServeHTTP(rr2, makeUserCookieReq("/api/v1/user/buckets/connect", second))
	if rr2.Code != http.StatusCreated {
		t.Fatalf("second connect: expected 201, got %d (body=%s)", rr2.Code, rr2.Body.String())
	}

	list, _ := conns.List(context.Background())
	if len(list) != 1 {
		t.Errorf("expected 1 reused Connection across two aliases, got %d", len(list))
	}

	grants, _ := srv.store.CredGrants().ListForUser(context.Background(), "user")
	if len(grants) != 2 {
		t.Errorf("expected 2 grants (one per alias), got %d", len(grants))
	}
}
