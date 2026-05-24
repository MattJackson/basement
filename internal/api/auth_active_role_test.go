// Tests for the active role selector endpoint (v1.13.18).
//
// Tests cover:
//   - Eligibility validation (cluster grants, uiAdmin flag)
//   - 423 LOCKED response when elevation required
//   - Persistence of activeRole in session cookie
//   - /auth/me returns updated activeRole + availableRoles
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/store"
)

// newActiveRoleTestServer builds a minimal Server wired with admin creds and a real file-backed policy enforcer.
func newActiveRoleTestServer(t *testing.T) (*Server, []byte) {
	t.Helper()
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i)
	}
	cfg := &config.Config{
		Listen:     ":0",
		SessionTTL: 24 * time.Hour,
		Admin: config.AdminConfig{
			User:         "admin",
			PasswordHash: elevateTestPasswordHash, // bcrypt("test")
		},
		JWT: config.JWTConfig{Secret: secret},
	}
	st := &store.Store{}
	srv := New(cfg, st, nil, nil, nil)
	
	// Use a real file-backed enforcer for policy assignment tests
	enforcer, err := policy.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open policy enforcer: %v", err)
	}
	srv.SetPolicy(enforcer)
	return srv, secret
}

// TestActiveRoleHandler_HappyPath_UserSwitch: switching to user role is always free.
func TestActiveRoleHandler_HappyPath_UserSwitch(t *testing.T) {
	srv, secret := newActiveRoleTestServer(t)

	// Start as cluster-admin on "classe" (not elevated)
	activeRole := &auth.ActiveRole{Kind: "cluster-admin", Cluster: "classe"}
	tok := mintActiveRoleToken(t, secret, "admin", "admin", true, activeRole)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/active-role", bytes.NewReader([]byte(`{"kind":"user"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok, Path: "/"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp UserResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.ActiveRole == nil || resp.ActiveRole.Kind != "user" {
		t.Errorf("ActiveRole = %+v, want {kind:\"user\"}", resp.ActiveRole)
	}

	// Verify Set-Cookie carries updated token
	var newCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == auth.CookieName {
			newCookie = c
			break
		}
	}
	if newCookie == nil {
		t.Fatal("expected Set-Cookie")
	}
	claims, err := auth.ParseToken(secret, newCookie.Value)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if claims.ActiveRole == nil || claims.ActiveRole.Kind != "user" {
		t.Errorf("new cookie ActiveRole = %+v, want {kind:\"user\"}", claims.ActiveRole)
	}
}

// TestActiveRoleHandler_CrossClusterSwitch: switching between cluster admins requires elevation.
func TestActiveRoleHandler_CrossClusterSwitch(t *testing.T) {
	srv, secret := newActiveRoleTestServer(t)

	// Grant user cluster_admin on both "classe" and "lsi" via policy enforcer
	srv.policy.AssignRole(policy.RoleAssignment{
		UserID:  "admin",
		RoleID:  "cluster_admin",
		Scope:   "cluster:classe",
	})
	srv.policy.AssignRole(policy.RoleAssignment{
		UserID:  "admin",
		RoleID:  "cluster_admin",
		Scope:   "cluster:lsi",
	})

	// Start as cluster-admin on "classe" (not elevated)
	activeRole := &auth.ActiveRole{Kind: "cluster-admin", Cluster: "classe"}
	tok := mintActiveRoleToken(t, secret, "admin", "admin", true, activeRole)

	// Try to switch to "lsi" without elevation -> 423 LOCKED
	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/active-role", bytes.NewReader([]byte(`{"kind":"cluster-admin","cluster":"lsi"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok, Path: "/"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusLocked {
		t.Fatalf("expected 423 LOCKED, got %d body=%s", rr.Code, rr.Body.String())
	}

	var lockResp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &lockResp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !lockResp["requires_elevation"].(bool) {
		t.Errorf("requires_elevation = false, want true")
	}
}

// TestActiveRoleHandler_UIAdminSwitch_ElevationRequired: UI Admin switch requires elevation.
func TestActiveRoleHandler_UIAdminSwitch_ElevationRequired(t *testing.T) {
	srv, secret := newActiveRoleTestServer(t)

	// Start as user (not elevated), but uiAdmin=true
	activeRole := &auth.ActiveRole{Kind: "user"}
	tok := mintActiveRoleToken(t, secret, "admin", "admin", true, activeRole)

	// Try to switch to UI Admin without elevation -> 423 LOCKED
	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/active-role", bytes.NewReader([]byte(`{"kind":"ui-admin"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok, Path: "/"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusLocked {
		t.Fatalf("expected 423 LOCKED, got %d body=%s", rr.Code, rr.Body.String())
	}

	var lockResp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &lockResp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !lockResp["requires_elevation"].(bool) {
		t.Errorf("requires_elevation = false, want true")
	}
}

// TestActiveRoleHandler_UIAdminSwitch_Elevated: UI Admin switch succeeds when already elevated.
func TestActiveRoleHandler_UIAdminSwitch_Elevated(t *testing.T) {
	srv, secret := newActiveRoleTestServer(t)

	// Start as ui-admin but already elevated (mode=admin)
	activeRole := &auth.ActiveRole{Kind: "user"}
	tok, _ := auth.IssueTokenWithActiveRole(secret, "admin", "admin", true, "admin", time.Now().Add(10*time.Minute).Unix(), 24*time.Hour, activeRole)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/active-role", bytes.NewReader([]byte(`{"kind":"ui-admin"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok, Path: "/"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp UserResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.ActiveRole == nil || resp.ActiveRole.Kind != "ui-admin" {
		t.Errorf("ActiveRole = %+v, want {kind:\"ui-admin\"}", resp.ActiveRole)
	}
}

// TestMeHandler_ReturnsActiveRoleAndAvailableRoles: /auth/me returns active role + eligibility list.
func TestMeHandler_ReturnsActiveRoleAndAvailableRoles(t *testing.T) {
	srv, secret := newActiveRoleTestServer(t)

	// Grant user cluster_admin on "classe" and "lsi", and set uiAdmin=true via policy enforcer
	srv.policy.AssignRole(policy.RoleAssignment{
		UserID:  "admin",
		RoleID:  "cluster_admin",
		Scope:   "cluster:classe",
	})
	srv.policy.AssignRole(policy.RoleAssignment{
		UserID:  "admin",
		RoleID:  "cluster_admin",
		Scope:   "cluster:lsi",
	})

	activeRole := &auth.ActiveRole{Kind: "cluster-admin", Cluster: "classe"}
	tok, _ := auth.IssueTokenWithActiveRole(secret, "admin", "admin", true, "user", 0, 24*time.Hour, activeRole)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok, Path: "/"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp UserResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	// Check activeRole
	if resp.ActiveRole == nil || resp.ActiveRole.Kind != "cluster-admin" || resp.ActiveRole.Cluster != "classe" {
		t.Errorf("ActiveRole = %+v, want {kind:\"cluster-admin\", cluster:\"classe\"}", resp.ActiveRole)
	}

	// Check availableRoles has user, both clusters, and ui-admin
	if len(resp.AvailableRoles) < 4 {
		t.Errorf("AvailableRoles length = %d, want at least 4", len(resp.AvailableRoles))
	}

	foundUser := false
	foundClasse := false
	foundLsi := false
	foundUIAdmin := false
	for _, r := range resp.AvailableRoles {
		switch r.Kind {
		case "user":
			foundUser = true
		case "cluster-admin":
			if r.Cluster == "classe" {
				foundClasse = true
			} else if r.Cluster == "lsi" {
				foundLsi = true
			}
		case "ui-admin":
			foundUIAdmin = true
		}
	}

	if !foundUser {
		t.Error("AvailableRoles missing user role")
	}
	if !foundClasse {
		t.Error("AvailableRoles missing cluster:classe")
	}
	if !foundLsi {
		t.Error("AvailableRoles missing cluster:lsi")
	}
	if !foundUIAdmin {
		t.Error("AvailableRoles missing ui-admin role")
	}
}

// TestActiveRoleHandler_InvalidKind: invalid role kind returns 400.
func TestActiveRoleHandler_InvalidKind(t *testing.T) {
	srv, secret := newActiveRoleTestServer(t)

	activeRole := &auth.ActiveRole{Kind: "user"}
	tok := mintActiveRoleToken(t, secret, "admin", "admin", true, activeRole)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/active-role", bytes.NewReader([]byte(`{"kind":"super-admin"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok, Path: "/"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestActiveRoleHandler_ClusterAdminRequiresClusterParam: cluster-admin kind requires cluster parameter.
func TestActiveRoleHandler_ClusterAdminRequiresClusterParam(t *testing.T) {
	srv, secret := newActiveRoleTestServer(t)

	activeRole := &auth.ActiveRole{Kind: "user"}
	tok := mintActiveRoleToken(t, secret, "admin", "admin", true, activeRole)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/active-role", bytes.NewReader([]byte(`{"kind":"cluster-admin"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok, Path: "/"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestActiveRoleHandler_UIAdminFalse: non-ui-admin cannot switch to ui-admin.
func TestActiveRoleHandler_UIAdminFalse(t *testing.T) {
	srv, secret := newActiveRoleTestServer(t)

	// User is NOT a UI admin (uiAdmin=false)
	activeRole := &auth.ActiveRole{Kind: "user"}
	tok := mintActiveRoleToken(t, secret, "admin", "admin", false, activeRole)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/active-role", bytes.NewReader([]byte(`{"kind":"ui-admin"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok, Path: "/"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 FORBIDDEN, got %d body=%s", rr.Code, rr.Body.String())
	}
}
