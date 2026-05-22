package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/auth/policy"
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

	claims, allClaims, err := s.oidc.VerifyIDTokenWithAllClaims(r.Context(), rawIDToken, cookieNonce)
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
			UIAdmin:  false,
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

	// v1.3.0a: apply the operator-configured OIDC group-claim ->
	// role mapping. Best-effort: a sync failure logs + audits but
	// does NOT block the login — falling back to "whatever
	// assignments existed before this login" is safer than locking
	// the user out of a deployment they have a valid session for.
	s.syncOIDCRoleAssignments(r, user.ID, allClaims)

	ttl := s.cfg.SessionTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	token, err := auth.IssueToken(s.cfg.JWT.Secret, user.ID, user.Role, user.UIAdmin, ttl)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "TOKEN_ISSUE", "Failed to issue session token")
		return
	}

	auth.SetSessionCookie(w, token, ttl)

	http.Redirect(w, r, successRedirectPath, http.StatusFound)
}

// syncOIDCRoleAssignments runs the v1.3.0a group-claim -> role
// auto-assignment reconcile for a freshly-logged-in OIDC user.
//
// Steps:
//
//  1. Load the operator-configured mapping list from the store. If the
//     store is unwired (tests) or the mapping list is empty, exit early
//     — no work to do, no audit noise to emit.
//  2. For each mapping whose claim value appears in the user's ID
//     token, build a wanted (RoleID, Scope) triple.
//  3. Hand the wanted set to the enforcer's SyncOIDCAssignments which
//     atomically reconciles the user's Source="oidc" rows.
//  4. Audit-log one event per added/revoked assignment so an operator
//     can trace "alice gained host_admin because she joined the
//     'platform-admins' group in Authentik" or the inverse.
//
// Failures are logged + audited but never block login. Falling back to
// "no change" is safer than locking the user out of an already-valid
// session.
func (s *Server) syncOIDCRoleAssignments(r *http.Request, userID string, allClaims map[string]interface{}) {
	if s.policy == nil || userID == "" {
		return
	}
	if s.store == nil || s.store.OIDCGroupMappings() == nil {
		return
	}

	mappings := s.store.OIDCGroupMappings().Get().Mappings
	if len(mappings) == 0 {
		// No mappings configured: still call sync with empty `wanted`
		// so any stale Source="oidc" assignment from a previously-
		// removed mapping gets revoked. Without this, deleting every
		// mapping would leave orphaned auto-assignments behind.
	}

	// If the user has zero of the configured claims AND zero existing
	// OIDC assignments to revoke, the sync would be a no-op — skip
	// per the cycle-spec "If user has 0 group claims, code path is
	// skipped entirely" rule. We still need to revoke stale rows, so
	// keep the call when there is anything to potentially revoke.
	wantedHits := []policy.RoleAssignment{}
	for _, m := range mappings {
		if m.Claim == "" || m.ClaimValue == "" || m.RoleID == "" || m.Scope == "" {
			continue
		}
		values := auth.ClaimStringValues(allClaims, m.Claim)
		for _, v := range values {
			if v == m.ClaimValue {
				wantedHits = append(wantedHits, policy.RoleAssignment{
					UserID: userID,
					RoleID: m.RoleID,
					Scope:  m.Scope,
				})
				break
			}
		}
	}

	added, revoked, err := s.policy.SyncOIDCAssignments(userID, wantedHits)
	if err != nil {
		slog.Warn("oidc: role sync failed", "user", userID, "error", err)
		s.audit.Log(audit.Event{
			Actor:     userID,
			ActorRole: "user",
			Action:    "auth:oidc_role_sync_failed",
			Resource:  resourceUser(userID),
			Result:    audit.ResultFailure,
			Detail:    err.Error(),
			IP:        clientIP(r),
			UserAgent: r.UserAgent(),
		})
		return
	}

	for _, a := range added {
		s.audit.Log(audit.Event{
			Actor:     userID,
			ActorRole: "user",
			Action:    "auth:oidc_role_assigned",
			Resource:  resourceAssignment(a.UserID, a.RoleID, a.Scope),
			Result:    audit.ResultSuccess,
			IP:        clientIP(r),
			UserAgent: r.UserAgent(),
		})
	}
	for _, a := range revoked {
		s.audit.Log(audit.Event{
			Actor:     userID,
			ActorRole: "user",
			Action:    "auth:oidc_role_revoked",
			Resource:  resourceAssignment(a.UserID, a.RoleID, a.Scope),
			Result:    audit.ResultSuccess,
			IP:        clientIP(r),
			UserAgent: r.UserAgent(),
		})
	}
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
