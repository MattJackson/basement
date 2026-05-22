// Package api: sudo-style elevation endpoint (ADR-0003, cycle v1.2.0a).
//
// POST /api/v1/auth/elevate
//
//	Request:  {"password": "...", "target_mode": "admin" | "elevated"}
//	Response: 200 + Set-Cookie with a fresh JWT carrying Mode +
//	          ModeExpiresAt. Returns the granted mode + expiry in the
//	          body so the FE can drive its countdown chip without
//	          having to parse the cookie.
//
// State-machine rules enforced here:
//
//   - USER → ADMIN: password re-auth required, 15min TTL.
//   - ADMIN → ELEVATED: password re-auth required, 5min TTL.
//   - USER → ELEVATED directly: rejected (400 INVALID_TARGET_MODE).
//     The operator must elevate through ADMIN first; this matches
//     Linux sudo's "you can't skip a level" model.
//   - OIDC-only users: rejected here (403 OIDC_USER_ELEVATION_NOT_SUPPORTED).
//     v1.2.0c will wire the OIDC `prompt=login` challenge for them.
//
// Backwards compatibility: pre-v1.2 cookies with no Mode claim are
// treated as ADMIN by the gate's currentMode() helper for a 7-day
// grace window — so an existing matthew session can elevate to ELEVATED
// directly through this endpoint without re-logging-in. See
// policy_gates.go for the grace logic.
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/auth/policy"
)

// Elevation TTLs. ADR-0003 calls these the "idle timeouts" — they're
// the maximum mode lifetime, not idle resets (the FE may bump on
// activity in a later cycle). Defaults in code; future cycles wire
// BASEMENT_ADMIN_TTL_SEC / BASEMENT_ELEVATED_TTL_SEC env overrides.
const (
	adminModeTTL    = 15 * time.Minute
	elevatedModeTTL = 5 * time.Minute
)

// elevateRequest is the POST body shape.
type elevateRequest struct {
	Password   string `json:"password"`
	TargetMode string `json:"target_mode"`
}

// elevateResponse is the 200 body shape — mirrors what the FE needs to
// drive the persona pill countdown without parsing the JWT itself.
type elevateResponse struct {
	Mode            string `json:"mode"`
	ModeExpiresAt   int64  `json:"mode_expires_at"` // unix seconds
	ModeTTLSeconds  int64  `json:"mode_ttl_seconds"`
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

	// Validate target mode. Anything outside {admin, elevated} is a
	// 400; USER as a target is meaningless via this endpoint (there's
	// a separate "drop privileges" path in v1.2.0b).
	target := policy.Mode(req.TargetMode)
	if target != policy.ModeAdmin && target != policy.ModeElevated {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_TARGET_MODE",
			"target_mode must be \"admin\" or \"elevated\"")
		return
	}

	// State-machine guard: USER can only step up to ADMIN. USER →
	// ELEVATED is rejected so the FE never accidentally bypasses the
	// "are you sure you want admin?" prompt by jumping two levels.
	current := currentMode(r)
	if current == policy.ModeUser && target == policy.ModeElevated {
		writeError(w, http.StatusBadRequest, "INVALID_TARGET_MODE",
			"Cannot elevate directly from user to elevated; elevate to admin first.",
			map[string]any{
				"current_mode":  string(current),
				"target_mode":   string(target),
				"required_path": []string{"admin", "elevated"},
			})
		return
	}

	// OIDC-only check: a user whose only credential is OIDC has no
	// password to verify here. v1.2.0c wires the OIDC `prompt=login`
	// challenge — until then we 403 with a discoverable error code.
	// The env-seeded admin (claims.UserID == adminUser) always has a
	// local password, so it's exempt.
	loadAdminCreds(s.cfg)
	if claims.UserID != adminUser {
		if s.store != nil {
			if u, err := s.store.UserByUsername(claims.UserID); err == nil {
				if u.PasswordHash == "" && u.Provider != "" {
					s.audit.Log(audit.Event{
						Time:      time.Now().UTC(),
						Actor:     claims.UserID,
						ActorRole: string(current),
						Action:    "auth:elevate_oidc_unsupported",
						Resource:  "mode:" + string(target),
						Detail:    "OIDC-only user attempted password elevation (v1.2.0c will wire OIDC challenge)",
						Result:    audit.ResultFailure,
						IP:        clientIP(r),
						UserAgent: r.UserAgent(),
					})
					writeErrorSimple(w, http.StatusForbidden,
						"OIDC_USER_ELEVATION_NOT_SUPPORTED",
						"OIDC users cannot elevate via password; OIDC challenge support arrives in v1.2.0c.")
					return
				}
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

	// Pick TTL by target mode + mint a fresh cookie with bumped mode.
	var ttl time.Duration
	switch target {
	case policy.ModeAdmin:
		ttl = adminModeTTL
	case policy.ModeElevated:
		ttl = elevatedModeTTL
	}

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
