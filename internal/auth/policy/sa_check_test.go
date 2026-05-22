// Package policy: ServiceAccountAllows tests (v1.7.0b).
//
// Covers the matcher's contract:
//   - Direct capability match (granted leaf == requested leaf).
//   - Scope wildcard match (cluster:* covers cluster:716e).
//   - Bucket-grammar match (bucket:cid:* covers bucket:cid:bid).
//   - Domain wildcard capability ("bucket:*" covers bucket:view).
//   - Wrong capability → false.
//   - Wrong scope → false.
//   - Revoked / empty-scopes / empty-caps edge cases.
//
// The matcher is a pure function — no fixtures, no I/O.
package policy

import (
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/serviceaccount"
)

// helper: builds a healthy (non-revoked, non-expired) SA with the
// given capabilities + scopes.
func saWith(caps []serviceaccount.Capability, scopes []string) serviceaccount.ServiceAccount {
	return serviceaccount.ServiceAccount{
		ID:           "sa-test",
		OwnerUserID:  "matthew",
		Name:         "test",
		AccessKeyID:  "BMNTAAAAAAAAAAAAAAAA",
		Capabilities: caps,
		Scopes:       scopes,
		CreatedAt:    time.Now().UTC(),
	}
}

func TestServiceAccountAllows_DirectCapMatch(t *testing.T) {
	sa := saWith(
		[]serviceaccount.Capability{
			{ID: "bucket:view", Scope: "bucket:c1:b1"},
		},
		[]string{"bucket:c1:b1"},
	)
	if !ServiceAccountAllows(sa, "bucket:view", "bucket:c1:b1") {
		t.Error("expected allow on exact cap+scope match")
	}
}

func TestServiceAccountAllows_ScopeWildcardMatch(t *testing.T) {
	// SA granted cluster-wide capability via cluster:* wildcard.
	// Request comes in at a concrete cluster:716e — the wildcard
	// must cover it.
	sa := saWith(
		[]serviceaccount.Capability{
			{ID: "cluster:edit", Scope: "cluster:*"},
		},
		[]string{"cluster:*"},
	)
	if !ServiceAccountAllows(sa, "cluster:edit", "cluster:716e") {
		t.Error("expected cluster:* to cover cluster:716e")
	}
}

func TestServiceAccountAllows_BucketGrammarMatch(t *testing.T) {
	// bucket:cid:* covers bucket:cid:bid for a specific cluster.
	sa := saWith(
		[]serviceaccount.Capability{
			{ID: "bucket:view", Scope: "bucket:c1:*"},
		},
		[]string{"bucket:c1:*"},
	)
	if !ServiceAccountAllows(sa, "bucket:view", "bucket:c1:lsi") {
		t.Error("expected bucket:c1:* to cover bucket:c1:lsi")
	}
	if ServiceAccountAllows(sa, "bucket:view", "bucket:c2:lsi") {
		t.Error("bucket:c1:* must NOT cover bucket:c2:lsi (different cluster)")
	}
}

func TestServiceAccountAllows_DomainWildcardCapability(t *testing.T) {
	// "bucket:*" on the capability ID covers bucket:view, bucket:create,
	// etc. via Expand. The scope grammar is independent.
	sa := saWith(
		[]serviceaccount.Capability{
			{ID: "bucket:*", Scope: "bucket:c1:*"},
		},
		[]string{"bucket:c1:*"},
	)
	if !ServiceAccountAllows(sa, "bucket:view", "bucket:c1:lsi") {
		t.Error("expected bucket:* cap-wildcard to cover bucket:view")
	}
	if !ServiceAccountAllows(sa, "bucket:set_quota", "bucket:c1:lsi") {
		t.Error("expected bucket:* cap-wildcard to cover bucket:set_quota")
	}
}

func TestServiceAccountAllows_SuperuserCapability(t *testing.T) {
	// "*:*" covers anything in the registry. Used for host-admin-class
	// service accounts.
	sa := saWith(
		[]serviceaccount.Capability{
			{ID: "*:*", Scope: "*"},
		},
		[]string{"*"},
	)
	if !ServiceAccountAllows(sa, "cluster:delete", "cluster:abc") {
		t.Error("expected *:* to cover cluster:delete")
	}
	if !ServiceAccountAllows(sa, "host:manage_users", "host:*") {
		t.Error("expected *:* to cover host:manage_users")
	}
}

func TestServiceAccountAllows_WrongCapability(t *testing.T) {
	sa := saWith(
		[]serviceaccount.Capability{
			{ID: "bucket:view", Scope: "bucket:c1:b1"},
		},
		[]string{"bucket:c1:b1"},
	)
	if ServiceAccountAllows(sa, "bucket:delete", "bucket:c1:b1") {
		t.Error("expected deny on capability not in SA bundle")
	}
}

func TestServiceAccountAllows_WrongScope(t *testing.T) {
	sa := saWith(
		[]serviceaccount.Capability{
			{ID: "bucket:view", Scope: "bucket:c1:b1"},
		},
		[]string{"bucket:c1:b1"},
	)
	if ServiceAccountAllows(sa, "bucket:view", "bucket:c1:b2") {
		t.Error("expected deny when scope outside the granted range")
	}
}

func TestServiceAccountAllows_OuterScopeBound(t *testing.T) {
	// The Capability.Scope says "cluster:*", but the SA's outer
	// Scopes envelope restricts to a single cluster. The outer bound
	// MUST hold — the matcher is an AND, not an OR.
	sa := saWith(
		[]serviceaccount.Capability{
			{ID: "cluster:edit", Scope: "cluster:*"},
		},
		[]string{"cluster:c1"},
	)
	if !ServiceAccountAllows(sa, "cluster:edit", "cluster:c1") {
		t.Error("expected allow at the envelope-bounded scope")
	}
	if ServiceAccountAllows(sa, "cluster:edit", "cluster:c2") {
		t.Error("expected deny outside the outer Scopes envelope")
	}
}

func TestServiceAccountAllows_EmptyInputs(t *testing.T) {
	sa := saWith(
		[]serviceaccount.Capability{{ID: "bucket:view", Scope: "bucket:c1:b1"}},
		[]string{"bucket:c1:b1"},
	)
	if ServiceAccountAllows(sa, "", "bucket:c1:b1") {
		t.Error("empty capability must deny")
	}
	if ServiceAccountAllows(sa, "bucket:view", "") {
		t.Error("empty scope must deny")
	}
}

func TestServiceAccountAllows_RevokedDenies(t *testing.T) {
	// A revoked SA must deny even when the cap+scope would match. The
	// bearer middleware screens revoked rows out first, but the matcher
	// belt-and-braces refuses on its own.
	revoked := time.Now().Add(-1 * time.Hour)
	sa := saWith(
		[]serviceaccount.Capability{{ID: "bucket:view", Scope: "bucket:c1:b1"}},
		[]string{"bucket:c1:b1"},
	)
	sa.RevokedAt = &revoked
	if ServiceAccountAllows(sa, "bucket:view", "bucket:c1:b1") {
		t.Error("revoked SA must deny regardless of cap+scope")
	}
}

func TestServiceAccountAllows_EmptyScopesEnvelope(t *testing.T) {
	// SA with capabilities but NO outer Scopes is scoped to nothing —
	// secure default refuses everything.
	sa := saWith(
		[]serviceaccount.Capability{{ID: "bucket:view", Scope: "bucket:c1:b1"}},
		nil,
	)
	if ServiceAccountAllows(sa, "bucket:view", "bucket:c1:b1") {
		t.Error("SA with empty Scopes envelope must deny everything")
	}
}

func TestServiceAccountAllows_EmptyCapabilities(t *testing.T) {
	sa := saWith(nil, []string{"*"})
	if ServiceAccountAllows(sa, "bucket:view", "bucket:c1:b1") {
		t.Error("SA with no granted capabilities must deny everything")
	}
}
