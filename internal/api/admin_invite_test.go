// Package api: tests for the persistent invite-token endpoints
// (v1.3.0d). Covers create + redeem + revoke + rotate + expiry.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/store"
)

// newInvitesTestEnv builds a Server with a real store + enforcer and
// (optionally) the host_admin assignment needed for the
// host:manage_users gate to pass on the /admin/invites surface.
func newInvitesTestEnv(t *testing.T, grant bool) (*Server, *store.Store, func()) {
	t.Helper()

	tmp, err := os.MkdirTemp("", "v130d-invites-")
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

	if grant {
		if err := enf.AssignRole(policy.RoleAssignment{
			UserID: "admin", RoleID: "host_admin", Scope: "host:*",
		}); err != nil {
			cleanup()
			t.Fatalf("AssignRole: %v", err)
		}
	}

	return srv, st, cleanup
}

func TestInvites_CreatePersistsAndReturnsPlaintext(t *testing.T) {
	srv, st, cleanup := newInvitesTestEnv(t, true)
	defer cleanup()

	body := map[string]interface{}{"label": "wife", "ttlHours": 24}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/invites", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateUIAdminToken(),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var resp createInviteResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Token == "" {
		t.Error("expected plaintext token in response")
	}
	if resp.Invite.ID == "" {
		t.Error("expected non-empty invite ID")
	}
	if resp.Invite.Label != "wife" {
		t.Errorf("Label=%q, want 'wife'", resp.Invite.Label)
	}
	// Persisted in store
	list, _ := st.Invites().List()
	if len(list) != 1 {
		t.Errorf("expected 1 invite in store, got %d", len(list))
	}
}

func TestInvites_Redeem_HappyPath_CreatesUser(t *testing.T) {
	srv, st, cleanup := newInvitesTestEnv(t, true)
	defer cleanup()

	// First, create an invite via the persisted store directly (so we
	// have the plaintext to redeem with).
	inv, plain, err := st.Invites().Create("partner", "matthew", 24*time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = inv

	body := map[string]string{"password": "verysecure123"}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/invites/"+plain+"/redeem", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var resp UserResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Username == "" {
		t.Error("expected non-empty username")
	}
	// Should have used the label
	if resp.Username != "partner" {
		t.Errorf("expected username=partner (from label), got %q", resp.Username)
	}

	// Invite is one-shot — gone from store
	list, _ := st.Invites().List()
	if len(list) != 0 {
		t.Errorf("expected invite consumed, %d remain", len(list))
	}

	// User actually persisted
	if _, err := st.UserByUsername("partner"); err != nil {
		t.Errorf("expected user 'partner' created, got %v", err)
	}

	// Second redemption with the same token must fail
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/invites/"+plain+"/redeem", bytes.NewReader(data))
	req2.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on re-redeem, got %d body=%s", rr2.Code, rr2.Body.String())
	}
}

func TestInvites_Redeem_BogusToken_Fails(t *testing.T) {
	srv, _, cleanup := newInvitesTestEnv(t, true)
	defer cleanup()

	body := map[string]string{"password": "verysecure123"}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/invites/not-a-real-token/redeem", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestInvites_Revoke_RemovesAndBlocksRedeem(t *testing.T) {
	srv, st, cleanup := newInvitesTestEnv(t, true)
	defer cleanup()

	inv, plain, err := st.Invites().Create("father", "matthew", 24*time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/invites/"+inv.ID, nil)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateUIAdminToken(),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("revoke status=%d body=%s", rr.Code, rr.Body.String())
	}

	// Subsequent redeem with the same plaintext fails
	body, _ := json.Marshal(map[string]string{"password": "verysecure123"})
	rrR := httptest.NewRecorder()
	reqR := httptest.NewRequest(http.MethodPost, "/api/v1/invites/"+plain+"/redeem", bytes.NewReader(body))
	reqR.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rrR, reqR)
	if rrR.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 after revoke, got %d", rrR.Code)
	}
}

func TestInvites_Rotate_ReplacesToken(t *testing.T) {
	srv, st, cleanup := newInvitesTestEnv(t, true)
	defer cleanup()

	inv, oldPlain, err := st.Invites().Create("teammate", "matthew", 24*time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	rotateBody, _ := json.Marshal(map[string]interface{}{"ttlHours": 48})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/invites/"+inv.ID+"/rotate", bytes.NewReader(rotateBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateUIAdminToken(),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("rotate status=%d body=%s", rr.Code, rr.Body.String())
	}
	var rotResp createInviteResponse
	_ = json.NewDecoder(rr.Body).Decode(&rotResp)
	if rotResp.Token == oldPlain {
		t.Error("expected rotated token to differ from old plaintext")
	}

	// Old plaintext fails redemption
	body, _ := json.Marshal(map[string]string{"password": "verysecure123"})
	rrOld := httptest.NewRecorder()
	reqOld := httptest.NewRequest(http.MethodPost, "/api/v1/invites/"+oldPlain+"/redeem", bytes.NewReader(body))
	reqOld.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rrOld, reqOld)
	if rrOld.Code != http.StatusUnauthorized {
		t.Errorf("expected old token to fail after rotate, got %d", rrOld.Code)
	}

	// New plaintext succeeds
	rrNew := httptest.NewRecorder()
	reqNew := httptest.NewRequest(http.MethodPost, "/api/v1/invites/"+rotResp.Token+"/redeem", bytes.NewReader(body))
	reqNew.Header.Set("Content-Type", "application/json")
	srv.router.ServeHTTP(rrNew, reqNew)
	if rrNew.Code != http.StatusOK {
		t.Errorf("expected new token to redeem, got %d body=%s", rrNew.Code, rrNew.Body.String())
	}

	// And the store row is gone (one-shot)
	if _, err := st.Invites().Get(inv.ID); !errors.Is(err, store.ErrInviteNotFound) {
		t.Errorf("expected invite consumed after new-token redeem, got %v", err)
	}
}

func TestInvites_ExpiryRejection(t *testing.T) {
	srv, st, cleanup := newInvitesTestEnv(t, true)
	defer cleanup()

	// Create with a tiny TTL through the store directly.
	_, plain, err := st.Invites().Create("expired", "matthew", 10*time.Millisecond)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	body, _ := json.Marshal(map[string]string{"password": "verysecure123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/invites/"+plain+"/redeem", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 on expired token, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestInvites_List_ShowsPending(t *testing.T) {
	srv, st, cleanup := newInvitesTestEnv(t, true)
	defer cleanup()

	_, _, _ = st.Invites().Create("a", "matthew", 24*time.Hour)
	_, _, _ = st.Invites().Create("b", "matthew", 24*time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/invites", nil)
	req.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateUIAdminToken(),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rr.Code, rr.Body.String())
	}

	var resp []invitePublicResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 2 {
		t.Errorf("expected 2 invites, got %d", len(resp))
	}
	// No plaintext leakage — last4 only, never full token.
	for _, inv := range resp {
		if len(inv.TokenLast4) != 4 {
			t.Errorf("expected TokenLast4 length 4, got %q", inv.TokenLast4)
		}
	}
}

func TestInvites_RequiresHostAdmin(t *testing.T) {
	// grant=false → admin user has no host:manage_users assignment.
	srv, _, cleanup := newInvitesTestEnv(t, false)
	defer cleanup()

	body, _ := json.Marshal(map[string]string{"label": "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/invites", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateUIAdminToken(),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 without host:manage_users, got %d", rr.Code)
	}
}

// silence unused imports if tree changes
var _ = context.Background
