package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/store"
)

// oidcStateCookieTTL is how long the state+nonce cookie stays valid.
// 5 minutes is comfortably longer than a normal IdP roundtrip while
// keeping the attack window short.
const oidcStateCookieTTL = 5 * time.Minute

// oidcStateSeparator joins state + nonce inside the single state cookie.
// "." is the only ASCII non-hex character we need to disambiguate
// hex-encoded halves.
const oidcStateSeparator = "."

// successRedirectPath is the relative URL the callback handler redirects
// the browser to after a successful OIDC login. Kept as "/" so the SPA
// owns the post-login landing logic.
const successRedirectPath = "/"

// randomHex returns hex(n random bytes). Used for state + nonce.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// setOIDCStateCookie writes the short-lived state+nonce cookie. Cookie
// uses SameSite=Lax (NOT Strict) because the IdP redirects the browser
// cross-site to /api/v1/auth/oidc/callback; Strict would drop it.
func setOIDCStateCookie(w http.ResponseWriter, state, nonce string) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.OIDCStateCookieName,
		Value:    state + oidcStateSeparator + nonce,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(oidcStateCookieTTL),
		MaxAge:   int(oidcStateCookieTTL.Seconds()),
	})
}

// clearOIDCStateCookie removes the state cookie once the callback has
// consumed it (or rejected the request).
func clearOIDCStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.OIDCStateCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

// oidcStartHandler kicks off the OIDC authorization-code flow:
//  1. mints fresh state + nonce
//  2. stores them in __Host-basement_oidc_state
//  3. 302-redirects to the IdP's authorize endpoint
func (s *Server) oidcStartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}
	if s.oidc == nil {
		writeErrorSimple(w, http.StatusNotImplemented, "OIDC_NOT_CONFIGURED", "OIDC is not configured on this server")
		return
	}

	state, err := randomHex(16)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL", "Failed to generate state")
		return
	}
	nonce, err := randomHex(16)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL", "Failed to generate nonce")
		return
	}

	setOIDCStateCookie(w, state, nonce)

	http.Redirect(w, r, s.oidc.AuthCodeURL(state, nonce), http.StatusFound)
}

// oidcCallbackHandler handles the IdP's redirect back to basement:
//  1. reads + clears the state cookie
//  2. compares the cookie's state half to the query's state (CSRF guard)
//  3. exchanges the authorization code for an OAuth2 token
//  4. verifies the ID token (signature, audience, expiry, nonce)
//  5. looks up the user by (provider, subject); creates one only if
//     AutoProvision=true
//  6. issues the session JWT and 302s to "/"
func (s *Server) oidcCallbackHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}
	if s.oidc == nil {
		writeErrorSimple(w, http.StatusNotImplemented, "OIDC_NOT_CONFIGURED", "OIDC is not configured on this server")
		return
	}

	cookie, err := r.Cookie(auth.OIDCStateCookieName)
	clearOIDCStateCookie(w)
	if err != nil || cookie.Value == "" {
		writeErrorSimple(w, http.StatusBadRequest, "OIDC_STATE_MISSING", "OIDC state cookie missing or expired")
		return
	}

	cookieState, cookieNonce, ok := strings.Cut(cookie.Value, oidcStateSeparator)
	if !ok || cookieState == "" || cookieNonce == "" {
		writeErrorSimple(w, http.StatusBadRequest, "OIDC_STATE_INVALID", "OIDC state cookie malformed")
		return
	}

	queryState := r.URL.Query().Get("state")
	if queryState == "" || queryState != cookieState {
		writeErrorSimple(w, http.StatusBadRequest, "OIDC_STATE_MISMATCH", "OIDC state mismatch")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		writeErrorSimple(w, http.StatusBadRequest, "OIDC_CODE_MISSING", "Missing authorization code")
		return
	}

	tok, err := s.oidc.Exchange(r.Context(), code)
	if err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "OIDC_EXCHANGE_FAILED", "Failed to exchange authorization code")
		return
	}

	rawIDToken, err := auth.IDTokenFromOAuth2(tok)
	if err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "OIDC_ID_TOKEN_MISSING", "Provider response missing id_token")
		return
	}

	claims, err := s.oidc.VerifyIDToken(r.Context(), rawIDToken, cookieNonce)
	if err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "OIDC_ID_TOKEN_INVALID", "ID token verification failed")
		return
	}

	user, err := s.store.FindUserByProviderSubject(claims.Provider, claims.Subject)
	if err != nil {
		if !errors.Is(err, store.ErrUserNotFound) {
			writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL", "user lookup failed")
			return
		}

		if !s.oidc.AutoProvision() {
			writeErrorSimple(w, http.StatusForbidden, "USER_NOT_PROVISIONED", "Your account is not provisioned. Contact your administrator.")
			return
		}

		user = store.User{
			ID:       uuid.New().String(),
			Username: usernameFromClaims(claims),
			Role:     "user",
			Provider: claims.Provider,
			Subject:  claims.Subject,
			Email:    claims.Email,
			Name:     claims.Name,
		}
		if err := s.store.CreateUser(user); err != nil {
			writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL", "Failed to create user")
			return
		}
	}

	ttl := s.cfg.SessionTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	token, err := auth.IssueToken(s.cfg.JWT.Secret, user.ID, user.Role, ttl)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "TOKEN_ISSUE", "Failed to issue session token")
		return
	}

	auth.SetSessionCookie(w, token, ttl)

	http.Redirect(w, r, successRedirectPath, http.StatusFound)
}

// usernameFromClaims picks a stable display username for a new
// auto-provisioned user. Preference: email > name > "subject@issuer".
func usernameFromClaims(c *auth.OIDCClaims) string {
	if c.Email != "" {
		return c.Email
	}
	if c.Name != "" {
		return c.Name
	}
	return c.Subject + "@" + c.Provider
}
