// Package api: tests for the per-cluster admin listing handler
// (v1.3.0e CLUSTER.ADMINS).
package api

import (
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

// newClusterAdminsTestEnv builds a Server with a file-backed enforcer
// + a connection store containing one cluster (cid="cluster-x"), and
// grants the calling admin user host_admin @ host:* so the policy gate
// passes. Returns the enforcer so tests can stage extra assignments,
// and the store so tests can stage user records for displayName joins.
func newClusterAdminsTestEnv(t *testing.T) (*Server, policy.Enforcer, *store.Store, func()) {
	t.Helper()

	tmp, err := os.MkdirTemp("", "v130e-cluster-admins-")
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

	conns := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "cluster-x", Label: "cluster-x", Driver: "garage", Config: map[string]string{"admin_url": "http://localhost:3476"}},
		},
	}

	srv := New(cfg, st, conns, nil, nil)
	srv.SetPolicy(enf)

	// The test admin token resolves to UserID="admin"; grant it
	// policy:view_matrix via host_admin @ host:* so the gate passes.
	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "admin", RoleID: "host_admin", Scope: "host:*",
	}); err != nil {
		cleanup()
		t.Fatalf("AssignRole admin: %v", err)
	}

	return srv, enf, st, cleanup
}

// TestListClusterAdmins_ScopedAndWildcard verifies that the handler
// returns BOTH the cluster-specific assignment AND the wildcard
// inherited assignment, marking the wildcard row with inherited=true.
// Assignments scoped to a DIFFERENT cluster are excluded.
func TestListClusterAdmins_ScopedAndWildcard(t *testing.T) {
	srv, enf, _, cleanup := newClusterAdminsTestEnv(t)
	defer cleanup()

	// Manual cluster-scoped assignment (wife as cluster_admin on this cluster).
	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "wife", RoleID: "cluster_admin", Scope: "cluster:cluster-x",
	}); err != nil {
		t.Fatalf("assign wife: %v", err)
	}
	// Wildcard cluster_admin for "globalop" (covers every cluster).
	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "globalop", RoleID: "cluster_admin", Scope: "cluster:*",
	}); err != nil {
		t.Fatalf("assign globalop: %v", err)
	}
	// Different cluster — must NOT appear in cluster-x's response.
	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "stranger", RoleID: "cluster_admin", Scope: "cluster:cluster-other",
	}); err != nil {
		t.Fatalf("assign stranger: %v", err)
	}

	req := adminPolicyReq(http.MethodGet, "/api/v1/admin/clusters/cluster-x/admins", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}

	var resp clusterAdminsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Expect two rows: wife (cluster_admin @ cluster:cluster-x — manual,
	// exact match) and globalop (cluster_admin @ cluster:* — wildcard
	// inherited). The "admin" caller's host_admin @ host:* assignment
	// does NOT match cluster:cluster-x (different scope domain — host:*
	// covers host scope, not cluster scope), and stranger is filtered
	// out for being scoped to a different cluster.
	gotByUser := make(map[string]clusterAdminAssignmentDTO, len(resp.Assignments))
	for _, a := range resp.Assignments {
		gotByUser[a.UserID] = a
	}

	if _, ok := gotByUser["stranger"]; ok {
		t.Errorf("stranger (different cluster) should not be in response; got %+v", resp.Assignments)
	}
	if _, ok := gotByUser["admin"]; ok {
		t.Errorf("admin (host_admin @ host:*) should NOT match cluster:cluster-x; got %+v", resp.Assignments)
	}

	wife, ok := gotByUser["wife"]
	if !ok {
		t.Fatalf("expected wife row; got %+v", resp.Assignments)
	}
	if wife.Inherited {
		t.Errorf("wife scope cluster:cluster-x should be exact match (inherited=false); got inherited=true")
	}
	if wife.Scope != "cluster:cluster-x" {
		t.Errorf("wife scope = %q, want cluster:cluster-x", wife.Scope)
	}

	globalop, ok := gotByUser["globalop"]
	if !ok {
		t.Fatalf("expected globalop row; got %+v", resp.Assignments)
	}
	if !globalop.Inherited {
		t.Errorf("globalop scope cluster:* should be inherited=true; got inherited=false")
	}
}

// TestListClusterAdmins_SuperuserScopeMatches confirms that an
// assignment at the superuser scope ("*") DOES match a requested
// cluster scope — this is how matthew's host_admin @ * seed surfaces
// in the per-cluster view as an inherited row.
func TestListClusterAdmins_SuperuserScopeMatches(t *testing.T) {
	srv, enf, _, cleanup := newClusterAdminsTestEnv(t)
	defer cleanup()

	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "matthew", RoleID: "host_admin", Scope: "*",
	}); err != nil {
		t.Fatalf("assign matthew: %v", err)
	}

	req := adminPolicyReq(http.MethodGet, "/api/v1/admin/clusters/cluster-x/admins", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}

	var resp clusterAdminsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var matthewRow *clusterAdminAssignmentDTO
	for i, a := range resp.Assignments {
		if a.UserID == "matthew" {
			matthewRow = &resp.Assignments[i]
			break
		}
	}
	if matthewRow == nil {
		t.Fatalf("expected matthew row from host_admin @ *; got %+v", resp.Assignments)
	}
	if !matthewRow.Inherited {
		t.Errorf("matthew @ * should be inherited=true; got inherited=false")
	}
}

// TestListClusterAdmins_DisplayNameJoin confirms the handler joins
// the user's record Name field server-side so the FE doesn't need a
// second lookup per row.
func TestListClusterAdmins_DisplayNameJoin(t *testing.T) {
	srv, enf, st, cleanup := newClusterAdminsTestEnv(t)
	defer cleanup()

	if err := st.CreateUser(store.User{
		Username: "wife",
		Name:     "Wife McUser",
		Role:     "user",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "wife", RoleID: "cluster_admin", Scope: "cluster:cluster-x",
	}); err != nil {
		t.Fatalf("assign: %v", err)
	}

	req := adminPolicyReq(http.MethodGet, "/api/v1/admin/clusters/cluster-x/admins", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}

	var resp clusterAdminsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, a := range resp.Assignments {
		if a.UserID == "wife" && a.DisplayName != "Wife McUser" {
			t.Errorf("wife.DisplayName = %q, want %q", a.DisplayName, "Wife McUser")
		}
	}
}

// TestListClusterAdmins_NoCapability returns 403 when the caller is
// not assigned policy:view_matrix on host:*.
func TestListClusterAdmins_NoCapability(t *testing.T) {
	t.Helper()

	tmp, err := os.MkdirTemp("", "v130e-cluster-admins-nocap-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer os.RemoveAll(tmp)

	cfg := newTestConfig()
	cfg.DataDir = tmp

	st, err := store.Open(tmp, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	enf, err := policy.Open(filepath.Join(tmp, "policy"))
	if err != nil {
		t.Fatalf("policy.Open: %v", err)
	}

	conns := &testMockConnectionStore{
		conns: []store.Connection{{ID: "cluster-x", Label: "cluster-x"}},
	}

	srv := New(cfg, st, conns, nil, nil)
	srv.SetPolicy(enf) // no host_admin assignment for "admin"

	req := adminPolicyReq(http.MethodGet, "/api/v1/admin/clusters/cluster-x/admins", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (body=%s)", rr.Code, rr.Body.String())
	}
}

// TestListClusterAdmins_UnknownCluster returns 404 when the cluster
// id does not exist, even with the policy gate satisfied — saves the
// FE from rendering a misleading empty table for a stale link.
func TestListClusterAdmins_UnknownCluster(t *testing.T) {
	srv, _, _, cleanup := newClusterAdminsTestEnv(t)
	defer cleanup()

	req := adminPolicyReq(http.MethodGet, "/api/v1/admin/clusters/ghost-cid/admins", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (body=%s)", rr.Code, rr.Body.String())
	}
}
