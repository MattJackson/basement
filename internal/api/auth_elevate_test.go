// Tests for the sudo-style elevation endpoint (ADR-0003, cycle v1.2.0a).
//
// The adminCredsOnce sync.Once in auth.go pins admin user + hash for
// the process lifetime, so a test running BEFORE TestAuthEndpoints
// could shadow the "admin"/"test" creds the rest of the suite relies
// on. We therefore reuse the same admin user/hash ("admin" / bcrypt of
// "test") that TestAuthEndpoints installs — there's no cross-test
// state poisoning either way.
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/store"
)

// mintLegacyModelessToken hand-builds a JWT with Mode="" so the
// resulting claim is omitted from the serialized payload (thanks to
// `json:"mode,omitempty"`). Round-tripping through ParseToken then
// produces Claims with Mode="" — the pre-v1.2 cookie shape the
// back-compat grace window targets. Cannot be produced via
// IssueToken/IssueTokenWithMode because both default an empty mode
// to "user".
func mintLegacyModelessToken(t *testing.T, secret []byte, userID string) string {
	t.Helper()
	claims := &auth.Claims{
		UserID:  userID,
		Role:    "admin",
		UIAdmin: true,
		// Mode + ModeExpiresAt deliberately omitted — they're
		// zero-valued + omitempty, so the wire payload has neither.
		RegisteredClaims: &jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Subject:   userID,
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(secret)
	if err != nil {
		t.Fatalf("sign modeless token: %v", err)
	}
	return signed
}

// elevateTestPasswordHash is bcrypt("test", cost=12). Same hash as
// TestAuthEndpoints uses — the sync.Once means whichever test runs
// first writes the global, and reusing the same creds keeps them in
// lockstep regardless of run order.
const elevateTestPasswordHash = "$2a$12$sbmfdAJgsk09h5tQrKQkdu9QK2rhwQgMypco87QpYUWIDRFxh7D96"

// newElevateTestServer builds a minimal Server wired with admin creds
// matching elevateTestPasswordHash. Returns the server + the JWT
// secret so callers can mint tokens with explicit modes.
func newElevateTestServer(t *testing.T) (*Server, []byte) {
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
			PasswordHash: elevateTestPasswordHash,
		},
		JWT: config.JWTConfig{Secret: secret},
	}
	st := &store.Store{}
	srv := New(cfg, st, nil, nil, nil)
	return srv, secret
}

// tokenWithMode mints a session cookie token at the given mode +
// expiry. modeExpiresAt=0 means "never expires at the mode layer."
func tokenWithMode(t *testing.T, secret []byte, userID, mode string, modeExpiresAtUnix int64) string {
	t.Helper()
	tok, err := auth.IssueTokenWithMode(secret, userID, "admin", true,
		mode, modeExpiresAtUnix, 24*time.Hour)
	if err != nil {
		t.Fatalf("IssueTokenWithMode: %v", err)
	}
	return tok
}

// elevateRequest sends a POST to /api/v1/auth/elevate with the given
// token + body, returns the recorder.
func sendElevate(t *testing.T, srv *Server, token string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/elevate", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
	})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	return rr
}

// TestElevate_HappyPath_UserToAdmin: valid password from USER mode
// gets 200 + new cookie carrying mode=admin + ~15min TTL. The
// response body echoes the granted mode + expiry so the FE can drive
// its countdown.
func TestElevate_HappyPath_UserToAdmin(t *testing.T) {
	srv, secret := newElevateTestServer(t)
	tok := tokenWithMode(t, secret, "admin", "user", 0)

	rr := sendElevate(t, srv, tok, map[string]any{
		"password":    "test",
		"target_mode": "admin",
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Decode body.
	var resp elevateResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.Mode != "admin" {
		t.Errorf("Mode = %q, want admin", resp.Mode)
	}
	// Expect TTL ~= 15min. Allow a few seconds of jitter.
	if resp.ModeTTLSeconds < int64(adminModeTTL.Seconds()-2) || resp.ModeTTLSeconds > int64(adminModeTTL.Seconds()+2) {
		t.Errorf("ModeTTLSeconds = %d, want ~%d", resp.ModeTTLSeconds, int64(adminModeTTL.Seconds()))
	}
	if resp.ModeExpiresAt <= time.Now().Unix() {
		t.Errorf("ModeExpiresAt %d should be in the future", resp.ModeExpiresAt)
	}

	// Verify the Set-Cookie carries a token with mode=admin.
	var newCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == auth.CookieName {
			newCookie = c
			break
		}
	}
	if newCookie == nil {
		t.Fatal("expected Set-Cookie with session cookie")
	}
	claims, err := auth.ParseToken(secret, newCookie.Value)
	if err != nil {
		t.Fatalf("ParseToken on new cookie: %v", err)
	}
	if claims.Mode != "admin" {
		t.Errorf("new cookie Mode = %q, want admin", claims.Mode)
	}
	if claims.ModeExpiresAt == 0 {
		t.Errorf("new cookie ModeExpiresAt should be set (got 0)")
	}
}

// TestElevate_WrongPassword: a 401 INVALID_PASSWORD with no cookie
// rotation. The audit emits an auth:elevate_failure event but that's
// asserted in the audit handler tests, not here.
func TestElevate_WrongPassword(t *testing.T) {
	srv, secret := newElevateTestServer(t)
	tok := tokenWithMode(t, secret, "admin", "user", 0)

	rr := sendElevate(t, srv, tok, map[string]any{
		"password":    "wrong-password",
		"target_mode": "admin",
	})

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rr.Code, rr.Body.String())
	}
	var er ErrorResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &er)
	if er.Error.Code != "INVALID_PASSWORD" {
		t.Errorf("error code = %q, want INVALID_PASSWORD", er.Error.Code)
	}

	// No cookie rotation on failure — verify there's no Set-Cookie.
	if len(rr.Result().Cookies()) > 0 {
		t.Errorf("expected no Set-Cookie on failure, got %d cookies", len(rr.Result().Cookies()))
	}
}

// TestElevate_InvalidTargetMode: only "admin" and "elevated" are
// valid. "user", "superuser", empty string, etc. all 400.
func TestElevate_InvalidTargetMode(t *testing.T) {
	srv, secret := newElevateTestServer(t)
	tok := tokenWithMode(t, secret, "admin", "user", 0)

	for _, badMode := range []string{"user", "superuser", "", "ADMIN"} {
		rr := sendElevate(t, srv, tok, map[string]any{
			"password":    "test",
			"target_mode": badMode,
		})
		if rr.Code != http.StatusBadRequest {
			t.Errorf("target_mode=%q: expected 400, got %d body=%s", badMode, rr.Code, rr.Body.String())
			continue
		}
		var er ErrorResponse
		_ = json.Unmarshal(rr.Body.Bytes(), &er)
		if er.Error.Code != "INVALID_TARGET_MODE" {
			t.Errorf("target_mode=%q: error code = %q, want INVALID_TARGET_MODE", badMode, er.Error.Code)
		}
	}
}

// TestElevate_UserToElevatedDirectly_Rejected: the state machine
// forbids a USER → ELEVATED jump. Must go through ADMIN first.
func TestElevate_UserToElevatedDirectly_Rejected(t *testing.T) {
	srv, secret := newElevateTestServer(t)
	tok := tokenWithMode(t, secret, "admin", "user", 0)

	rr := sendElevate(t, srv, tok, map[string]any{
		"password":    "test",
		"target_mode": "elevated",
	})

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
	}
	var er ErrorResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &er)
	if er.Error.Code != "INVALID_TARGET_MODE" {
		t.Errorf("error code = %q, want INVALID_TARGET_MODE", er.Error.Code)
	}
}

// TestElevate_AdminToElevated_Works: already in ADMIN, the elevation
// to ELEVATED is allowed and gets the shorter (5min) TTL.
func TestElevate_AdminToElevated_Works(t *testing.T) {
	srv, secret := newElevateTestServer(t)
	// Mint a token that's already ADMIN (15min from now).
	adminExp := time.Now().Add(10 * time.Minute).Unix()
	tok := tokenWithMode(t, secret, "admin", "admin", adminExp)

	rr := sendElevate(t, srv, tok, map[string]any{
		"password":    "test",
		"target_mode": "elevated",
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp elevateResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Mode != "elevated" {
		t.Errorf("Mode = %q, want elevated", resp.Mode)
	}
	if resp.ModeTTLSeconds < int64(elevatedModeTTL.Seconds()-2) || resp.ModeTTLSeconds > int64(elevatedModeTTL.Seconds()+2) {
		t.Errorf("ModeTTLSeconds = %d, want ~%d (elevated TTL)",
			resp.ModeTTLSeconds, int64(elevatedModeTTL.Seconds()))
	}
}

// TestCurrentMode_ExpiredModeAutoDowngrade: a token in ELEVATED whose
// ModeExpiresAt has passed is treated by currentMode() as ADMIN for
// this request. A token in ADMIN whose ModeExpiresAt has passed is
// treated as USER. Per ADR-0003 the cookie itself is not rewritten
// here; downstream handlers that mint fresh cookies pick up the
// downgrade.
func TestCurrentMode_ExpiredModeAutoDowngrade(t *testing.T) {
	srv, secret := newElevateTestServer(t)
	// Token in ELEVATED but expired 1s ago — currentMode should
	// return ADMIN.
	expired := time.Now().Add(-1 * time.Second).Unix()
	tok := tokenWithMode(t, secret, "admin", "elevated", expired)

	// Wrap a tiny handler that captures currentMode for us.
	var observed string
	captured := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		observed = string(currentMode(r))
	})
	// Use the real auth middleware so the claims context gets populated.
	wrapped := auth.Middleware(secret)(captured)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok, Path: "/"})
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if observed != "admin" {
		t.Errorf("expired ELEVATED: observed mode = %q, want admin", observed)
	}

	// Same trick for ADMIN → USER downgrade on expiry.
	tok = tokenWithMode(t, secret, "admin", "admin", expired)
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok, Path: "/"})
	rr2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rr2, req2)
	if observed != "user" {
		t.Errorf("expired ADMIN: observed mode = %q, want user", observed)
	}

	// Sanity: a token whose expiry is in the future stays at its
	// stated mode.
	future := time.Now().Add(10 * time.Minute).Unix()
	tok = tokenWithMode(t, secret, "admin", "admin", future)
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok, Path: "/"})
	rr3 := httptest.NewRecorder()
	wrapped.ServeHTTP(rr3, req3)
	if observed != "admin" {
		t.Errorf("live ADMIN: observed mode = %q, want admin", observed)
	}

	_ = srv // server unused beyond setup; this test exercises currentMode directly
}

// TestLogoutElevation_DropsToUser: hitting POST /auth/logout-elevation
// from ADMIN mints a fresh cookie with mode=user, ModeExpiresAt=0.
// The user/role/uiAdmin claims and the outer session TTL stay intact —
// only the elevation collapses. Used by the v1.2.0b "drop privileges"
// button next to the persona pill countdown.
func TestLogoutElevation_DropsToUser(t *testing.T) {
	srv, secret := newElevateTestServer(t)
	adminExp := time.Now().Add(10 * time.Minute).Unix()
	tok := tokenWithMode(t, secret, "admin", "admin", adminExp)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout-elevation", nil)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:     auth.CookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
	})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["mode"] != "user" {
		t.Errorf("body mode = %v, want user", body["mode"])
	}
	if exp, ok := body["mode_expires_at"].(float64); !ok || exp != 0 {
		t.Errorf("body mode_expires_at = %v, want 0", body["mode_expires_at"])
	}

	// Verify the Set-Cookie carries a token with mode=user + no
	// mode-layer expiry.
	var newCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == auth.CookieName {
			newCookie = c
			break
		}
	}
	if newCookie == nil {
		t.Fatal("expected Set-Cookie with session cookie")
	}
	claims, err := auth.ParseToken(secret, newCookie.Value)
	if err != nil {
		t.Fatalf("ParseToken on new cookie: %v", err)
	}
	if claims.Mode != "user" {
		t.Errorf("new cookie Mode = %q, want user", claims.Mode)
	}
	if claims.ModeExpiresAt != 0 {
		t.Errorf("new cookie ModeExpiresAt = %d, want 0", claims.ModeExpiresAt)
	}
	if claims.UserID != "admin" {
		t.Errorf("UserID = %q, want admin (must survive the drop)", claims.UserID)
	}
	if claims.Role != "admin" {
		t.Errorf("Role = %q, want admin (must survive the drop)", claims.Role)
	}
	if !claims.UIAdmin {
		t.Errorf("UIAdmin lost in drop — drop should ONLY downgrade mode, not strip role")
	}
}

// TestLogoutElevation_NoSession: missing cookie returns 401, no
// downgrade attempted. The middleware short-circuits before the
// handler runs so we get the standard auth/SESSION_REQUIRED message.
func TestLogoutElevation_NoSession(t *testing.T) {
	srv, _ := newElevateTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout-elevation", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestMeHandler_IncludesModeAndExpiry: /auth/me must echo the live
// mode + mode-expires-at so the frontend can hydrate its sudo state
// machine on first render. A live ADMIN token comes back with the
// claim's stored expiry; a pre-v1.2 modeless cookie comes back as
// "admin" inside the grace window (the gate's promotion).
func TestMeHandler_IncludesModeAndExpiry(t *testing.T) {
	srv, secret := newElevateTestServer(t)

	t.Run("admin token round-trips mode + expiry", func(t *testing.T) {
		exp := time.Now().Add(10 * time.Minute).Unix()
		tok := tokenWithMode(t, secret, "admin", "admin", exp)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok, Path: "/"})
		rr := httptest.NewRecorder()
		srv.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var resp UserResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Mode != "admin" {
			t.Errorf("Mode = %q, want admin", resp.Mode)
		}
		if resp.ModeExpiresAt != exp {
			t.Errorf("ModeExpiresAt = %d, want %d", resp.ModeExpiresAt, exp)
		}
	})

	t.Run("pre-v1.2 cookie surfaces as admin during grace", func(t *testing.T) {
		tok := mintLegacyModelessToken(t, secret, "matthew")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok, Path: "/"})
		rr := httptest.NewRecorder()
		srv.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
		}
		var resp UserResponse
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp.Mode != "admin" {
			t.Errorf("legacy cookie Mode = %q, want admin (grace window promotion)", resp.Mode)
		}
		// Legacy claim has no mode-layer expiry — must surface as 0
		// so the FE doesn't render a misleading countdown.
		if resp.ModeExpiresAt != 0 {
			t.Errorf("legacy cookie ModeExpiresAt = %d, want 0", resp.ModeExpiresAt)
		}
	})
}

// TestCurrentMode_PreV12Cookie_TreatedAsAdmin: a token minted by
// pre-v1.2 code has Mode="" (the field is omitempty). During the
// 7-day grace window the gate treats it as ADMIN so the existing
// matthew session keeps working. Past the window it drops to USER.
func TestCurrentMode_PreV12Cookie_TreatedAsAdmin(t *testing.T) {
	srv, secret := newElevateTestServer(t)

	// Build a token with the legacy IssueToken — which now defaults
	// Mode="user". We need a TRULY mode-less token to test the
	// pre-v1.2 path; mint one by hand via the jwt library.
	tok := mintLegacyModelessToken(t, secret, "matthew")

	var observed string
	captured := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		observed = string(currentMode(r))
	})
	wrapped := auth.Middleware(secret)(captured)

	// Grace window: now < preV12GraceUntil (set to now+7d at package init).
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok, Path: "/"})
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)
	if observed != "admin" {
		t.Errorf("pre-v1.2 cookie in grace window: observed mode = %q, want admin", observed)
	}

	// Simulate post-grace by stubbing nowFunc.
	origNow := nowFunc
	nowFunc = func() time.Time { return preV12GraceUntil.Add(time.Hour) }
	t.Cleanup(func() { nowFunc = origNow })

	observed = ""
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(&http.Cookie{Name: auth.CookieName, Value: tok, Path: "/"})
	rr2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rr2, req2)
	if observed != "user" {
		t.Errorf("pre-v1.2 cookie past grace window: observed mode = %q, want user", observed)
	}

	_ = srv
}
