// Package api: tests for the v0.9.0f capability gates.
//
// Admin-side: a non-admin user with no host_admin / cluster_admin
// assignment gets a 403 FORBIDDEN when they try a mutating admin op
// (createBucket / createCluster). With the matching capability, the
// request passes the gate.
//
// The legacy admin-role middleware would already reject a non-admin
// caller on /admin/* with 403 INSUFFICIENT_ROLE — the new gate sits
// behind it as defense in depth, so for admin-side capability tests we
// use an admin-role token but no policy assignment.
//
// User-side gate tests retired with ADR-0002 v1.1.0e — the legacy
// per-bucket capability path lived on /user/clusters/{cid}/buckets/...
// which no longer exists. The region-tier replacement gates via the
// owner-check in requireOwnedRegion (see user_regions_test.go).
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

	"github.com/mattjackson/basement/internal/auth"
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

// TestSeedEnvAdmin_GrantsThreeBlankets: SeedEnvAdmin gives the env
// admin host_admin / cluster_admin blanket assignments PLUS host_admin
// @ "*" (true superuser scope, v0.9.0m.1), satisfying capabilities at
// every relevant scope domain — including domains added by future
// cycles (key:*, lifecycle:*, etc.) which the per-domain seeds alone
// don't cover.
//
// ADR-0002 (v1.1.0f) dropped the fourth `bucket_user @ bucket:*` seed
// because the bucket_user role retired post-region-keychain. The
// host_admin @ "*" superuser row still covers any bucket-scoped check
// the env admin needs, so removing the bucket_user seed doesn't
// regress any gate (verified by the per-domain Can checks below
// continuing to pass).
func TestSeedEnvAdmin_GrantsThreeBlankets(t *testing.T) {
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
	if len(assignments) != 3 {
		t.Errorf("expected 3 assignments after idempotent re-seed, got %d: %#v",
			len(assignments), assignments)
	}

	// One of those three must be host_admin @ "*" — the superuser row
	// is the cycle's whole point. Other tests assert the role/scope
	// combos for the other two.
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

	// ADR-0002 (v1.1.0f): SeedEnvAdmin must NOT create a new
	// bucket_user @ bucket:* assignment on a fresh deployment. Existing
	// rows (back-compat) stay via AssignRole's idempotency, but a clean
	// install starts without one. The role exists deprecated; new
	// installs simply don't bind matthew to it.
	for _, a := range assignments {
		if a.RoleID == "bucket_user" {
			t.Errorf("v1.1.0f: SeedEnvAdmin must not create bucket_user assignments on fresh deploys; got %#v", a)
		}
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

// ---- ADR-0003 mode-gate tests (v1.2.0a) ----------------------------
//
// These exercise the requireCapability extension that adds a
// MinModeFor(capability) check on top of the existing Can() check.
// USER mode hitting an ELEVATED-required capability gets 403
// ELEVATION_REQUIRED with the structured payload the FE consumes;
// ELEVATED mode hits the same capability cleanly.
//
// We call requireCapability directly (rather than going through a
// chi route) because the registered admin handlers all check
// cluster:create / bucket:create — caps which default to ADMIN, not
// ELEVATED. Direct calls let us prove the gate's branching for the
// exact capability classes the ADR catalogues, without inventing a
// fake handler that wires a destructive cap.

// newModeGateEnv extends newGateTestEnv by also returning the JWT
// secret so callers can mint tokens with explicit modes.
func newModeGateEnv(t *testing.T) (*Server, policy.Enforcer, []byte, func()) {
	t.Helper()
	srv, _, enf, cleanup := newGateTestEnv(t)
	return srv, enf, srv.cfg.JWT.Secret, cleanup
}

// callRequireCapabilityWithMode mints a token at the given mode,
// stuffs it into a request, runs the auth middleware, then invokes
// requireCapability against the resulting context. Returns the
// recorder + the (userID, ok) the gate returned.
func callRequireCapabilityWithMode(t *testing.T, srv *Server, secret []byte, mode string, modeExpiresAtUnix int64, capability, scope string) (*httptest.ResponseRecorder, string, bool) {
	t.Helper()

	// Mint a fresh token at the requested mode.
	var tok string
	var err error
	if mode == "" {
		tok, err = auth.IssueToken(secret, "matthew", "admin", true, 24*time.Hour)
	} else {
		tok, err = auth.IssueTokenWithMode(secret, "matthew", "admin", true,
			mode, modeExpiresAtUnix, 24*time.Hour)
	}
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok, Path: "/"})
	rr := httptest.NewRecorder()

	// Run through auth middleware so claims land in context.
	var capturedUID string
	var capturedOK bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUID, capturedOK = srv.requireCapability(w, r, capability, scope)
	})
	auth.Middleware(secret)(handler).ServeHTTP(rr, req)
	return rr, capturedUID, capturedOK
}

// TestRequireCapability_UserMode_ElevatedCapability_403: a user with
// the Can() bit for cluster:delete still hits 403 ELEVATION_REQUIRED
// when their session mode is USER. Response carries the structured
// payload the FE uses to pop the elevation modal in-line.
func TestRequireCapability_UserMode_ElevatedCapability_403(t *testing.T) {
	srv, enf, secret, cleanup := newModeGateEnv(t)
	defer cleanup()

	// Give matthew the policy to delete clusters — but the gate
	// should STILL block him because his session mode is USER.
	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "matthew", RoleID: "host_admin", Scope: "*",
	}); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	rr, _, ok := callRequireCapabilityWithMode(t, srv, secret, "user", 0,
		"cluster:delete", "cluster:abc")

	if ok {
		t.Fatal("expected gate to short-circuit; got ok=true")
	}
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rr.Code, rr.Body.String())
	}

	var er struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &er); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if er.Error.Code != "ELEVATION_REQUIRED" {
		t.Errorf("error code = %q, want ELEVATION_REQUIRED", er.Error.Code)
	}
	if got := er.Error.Details["mode_required"]; got != "elevated" {
		t.Errorf("details.mode_required = %v, want elevated", got)
	}
	if got := er.Error.Details["current_mode"]; got != "user" {
		t.Errorf("details.current_mode = %v, want user", got)
	}
	if got := er.Error.Details["endpoint"]; got != "/api/v1/auth/elevate" {
		t.Errorf("details.endpoint = %v, want /api/v1/auth/elevate", got)
	}
}

// TestRequireCapability_ElevatedMode_ElevatedCapability_Passes: same
// capability, but session is ELEVATED with a future expiry → gate
// passes and returns the userID.
func TestRequireCapability_ElevatedMode_ElevatedCapability_Passes(t *testing.T) {
	srv, enf, secret, cleanup := newModeGateEnv(t)
	defer cleanup()

	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "matthew", RoleID: "host_admin", Scope: "*",
	}); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	expiresAt := time.Now().Add(5 * time.Minute).Unix()
	rr, uid, ok := callRequireCapabilityWithMode(t, srv, secret,
		"elevated", expiresAt, "cluster:delete", "cluster:abc")

	if !ok {
		t.Fatalf("expected gate to pass; got 403 body=%s", rr.Body.String())
	}
	if uid != "matthew" {
		t.Errorf("returned userID = %q, want matthew", uid)
	}
}

// TestRequireCapability_UserMode_UserCapability_Passes: a USER mode
// caller exercising a USER-min capability (objects:list) passes the
// gate cleanly — no elevation prompt for read ops.
func TestRequireCapability_UserMode_UserCapability_Passes(t *testing.T) {
	srv, enf, secret, cleanup := newModeGateEnv(t)
	defer cleanup()

	// objects:list at any scope: give matthew the policy-level
	// grant via host_admin@*. The mode gate is what we're really
	// testing; the policy.Can side must pass independently.
	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "matthew", RoleID: "host_admin", Scope: "*",
	}); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	rr, uid, ok := callRequireCapabilityWithMode(t, srv, secret,
		"user", 0, "objects:list", "bucket:abc:lsi")

	if !ok {
		t.Fatalf("expected gate to pass; got %d body=%s", rr.Code, rr.Body.String())
	}
	if uid != "matthew" {
		t.Errorf("returned userID = %q, want matthew", uid)
	}
}

// TestRequireCapability_AdminMode_AdminCapability_Passes: ADMIN
// session on an ADMIN-min cap (cluster:edit) — the most common admin
// flow. Confirms the default branch of MinModeFor (everything not on
// the USER or ELEVATED list) plays nicely with a freshly-elevated
// session.
func TestRequireCapability_AdminMode_AdminCapability_Passes(t *testing.T) {
	srv, enf, secret, cleanup := newModeGateEnv(t)
	defer cleanup()

	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "matthew", RoleID: "host_admin", Scope: "*",
	}); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	expiresAt := time.Now().Add(15 * time.Minute).Unix()
	rr, _, ok := callRequireCapabilityWithMode(t, srv, secret,
		"admin", expiresAt, "cluster:edit", "cluster:abc")
	if !ok {
		t.Fatalf("expected gate to pass; got %d body=%s", rr.Code, rr.Body.String())
	}
}
