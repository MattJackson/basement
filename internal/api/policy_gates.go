// Package api: capability gates + per-user driver resolution
// (ADR-0001, v0.9.0f).
//
// All user- and admin-facing handlers go through a tiny helper layer so
// the gate pattern stays uniform and grep-able:
//
//   - requireCapability(w, r, capID, scope) -> userID, ok
//     Resolves the caller from the JWT, asks s.policy.Can, writes
//     401 / 403 as appropriate, and short-circuits the handler when
//     the check fails.
//
// The legacy UIAdmin middleware that protects /admin/* still runs;
// capability checks ADD a finer layer per ADR-0001's "defense in
// depth" note. Once /admin/policies (v0.9.0g) lets operators rebalance
// the matrix and the seed assignments cover everyone who used to be
// an UIAdmin, the UIAdmin middleware can retire.
//
// ADR-0002 (v1.1.0e) note: the per-bucket userGrantDriver / NO_GRANT
// helpers retired with the legacy cluster-tier user surface. User-tier
// S3 access now flows through the region keychain — see user_regions.go
// for the requireOwnedRegion + regionDriver pair that replaces it.
package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/auth/policy"
)

// preV12GraceUntil is how long the gate treats pre-v1.2.0a cookies
// (no Mode claim) as ADMIN for back-compat. After the grace window
// elapses, such cookies drop to USER and the user must log in again
// to mint a v1.2-shaped token. See ADR-0003 "Backwards compatibility".
//
// Resolved at startup time, not request time, so the window starts on
// the v1.2.0a deploy date — operators upgrading mid-month don't see
// the window slide. 7 * 24h per the prompt.
var preV12GraceUntil = time.Now().Add(7 * 24 * time.Hour)

// nowFunc is overrideable in tests so expiry/mode tests don't have to
// sleep through real time.
var nowFunc = time.Now

// currentMode reads the session's current mode from the JWT claims in
// the request context, applying the two rules from ADR-0003:
//
//  1. ModeExpiresAt < now() → downgrade to USER for this request (the
//     cookie itself is not re-issued here; downstream handlers that
//     mint a fresh cookie will pick up the downgraded mode).
//  2. No Mode claim at all (pre-v1.2 cookie) → treat as ADMIN for the
//     v1.2-grace window so matthew's existing session keeps working,
//     then drop to USER after the window. A slog.Warn fires the first
//     time per call.
//
// Returns ModeUser if there are no claims at all (handler will 401
// downstream anyway).
func currentMode(r *http.Request) policy.Mode {
	claims, ok := auth.FromContext(r.Context())
	if !ok || claims == nil {
		return policy.ModeUser
	}

	now := nowFunc()

	// Pre-v1.2 cookie: no Mode claim at all. Back-compat: treat as
	// ADMIN for the grace window so the matthew session minted before
	// the v1.2.0a deploy doesn't get a wave of 403s on next request.
	if claims.Mode == "" {
		if now.Before(preV12GraceUntil) {
			slog.Warn("auth: pre-v1.2 JWT claims seen; treating as admin for back-compat grace window",
				"user", claims.UserID, "grace_until", preV12GraceUntil.Format(time.RFC3339))
			return policy.ModeAdmin
		}
		// Past the grace: pre-v1.2 cookie loses its admin privilege.
		slog.Warn("auth: pre-v1.2 JWT claims past grace window; dropping to user mode",
			"user", claims.UserID)
		return policy.ModeUser
	}

	// Silent migration (v1.3.0a.4): v1.2-era cookies carry mode="elevated".
	// The amendment collapsed ELEVATED into ADMIN; treat the legacy
	// string as ADMIN on read so an in-flight session doesn't need to
	// log out across the upgrade. The cookie keeps its original
	// ModeExpiresAt — the next mint will write the canonical "admin"
	// string. (ModeElevated is also a string-alias for ModeAdmin, so
	// downstream comparisons would work either way; this normalisation
	// is for clarity in audit logs + the FE's mode echo.)
	rawMode := claims.Mode
	if rawMode == "elevated" {
		rawMode = "admin"
	}
	mode := policy.Mode(rawMode)

	// Mode expired since the cookie was minted → downgrade to USER for
	// this request. The cookie itself is not rewritten here; downstream
	// handlers that mint a fresh cookie pick up the downgrade. USER
	// never expires.
	if claims.ModeExpiresAt > 0 && now.Unix() >= claims.ModeExpiresAt {
		if mode == policy.ModeAdmin {
			return policy.ModeUser
		}
	}

	return mode
}

// permissiveEnforcer is the default enforcer installed by api.New() to
// keep test callers that don't care about RBAC working. It grants
// every capability at every scope and no-ops the mutation methods.
// Production main.go REPLACES this with a real file-backed enforcer
// via SetPolicy() before Start(), so this never serves real traffic.
type permissiveEnforcer struct{}

func (permissiveEnforcer) Can(userID, capability, scope string) bool { return userID != "" }
func (permissiveEnforcer) CanWithReason(userID, capability, scope string) (bool, []policy.RoleAssignment, []policy.ReasoningStep) {
	// Mirrors Can: any non-empty user is allowed. The single reasoning
	// step makes it obvious in test output that the permissive stub
	// (not a real enforcer) produced this answer.
	if userID == "" {
		return false, nil, []policy.ReasoningStep{{
			Step:   "permissive enforcer: empty user",
			Detail: "no JWT user id present",
		}}
	}
	return true, nil, []policy.ReasoningStep{{
		Step:   "permissive enforcer",
		Detail: "test default grants every capability at every scope to authenticated users",
	}}
}
func (permissiveEnforcer) Capabilities(userID, scope string) []string {
	// Returning empty here is fine — Capabilities() is for UI gating
	// (which buttons to render), and tests that use the permissive
	// default never inspect this. Real-policy tests install a real
	// enforcer.
	return []string{}
}
func (permissiveEnforcer) AssignmentsFor(userID string) []policy.RoleAssignment { return nil }
func (permissiveEnforcer) Roles() []policy.Role                                 { return nil }
func (permissiveEnforcer) Assignments() []policy.RoleAssignment                 { return nil }
func (permissiveEnforcer) UpsertRole(_ policy.Role) error                       { return nil }
func (permissiveEnforcer) DeleteRole(_ string) error                            { return nil }
func (permissiveEnforcer) AssignRole(_ policy.RoleAssignment) error             { return nil }
func (permissiveEnforcer) UnassignRole(_, _, _ string) error                    { return nil }
func (permissiveEnforcer) SyncOIDCAssignments(_ string, _ []policy.RoleAssignment) ([]policy.RoleAssignment, []policy.RoleAssignment, error) {
	return nil, nil, nil
}
func (permissiveEnforcer) SeedEnvAdmin(_ string) error { return nil }

// requireCapability resolves the caller, runs s.policy.Can on the
// requested (capability, scope), and short-circuits the response on
// failure. Returns the caller's userID + true when the check passes.
//
// 503 POLICY_NOT_WIRED if the enforcer hasn't been set on the server
// (defense against misconfigured boots — better to fail loud than
// silently allow). 401 / 403 otherwise.
//
// v1.7.0b: when the request authed via a service-account bearer token
// (claims.ServiceAccountID != ""), the policy check is routed through
// policy.ServiceAccountAllows on the SA row's capability + scope
// bundle rather than the user's role assignments. SA tokens cannot
// elevate (sudo mode requires fresh password/OIDC), so a missing
// capability returns 403 ELEVATION_NOT_AVAILABLE rather than
// ELEVATION_REQUIRED — the FE shouldn't render an "elevate" CTA for
// machine clients.
func (s *Server) requireCapability(w http.ResponseWriter, r *http.Request, capability, scope string) (userID string, ok bool) {
	if s.policy == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "POLICY_NOT_WIRED",
			"Policy subsystem is not configured on this deployment.")
		return "", false
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return "", false
	}

	// Service-account branch (v1.7.0b). Bearer-authed requests bypass
	// the JWT mode-elevation machinery entirely — there's no cookie to
	// upgrade and no sudo flow available — so the entire gate boils
	// down to "does the SA's capability bundle cover this call?".
	if claims.ServiceAccountID != "" {
		return s.requireCapabilityServiceAccount(w, r, claims, capability, scope)
	}

	if !s.policy.Can(claims.UserID, capability, scope) {
		// Per v1.0.0c: a forbidden capability check is an audit-
		// worthy event — operators want to see "alice tried to
		// delete cluster prod and was blocked" so they can
		// investigate. The Resource encodes capability+scope so
		// the audit log self-documents what the user was after.
		s.auditFailureDetail(r, "auth:forbidden", capability+"@"+scope, "policy denied "+capability+" on "+scope)

		writeErrorSimple(w, http.StatusForbidden, "FORBIDDEN",
			fmt.Sprintf("Requires %s on %s", capability, scope))
		return "", false
	}

	// ADR-0003 mode gate: a user may HOLD the capability via their
	// role assignments yet still need to elevate their session before
	// exercising it. cluster:delete in USER mode → 403 ELEVATION_REQUIRED
	// with a structured payload the FE uses to pop the elevation modal
	// in-line (v1.2.0b) instead of navigating to a re-auth page.
	required := policy.MinModeFor(capability)
	current := currentMode(r)
	if !current.Includes(required) {
		// Audit the elevation prompt. Records what they tried + what
		// mode they were in so an operator scanning for unusual
		// patterns sees "alice tried bucket:delete in USER mode" as
		// a distinct event from a forbidden capability denial.
		s.audit.Log(audit.Event{
			Time:      nowFunc().UTC(),
			Actor:     claims.UserID,
			ActorRole: string(current),
			Action:    "auth:elevation_required",
			Resource:  capability + "@" + scope,
			Detail:    fmt.Sprintf("required=%s current=%s", required, current),
			Result:    audit.ResultFailure,
			IP:        clientIP(r),
			UserAgent: r.UserAgent(),
		})

		writeError(w, http.StatusForbidden, "ELEVATION_REQUIRED",
			"Re-authentication required to perform this action.",
			map[string]any{
				"mode_required": string(required),
				"current_mode":  string(current),
				"endpoint":      "/api/v1/auth/elevate",
			})
		return "", false
	}

	return claims.UserID, true
}

// requireCapabilityServiceAccount is the v1.7.0b SA branch of
// requireCapability. Resolves the SA row from claims.ServiceAccountID,
// asks policy.ServiceAccountAllows whether the SA's bundle covers
// (capability, scope), and writes a 403 ELEVATION_NOT_AVAILABLE if not.
//
// ELEVATION_NOT_AVAILABLE is a deliberate variant of ELEVATION_REQUIRED
// for machine clients: the FE shouldn't render an "elevate your
// session" CTA for a CI runner, and a CLI library shouldn't loop
// retrying an action it structurally cannot perform. The error code
// makes the difference machine-readable on the caller side.
//
// On a successful gate we audit-attribute the action with actor =
// "sa:{SA.ID}" so the audit log can distinguish "matthew via
// service-account ci-prod" from "matthew via cookie" — the resource
// trail is the same, but the principal is different. The caller of
// this method returns the userID (SA's OwnerUserID), keeping the
// downstream handler's "who owns this action" attribution sensible.
func (s *Server) requireCapabilityServiceAccount(w http.ResponseWriter, r *http.Request, claims *auth.Claims, capability, scope string) (userID string, ok bool) {
	// Resolve the SA row. The store handle hangs off s.store; tests
	// that don't wire it up should not be exercising the SA branch
	// (they'd need ServiceAccountID claims, which only the bearer
	// middleware sets).
	if s.store == nil || s.store.ServiceAccounts() == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "SERVICE_ACCOUNTS_NOT_WIRED",
			"Service account subsystem is not configured on this deployment.")
		return "", false
	}
	saRow, getErr := s.store.ServiceAccounts().Get(r.Context(), claims.ServiceAccountID)
	if getErr != nil {
		// The middleware already verified the SA exists at request
		// time, so a Get failure here means the SA was revoked
		// between auth + gate check (a narrow race). Treat as 401
		// — the credential is no longer valid for THIS request.
		writeErrorSimple(w, http.StatusUnauthorized, "SERVICE_ACCOUNT_REVOKED",
			"Service account is no longer valid")
		return "", false
	}

	if !policy.ServiceAccountAllows(saRow, capability, scope) {
		// Audit the denial. Actor is the SA — gives the operator a
		// signal of "ci-prod tried to delete bucket X" rather than
		// "matthew tried..." which would mis-attribute to the human
		// owner. ActorRole stays "service_account" for the same
		// reason; differentiating M2M denials from human denials in a
		// dashboard query needs both axes.
		s.audit.Log(audit.Event{
			Time:      nowFunc().UTC(),
			Actor:     "sa:" + saRow.ID,
			ActorRole: "service_account",
			Action:    "auth:elevation_not_available",
			Resource:  capability + "@" + scope,
			Detail:    fmt.Sprintf("sa %q lacks %s on %s", saRow.Name, capability, scope),
			Result:    audit.ResultFailure,
			IP:        clientIP(r),
			UserAgent: r.UserAgent(),
		})

		writeError(w, http.StatusForbidden, "ELEVATION_NOT_AVAILABLE",
			"This service account cannot perform this action.",
			map[string]any{
				"capability":          capability,
				"scope":               scope,
				"service_account_id":  saRow.ID,
			})
		return "", false
	}

	return claims.UserID, true
}

// saActor returns the audit actor string for an authed request: the
// canonical "sa:{ID}" form when a service-account token authed the
// call, falling back to claims.UserID for cookie-authed humans. Used
// by audit_helpers.go so SA-authed mutations attribute correctly.
func saActor(claims *auth.Claims) string {
	if claims == nil {
		return ""
	}
	if claims.ServiceAccountID != "" {
		return "sa:" + claims.ServiceAccountID
	}
	return claims.UserID
}

// scopeBucket builds a "bucket:{cid}:{bid}" scope string. Centralised
// so a future scope-grammar change (e.g. adding cluster role to the
// path) updates every call site at once.
func scopeBucket(cid, bid string) string {
	return "bucket:" + cid + ":" + bid
}

// scopeCluster builds a "cluster:{cid}" scope string.
func scopeCluster(cid string) string {
	return "cluster:" + cid
}
