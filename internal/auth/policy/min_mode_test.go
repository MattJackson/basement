package policy

import "testing"

// TestMode_Includes_TruthTable codifies the v1.3.0a.4 two-mode lattice:
// ADMIN >= USER. Each Includes(other) call answers "does my current
// mode grant me the privileges other demands?".
func TestMode_Includes_TruthTable(t *testing.T) {
	cases := []struct {
		current Mode
		other   Mode
		want    bool
	}{
		// USER current
		{ModeUser, ModeUser, true},
		{ModeUser, ModeAdmin, false},
		// ADMIN current
		{ModeAdmin, ModeUser, true},
		{ModeAdmin, ModeAdmin, true},
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
	for _, m := range []Mode{ModeUser, ModeAdmin} {
		if zero.Includes(m) {
			t.Errorf("zero Mode.Includes(%q) = true, want false (fail-closed)", m)
		}
	}
}

// TestModeElevated_AliasesAdmin: v1.3.0a.4 collapsed ELEVATED into
// ADMIN; the ModeElevated constant is kept as an alias for one release
// cycle so v1.2-era call sites compile. Confirm the alias is identical
// to ModeAdmin (same string value, same Includes behaviour).
func TestModeElevated_AliasesAdmin(t *testing.T) {
	if ModeElevated != ModeAdmin {
		t.Errorf("ModeElevated = %q, want it to alias ModeAdmin (%q)",
			ModeElevated, ModeAdmin)
	}
	// Includes behaviour identical too.
	if got := ModeElevated.Includes(ModeUser); !got {
		t.Errorf("ModeElevated.Includes(ModeUser) = false, want true")
	}
	if got := ModeElevated.Includes(ModeAdmin); !got {
		t.Errorf("ModeElevated.Includes(ModeAdmin) = false, want true")
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

// TestMinModeFor_Admin: every admin capability — destructive or not —
// requires ADMIN under the v1.3.0a.4 amendment. The previously-ELEVATED
// caps (cluster:delete et al.) collapse into the same tier as the
// previously-ADMIN caps (cluster:edit et al.). Includes one unknown ID
// as the load-bearing fail-safe: a new gate added in a future cycle
// that no one classifies stays callable only from ADMIN, not USER.
func TestMinModeFor_Admin(t *testing.T) {
	admin := []string{
		// Previously-ELEVATED capabilities — same tier now.
		"cluster:delete",
		"bucket:delete",
		"key:delete",
		"host:manage_users",
		"host:manage_policies",
		"policy:edit_matrix",
		"policy:assign_role",
		"cluster:edit_layout",
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
