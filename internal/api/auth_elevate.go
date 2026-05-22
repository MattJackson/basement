// Package api: sudo-style elevation endpoint (ADR-0003 + v1.3.0a.4
// amendment).
//
// POST /api/v1/auth/elevate
//
//	Request:  {"password": "...", "target_mode": "admin"}
//	          (legacy v1.2 callers may send "elevated"; the dispatcher
//	          silently treats it as "admin" per the amendment.)
//	Response: 200 + Set-Cookie with a fresh JWT carrying Mode="admin" +
//	          ModeExpiresAt. Returns the granted mode + expiry in the
//	          body so the FE can drive its countdown chip without
//	          having to parse the cookie.
//
// State-machine rules enforced here:
//
//   - USER → ADMIN: password re-auth required. TTL comes from
//     OrgCapabilities.AdminSessionTTLSec (operator-configurable via
//     /admin/system; default 15 min, range 60s – 24h).
//   - target_mode="elevated" (v1.2 wire shape): silently rewritten to
//     "admin" and the flow proceeds normally.
//
// Backwards compatibility: pre-v1.2 cookies with no Mode claim are
// treated as ADMIN by the gate's currentMode() helper for a 7-day
// grace window — so an existing matthew session can elevate through
// this endpoint without re-logging-in. See policy_gates.go for the
// grace logic.
//
// v1.2.0c update: OIDC-only users no longer 403 here. The dispatcher
// pivots them into the OIDC elevation flow by returning 200 +
// {requires_oidc: true, start_url: "/api/v1/auth/elevate/oidc/start"}
// — the FE then POSTs to that URL and full-page-navigates to the IdP.
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/store"
)

// adminModeTTL is the legacy v1.2 hardcoded default. Kept as a
// fallback when the org capabilities store isn't wired (e.g. tests
// that build Server with a zero-value Store) so the elevate flow can
// still stamp a sensible expiry. Production callers go through
// adminSessionTTLFor() which prefers the operator-configured value.
const adminModeTTL = time.Duration(store.AdminSessionTTLDefaultSec) * time.Second

// adminSessionTTLFor resolves the operator-configured admin session
// TTL from OrgCapabilities, clamped into [60s, 24h]. Falls back to
// adminModeTTL when the store isn't available (test paths) so the
// caller always gets a usable duration. The clamping uses the same
// ClampAdminSessionTTL helper Update() uses on the write side, so the
// floor + ceiling are enforced regardless of how the on-disk file got
// into its current state.
func (s *Server) adminSessionTTL() time.Duration {
	if s.store == nil {
		return adminModeTTL
	}
	caps := s.store.OrgCapabilities()
	if caps == nil {
		return adminModeTTL
	}
	secs := store.ClampAdminSessionTTL(caps.Get().AdminSessionTTLSec)
	return time.Duration(secs) * time.Second
}

// elevateRequest is the POST body shape.
type elevateRequest struct {
	Password   string `json:"password"`
	TargetMode string `json:"target_mode"`
}

// elevateResponse is the 200 body shape — mirrors what the FE needs to
// drive the persona pill countdown without parsing the JWT itself.
//
// RequiresOIDC + StartURL are populated only on the OIDC pivot
// (v1.2.0c): when the dispatcher detects an OIDC-only user it returns
// 200 with RequiresOIDC=true so the FE knows to POST to StartURL and
// follow the returned redirect_url. Mode + ModeExpiresAt + TTL are
// zero in that case because no elevation has happened yet.
type elevateResponse struct {
	Mode           string `json:"mode,omitempty"`
	ModeExpiresAt  int64  `json:"mode_expires_at,omitempty"` // unix seconds
	ModeTTLSeconds int64  `json:"mode_ttl_seconds,omitempty"`
	RequiresOIDC   bool   `json:"requires_oidc,omitempty"`
	StartURL       string `json:"start_url,omitempty"`
}

// elevateHandler handles POST /api/v1/auth/elevate.
func (s *Server) elevateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok || claims == nil || claims.UserID == "" {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	var req elevateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	// Validate target mode. Per the v1.3.0a.4 amendment only "admin"
	// is a real target — but v1.2-era FE code may still send
	// "elevated"; we silently rewrite that to "admin" so a half-
	// upgraded deploy doesn't reject in-flight elevation submits.
	target := policy.Mode(req.TargetMode)
	if target == "elevated" {
		target = policy.ModeAdmin
	}
	if target != policy.ModeAdmin {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_TARGET_MODE",
			"target_mode must be \"admin\"")
		return
	}

	current := currentMode(r)

	// OIDC-only pivot (v1.2.0c): a user whose only credential is
	// OIDC has no password to verify here. Instead of failing, return
	// 200 + {requires_oidc:true, start_url} so the FE knows to switch
	// to the OIDC challenge flow (it then POSTs the start URL and
	// follows the returned redirect_url to the IdP).
	//
	// The env-seeded admin (claims.UserID == adminUser) always has a
	// local password and is exempt. If OIDC is configured-but-the-user-
	// is-OIDC-only AND s.oidc is nil, we 503 OIDC_NOT_CONFIGURED so
	// the FE renders the "contact your administrator" message.
	loadAdminCreds(s.cfg)
	if claims.UserID != adminUser && s.store != nil {
		if u, err := s.store.UserByUsername(claims.UserID); err == nil {
			if u.PasswordHash == "" && u.Provider != "" {
				if s.oidc == nil {
					s.audit.Log(audit.Event{
						Time:      time.Now().UTC(),
						Actor:     claims.UserID,
						ActorRole: string(current),
						Action:    "auth:elevate_oidc_not_configured",
						Resource:  "mode:" + string(target),
						Detail:    "OIDC-only user requested elevation but OIDC is not configured on this server",
						Result:    audit.ResultFailure,
						IP:        clientIP(r),
						UserAgent: r.UserAgent(),
					})
					writeErrorSimple(w, http.StatusServiceUnavailable,
						"OIDC_NOT_CONFIGURED",
						"OIDC is not configured on this server; contact your administrator to enable elevation.")
					return
				}
				s.audit.Log(audit.Event{
					Time:      time.Now().UTC(),
					Actor:     claims.UserID,
					ActorRole: string(current),
					Action:    "auth:elevate_oidc_pivot",
					Resource:  "mode:" + string(target),
					Detail:    "OIDC-only user; FE will be redirected to /api/v1/auth/elevate/oidc/start",
					Result:    audit.ResultSuccess,
					IP:        clientIP(r),
					UserAgent: r.UserAgent(),
				})
				writeJSON(w, http.StatusOK, elevateResponse{
					RequiresOIDC: true,
					StartURL:     "/api/v1/auth/elevate/oidc/start",
				})
				return
			}
		}
	}

	// Verify the password. Two paths:
	//   - The env-seeded admin: compare against cfg.Admin.PasswordHash.
	//   - A store-backed local user: compare against their PasswordHash.
	// Either way, a failure 401s and emits an audit failure event.
	if !s.verifyElevationPassword(claims.UserID, req.Password) {
		s.audit.Log(audit.Event{
			Time:      time.Now().UTC(),
			Actor:     claims.UserID,
			ActorRole: string(current),
			Action:    "auth:elevate_failure",
			Resource:  "mode:" + string(target),
			Detail:    "invalid password",
			Result:    audit.ResultFailure,
			IP:        clientIP(r),
			UserAgent: r.UserAgent(),
		})
		writeErrorSimple(w, http.StatusUnauthorized, "INVALID_PASSWORD", "Invalid password")
		return
	}

	// Pick TTL from OrgCapabilities.AdminSessionTTLSec
	// (operator-configurable per the v1.3.0a.4 amendment). Two modes
	// only now; target is always ModeAdmin at this point.
	ttl := s.adminSessionTTL()

	// Session JWT TTL stays at the configured session lifetime — we're
	// only changing the mode claim + its expiry, not the outer session
	// lifetime. Falling back to 24h matches loginHandler's behaviour.
	sessionTTL := s.cfg.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = 24 * time.Hour
	}

	modeExpiresAt := time.Now().Add(ttl).Unix()

	token, err := auth.IssueTokenWithMode(
		s.cfg.JWT.Secret,
		claims.UserID,
		claims.Role,
		claims.UIAdmin,
		string(target),
		modeExpiresAt,
		sessionTTL,
	)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "TOKEN_ISSUE", "Failed to issue session token")
		return
	}

	auth.SetSessionCookie(w, token, sessionTTL)

	s.audit.Log(audit.Event{
		Time:      time.Now().UTC(),
		Actor:     claims.UserID,
		ActorRole: string(target),
		Action:    "auth:elevate_success",
		Resource:  "mode:" + string(target),
		Detail:    "elevated from " + string(current) + " to " + string(target),
		Result:    audit.ResultSuccess,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	})

	writeJSON(w, http.StatusOK, elevateResponse{
		Mode:           string(target),
		ModeExpiresAt:  modeExpiresAt,
		ModeTTLSeconds: int64(ttl.Seconds()),
	})
}

// verifyElevationPassword checks the password against whichever
// credential store backs the calling user. The env-seeded admin
// (claims.UserID == cfg.Admin.User) verifies against cfg.Admin.PasswordHash;
// any other user looks up the store record and verifies against
// PasswordHash there. A store user with an empty PasswordHash (OIDC-
// only) returns false here — the OIDC short-circuit above should have
// caught them already, this is the belt-and-braces guard.
func (s *Server) verifyElevationPassword(userID, password string) bool {
	if password == "" {
		return false
	}

	if userID == adminUser {
		return auth.VerifyPassword(adminHash, password)
	}

	if s.store == nil {
		return false
	}
	u, err := s.store.UserByUsername(userID)
	if err != nil {
		return false
	}
	if u.PasswordHash == "" {
		return false
	}
	return auth.VerifyPassword(u.PasswordHash, password)
}
