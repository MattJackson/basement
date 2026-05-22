// Package policy: service-account capability matching (v1.7.0b).
//
// Service-account-authed requests don't go through the JWT enforcer's
// role/assignment matrix. Instead the bearer-auth middleware
// (internal/auth/bearer.go) hands a serviceaccount.ServiceAccount to
// the gate, and the gate asks ServiceAccountAllows whether THAT SA's
// granted capability+scope bundle covers the requested (cap, scope).
//
// Why a separate path rather than re-using Enforcer.Can:
//
//   - SA grants live on the SA row, not in policies.json. There's no
//     RoleAssignment to look up; the capabilities are an explicit list
//     stamped at mint time by the operator.
//   - SA grants must be the floor AND the ceiling for that token. An
//     SA cannot inherit additional capabilities from its owner's
//     role assignments (it's an M2M credential — the principle of
//     least privilege requires the minter to declare exactly what the
//     token can do). Re-using Enforcer.Can would silently merge the
//     two surfaces.
//   - Capability granularity differs slightly: SA caps already pair
//     each ID with a scope (Capability.Scope), so the role-expansion
//     dance ("role lists bucket:* which expands to include bucket:view
//     at scope cluster:abc") is replaced by a direct pair match on
//     the SA's capability list.
//
// The scope-matching grammar is shared (ScopeMatches), so a SA
// granted bucket:view at cluster:abc:* matches a request for
// bucket:view at bucket:abc:lsi exactly like an Enforcer assignment
// at cluster:abc:* would.
package policy

import "github.com/mattjackson/basement/internal/serviceaccount"

// ServiceAccountAllows reports whether the SA's granted capability +
// scope bundle covers the requested (capability, scope) pair.
//
// Matching rules (both must hold for at least one Capability entry):
//
//   1. The Capability.ID equals the requested capability exactly OR
//      the Capability.ID is a wildcard expression that, when Expand'd,
//      includes the requested capability. The same "domain:*" and
//      "*:*" shorthand the Enforcer accepts on Role.Capabilities is
//      honoured here.
//   2. The Capability.Scope (per-capability scope from the SA row)
//      covers the requested scope per ScopeMatches.
//
// In addition to the per-Capability scope, the SA's top-level Scopes
// slice acts as an OUTER bound: a capability grant is only effective
// when at least one entry in sa.Scopes also covers the requested
// scope. This matches the operator UX where the FE presents "what
// capabilities" and "which clusters / buckets" as two independent
// pickers: the AND of the two is the effective grant.
//
// Returns false on a revoked or expired SA — the caller (auth gate)
// has usually already screened these out via the bearer middleware,
// but the belt-and-braces check keeps this function safe for any
// future direct call site.
func ServiceAccountAllows(sa serviceaccount.ServiceAccount, capability, scope string) bool {
	if capability == "" || scope == "" {
		return false
	}
	if sa.IsRevoked() {
		return false
	}
	// Top-level scope envelope: the SA must have AT LEAST ONE entry in
	// .Scopes that covers the requested scope. An empty .Scopes slice
	// means "scoped to nothing" — secure default, refuse.
	if !scopesCover(sa.Scopes, scope) {
		return false
	}
	for _, c := range sa.Capabilities {
		if !capabilityMatches(c.ID, capability) {
			continue
		}
		if !ScopeMatches(c.Scope, scope) {
			continue
		}
		return true
	}
	return false
}

// scopesCover reports whether any entry in sa.Scopes covers the
// requested scope per ScopeMatches. Pulled into a helper so the
// caller reads as a one-liner.
func scopesCover(scopes []string, requested string) bool {
	for _, s := range scopes {
		if ScopeMatches(s, requested) {
			return true
		}
	}
	return false
}

// capabilityMatches reports whether the granted capability expression
// (which may be a bare leaf, a "domain:*" shorthand, or "*:*") covers
// the requested leaf capability. Wildcards expand against the
// compile-time Registry; a leaf granted but absent from the Registry
// still matches by direct string compare so a future capability
// renamed in the registry doesn't silently invalidate live SAs.
func capabilityMatches(granted, requested string) bool {
	if granted == "" || requested == "" {
		return false
	}
	if granted == requested {
		return true
	}
	if granted == "*:*" {
		return true
	}
	// Re-use Expand so the wildcard semantics stay identical to the
	// role enforcer. Expand returns the set of leaf capabilities the
	// expression covers; a hit means the grant covers the request.
	for _, leaf := range Expand(granted) {
		if leaf == requested {
			return true
		}
	}
	return false
}
