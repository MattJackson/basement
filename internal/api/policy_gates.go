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
	"net/http"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/auth/policy"
)

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
func (permissiveEnforcer) SeedEnvAdmin(_ string) error                          { return nil }

// requireCapability resolves the caller, runs s.policy.Can on the
// requested (capability, scope), and short-circuits the response on
// failure. Returns the caller's userID + true when the check passes.
//
// 503 POLICY_NOT_WIRED if the enforcer hasn't been set on the server
// (defense against misconfigured boots — better to fail loud than
// silently allow). 401 / 403 otherwise.
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

	return claims.UserID, true
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
