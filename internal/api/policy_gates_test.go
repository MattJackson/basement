// Package api: tests for the v0.9.0f capability gates.
//
// Two flavours covered here:
//
//   1. User-side: a non-admin user without a BucketGrant gets a clean
//      403 NO_GRANT when they try to ListObjects on a bucket they
//      don't have access to. With a grant + a bucket_user assignment,
//      the per-user driver path activates and the request reaches the
//      backend.
//
//   2. Admin-side: a non-admin user with no host_admin / cluster_admin
//      assignment gets a 403 FORBIDDEN when they try a mutating admin
//      op (createBucket / createCluster). With the matching capability,
//      the request passes the gate.
//
// The legacy admin-role middleware would already reject a non-admin
// caller on /admin/* with 403 INSUFFICIENT_ROLE — the new gate sits
// behind it as defense in depth, so for admin-side capability tests
// we use an admin-role token but no policy assignment.
package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/store"
)

// newGateTestEnv builds a Server with a real Store + real (file-backed)
// policy enforcer at an isolated temp dir, plus an in-memory
// Connections mock. Returns the server, the connections mock, the
// enforcer (so tests can AssignRole), and a cleanup.
func newGateTestEnv(t *testing.T) (*Server, *testMockConnectionStore, policy.Enforcer, func()) {
	t.Helper()

	tmp, err := os.MkdirTemp("", "v090f-gate-")
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
	return srv, conns, enf, cleanup
}

// userCookieReqMethod is like makeUserCookieReq but lets the test
// choose method + omit body (for GET / DELETE handlers).
func userCookieReqMethod(method, url string) *http.Request {
	req := httptest.NewRequest(method, url, nil)
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

// TestUserListObjects_NoCapability: a non-admin user with NO policy
// assignment hits the objects:list gate first and receives 403
// FORBIDDEN. Verifies the gate fires before any driver call so the
// nil-driver path is never exercised.
func TestUserListObjects_NoCapability(t *testing.T) {
	srv, _, _, cleanup := newGateTestEnv(t)
	defer cleanup()

	req := userCookieReqMethod(http.MethodGet,
		"/api/v1/user/clusters/cid-x/buckets/lsi/objects")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if !bodyHasCode(rr, "FORBIDDEN") {
		t.Errorf("expected FORBIDDEN code, got body=%s", rr.Body.String())
	}
}

// TestUserListObjects_CapabilityButNoGrant: a non-admin user with
// objects:list assigned on the bucket scope but NO BucketGrant. The
// gate passes; the grant lookup fails with 403 NO_GRANT.
func TestUserListObjects_CapabilityButNoGrant(t *testing.T) {
	srv, _, enf, cleanup := newGateTestEnv(t)
	defer cleanup()

	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "user", RoleID: "bucket_user", Scope: "bucket:cid-x:lsi",
	}); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	req := userCookieReqMethod(http.MethodGet,
		"/api/v1/user/clusters/cid-x/buckets/lsi/objects")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if !bodyHasCode(rr, "NO_GRANT") {
		t.Errorf("expected NO_GRANT code, got body=%s", rr.Body.String())
	}
}

// TestAdminCreateCluster_NoCapability: an admin-role user without the
// cluster:create capability gets 403 FORBIDDEN at the v0.9.0f gate,
// even though the legacy admin-role middleware lets them through.
func TestAdminCreateCluster_NoCapability(t *testing.T) {
	srv, _, _, cleanup := newGateTestEnv(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]any{
		"label":  "x",
		"driver": "garage-v1",
		"config": map[string]string{"admin_url": "http://x:3903"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters",
		jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateAdminToken(),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if !bodyHasCode(rr, "FORBIDDEN") {
		t.Errorf("expected FORBIDDEN code, got body=%s", rr.Body.String())
	}
}

// TestAdminCreateCluster_WithCapability: assign host_admin on
// cluster:* and the create gate passes. (cluster_admin's seed caps
// don't include cluster:create — only Host Admin can mint NEW
// clusters per ADR-0001; once created, cluster_admin owns the
// edit/test/delete loop.) The underlying create still fails downstream
// because the mock store may reject inputs, but the failure is no
// longer at the gate.
func TestAdminCreateCluster_WithCapability(t *testing.T) {
	srv, _, enf, cleanup := newGateTestEnv(t)
	defer cleanup()

	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "admin", RoleID: "host_admin", Scope: "cluster:*",
	}); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"label":  "test-create",
		"driver": "garage-v1",
		"config": map[string]string{"admin_url": "http://x:3903"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters",
		jsonBody(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateAdminToken(),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	// Past the gate: either 201 (mock created it) or some non-403
	// downstream error. We assert NOT 403 to prove the gate let it
	// through.
	if rr.Code == http.StatusForbidden {
		t.Fatalf("gate blocked despite cluster_admin assignment; body=%s",
			rr.Body.String())
	}
}

// TestSeedEnvAdmin_GrantsFourBlankets: SeedEnvAdmin gives the env
// admin host_admin / cluster_admin / bucket_user blanket assignments
// PLUS host_admin @ "*" (true superuser scope, v0.9.0m.1), satisfying
// capabilities at every relevant scope domain — including domains
// added by future cycles (key:*, lifecycle:*, etc.) which the
// per-domain seeds alone don't cover.
func TestSeedEnvAdmin_GrantsFourBlankets(t *testing.T) {
	tmp, err := os.MkdirTemp("", "seed-env-admin-")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer os.RemoveAll(tmp)

	enf, err := policy.Open(tmp)
	if err != nil {
		t.Fatalf("policy.Open: %v", err)
	}
	if err := enf.SeedEnvAdmin("matthew"); err != nil {
		t.Fatalf("SeedEnvAdmin: %v", err)
	}

	cases := []struct {
		cap, scope string
	}{
		// Pre-v0.9.0m.1 coverage (still works).
		{"host:manage_users", "host:*"},
		{"cluster:edit", "cluster:some-cid"},
		{"objects:list", "bucket:some-cid:lsi"},
		// v0.9.0m.1 superuser-scope coverage — these were silently
		// blocked before the * seed because no per-domain assignment
		// matched the key: / bucket:cid:* / objects:cid:bid:* gates
		// minted in v0.9.0f and later cycles.
		{"key:create", "key:some-cid:*"},
		{"key:delete", "key:some-cid:some-kid"},
		{"key:edit_permissions", "key:some-cid:some-kid"},
		{"bucket:create", "bucket:some-cid:*"},
		{"bucket:delete", "bucket:some-cid:some-bid"},
	}
	for _, c := range cases {
		if !enf.Can("matthew", c.cap, c.scope) {
			t.Errorf("expected Can(matthew, %s, %s) = true after SeedEnvAdmin",
				c.cap, c.scope)
		}
	}

	// Idempotent: re-running doesn't error and doesn't duplicate.
	if err := enf.SeedEnvAdmin("matthew"); err != nil {
		t.Errorf("re-SeedEnvAdmin: %v", err)
	}
	assignments := enf.AssignmentsFor("matthew")
	if len(assignments) != 4 {
		t.Errorf("expected 4 assignments after idempotent re-seed, got %d: %#v",
			len(assignments), assignments)
	}

	// One of those four must be host_admin @ "*" — the superuser row
	// is the cycle's whole point. Other tests assert the role/scope
	// combos for the other three.
	var hasSuperuser bool
	for _, a := range assignments {
		if a.RoleID == "host_admin" && a.Scope == "*" {
			hasSuperuser = true
			break
		}
	}
	if !hasSuperuser {
		t.Errorf("v0.9.0m.1 expected host_admin @ \"*\" assignment, missing from %#v", assignments)
	}

	// Empty username is a no-op (preserves tests that don't seed an env admin).
	if err := enf.SeedEnvAdmin(""); err != nil {
		t.Errorf("SeedEnvAdmin(\"\"): %v", err)
	}
}

// --- helpers --------------------------------------------------------

// bodyHasCode checks the response body matches the standard error
// shape and the error code matches.
func bodyHasCode(rr *httptest.ResponseRecorder, code string) bool {
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		return false
	}
	return body.Error.Code == code
}

// jsonBody wraps a byte slice in a bytes.Reader compatible with
// http.NewRequest. Centralised so the EOF semantics stay correct.
func jsonBody(b []byte) io.Reader {
	return bytes.NewReader(b)
}
