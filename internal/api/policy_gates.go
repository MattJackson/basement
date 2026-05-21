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
//   - userGrantDriver(ctx, userID, cid, bid) -> Driver, error
//     Looks up the user's BucketGrant for (cid, bid), decrypts the
//     stored secret, and asks the Registry for a Driver bound to those
//     creds. Returns ErrNoGrant when the user has no grant; caller maps
//     to 403 / NO_GRANT.
//
// The legacy UIAdmin middleware that protects /admin/* still runs;
// capability checks ADD a finer layer per ADR-0001's "defense in
// depth" note. Once /admin/policies (v0.9.0g) lets operators rebalance
// the matrix and the seed assignments cover everyone who used to be
// an UIAdmin, the UIAdmin middleware can be retired.
package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// permissiveEnforcer is the default enforcer installed by api.New() to
// keep test callers that don't care about RBAC working. It grants
// every capability at every scope and no-ops the mutation methods.
// Production main.go REPLACES this with a real file-backed enforcer
// via SetPolicy() before Start(), so this never serves real traffic.
//
// Tests that DO care about RBAC (user_buckets_connect_test.go, the
// new v0.9.0f gate tests) call srv.SetPolicy(real) and override it.
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

// ErrNoGrant is returned by userGrantDriver when the calling user has
// no BucketGrant for the (cid, bid) pair. Callers map to 403 NO_GRANT.
var ErrNoGrant = errors.New("no bucket access grant for this user")

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
		writeErrorSimple(w, http.StatusForbidden, "FORBIDDEN",
			fmt.Sprintf("Requires %s on %s", capability, scope))
		return "", false
	}

	return claims.UserID, true
}

// userGrantDriver resolves the per-user S3 driver for (cid, bid). The
// returned Driver signs requests with the user's BucketGrant key so
// backend audit logs attribute activity to the right identity.
//
// Returns ErrNoGrant when the user has no grant on the bucket; that
// case is distinct from a registry / decryption / driver-build failure
// (which return wrapped errors).
//
// Order matters: we look up the grant BEFORE checking the registry so
// "no grant" stays distinguishable from "subsystem misconfigured" —
// otherwise an unwired registry in tests would mask a legitimate
// NO_GRANT response.
func (s *Server) userGrantDriver(ctx context.Context, userID, cid, bid string) (driver.Driver, error) {
	if s.store == nil || s.store.CredGrants() == nil {
		return nil, fmt.Errorf("credential-grant store not wired")
	}

	grant, err := s.store.CredGrants().GetByUserBucket(ctx, userID, cid, bid)
	if err != nil {
		if errors.Is(err, store.ErrBucketGrantNotFound) {
			return nil, ErrNoGrant
		}
		return nil, fmt.Errorf("looking up bucket grant: %w", err)
	}

	if s.reg == nil {
		return nil, fmt.Errorf("driver registry not wired")
	}

	secretKey, err := s.store.CredGrants().Decrypt(grant)
	if err != nil {
		return nil, fmt.Errorf("decrypting grant secret: %w", err)
	}

	drv, err := s.reg.ForUserGrant(ctx, cid, grant.AccessKeyID, secretKey)
	if err != nil {
		return nil, fmt.Errorf("building per-user driver: %w", err)
	}
	return drv, nil
}

// writeNoGrant emits the standard 403 NO_GRANT response for user-side
// endpoints when the calling user has no BucketGrant for the bucket.
// Distinct error code from FORBIDDEN so the UI can surface a tailored
// "Connect this bucket" CTA instead of a generic permission error.
func writeNoGrant(w http.ResponseWriter) {
	writeErrorSimple(w, http.StatusForbidden, "NO_GRANT",
		"No bucket access grant for this user. Connect the bucket from your home page first.")
}

// writeGrantInternalError emits a 500 for unexpected grant-driver
// failures (decrypt error, registry build error). Distinct from
// NO_GRANT so operator can tell "the user just doesn't have access"
// from "the grant subsystem is broken."
func writeGrantInternalError(w http.ResponseWriter, err error) {
	writeErrorSimple(w, http.StatusInternalServerError, "GRANT_INTERNAL",
		"Internal error resolving bucket grant: "+err.Error())
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
