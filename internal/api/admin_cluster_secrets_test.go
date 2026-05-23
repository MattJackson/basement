// Package api: tests for the v1.12.0a per-cluster envelope encryption
// HTTP surface (ADR-0007).

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/clustersecret"
	"github.com/mattjackson/basement/internal/store"
)

// newClusterSecretsTestEnv builds a Server wired with:
//   - one Connection ("cluster-x") in a mock store
//   - a permissive enforcer that grants the calling admin
//     cluster:edit and cluster:test on the cluster
//   - a clustersecret manager backed by MemoryStore
//
// Returns the server + manager so individual tests can inspect/mutate
// state directly when needed.
func newClusterSecretsTestEnv(t *testing.T) (*Server, *clustersecret.ClusterSecretManager) {
	t.Helper()

	cfg := newTestConfig()
	conns := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "cluster-x", Label: "cluster-x", Driver: "garage",
				Config: map[string]string{"admin_url": "http://localhost:3476"}},
		},
	}
	srv := New(cfg, nil, conns, nil, nil)

	// File-backed enforcer in a tempdir; grant the admin user
	// host_admin @ host:* so all admin-scoped capabilities pass.
	enf, err := policy.Open(t.TempDir())
	if err != nil {
		t.Fatalf("policy.Open: %v", err)
	}
	// host_admin @ "*" is the superuser scope used by every-capability
	// admin tests; covers cluster:edit + cluster:test on every cluster
	// (the gates the CSK handlers enforce). See policy.SeedEnvAdmin.
	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "admin", RoleID: "host_admin", Scope: "*",
	}); err != nil {
		t.Fatalf("AssignRole admin: %v", err)
	}
	srv.SetPolicy(enf)

	mgr := clustersecret.New(clustersecret.NewMemoryStore())
	srv.SetClusterSecrets(mgr)
	return srv, mgr
}

// adminReq builds an admin-authed httptest request with JSON body.
func adminReq(t *testing.T, method, url string, body any) *http.Request {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(data)
	}
	var req *http.Request
	if rdr == nil {
		req = httptest.NewRequest(method, url, nil)
	} else {
		req = httptest.NewRequest(method, url, rdr)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateAdminToken(),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	return req
}

// TestAddAdmin_BootstrapFirstAdmin: POSTing /admins to a fresh cluster
// generates a CSK, wraps it under the supplied password, and leaves
// the cluster unlocked.
func TestAddAdmin_BootstrapFirstAdmin(t *testing.T) {
	srv, mgr := newClusterSecretsTestEnv(t)

	req := adminReq(t, http.MethodPost,
		"/api/v1/admin/clusters/cluster-x/admins",
		map[string]string{"adminUserId": "matthew", "password": "hunter2"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status: want 201 got %d: body=%s", rr.Code, rr.Body.String())
	}
	if !mgr.IsUnlocked("cluster-x") {
		t.Fatalf("cluster should be unlocked after first-admin bootstrap")
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["bootstrap"] != true {
		t.Fatalf("expected bootstrap=true in response, got %v", body)
	}
}

// TestAddAdmin_SecondRequiresUnlock: adding a second admin while the
// cluster is locked returns 423 LOCKED.
func TestAddAdmin_SecondRequiresUnlock(t *testing.T) {
	srv, mgr := newClusterSecretsTestEnv(t)
	if err := mgr.BootstrapFirstAdmin("cluster-x", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	mgr.Lock("cluster-x")

	req := adminReq(t, http.MethodPost,
		"/api/v1/admin/clusters/cluster-x/admins",
		map[string]string{"adminUserId": "wife", "password": "eggcream"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusLocked {
		t.Fatalf("status: want 423 got %d: body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "LOCKED") {
		t.Fatalf("body should mention LOCKED: %s", rr.Body.String())
	}
}

// TestAddAdmin_SecondSucceedsWhenUnlocked: with the cluster already
// unlocked, adding a second admin returns 201 and the new admin can
// unlock independently.
func TestAddAdmin_SecondSucceedsWhenUnlocked(t *testing.T) {
	srv, mgr := newClusterSecretsTestEnv(t)
	if err := mgr.BootstrapFirstAdmin("cluster-x", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}

	req := adminReq(t, http.MethodPost,
		"/api/v1/admin/clusters/cluster-x/admins",
		map[string]string{"adminUserId": "wife", "password": "eggcream"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status: want 201 got %d: body=%s", rr.Code, rr.Body.String())
	}

	// Lock, then verify wife can unlock with her own password.
	mgr.Lock("cluster-x")
	if err := mgr.Unlock("cluster-x", "eggcream"); err != nil {
		t.Fatalf("wife unlock after add: %v", err)
	}
}

// TestUnlock_CorrectPassword: unlock with the right password returns 200
// and flips the in-memory state.
func TestUnlock_CorrectPassword(t *testing.T) {
	srv, mgr := newClusterSecretsTestEnv(t)
	if err := mgr.BootstrapFirstAdmin("cluster-x", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	mgr.Lock("cluster-x")

	req := adminReq(t, http.MethodPost,
		"/api/v1/admin/clusters/cluster-x/unlock",
		map[string]string{"password": "hunter2"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200 got %d: body=%s", rr.Code, rr.Body.String())
	}
	if !mgr.IsUnlocked("cluster-x") {
		t.Fatalf("unlock should have put CSK in memory")
	}
}

// TestUnlock_WrongPassword: 401 INVALID_PASSWORD on bad password.
func TestUnlock_WrongPassword(t *testing.T) {
	srv, mgr := newClusterSecretsTestEnv(t)
	if err := mgr.BootstrapFirstAdmin("cluster-x", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	mgr.Lock("cluster-x")

	req := adminReq(t, http.MethodPost,
		"/api/v1/admin/clusters/cluster-x/unlock",
		map[string]string{"password": "wrong"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401 got %d: body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "INVALID_PASSWORD") {
		t.Fatalf("body should mention INVALID_PASSWORD: %s", rr.Body.String())
	}
}

// TestUnlock_NoCSKAdmin: 404 when the cluster has never had CSK set up.
func TestUnlock_NoCSKAdmin(t *testing.T) {
	srv, _ := newClusterSecretsTestEnv(t)

	req := adminReq(t, http.MethodPost,
		"/api/v1/admin/clusters/cluster-x/unlock",
		map[string]string{"password": "anything"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status: want 404 got %d: body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "NO_CSK_ADMIN") {
		t.Fatalf("body should mention NO_CSK_ADMIN: %s", rr.Body.String())
	}
}

// TestLock_Idempotent: lock returns 204 even on an already-locked cluster.
func TestLock_Idempotent(t *testing.T) {
	srv, mgr := newClusterSecretsTestEnv(t)
	if err := mgr.BootstrapFirstAdmin("cluster-x", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}

	for i := 0; i < 2; i++ {
		req := adminReq(t, http.MethodPost,
			"/api/v1/admin/clusters/cluster-x/lock", nil)
		rr := httptest.NewRecorder()
		srv.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("lock #%d: want 204 got %d body=%s", i, rr.Code, rr.Body.String())
		}
		if mgr.IsUnlocked("cluster-x") {
			t.Fatalf("should be locked after lock #%d", i)
		}
	}
}

// TestLockStatus_ReflectsManagerState: GET /lock-status reports
// unlocked/hasCsk/admins consistently across the lifecycle.
func TestLockStatus_ReflectsManagerState(t *testing.T) {
	srv, mgr := newClusterSecretsTestEnv(t)

	// 1. No CSK at all.
	{
		req := adminReq(t, http.MethodGet,
			"/api/v1/admin/clusters/cluster-x/lock-status", nil)
		rr := httptest.NewRecorder()
		srv.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status: want 200 got %d: body=%s", rr.Code, rr.Body.String())
		}
		var body lockStatusResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body.Unlocked || body.HasCSK || len(body.Admins) != 0 {
			t.Fatalf("fresh cluster status mismatch: %+v", body)
		}
	}

	// 2. Bootstrap → unlocked + hasCsk + admins=["matthew"].
	if err := mgr.BootstrapFirstAdmin("cluster-x", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	{
		req := adminReq(t, http.MethodGet,
			"/api/v1/admin/clusters/cluster-x/lock-status", nil)
		rr := httptest.NewRecorder()
		srv.router.ServeHTTP(rr, req)
		var body lockStatusResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !body.Unlocked || !body.HasCSK || len(body.Admins) != 1 || body.Admins[0] != "matthew" {
			t.Fatalf("post-bootstrap status mismatch: %+v", body)
		}
	}

	// 3. Lock → unlocked=false but hasCsk=true (admins persist).
	mgr.Lock("cluster-x")
	{
		req := adminReq(t, http.MethodGet,
			"/api/v1/admin/clusters/cluster-x/lock-status", nil)
		rr := httptest.NewRecorder()
		srv.router.ServeHTTP(rr, req)
		var body lockStatusResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body.Unlocked {
			t.Fatalf("expected locked after Lock(): %+v", body)
		}
		if !body.HasCSK {
			t.Fatalf("expected hasCsk=true (admins persist): %+v", body)
		}
	}
}

// TestRemoveAdmin_RemovesRecord: DELETE /admins/{user} returns 204
// and the admin no longer appears in the status response.
func TestRemoveAdmin_RemovesRecord(t *testing.T) {
	srv, mgr := newClusterSecretsTestEnv(t)
	if err := mgr.BootstrapFirstAdmin("cluster-x", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	if err := mgr.AddAdmin("cluster-x", "wife", "eggcream"); err != nil {
		t.Fatalf("AddAdmin: %v", err)
	}

	req := adminReq(t, http.MethodDelete,
		"/api/v1/admin/clusters/cluster-x/admins/wife", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status: want 204 got %d: body=%s", rr.Code, rr.Body.String())
	}

	admins, err := mgr.ListAdmins("cluster-x")
	if err != nil {
		t.Fatalf("ListAdmins: %v", err)
	}
	if len(admins) != 1 || admins[0] != "matthew" {
		t.Fatalf("ListAdmins after remove: %v", admins)
	}
}

// TestNotWiredReturns503: when SetClusterSecrets is never called,
// CSK handlers return 503 CLUSTER_SECRETS_NOT_WIRED.
func TestNotWiredReturns503(t *testing.T) {
	cfg := newTestConfig()
	conns := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "cluster-x", Label: "cluster-x", Driver: "garage",
				Config: map[string]string{"admin_url": "http://localhost:3476"}},
		},
	}
	srv := New(cfg, nil, conns, nil, nil)

	enf, err := policy.Open(t.TempDir())
	if err != nil {
		t.Fatalf("policy.Open: %v", err)
	}
	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "admin", RoleID: "host_admin", Scope: "*",
	}); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	srv.SetPolicy(enf)
	// Deliberately NOT calling srv.SetClusterSecrets.

	req := adminReq(t, http.MethodPost,
		"/api/v1/admin/clusters/cluster-x/unlock",
		map[string]string{"password": "anything"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: want 503 got %d: body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "CLUSTER_SECRETS_NOT_WIRED") {
		t.Fatalf("body should mention CLUSTER_SECRETS_NOT_WIRED: %s", rr.Body.String())
	}
}
