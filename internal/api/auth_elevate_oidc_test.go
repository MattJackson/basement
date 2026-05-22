// Tests for the OIDC sudo-style elevation flow (ADR-0003, v1.2.0c).
//
// Coverage:
//   - /auth/elevate/oidc/start: 200 with redirect_url that carries
//     `prompt=login`, `max_age=0`, and the right state binding.
//   - /auth/elevate/oidc/start: rejects USER→ELEVATED jump.
//   - /auth/elevate/oidc/start: 503 when OIDC not configured.
//   - /auth/elevate/oidc/callback: valid state + fresh auth_time →
//     elevated cookie issued; 302 to "/?elevated=<mode>".
//   - /auth/elevate/oidc/callback: stale auth_time (>60s) → 401.
//   - /auth/elevate/oidc/callback: invalid state → 400.
//   - /auth/elevate/oidc/callback: expired state (>5min) → 400.
//   - /auth/elevate/oidc/callback: cross-session state → 400.
//   - /auth/elevate (dispatcher) OIDC user → 200 {requires_oidc:true,
//     start_url}.

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/store"
)

// newOIDCElevateTestServer builds a server with both admin creds AND a
// configured OIDC provider so the elevation OIDC routes are mounted.
// Returns the server + secret + the fake OIDC + a store (callers may
// add an OIDC-only user before exercising the dispatcher).
func newOIDCElevateTestServer(t *testing.T) (*Server, []byte, *fakeOIDC, *store.Store) {
	t.Helper()
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i)
	}
	st, err := store.Open(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	cfg := &config.Config{
		Listen:     ":0",
		SessionTTL: 24 * time.Hour,
		Admin: config.AdminConfig{
			User:         "admin",
			PasswordHash: elevateTestPasswordHash,
		},
		JWT:  config.JWTConfig{Secret: secret},
		OIDC: config.OIDCConfig{ElevationPrompt: "login"},
	}
	fake := &fakeOIDC{issuer: "https://idp.example.com"}
	srv := New(cfg, st, nil, &stubDriver{}, nil)
	srv.SetOIDC(fake)
	return srv, secret, fake, st
}

// addOIDCUser creates a store user with no password + an issuer so the
// dispatcher recognises them as OIDC-only.
func addOIDCUser(t *testing.T, st *store.Store, username string) {
	t.Helper()
	u := store.User{
		Username: username,
		Role:     "user",
		Provider: "https://idp.example.com",
		Subject:  "subj-" + username,
		Email:    username + "@example.com",
	}
	if err := st.CreateUser(u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
}

// sendElevateOIDCStart POSTs to /auth/elevate/oidc/start.
func sendElevateOIDCStart(t *testing.T, srv *Server, token string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/elevate/oidc/start", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token, Path: "/"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	return rr
}

// sendElevateOIDCCallback GETs /auth/elevate/oidc/callback with query
// params + session cookie.
func sendElevateOIDCCallback(t *testing.T, srv *Server, token, state, code string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/api/v1/auth/elevate/oidc/callback?state=" + url.QueryEscape(state) +
		"&code=" + url.QueryEscape(code)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token, Path: "/"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	return rr
}

// TestElevateOIDCStart_ReturnsRedirectURLWithPromptAndState verifies
// the happy-path 200 response shape AND that the state binding the
// callback will read is in place.
func TestElevateOIDCStart_ReturnsRedirectURLWithPromptAndState(t *testing.T) {
	srv, secret, fake, st := newOIDCElevateTestServer(t)
	addOIDCUser(t, st, "alice")
	tok := tokenWithMode(t, secret, "alice", "user", 0)

	rr := sendElevateOIDCStart(t, srv, tok, map[string]any{"target_mode": "admin"})

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200. Body: %s", rr.Code, rr.Body.String())
	}

	var resp elevateOIDCStartResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RedirectURL == "" {
		t.Fatal("redirect_url empty")
	}
	if !strings.Contains(resp.RedirectURL, "prompt=login") {
		t.Errorf("redirect_url missing prompt=login: %s", resp.RedirectURL)
	}
	if !strings.Contains(resp.RedirectURL, "max_age=0") {
		t.Errorf("redirect_url missing max_age=0: %s", resp.RedirectURL)
	}
	if fake.lastState == "" || fake.lastNonce == "" {
		t.Error("ElevationAuthCodeURL was not called with state/nonce")
	}
	if fake.lastPrompt != "login" {
		t.Errorf("ElevationAuthCodeURL prompt=%q, want login", fake.lastPrompt)
	}

	// State should be stashed so the callback can take it.
	entry, ok := srv.ensureOIDCElevationStore().take(fake.lastState)
	if !ok {
		t.Fatalf("state %q not stored", fake.lastState)
	}
	if entry.UserID != "alice" {
		t.Errorf("entry.UserID=%q, want alice", entry.UserID)
	}
	if string(entry.TargetMode) != "admin" {
		t.Errorf("entry.TargetMode=%q, want admin", entry.TargetMode)
	}
	if entry.Nonce != fake.lastNonce {
		t.Errorf("entry.Nonce=%q, want %q", entry.Nonce, fake.lastNonce)
	}
}

// TestElevateOIDCStart_UserToElevatedRejected: same state-machine rule
// as the password endpoint — USER → ELEVATED is forbidden.
func TestElevateOIDCStart_UserToElevatedRejected(t *testing.T) {
	srv, secret, _, st := newOIDCElevateTestServer(t)
	addOIDCUser(t, st, "alice")
	tok := tokenWithMode(t, secret, "alice", "user", 0)

	rr := sendElevateOIDCStart(t, srv, tok, map[string]any{"target_mode": "elevated"})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rr.Code)
	}
	var er ErrorResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &er)
	if er.Error.Code != "INVALID_TARGET_MODE" {
		t.Errorf("code=%q, want INVALID_TARGET_MODE", er.Error.Code)
	}
}

// TestElevateOIDCStart_OIDCNotConfigured: when s.oidc is nil the
// endpoint returns 503 OIDC_NOT_CONFIGURED so the FE can show the
// "contact your administrator" message.
func TestElevateOIDCStart_OIDCNotConfigured(t *testing.T) {
	srv, secret, _, st := newOIDCElevateTestServer(t)
	addOIDCUser(t, st, "alice")
	srv.SetOIDC(nil) // strip OIDC provider
	tok := tokenWithMode(t, secret, "alice", "user", 0)

	rr := sendElevateOIDCStart(t, srv, tok, map[string]any{"target_mode": "admin"})

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", rr.Code)
	}
	var er ErrorResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &er)
	if er.Error.Code != "OIDC_NOT_CONFIGURED" {
		t.Errorf("code=%q, want OIDC_NOT_CONFIGURED", er.Error.Code)
	}
}

// TestElevateOIDCCallback_FreshAuthTime_IssuesElevatedCookie is the
// happy path: valid state, fresh auth_time, IdP claims verify → new
// cookie at mode=admin + 302 to /?elevated=admin.
func TestElevateOIDCCallback_FreshAuthTime_IssuesElevatedCookie(t *testing.T) {
	srv, secret, fake, st := newOIDCElevateTestServer(t)
	addOIDCUser(t, st, "alice")
	tok := tokenWithMode(t, secret, "alice", "user", 0)

	// Kick off start to establish state binding.
	startRR := sendElevateOIDCStart(t, srv, tok, map[string]any{"target_mode": "admin"})
	if startRR.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", startRR.Code, startRR.Body.String())
	}
	state := fake.lastState

	// Make the verifier return a fresh auth_time (= now).
	fake.verifyAuthTimeFn = func(_ context.Context, _, expectedNonce string) (*auth.OIDCClaims, int64, error) {
		if expectedNonce != fake.lastNonce {
			t.Errorf("verifier got nonce=%q, want %q", expectedNonce, fake.lastNonce)
		}
		return &auth.OIDCClaims{Subject: "subj-alice", Provider: "https://idp.example.com"},
			time.Now().Unix(), nil
	}

	rr := sendElevateOIDCCallback(t, srv, tok, state, "code-abc")

	if rr.Code != http.StatusFound {
		t.Fatalf("status=%d, want 302. Body: %s", rr.Code, rr.Body.String())
	}
	loc := rr.Header().Get("Location")
	if loc != "/?elevated=admin" {
		t.Errorf("Location=%q, want /?elevated=admin", loc)
	}

	var newCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == auth.CookieName {
			newCookie = c
			break
		}
	}
	if newCookie == nil {
		t.Fatal("no session cookie issued")
	}
	claims, err := auth.ParseToken(secret, newCookie.Value)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if claims.Mode != "admin" {
		t.Errorf("cookie Mode=%q, want admin", claims.Mode)
	}
	if claims.ModeExpiresAt <= time.Now().Unix() {
		t.Errorf("ModeExpiresAt %d should be in the future", claims.ModeExpiresAt)
	}
	if claims.UserID != "alice" {
		t.Errorf("cookie UserID=%q, want alice", claims.UserID)
	}
}

// TestElevateOIDCCallback_StaleAuthTime_401: when the IdP returns an
// auth_time older than the 60s freshness window, the callback rejects
// with 401 OIDC_AUTH_TIME_STALE. This protects against an IdP that
// ignores prompt=login + max_age=0 and serves a cached session.
func TestElevateOIDCCallback_StaleAuthTime_401(t *testing.T) {
	srv, secret, fake, st := newOIDCElevateTestServer(t)
	addOIDCUser(t, st, "alice")
	tok := tokenWithMode(t, secret, "alice", "user", 0)

	startRR := sendElevateOIDCStart(t, srv, tok, map[string]any{"target_mode": "admin"})
	if startRR.Code != http.StatusOK {
		t.Fatalf("start status=%d", startRR.Code)
	}
	state := fake.lastState

	// Stale auth_time = 1 hour ago.
	fake.verifyAuthTimeFn = func(_ context.Context, _, _ string) (*auth.OIDCClaims, int64, error) {
		return &auth.OIDCClaims{Subject: "subj-alice", Provider: "https://idp.example.com"},
			time.Now().Add(-1 * time.Hour).Unix(), nil
	}

	rr := sendElevateOIDCCallback(t, srv, tok, state, "code-abc")

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401. Body: %s", rr.Code, rr.Body.String())
	}
	var er ErrorResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &er)
	if er.Error.Code != "OIDC_AUTH_TIME_STALE" {
		t.Errorf("code=%q, want OIDC_AUTH_TIME_STALE", er.Error.Code)
	}
}

// TestElevateOIDCCallback_InvalidState_400: unknown state → 400.
func TestElevateOIDCCallback_InvalidState_400(t *testing.T) {
	srv, secret, _, st := newOIDCElevateTestServer(t)
	addOIDCUser(t, st, "alice")
	tok := tokenWithMode(t, secret, "alice", "user", 0)

	rr := sendElevateOIDCCallback(t, srv, tok, "never-issued-state", "code-abc")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rr.Code)
	}
	var er ErrorResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &er)
	if er.Error.Code != "OIDC_STATE_INVALID" {
		t.Errorf("code=%q, want OIDC_STATE_INVALID", er.Error.Code)
	}
}

// TestElevateOIDCCallback_ExpiredState_400: a state that was minted
// more than 5 minutes ago is treated as gone.
func TestElevateOIDCCallback_ExpiredState_400(t *testing.T) {
	srv, secret, _, st := newOIDCElevateTestServer(t)
	addOIDCUser(t, st, "alice")
	tok := tokenWithMode(t, secret, "alice", "user", 0)

	// Hand-craft an expired state entry directly.
	expiredState := "expired-state-abc"
	srv.ensureOIDCElevationStore().entries[expiredState] = oidcElevationStateEntry{
		TargetMode: "admin",
		UserID:     "alice",
		Nonce:      "nonce-x",
		CreatedAt:  time.Now().Add(-10 * time.Minute), // way past TTL
	}

	rr := sendElevateOIDCCallback(t, srv, tok, expiredState, "code-abc")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400. Body: %s", rr.Code, rr.Body.String())
	}
	var er ErrorResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &er)
	if er.Error.Code != "OIDC_STATE_INVALID" {
		t.Errorf("code=%q, want OIDC_STATE_INVALID", er.Error.Code)
	}
}

// TestElevateOIDCCallback_SessionMismatch_400: state was minted in
// alice's session but bob's session is presenting the callback. The
// same-session check stops the elevation.
func TestElevateOIDCCallback_SessionMismatch_400(t *testing.T) {
	srv, secret, fake, st := newOIDCElevateTestServer(t)
	addOIDCUser(t, st, "alice")
	addOIDCUser(t, st, "bob")
	aliceTok := tokenWithMode(t, secret, "alice", "user", 0)
	bobTok := tokenWithMode(t, secret, "bob", "user", 0)

	// Start in alice's session.
	startRR := sendElevateOIDCStart(t, srv, aliceTok, map[string]any{"target_mode": "admin"})
	if startRR.Code != http.StatusOK {
		t.Fatalf("start status=%d", startRR.Code)
	}
	state := fake.lastState

	// Callback in bob's session.
	rr := sendElevateOIDCCallback(t, srv, bobTok, state, "code-abc")

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", rr.Code)
	}
	var er ErrorResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &er)
	if er.Error.Code != "OIDC_STATE_INVALID" {
		t.Errorf("code=%q, want OIDC_STATE_INVALID", er.Error.Code)
	}
}

// TestElevate_DispatcherPivotsOIDCUser: the v1.2.0a /auth/elevate
// endpoint now detects OIDC-only users and returns 200 +
// {requires_oidc:true, start_url:"/api/v1/auth/elevate/oidc/start"}
// instead of the old 403 OIDC_USER_ELEVATION_NOT_SUPPORTED.
func TestElevate_DispatcherPivotsOIDCUser(t *testing.T) {
	srv, secret, _, st := newOIDCElevateTestServer(t)
	addOIDCUser(t, st, "alice")
	tok := tokenWithMode(t, secret, "alice", "user", 0)

	rr := sendElevate(t, srv, tok, map[string]any{
		"password":    "irrelevant",
		"target_mode": "admin",
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200. Body: %s", rr.Code, rr.Body.String())
	}
	var resp elevateResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.RequiresOIDC {
		t.Errorf("RequiresOIDC=false, want true")
	}
	if resp.StartURL != "/api/v1/auth/elevate/oidc/start" {
		t.Errorf("StartURL=%q, want /api/v1/auth/elevate/oidc/start", resp.StartURL)
	}
	// No cookie rotation on pivot — the elevation hasn't happened yet.
	for _, c := range rr.Result().Cookies() {
		if c.Name == auth.CookieName {
			t.Errorf("unexpected session cookie issued on OIDC pivot: %v", c)
		}
	}
}

// TestElevate_DispatcherOIDCUserButOIDCNotConfigured: the rare case
// where an OIDC-provisioned user exists but the server lost OIDC
// config (e.g. operator unset BASEMENT_OIDC_ISSUER after provisioning).
// Returns 503 OIDC_NOT_CONFIGURED so the FE can show the "contact
// your administrator" message.
func TestElevate_DispatcherOIDCUserButOIDCNotConfigured(t *testing.T) {
	srv, secret, _, st := newOIDCElevateTestServer(t)
	addOIDCUser(t, st, "alice")
	srv.SetOIDC(nil) // strip OIDC provider
	tok := tokenWithMode(t, secret, "alice", "user", 0)

	rr := sendElevate(t, srv, tok, map[string]any{
		"password":    "irrelevant",
		"target_mode": "admin",
	})

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503. Body: %s", rr.Code, rr.Body.String())
	}
	var er ErrorResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &er)
	if er.Error.Code != "OIDC_NOT_CONFIGURED" {
		t.Errorf("code=%q, want OIDC_NOT_CONFIGURED", er.Error.Code)
	}
}

// TestOIDCElevationStateStore_Sweep verifies the on-insert sweep
// deletes expired entries.
func TestOIDCElevationStateStore_Sweep(t *testing.T) {
	s := newOIDCElevationStateStore()
	old := "old"
	fresh := "fresh"

	// Drop in an old entry directly (pre-dates TTL).
	s.entries[old] = oidcElevationStateEntry{
		CreatedAt: time.Now().Add(-10 * time.Minute),
	}

	// Inserting a fresh one should sweep the old.
	s.put(fresh, oidcElevationStateEntry{
		TargetMode: "admin",
		UserID:     "alice",
		Nonce:      "n",
		CreatedAt:  time.Now(),
	})

	if _, ok := s.entries[old]; ok {
		t.Errorf("expired entry %q should have been swept", old)
	}
	if _, ok := s.entries[fresh]; !ok {
		t.Errorf("fresh entry %q missing", fresh)
	}
}
