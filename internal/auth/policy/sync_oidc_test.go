// Package policy: tests for SyncOIDCAssignments (v1.3.0a).
//
// SyncOIDCAssignments is the per-OIDC-login reconcile that grants
// Source="oidc" assignments matching the user's current claims and
// revokes stale ones, while leaving Source="manual" assignments
// untouched. These tests cover the four cases the OIDC callback
// relies on:
//
//  1. Empty -> add: a fresh user gets their first auto-assignment.
//  2. No change: re-syncing identical wants makes no mutation.
//  3. Revoke: a previously auto-assigned role disappears from wants.
//  4. Manual sacred: a manual assignment overlapping with a wanted
//     triple survives untouched (no duplicate, no Source downgrade).
package policy

import (
	"testing"
)

func TestSyncOIDCAssignments_AddsNewAssignment(t *testing.T) {
	e, _ := newTestEnforcer(t)

	added, revoked, err := e.SyncOIDCAssignments("alice", []RoleAssignment{
		{RoleID: "host_admin", Scope: "host:*"},
	})
	if err != nil {
		t.Fatalf("SyncOIDCAssignments: %v", err)
	}
	if len(added) != 1 {
		t.Fatalf("added=%d, want 1", len(added))
	}
	if len(revoked) != 0 {
		t.Errorf("revoked=%d, want 0", len(revoked))
	}
	if added[0].Source != "oidc" || !added[0].AutoAssigned {
		t.Errorf("added=%+v, want Source=oidc + AutoAssigned=true", added[0])
	}

	got := e.AssignmentsFor("alice")
	if len(got) != 1 {
		t.Fatalf("AssignmentsFor=%d, want 1", len(got))
	}
	if got[0].RoleID != "host_admin" || got[0].Scope != "host:*" || got[0].Source != "oidc" {
		t.Errorf("got=%+v, want {host_admin@host:*, source=oidc}", got[0])
	}

	// Capability check confirms the auto-assignment actually grants
	// the role's capabilities — Sync wires it into Can/Capabilities
	// the same as any other assignment.
	if !e.Can("alice", "host:manage_users", "host:*") {
		t.Error("Can=false, want true — OIDC auto-assignment should grant host_admin caps")
	}
}

func TestSyncOIDCAssignments_NoChangeIdempotent(t *testing.T) {
	e, _ := newTestEnforcer(t)

	want := []RoleAssignment{{RoleID: "host_admin", Scope: "host:*"}}
	if _, _, err := e.SyncOIDCAssignments("alice", want); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	added, revoked, err := e.SyncOIDCAssignments("alice", want)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(added) != 0 || len(revoked) != 0 {
		t.Errorf("added=%d revoked=%d, want both 0 (idempotent re-sync)", len(added), len(revoked))
	}
}

func TestSyncOIDCAssignments_RevokesStale(t *testing.T) {
	e, _ := newTestEnforcer(t)

	// Two auto-assignments.
	if _, _, err := e.SyncOIDCAssignments("alice", []RoleAssignment{
		{RoleID: "host_admin", Scope: "host:*"},
		{RoleID: "cluster_admin", Scope: "cluster:*"},
	}); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	// Re-sync drops cluster_admin (claims no longer present).
	added, revoked, err := e.SyncOIDCAssignments("alice", []RoleAssignment{
		{RoleID: "host_admin", Scope: "host:*"},
	})
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("added=%d, want 0", len(added))
	}
	if len(revoked) != 1 {
		t.Fatalf("revoked=%d, want 1", len(revoked))
	}
	if revoked[0].RoleID != "cluster_admin" {
		t.Errorf("revoked=%+v, want cluster_admin", revoked[0])
	}

	got := e.AssignmentsFor("alice")
	if len(got) != 1 || got[0].RoleID != "host_admin" {
		t.Errorf("AssignmentsFor=%+v, want only host_admin", got)
	}
}

func TestSyncOIDCAssignments_RevokesAllWhenWantedEmpty(t *testing.T) {
	e, _ := newTestEnforcer(t)

	if _, _, err := e.SyncOIDCAssignments("alice", []RoleAssignment{
		{RoleID: "host_admin", Scope: "host:*"},
	}); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	added, revoked, err := e.SyncOIDCAssignments("alice", nil)
	if err != nil {
		t.Fatalf("empty sync: %v", err)
	}
	if len(added) != 0 || len(revoked) != 1 {
		t.Errorf("added=%d revoked=%d, want 0 added 1 revoked", len(added), len(revoked))
	}
	if got := e.AssignmentsFor("alice"); len(got) != 0 {
		t.Errorf("AssignmentsFor=%+v, want empty after revoke-all", got)
	}
}

func TestSyncOIDCAssignments_ManualSacred(t *testing.T) {
	e, _ := newTestEnforcer(t)

	// Operator manually assigns host_admin.
	if err := e.AssignRole(RoleAssignment{
		UserID: "alice", RoleID: "host_admin", Scope: "host:*",
	}); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	// OIDC sync also "wants" host_admin@host:*. The existing manual
	// assignment satisfies it — no new OIDC row, manual untouched.
	added, revoked, err := e.SyncOIDCAssignments("alice", []RoleAssignment{
		{RoleID: "host_admin", Scope: "host:*"},
		{RoleID: "cluster_admin", Scope: "cluster:*"},
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	// Only cluster_admin should have been added.
	if len(added) != 1 || added[0].RoleID != "cluster_admin" {
		t.Errorf("added=%+v, want only cluster_admin", added)
	}
	if len(revoked) != 0 {
		t.Errorf("revoked=%d, want 0", len(revoked))
	}

	got := e.AssignmentsFor("alice")
	if len(got) != 2 {
		t.Fatalf("AssignmentsFor=%+v, want 2", got)
	}

	// host_admin row must still be the manual one (Source != "oidc").
	for _, a := range got {
		if a.RoleID == "host_admin" && a.Source == "oidc" {
			t.Errorf("manual host_admin was downgraded to OIDC source: %+v", a)
		}
	}

	// Subsequent revoke-all sync only kills the OIDC row — manual
	// host_admin survives.
	_, revoked2, err := e.SyncOIDCAssignments("alice", nil)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(revoked2) != 1 || revoked2[0].RoleID != "cluster_admin" {
		t.Errorf("revoked2=%+v, want only cluster_admin", revoked2)
	}
	got = e.AssignmentsFor("alice")
	if len(got) != 1 || got[0].RoleID != "host_admin" || got[0].Source == "oidc" {
		t.Errorf("AssignmentsFor after revoke-all=%+v, want manual host_admin only", got)
	}
}

func TestSyncOIDCAssignments_UnknownRoleSkipped(t *testing.T) {
	e, _ := newTestEnforcer(t)

	added, _, err := e.SyncOIDCAssignments("alice", []RoleAssignment{
		{RoleID: "does-not-exist", Scope: "host:*"},
		{RoleID: "host_admin", Scope: "host:*"},
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(added) != 1 || added[0].RoleID != "host_admin" {
		t.Errorf("added=%+v, want only host_admin (unknown role silently dropped)", added)
	}
}

func TestSyncOIDCAssignments_OnlyAffectsTargetUser(t *testing.T) {
	e, _ := newTestEnforcer(t)

	if _, _, err := e.SyncOIDCAssignments("alice", []RoleAssignment{
		{RoleID: "host_admin", Scope: "host:*"},
	}); err != nil {
		t.Fatalf("alice sync: %v", err)
	}
	if _, _, err := e.SyncOIDCAssignments("bob", []RoleAssignment{
		{RoleID: "cluster_admin", Scope: "cluster:*"},
	}); err != nil {
		t.Fatalf("bob sync: %v", err)
	}

	// Re-syncing alice with a different wanted set must not touch bob.
	if _, _, err := e.SyncOIDCAssignments("alice", nil); err != nil {
		t.Fatalf("alice revoke: %v", err)
	}

	bobs := e.AssignmentsFor("bob")
	if len(bobs) != 1 || bobs[0].RoleID != "cluster_admin" {
		t.Errorf("bob's assignments mutated by alice sync: %+v", bobs)
	}
}

func TestSyncOIDCAssignments_PersistsAcrossReopen(t *testing.T) {
	e, dir := newTestEnforcer(t)

	if _, _, err := e.SyncOIDCAssignments("alice", []RoleAssignment{
		{RoleID: "host_admin", Scope: "host:*"},
	}); err != nil {
		t.Fatalf("sync: %v", err)
	}

	e2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got := e2.AssignmentsFor("alice")
	if len(got) != 1 || got[0].Source != "oidc" {
		t.Errorf("AssignmentsFor after reopen=%+v, want OIDC source", got)
	}
}
