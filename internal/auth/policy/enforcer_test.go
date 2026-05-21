package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// helper: open a fresh enforcer in a temp dir.
func newTestEnforcer(t *testing.T) (Enforcer, string) {
	t.Helper()
	dir := t.TempDir()
	e, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return e, dir
}

func TestEnforcer_SeedOnFirstLoad(t *testing.T) {
	e, dir := newTestEnforcer(t)

	roles := e.Roles()
	if len(roles) != 3 {
		t.Fatalf("expected 3 seed roles, got %d: %+v", len(roles), roles)
	}

	wantIDs := map[string]bool{"host_admin": false, "cluster_admin": false, "bucket_user": false}
	for _, r := range roles {
		if _, ok := wantIDs[r.ID]; !ok {
			t.Errorf("unexpected seed role %q", r.ID)
			continue
		}
		wantIDs[r.ID] = true
		if !r.Seed {
			t.Errorf("seed role %q has Seed=false", r.ID)
		}
	}
	for id, found := range wantIDs {
		if !found {
			t.Errorf("missing seed role %q", id)
		}
	}

	// Matthew assignment present.
	assigns := e.AssignmentsFor("matthew")
	if len(assigns) != 1 {
		t.Fatalf("expected 1 assignment for matthew, got %d", len(assigns))
	}
	if assigns[0].RoleID != "host_admin" || assigns[0].Scope != "host:*" {
		t.Errorf("matthew assignment = %+v, want host_admin@host:*", assigns[0])
	}

	// File actually written.
	if _, err := os.Stat(filepath.Join(dir, "policies.json")); err != nil {
		t.Errorf("policies.json not written: %v", err)
	}
}

func TestEnforcer_Can_ExactMatch(t *testing.T) {
	e, _ := newTestEnforcer(t)

	if !e.Can("matthew", "host:manage_users", "host:*") {
		t.Errorf("matthew should be able to host:manage_users at host:*")
	}
	if !e.Can("matthew", "host:manage_policies", "host:*") {
		t.Errorf("matthew should be able to host:manage_policies at host:*")
	}
}

func TestEnforcer_Can_Wildcards(t *testing.T) {
	e, _ := newTestEnforcer(t)

	// Assign Alice cluster_admin on cluster:abc.
	if err := e.AssignRole(RoleAssignment{UserID: "alice", RoleID: "cluster_admin", Scope: "cluster:abc"}); err != nil {
		t.Fatalf("AssignRole cluster: %v", err)
	}
	if err := e.AssignRole(RoleAssignment{UserID: "alice", RoleID: "cluster_admin", Scope: "bucket:abc:*"}); err != nil {
		t.Fatalf("AssignRole bucket: %v", err)
	}

	// cluster_admin has bucket:* — so bucket:create on bucket:abc:* is granted.
	if !e.Can("alice", "bucket:create", "bucket:abc:lsi") {
		t.Errorf("alice should be able to bucket:create on bucket:abc:lsi")
	}
	// But NOT on a different cluster.
	if e.Can("alice", "bucket:create", "bucket:xyz:lsi") {
		t.Errorf("alice should NOT be able to bucket:create on bucket:xyz:lsi (different cluster)")
	}
	// cluster:edit at the cluster scope works because she has cluster_admin@cluster:abc.
	if !e.Can("alice", "cluster:edit", "cluster:abc") {
		t.Errorf("alice should be able to cluster:edit on cluster:abc")
	}
	// But not at a different cluster.
	if e.Can("alice", "cluster:edit", "cluster:xyz") {
		t.Errorf("alice should NOT be able to cluster:edit on cluster:xyz")
	}
}

func TestEnforcer_Can_Superuser(t *testing.T) {
	e, _ := newTestEnforcer(t)

	// Grant matthew an additional assignment at scope "*" — full superuser.
	// (His seed host_admin@host:* only covers host:*; an explicit *-scope
	// assignment of host_admin demonstrates the *:* capability + wildcard
	// scope combo means "can do everything anywhere".)
	if err := e.AssignRole(RoleAssignment{UserID: "matthew", RoleID: "host_admin", Scope: "*"}); err != nil {
		t.Fatalf("AssignRole superuser: %v", err)
	}

	cases := []struct {
		cap, scope string
	}{
		{"host:manage_users", "host:*"},
		{"cluster:create", "cluster:abc"},
		{"bucket:delete", "bucket:abc:lsi"},
		{"objects:put", "bucket:abc:lsi"},
		{"policy:edit_matrix", "host:*"},
		{"key:delete", "key:abc:somekey"},
	}
	for _, c := range cases {
		if !e.Can("matthew", c.cap, c.scope) {
			t.Errorf("superuser matthew should be able to %s @ %s", c.cap, c.scope)
		}
	}
}

func TestEnforcer_Can_NoAssignment_DefaultsFalse(t *testing.T) {
	e, _ := newTestEnforcer(t)

	if e.Can("randomguy", "bucket:view", "bucket:abc:lsi") {
		t.Errorf("unassigned user should not be able to bucket:view")
	}
	if e.Can("randomguy", "host:manage_users", "host:*") {
		t.Errorf("unassigned user should not be able to host:manage_users")
	}
	// Empty inputs.
	if e.Can("", "bucket:view", "bucket:abc:lsi") {
		t.Errorf("empty userID should not be able to bucket:view")
	}
	if e.Can("matthew", "", "host:*") {
		t.Errorf("empty capability should not pass")
	}
	if e.Can("matthew", "host:manage_users", "") {
		t.Errorf("empty scope should not pass")
	}
}

func TestEnforcer_DeleteRole_RefusesSeed(t *testing.T) {
	e, _ := newTestEnforcer(t)

	if err := e.DeleteRole("host_admin"); err == nil {
		t.Errorf("DeleteRole(host_admin) should return error (seed role)")
	}
	if err := e.DeleteRole("cluster_admin"); err == nil {
		t.Errorf("DeleteRole(cluster_admin) should return error (seed role)")
	}
	if err := e.DeleteRole("bucket_user"); err == nil {
		t.Errorf("DeleteRole(bucket_user) should return error (seed role)")
	}

	// Seed roles still present.
	if len(e.Roles()) != 3 {
		t.Errorf("seed roles missing after DeleteRole attempts: %d", len(e.Roles()))
	}

	// Custom role: should delete fine, and assignments referencing it
	// should be cleaned up.
	if err := e.UpsertRole(Role{
		ID:           "custom_reader",
		Label:        "Custom Reader",
		Capabilities: []string{"objects:list", "objects:get"},
	}); err != nil {
		t.Fatalf("UpsertRole custom: %v", err)
	}
	if err := e.AssignRole(RoleAssignment{UserID: "bob", RoleID: "custom_reader", Scope: "bucket:abc:lsi"}); err != nil {
		t.Fatalf("AssignRole custom: %v", err)
	}
	if err := e.DeleteRole("custom_reader"); err != nil {
		t.Errorf("DeleteRole(custom_reader) should succeed, got %v", err)
	}
	if got := e.AssignmentsFor("bob"); len(got) != 0 {
		t.Errorf("bob's assignment should have been cleaned up after role delete: %+v", got)
	}

	// Unknown role -> error.
	if err := e.DeleteRole("does_not_exist"); err == nil {
		t.Errorf("DeleteRole(unknown) should return error")
	}
}

func TestEnforcer_UpsertRole_ValidatesCapabilities(t *testing.T) {
	e, _ := newTestEnforcer(t)

	// Bogus capability ID.
	err := e.UpsertRole(Role{
		ID:           "junk",
		Capabilities: []string{"bogus:verb"},
	})
	if err == nil {
		t.Errorf("UpsertRole with unknown capability should fail")
	}

	// Bogus domain wildcard.
	err = e.UpsertRole(Role{
		ID:           "junk2",
		Capabilities: []string{"madeup:*"},
	})
	if err == nil {
		t.Errorf("UpsertRole with unknown domain wildcard should fail")
	}

	// Empty ID.
	if err := e.UpsertRole(Role{ID: "", Capabilities: []string{"objects:list"}}); err == nil {
		t.Errorf("UpsertRole with empty ID should fail")
	}

	// Valid role.
	if err := e.UpsertRole(Role{
		ID:           "viewer",
		Label:        "Viewer",
		Capabilities: []string{"objects:list", "objects:get", "bucket:view"},
	}); err != nil {
		t.Errorf("UpsertRole valid should succeed, got %v", err)
	}

	// Wildcard valid.
	if err := e.UpsertRole(Role{
		ID:           "obj_pwn",
		Capabilities: []string{"objects:*"},
	}); err != nil {
		t.Errorf("UpsertRole with valid domain wildcard should succeed, got %v", err)
	}

	// *:* superuser shorthand allowed.
	if err := e.UpsertRole(Role{
		ID:           "super",
		Capabilities: []string{"*:*"},
	}); err != nil {
		t.Errorf("UpsertRole with *:* should succeed, got %v", err)
	}

	// Caller cannot mint a new seed role via UpsertRole.
	if err := e.UpsertRole(Role{
		ID:           "fake_seed",
		Capabilities: []string{"objects:list"},
		Seed:         true,
	}); err != nil {
		t.Fatalf("UpsertRole new role failed: %v", err)
	}
	for _, r := range e.Roles() {
		if r.ID == "fake_seed" && r.Seed {
			t.Errorf("UpsertRole should strip Seed=true on new roles")
		}
	}

	// Editing a seed role keeps Seed=true regardless of caller's intent.
	if err := e.UpsertRole(Role{
		ID:           "bucket_user",
		Label:        "Bucket User Renamed",
		Capabilities: []string{"objects:list"},
		Seed:         false, // try to demote
	}); err != nil {
		t.Fatalf("UpsertRole edit seed: %v", err)
	}
	for _, r := range e.Roles() {
		if r.ID == "bucket_user" {
			if !r.Seed {
				t.Errorf("seed role bucket_user lost Seed=true after edit")
			}
			if r.Label != "Bucket User Renamed" {
				t.Errorf("seed role edit didn't persist label change")
			}
		}
	}
}

func TestEnforcer_Persists(t *testing.T) {
	e, dir := newTestEnforcer(t)

	if err := e.AssignRole(RoleAssignment{UserID: "wife", RoleID: "bucket_user", Scope: "bucket:abc:family-photos"}); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	if err := e.UpsertRole(Role{
		ID:           "viewer",
		Label:        "Viewer",
		Capabilities: []string{"objects:list", "objects:get"},
	}); err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}

	// Re-open from the same dir.
	e2, err := Open(dir)
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}

	// Wife's assignment survived.
	got := e2.AssignmentsFor("wife")
	if len(got) != 1 || got[0].RoleID != "bucket_user" || got[0].Scope != "bucket:abc:family-photos" {
		t.Errorf("wife assignment did not persist: %+v", got)
	}

	// Custom role survived (and was NOT re-seeded because roles already
	// present means seed-on-empty is skipped).
	found := false
	for _, r := range e2.Roles() {
		if r.ID == "viewer" {
			found = true
			if r.Seed {
				t.Errorf("viewer should not be a seed role")
			}
		}
	}
	if !found {
		t.Errorf("custom viewer role did not persist")
	}

	// Total role count is 4 (3 seed + viewer).
	if n := len(e2.Roles()); n != 4 {
		t.Errorf("expected 4 roles after persist+reopen, got %d", n)
	}

	// And the seed assignment is still present too.
	mAssigns := e2.AssignmentsFor("matthew")
	if len(mAssigns) != 1 {
		t.Errorf("matthew's seed assignment lost on reopen: %+v", mAssigns)
	}
}

func TestEnforcer_UnassignRole(t *testing.T) {
	e, _ := newTestEnforcer(t)

	a := RoleAssignment{UserID: "bob", RoleID: "bucket_user", Scope: "bucket:abc:photos"}
	if err := e.AssignRole(a); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	// Idempotent re-add.
	if err := e.AssignRole(a); err != nil {
		t.Fatalf("AssignRole idempotent: %v", err)
	}
	got := e.AssignmentsFor("bob")
	if len(got) != 1 {
		t.Fatalf("expected 1 assignment after idempotent re-add, got %d", len(got))
	}

	// Unassign.
	if err := e.UnassignRole("bob", "bucket_user", "bucket:abc:photos"); err != nil {
		t.Fatalf("UnassignRole: %v", err)
	}
	got = e.AssignmentsFor("bob")
	if len(got) != 0 {
		t.Errorf("bob's assignment should be gone, got %+v", got)
	}

	// Unassigning an absent triple is a no-op.
	if err := e.UnassignRole("bob", "bucket_user", "bucket:abc:photos"); err != nil {
		t.Errorf("UnassignRole absent should be no-op, got %v", err)
	}
}

func TestEnforcer_AssignRole_UnknownRole(t *testing.T) {
	e, _ := newTestEnforcer(t)
	if err := e.AssignRole(RoleAssignment{UserID: "bob", RoleID: "ghost", Scope: "bucket:abc:lsi"}); err == nil {
		t.Errorf("AssignRole with unknown role should fail")
	}
}

func TestEnforcer_Capabilities(t *testing.T) {
	e, _ := newTestEnforcer(t)

	if err := e.AssignRole(RoleAssignment{UserID: "alice", RoleID: "bucket_user", Scope: "bucket:abc:lsi"}); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	caps := e.Capabilities("alice", "bucket:abc:lsi")
	want := []string{
		"bucket:view",
		"objects:get",
		"objects:list",
		"objects:put",
		"objects:share_create",
		"objects:share_revoke",
	}
	if !reflect.DeepEqual(caps, want) {
		t.Errorf("Capabilities(alice, bucket:abc:lsi) = %v, want %v", caps, want)
	}

	// At a different bucket -> empty.
	if got := e.Capabilities("alice", "bucket:xyz:lsi"); len(got) != 0 {
		t.Errorf("Capabilities at unrelated bucket should be empty, got %v", got)
	}

	// Empty user / scope -> empty.
	if got := e.Capabilities("", "bucket:abc:lsi"); len(got) != 0 {
		t.Errorf("Capabilities empty user should be empty, got %v", got)
	}
}

func TestCapabilities_Expand(t *testing.T) {
	got := Expand("bucket:*")
	sort.Strings(got)
	want := []string{
		"bucket:create",
		"bucket:delete",
		"bucket:edit_alias",
		"bucket:set_quota",
		"bucket:view",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Expand(bucket:*) = %v, want %v", got, want)
	}

	// Leaf passes through.
	if got := Expand("objects:list"); !reflect.DeepEqual(got, []string{"objects:list"}) {
		t.Errorf("Expand(objects:list) = %v, want [objects:list]", got)
	}

	// Unknown leaf -> empty (not an error here).
	if got := Expand("bogus:verb"); len(got) != 0 {
		t.Errorf("Expand(bogus:verb) = %v, want empty", got)
	}

	// Empty -> empty (non-nil).
	if got := Expand(""); got == nil || len(got) != 0 {
		t.Errorf("Expand(empty) = %v, want empty non-nil", got)
	}

	// *:* -> everything in registry.
	if got := Expand("*:*"); len(got) != len(Registry) {
		t.Errorf("Expand(*:*) returned %d, want %d (registry size)", len(got), len(Registry))
	}
}

func TestCapabilities_Validate(t *testing.T) {
	if err := Validate("host:manage_users"); err != nil {
		t.Errorf("Validate(known) should pass, got %v", err)
	}
	if err := Validate("bogus:verb"); err == nil {
		t.Errorf("Validate(unknown) should fail")
	}
	if err := Validate(""); err == nil {
		t.Errorf("Validate(empty) should fail")
	}
	// Validate is for concrete leaves only — wildcards are not valid here.
	if err := Validate("bucket:*"); err == nil {
		t.Errorf("Validate(wildcard) should fail (wildcards belong in role lists, not as concrete caps)")
	}
}

func TestScopeMatches(t *testing.T) {
	cases := []struct {
		assignment, requested string
		want                  bool
	}{
		{"host:*", "host:*", true},
		{"host:*", "host:something", true},
		{"*", "bucket:abc:lsi", true},
		{"*", "host:*", true},
		{"bucket:*", "bucket:abc:lsi", true},
		{"bucket:abc:*", "bucket:abc:lsi", true},
		{"bucket:abc:lsi", "bucket:abc:lsi", true},
		{"bucket:xyz:*", "bucket:abc:lsi", false},
		{"cluster:abc", "bucket:abc:lsi", false},
		{"cluster:abc", "cluster:xyz", false},
		{"", "bucket:abc:lsi", false},
		{"bucket:abc:lsi", "", false},
		{"bucket:abc:lsi", "bucket:abc:other", false},
	}
	for _, c := range cases {
		got := ScopeMatches(c.assignment, c.requested)
		if got != c.want {
			t.Errorf("ScopeMatches(%q, %q) = %v, want %v", c.assignment, c.requested, got, c.want)
		}
	}
}

// TestEnforcer_FileShape sanity-checks the on-disk JSON is what we
// claim — single object with "roles" + "assignments" arrays. Future
// migration code reads this file directly so its shape is a contract.
func TestEnforcer_FileShape(t *testing.T) {
	_, dir := newTestEnforcer(t)
	data, err := os.ReadFile(filepath.Join(dir, "policies.json"))
	if err != nil {
		t.Fatalf("reading policies.json: %v", err)
	}
	var pf policyFile
	if err := json.Unmarshal(data, &pf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(pf.Roles) != 3 {
		t.Errorf("expected 3 roles on disk, got %d", len(pf.Roles))
	}
	if len(pf.Assignments) != 1 {
		t.Errorf("expected 1 assignment on disk, got %d", len(pf.Assignments))
	}
}
