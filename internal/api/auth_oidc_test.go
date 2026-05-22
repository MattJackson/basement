package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/store"
)

// fakeOIDC implements the api.oidcProvider interface for handler tests.
// It records the last state/nonce passed to AuthCodeURL and returns
// fixed claims from VerifyIDToken so tests can assert end-to-end
// behaviour without spinning up a real IdP.
type fakeOIDC struct {
	authURL          string
	elevationURL     string
	lastState        string
	lastNonce        string
	lastPrompt       string
	exchangeFn       func(ctx context.Context, code string) (*oauth2.Token, error)
	verifyFn         func(ctx context.Context, rawIDToken, expectedNonce string) (*auth.OIDCClaims, error)
	verifyAuthTimeFn func(ctx context.Context, rawIDToken, expectedNonce string) (*auth.OIDCClaims, int64, error)
	issuer           string
	autoProvFlag     bool
}

func (f *fakeOIDC) AuthCodeURL(state, nonce string) string {
	f.lastState = state
	f.lastNonce = nonce
	if f.authURL == "" {
		return "https://idp.example.com/authorize?state=" + url.QueryEscape(state) + "&nonce=" + url.QueryEscape(nonce)
	}
	return f.authURL + "?state=" + url.QueryEscape(state) + "&nonce=" + url.QueryEscape(nonce)
}

func (f *fakeOIDC) ElevationAuthCodeURL(state, nonce, prompt string) string {
	f.lastState = state
	f.lastNonce = nonce
	f.lastPrompt = prompt
	base := f.elevationURL
	if base == "" {
		base = f.authURL
	}
	if base == "" {
		base = "https://idp.example.com/authorize"
	}
	u := base + "?state=" + url.QueryEscape(state) + "&nonce=" + url.QueryEscape(nonce) + "&max_age=0"
	if prompt != "" {
		u += "&prompt=" + url.QueryEscape(prompt)
	}
	return u
}

func (f *fakeOIDC) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	if f.exchangeFn != nil {
		return f.exchangeFn(ctx, code)
	}
	tok := &oauth2.Token{AccessToken: "access-x"}
	return tok.WithExtra(map[string]interface{}{"id_token": "raw-id-token"}), nil
}

func (f *fakeOIDC) VerifyIDToken(ctx context.Context, raw, expectedNonce string) (*auth.OIDCClaims, error) {
	if f.verifyFn != nil {
		return f.verifyFn(ctx, raw, expectedNonce)
	}
	return &auth.OIDCClaims{
		Subject:  "subj-1",
		Email:    "newbie@example.com",
		Name:     "New User",
		Provider: f.issuer,
	}, nil
}

func (f *fakeOIDC) VerifyIDTokenWithAuthTime(ctx context.Context, raw, expectedNonce string) (*auth.OIDCClaims, int64, error) {
	if f.verifyAuthTimeFn != nil {
		return f.verifyAuthTimeFn(ctx, raw, expectedNonce)
	}
	claims, err := f.VerifyIDToken(ctx, raw, expectedNonce)
	if err != nil {
		return nil, 0, err
	}
	// Default fake auth_time is "now" so happy-path callback tests
	// pass the 60s freshness check without callers having to plumb it.
	return claims, time.Now().Unix(), nil
}

func (f *fakeOIDC) Issuer() string        { return f.issuer }
func (f *fakeOIDC) AutoProvision() bool   { return f.autoProvFlag }

func newOIDCTestServer(t *testing.T, fake *fakeOIDC, st *store.Store) *Server {
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
			PasswordHash: "$2a$12$sbmfdAJgsk09h5tQrKQkdu9QK2rhwQgMypco87QpYUWIDRFxh7D96",
		},
		JWT: config.JWTConfig{Secret: secret},
	}

	srv := New(cfg, st, nil, &stubDriver{}, nil)
	srv.SetOIDC(fake)
	return srv
}

func TestOIDCStart_SetsStateCookieAnd302s(t *testing.T) {
	fake := &fakeOIDC{issuer: "https://idp.example.com", authURL: "https://idp.example.com/authorize"}
	srv := newOIDCTestServer(t, fake, &store.Store{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/start", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("status=%d, want 302. Body: %s", rr.Code, rr.Body.String())
	}

	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://idp.example.com/authorize") {
		t.Errorf("Location=%q, want IdP URL", loc)
	}

	var stateCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == auth.OIDCStateCookieName {
			stateCookie = c
			break
		}
	}
	if stateCookie == nil {
		t.Fatal("state cookie not set")
	}
	if !strings.Contains(stateCookie.Value, ".") {
		t.Errorf("state cookie value should contain state.nonce, got %q", stateCookie.Value)
	}
	if !stateCookie.HttpOnly {
		t.Error("state cookie missing HttpOnly")
	}
	if !stateCookie.Secure {
		t.Error("state cookie missing Secure")
	}
	if stateCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("state cookie SameSite=%v, want Lax", stateCookie.SameSite)
	}

	// Verify state + nonce ended up in the AuthCodeURL too.
	if fake.lastState == "" || fake.lastNonce == "" {
		t.Error("AuthCodeURL was not called with state/nonce")
	}
	parts := strings.SplitN(stateCookie.Value, ".", 2)
	if parts[0] != fake.lastState || parts[1] != fake.lastNonce {
		t.Errorf("cookie %q does not match AuthCodeURL args state=%s nonce=%s", stateCookie.Value, fake.lastState, fake.lastNonce)
	}
}

func TestOIDCStart_WhenOIDCNotConfigured_501(t *testing.T) {
	srv := newOIDCTestServer(t, nil, &store.Store{})
	srv.SetOIDC(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/start", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d, want 501. Body: %s", rr.Code, rr.Body.String())
	}
}

func TestOIDCCallback_StateMismatch_400(t *testing.T) {
	fake := &fakeOIDC{issuer: "https://idp.example.com"}
	srv := newOIDCTestServer(t, fake, &store.Store{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?state=evil&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: auth.OIDCStateCookieName, Value: "real-state.real-nonce"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400. Body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "OIDC_STATE_MISMATCH") {
		t.Errorf("body missing OIDC_STATE_MISMATCH: %s", rr.Body.String())
	}
}

func TestOIDCCallback_MissingStateCookie_400(t *testing.T) {
	fake := &fakeOIDC{issuer: "https://idp.example.com"}
	srv := newOIDCTestServer(t, fake, &store.Store{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?state=x&code=abc", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400. Body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "OIDC_STATE_MISSING") {
		t.Errorf("body missing OIDC_STATE_MISSING: %s", rr.Body.String())
	}
}

func TestOIDCCallback_UnknownUser_AutoProvisionFalse_403(t *testing.T) {
	st, err := store.Open(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	fake := &fakeOIDC{
		issuer:       "https://idp.example.com",
		autoProvFlag: false,
		verifyFn: func(ctx context.Context, raw, expectedNonce string) (*auth.OIDCClaims, error) {
			return &auth.OIDCClaims{
				Subject:  "subj-unknown",
				Email:    "stranger@example.com",
				Provider: "https://idp.example.com",
			}, nil
		},
	}
	srv := newOIDCTestServer(t, fake, st)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?state=stateA&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: auth.OIDCStateCookieName, Value: "stateA.nonceB"})

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403. Body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "USER_NOT_PROVISIONED") {
		t.Errorf("body missing USER_NOT_PROVISIONED: %s", rr.Body.String())
	}

	// Confirm no user was created.
	if got := st.Users(); len(got) != 0 {
		t.Errorf("AutoProvision=false should not create users, got %d", len(got))
	}
}

func TestOIDCCallback_UnknownUser_AutoProvisionTrue_CreatesUserAndSetsSession(t *testing.T) {
	st, err := store.Open(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	fake := &fakeOIDC{
		issuer:       "https://idp.example.com",
		autoProvFlag: true,
		verifyFn: func(ctx context.Context, raw, expectedNonce string) (*auth.OIDCClaims, error) {
			if expectedNonce != "nonceB" {
				return nil, errors.New("nonce not threaded through")
			}
			return &auth.OIDCClaims{
				Subject:  "subj-new",
				Email:    "newuser@example.com",
				Name:     "New User",
				Provider: "https://idp.example.com",
			}, nil
		},
	}
	srv := newOIDCTestServer(t, fake, st)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?state=stateA&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: auth.OIDCStateCookieName, Value: "stateA.nonceB"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("status=%d, want 302. Body: %s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != "/" {
		t.Errorf("Location=%q, want \"/\"", loc)
	}

	// Session cookie set.
	var session *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == auth.CookieName {
			session = c
			break
		}
	}
	if session == nil {
		t.Fatal("session cookie not set")
	}
	if session.Value == "" {
		t.Error("session cookie value empty")
	}

	// State cookie cleared.
	for _, c := range rr.Result().Cookies() {
		if c.Name == auth.OIDCStateCookieName {
			if c.MaxAge >= 0 && c.Value != "" {
				t.Errorf("state cookie not cleared: %+v", c)
			}
		}
	}

	// User created with provider+subject and role=user.
	users := st.Users()
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	u := users[0]
	if u.Provider != "https://idp.example.com" {
		t.Errorf("user.Provider=%q", u.Provider)
	}
	if u.Subject != "subj-new" {
		t.Errorf("user.Subject=%q", u.Subject)
	}
	if u.Role != "user" {
		t.Errorf("user.Role=%q, want \"user\" (OIDC users never get admin role)", u.Role)
	}
	if u.Email != "newuser@example.com" {
		t.Errorf("user.Email=%q", u.Email)
	}
}

func TestOIDCCallback_ExistingUser_SkipsCreationAndIssuesSession(t *testing.T) {
	st, err := store.Open(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	existing := store.User{
		ID:       "existing-id",
		Username: "alice@example.com",
		Role:     "user",
		Provider: "https://idp.example.com",
		Subject:  "subj-existing",
	}
	if err := st.CreateUser(existing); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	fake := &fakeOIDC{
		issuer:       "https://idp.example.com",
		autoProvFlag: false, // even with AutoProvision off, existing users still log in
		verifyFn: func(ctx context.Context, raw, expectedNonce string) (*auth.OIDCClaims, error) {
			return &auth.OIDCClaims{
				Subject:  "subj-existing",
				Email:    "alice@example.com",
				Provider: "https://idp.example.com",
			}, nil
		},
	}
	srv := newOIDCTestServer(t, fake, st)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?state=stateA&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: auth.OIDCStateCookieName, Value: "stateA.nonceB"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("status=%d, want 302. Body: %s", rr.Code, rr.Body.String())
	}

	// Still exactly one user — no duplicate row.
	if got := st.Users(); len(got) != 1 {
		t.Errorf("expected 1 user, got %d", len(got))
	}
}

func TestOIDCCallback_ExchangeFails_400(t *testing.T) {
	fake := &fakeOIDC{
		issuer: "https://idp.example.com",
		exchangeFn: func(ctx context.Context, code string) (*oauth2.Token, error) {
			return nil, errors.New("bad code")
		},
	}
	srv := newOIDCTestServer(t, fake, &store.Store{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?state=stateA&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: auth.OIDCStateCookieName, Value: "stateA.nonceB"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400. Body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "OIDC_EXCHANGE_FAILED") {
		t.Errorf("body missing OIDC_EXCHANGE_FAILED: %s", rr.Body.String())
	}
}

func TestOIDCCallback_VerifyFails_400(t *testing.T) {
	fake := &fakeOIDC{
		issuer: "https://idp.example.com",
		verifyFn: func(ctx context.Context, raw, expectedNonce string) (*auth.OIDCClaims, error) {
			return nil, errors.New("verify failed")
		},
	}
	srv := newOIDCTestServer(t, fake, &store.Store{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?state=stateA&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: auth.OIDCStateCookieName, Value: "stateA.nonceB"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400. Body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "OIDC_ID_TOKEN_INVALID") {
		t.Errorf("body missing OIDC_ID_TOKEN_INVALID: %s", rr.Body.String())
	}
}

func TestOIDCCallback_MissingCode_400(t *testing.T) {
	fake := &fakeOIDC{issuer: "https://idp.example.com"}
	srv := newOIDCTestServer(t, fake, &store.Store{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?state=stateA", nil)
	req.AddCookie(&http.Cookie{Name: auth.OIDCStateCookieName, Value: "stateA.nonceB"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400. Body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "OIDC_CODE_MISSING") {
		t.Errorf("body missing OIDC_CODE_MISSING: %s", rr.Body.String())
	}
}
