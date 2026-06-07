// Package api: tests for the admin user handlers (audit r10).
//
// Covers the two functionally-broken handlers the audit flagged:
//   - deleteUserHandler read ?id= (query) but the route is /{id}
//     (path) — every chi-routed delete 400'd. Now reads the path param
//     and resolves username|UUID → store ID.
//   - createUserHandler's InviteOnly branch leaked the bcrypt
//     HashedToken AND never persisted the token (un-redeemable). Now
//     persists a real invite via the store and returns the plaintext
//     once with no hash.
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/store"
)

func newAdminUsersTestEnv(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	tmp := t.TempDir()

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

	srv := New(cfg, st, &testMockConnectionStore{}, nil, nil)
	srv.SetPolicy(enf)

	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "admin", RoleID: "host_admin", Scope: "host:*",
	}); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	return srv, st
}

func adminUsersCookie() *http.Cookie {
	return &http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateUIAdminToken(),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
}

// TestDeleteUser_ByPathParam_Username: DELETE /admin/users/{username}
// resolves the username to the store ID and removes the account. The
// previous query-param read always 400'd.
func TestDeleteUser_ByPathParam_Username(t *testing.T) {
	srv, st := newAdminUsersTestEnv(t)

	if err := st.CreateUser(store.User{ID: "u-1", Username: "alice", Role: "user", PasswordHash: "x"}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/alice", nil)
	req.AddCookie(adminUsersCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s; want 204", rr.Code, rr.Body.String())
	}
	if _, err := st.UserByUsername("alice"); err == nil {
		t.Error("user alice should have been deleted")
	}
}

// TestDeleteUser_ByPathParam_UUID: the same handler accepts the store
// UUID directly.
func TestDeleteUser_ByPathParam_UUID(t *testing.T) {
	srv, st := newAdminUsersTestEnv(t)

	if err := st.CreateUser(store.User{ID: "u-uuid-2", Username: "bob", Role: "user", PasswordHash: "x"}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/u-uuid-2", nil)
	req.AddCookie(adminUsersCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s; want 204", rr.Code, rr.Body.String())
	}
	if _, err := st.UserByID("u-uuid-2"); err == nil {
		t.Error("user bob should have been deleted")
	}
}

// TestDeleteUser_NotFound: an unknown id 404s rather than silently
// succeeding.
func TestDeleteUser_NotFound(t *testing.T) {
	srv, _ := newAdminUsersTestEnv(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/ghost", nil)
	req.AddCookie(adminUsersCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s; want 404", rr.Code, rr.Body.String())
	}
}

// TestCreateUser_InviteOnly_PersistsAndNoHashLeak: the InviteOnly path
// persists a redeemable invite, returns the plaintext token once, and
// NEVER returns the bcrypt hash. No user is pre-created (the account is
// minted at redeem time).
func TestCreateUser_InviteOnly_PersistsAndNoHashLeak(t *testing.T) {
	srv, st := newAdminUsersTestEnv(t)

	body := map[string]interface{}{"username": "carol", "inviteOnly": true}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminUsersCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s; want 201", rr.Code, rr.Body.String())
	}

	raw := rr.Body.String()
	// The bcrypt hash must never cross the wire.
	for _, banned := range []string{"hashedToken", "HashedToken", "passwordHash", "$2a$", "$2b$"} {
		if containsStr(raw, banned) {
			t.Errorf("response leaked %q: %s", banned, raw)
		}
	}

	var resp struct {
		Invite createInviteResponse `json:"invite"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Invite.Token == "" {
		t.Error("expected plaintext invite token in response")
	}
	if resp.Invite.Invite.ID == "" {
		t.Error("expected persisted invite ID in response")
	}

	// The token must be persisted (redeemable). No user pre-created.
	list, _ := st.Invites().List()
	if len(list) != 1 {
		t.Errorf("expected 1 persisted invite, got %d", len(list))
	}
	if _, err := st.UserByUsername("carol"); err == nil {
		t.Error("invite-only path should NOT pre-create the user account")
	}

	// The returned plaintext must actually redeem against the store.
	if _, err := st.Invites().Redeem(resp.Invite.Token); err != nil {
		t.Errorf("returned token should be redeemable, got %v", err)
	}
}

// TestCreateUser_Direct_RequiresPassword: a non-invite create with no
// password 400s.
func TestCreateUser_Direct_RequiresPassword(t *testing.T) {
	srv, _ := newAdminUsersTestEnv(t)

	body := map[string]interface{}{"username": "dave"}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminUsersCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s; want 400", rr.Code, rr.Body.String())
	}
}

// TestCreateUser_Direct_HappyPath: a non-invite create with a password
// creates the account and returns no hash.
func TestCreateUser_Direct_HappyPath(t *testing.T) {
	srv, st := newAdminUsersTestEnv(t)

	body := map[string]interface{}{"username": "erin", "password": "hunter2hunter2"}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminUsersCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s; want 201", rr.Code, rr.Body.String())
	}
	if containsStr(rr.Body.String(), "passwordHash") {
		t.Errorf("create response leaked passwordHash: %s", rr.Body.String())
	}
	u, err := st.UserByUsername("erin")
	if err != nil {
		t.Fatalf("user erin should exist: %v", err)
	}
	if u.PasswordHash == "" {
		t.Error("created user should have a password hash persisted")
	}
}

func containsStr(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
