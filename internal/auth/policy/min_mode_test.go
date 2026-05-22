package policy

import "testing"

// TestMode_Includes_TruthTable codifies the ADR-0003 mode lattice:
// ELEVATED >= ADMIN >= USER. Each Includes(other) call answers "does my
// current mode grant me the privileges other demands?".
func TestMode_Includes_TruthTable(t *testing.T) {
	cases := []struct {
		current Mode
		other   Mode
		want    bool
	}{
		// USER current
		{ModeUser, ModeUser, true},
		{ModeUser, ModeAdmin, false},
		{ModeUser, ModeElevated, false},
		// ADMIN current
		{ModeAdmin, ModeUser, true},
		{ModeAdmin, ModeAdmin, true},
		{ModeAdmin, ModeElevated, false},
		// ELEVATED current
		{ModeElevated, ModeUser, true},
		{ModeElevated, ModeAdmin, true},
		{ModeElevated, ModeElevated, true},
	}
	for _, c := range cases {
		got := c.current.Includes(c.other)
		if got != c.want {
			t.Errorf("Mode(%q).Includes(%q) = %v, want %v",
				c.current, c.other, got, c.want)
		}
	}
}

// TestMode_Includes_UnknownMode: a zero-value Mode includes nothing —
// fail-closed so a missing/empty claim never satisfies a real check.
func TestMode_Includes_UnknownMode(t *testing.T) {
	var zero Mode
	for _, m := range []Mode{ModeUser, ModeAdmin, ModeElevated} {
		if zero.Includes(m) {
			t.Errorf("zero Mode.Includes(%q) = true, want false (fail-closed)", m)
		}
	}
}

// TestMinModeFor_Elevated covers every capability ADR-0003 elevates.
// Adding a new destructive capability must also add it here so the
// table stays the contract.
func TestMinModeFor_Elevated(t *testing.T) {
	elevated := []string{
		"cluster:delete",
		"bucket:delete",
		"key:delete",
		"host:manage_users",
		"host:manage_policies",
		"policy:edit_matrix",
		"policy:assign_role",
		"cluster:edit_layout",
	}
	for _, cap := range elevated {
		if got := MinModeFor(cap); got != ModeElevated {
			t.Errorf("MinModeFor(%q) = %q, want %q", cap, got, ModeElevated)
		}
	}
}

// TestMinModeFor_User covers data-plane + harmless reads that USER mode
// satisfies without elevation. These are the capabilities a logged-in
// user can exercise without ever proving "I meant to do this."
func TestMinModeFor_User(t *testing.T) {
	user := []string{
		"objects:list",
		"objects:get",
		"objects:put",
		"objects:delete",
		"objects:share_create",
		"objects:share_revoke",
		"bucket:view",
	}
	for _, cap := range user {
		if got := MinModeFor(cap); got != ModeUser {
			t.Errorf("MinModeFor(%q) = %q, want %q", cap, got, ModeUser)
		}
	}
}

// TestMinModeFor_DefaultAdmin: anything not on the USER or ELEVATED
// list defaults to ADMIN. Includes a sample of registry caps and one
// unknown ID — the unknown-default is the load-bearing fail-safe: a
// new gate added in a future cycle that no one classifies stays
// callable only from ADMIN, not USER.
func TestMinModeFor_DefaultAdmin(t *testing.T) {
	admin := []string{
		// Registry caps that map to ADMIN.
		"cluster:create",
		"cluster:edit",
		"cluster:test",
		"cluster:view_layout",
		"bucket:create",
		"bucket:edit_alias",
		"bucket:set_quota",
		"key:create",
		"key:edit_permissions",
		"key:view",
		"host:manage_signup_mode",
		"host:manage_drivers",
		"host:manage_org_caps",
		"policy:view_matrix",
		// Unknown cap — defaults to ADMIN, NOT USER.
		"unknown:future_capability",
	}
	for _, cap := range admin {
		if got := MinModeFor(cap); got != ModeAdmin {
			t.Errorf("MinModeFor(%q) = %q, want %q", cap, got, ModeAdmin)
		}
	}
}
