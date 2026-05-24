// Package api: tests for the /admin/policies matrix editor handlers
// (ADR-0001 cycle v0.9.0g).
//
// The tests focus on the three behaviours the UI relies on:
//
//   1. GET returns capabilities (the registry), roles (seed + custom),
//      and assignments — gated on policy:view_matrix.
//   2. UpsertRole + AssignRole + UnassignRole round-trip cleanly,
//      with the gate (policy:edit_matrix / policy:assign_role) checked.
//   3. DeleteRole refuses seed roles with 409 ROLE_SEED so the UI's
//      "Delete" button stays disabled with a tooltip and the backend
//      stays consistent if a malicious client bypasses the UI gate.
//
// Each test installs a real file-backed enforcer at a temp dir and
// assigns the calling admin user host_admin @ host:* so the policy
// gates pass. Tests that exercise the gate's negative path skip the
// assignment.
package api

import (
	"bytes"
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

// newPolicyTestEnv builds a Server with a real file-backed enforcer
// and (optionally) a host_admin assignment for the calling admin
// token's UserID. The admin token in tests is issued with UserID
// "admin" (see generateAdminToken) so the seed assignment matches.
func newPolicyTestEnv(t *testing.T, grantHostAdmin bool) (*Server, policy.Enforcer, func()) {
	t.Helper()

	tmp, err := os.MkdirTemp("", "v090g-policy-")
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

	srv := New(cfg, st, &testMockConnectionStore{}, nil, nil)
	srv.SetPolicy(enf)

	if grantHostAdmin {
		if err := enf.AssignRole(policy.RoleAssignment{
			UserID: "admin", RoleID: "host_admin", Scope: "host:*",
		}); err != nil {
			cleanup()
			t.Fatalf("AssignRole: %v", err)
		}
	}

	return srv, enf, cleanup
}

// adminPolicyReq builds an admin-authenticated request with an
// optional JSON body for the policies API.
func adminPolicyReq(method, url string, body []byte) *http.Request {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, url, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, url, nil)
	}
	r.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateAdminToken(),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	return r
}

func TestListPolicies_OK(t *testing.T) {
	srv, _, cleanup := newPolicyTestEnv(t, true)
	defer cleanup()

	req := adminPolicyReq(http.MethodGet, "/api/v1/admin/policies", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}

	var resp policiesResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// At least the seed capabilities should be present.
	if len(resp.Capabilities) < len(policy.Registry) {
		t.Errorf("expected >=%d capabilities, got %d",
			len(policy.Registry), len(resp.Capabilities))
	}
	// Seed roles: host_admin, cluster_admin.
	if len(resp.Roles) < 2 {
		t.Errorf("expected >=2 seed roles, got %d", len(resp.Roles))
	}
}

func TestListPolicies_NoCapability(t *testing.T) {
	srv, _, cleanup := newPolicyTestEnv(t, false)
	defer cleanup()

	req := adminPolicyReq(http.MethodGet, "/api/v1/admin/policies", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (no host_admin assignment), got %d (body=%s)",
			rr.Code, rr.Body.String())
	}
}

func TestUpsertRole_RoundTrip(t *testing.T) {
	srv, enf, cleanup := newPolicyTestEnv(t, true)
	defer cleanup()

	body, _ := json.Marshal(policy.Role{
		ID:           "viewer",
		Label:        "Viewer",
		Description:  "Read-only across buckets.",
		Capabilities: []string{"bucket:view", "objects:list"},
	})
	req := adminPolicyReq(http.MethodPost, "/api/v1/admin/policies/roles", body)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("upsert expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}

	roles := enf.Roles()
	found := false
	for _, r := range roles {
		if r.ID == "viewer" {
			found = true
			if r.Seed {
				t.Error("custom role should not be seeded")
			}
		}
	}
	if !found {
		t.Errorf("custom role not persisted in enforcer; roles=%+v", roles)
	}
}

func TestUpsertRole_BadCapability(t *testing.T) {
	srv, _, cleanup := newPolicyTestEnv(t, true)
	defer cleanup()

	body, _ := json.Marshal(policy.Role{
		ID:           "bad",
		Label:        "Bad",
		Capabilities: []string{"nope:not_a_thing"},
	})
	req := adminPolicyReq(http.MethodPost, "/api/v1/admin/policies/roles", body)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 ROLE_INVALID, got %d (body=%s)",
			rr.Code, rr.Body.String())
	}
}

func TestDeleteRole_RefusesSeed(t *testing.T) {
	srv, _, cleanup := newPolicyTestEnv(t, true)
	defer cleanup()

	req := adminPolicyReq(http.MethodDelete,
		"/api/v1/admin/policies/roles/host_admin", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 ROLE_SEED, got %d (body=%s)",
			rr.Code, rr.Body.String())
	}
	if !bodyHasCode(rr, "ROLE_SEED") {
		t.Errorf("expected ROLE_SEED code; body=%s", rr.Body.String())
	}
}

func TestDeleteRole_CustomOK(t *testing.T) {
	srv, enf, cleanup := newPolicyTestEnv(t, true)
	defer cleanup()

	if err := enf.UpsertRole(policy.Role{
		ID:           "temp",
		Label:        "Temp",
		Capabilities: []string{"bucket:view"},
	}); err != nil {
		t.Fatalf("seed temp role: %v", err)
	}

	req := adminPolicyReq(http.MethodDelete,
		"/api/v1/admin/policies/roles/temp", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	for _, r := range enf.Roles() {
		if r.ID == "temp" {
			t.Error("temp role still present after DELETE")
		}
	}
}

func TestAssignAndUnassignRole(t *testing.T) {
	srv, enf, cleanup := newPolicyTestEnv(t, true)
	defer cleanup()

	body, _ := json.Marshal(policy.RoleAssignment{
		UserID: "wife", RoleID: "cluster_admin",
		Scope: "bucket:cid-x:family-photos",
	})
	req := adminPolicyReq(http.MethodPost,
		"/api/v1/admin/policies/assignments", body)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("assign expected 201, got %d (body=%s)", rr.Code, rr.Body.String())
	}

	// Verify the enforcer agrees.
	if !enf.Can("wife", "bucket:view", "bucket:cid-x:family-photos") {
		t.Error("Can(wife, bucket:view, bucket:cid-x:family-photos) = false after assign")
	}

	// Revoke it.
	delReq := adminPolicyReq(http.MethodDelete,
		"/api/v1/admin/policies/assignments?userId=wife&roleId=cluster_admin&scope=bucket:cid-x:family-photos",
		nil)
	delRR := httptest.NewRecorder()
	srv.router.ServeHTTP(delRR, delReq)

	if delRR.Code != http.StatusNoContent {
		t.Fatalf("unassign expected 204, got %d (body=%s)", delRR.Code, delRR.Body.String())
	}
	if enf.Can("wife", "objects:list", "bucket:cid-x:family-photos") {
		t.Error("Can(wife, ...) still true after unassign")
	}
}

func TestAssignRole_UnknownRole(t *testing.T) {
	srv, _, cleanup := newPolicyTestEnv(t, true)
	defer cleanup()

	body, _ := json.Marshal(policy.RoleAssignment{
		UserID: "wife", RoleID: "ghost", Scope: "bucket:cid-x:lsi",
	})
	req := adminPolicyReq(http.MethodPost,
		"/api/v1/admin/policies/assignments", body)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 ROLE_NOT_FOUND, got %d (body=%s)",
			rr.Code, rr.Body.String())
	}
}
