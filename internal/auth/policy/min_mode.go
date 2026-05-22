// Package policy: sudo-style mode model (ADR-0003).
//
// Each session is in one of three modes — USER, ADMIN, ELEVATED — and
// each capability declares the minimum mode required to exercise it.
// The gate (internal/api/policy_gates.go) checks both that the user
// HOLDS the capability via their role assignments AND that their current
// session mode meets the capability's minimum.
//
// Mode is orthogonal to roles: a Host Admin holding `cluster:delete` in
// USER mode still gets 403 ELEVATION_REQUIRED until they re-auth and
// bump to ELEVATED. Roles answer "are you allowed to do this?"; mode
// answers "have you proven you intended to right now?".
package policy

// Mode is one of "user" | "admin" | "elevated" per ADR-0003.
// USER is the default after login; the operator must explicitly
// re-authenticate to elevate to ADMIN, and again to reach ELEVATED.
type Mode string

const (
	// ModeUser is the read-mostly default. Covers UserRegion access
	// + share/sync ops + bucket viewing. Always allowed once logged
	// in; never requires elevation.
	ModeUser Mode = "user"
	// ModeAdmin grants non-destructive admin reads + most edits
	// (cluster:edit, key:edit_permissions, bucket:edit_alias, etc.).
	// Reached by re-entering the password from USER. 15 min idle TTL.
	ModeAdmin Mode = "admin"
	// ModeElevated grants destructive + authority-changing ops
	// (delete cluster/bucket/key, edit policy matrix, manage users,
	// edit layout). Reached by re-entering the password from ADMIN.
	// 5 min idle TTL — drops back to ADMIN, not USER.
	ModeElevated Mode = "elevated"
)

// MinModeFor returns the minimum session mode required to exercise the
// given capability. Per ADR-0003 the mapping is:
//
//   - ELEVATED: destructive + authority-changing ops
//     (cluster:delete, bucket:delete, key:delete,
//     host:manage_users, host:manage_policies,
//     policy:edit_matrix, policy:assign_role,
//     cluster:edit_layout)
//
//   - USER: data-plane + read-mostly ops
//     (objects:list/get/put/delete, objects:share_create/revoke,
//     bucket:view)
//
//   - ADMIN: everything else (the default — covers cluster:edit,
//     key:edit_permissions, bucket:edit_alias, bucket:set_quota,
//     cluster:test, cluster:view_layout, host:manage_*, etc.)
//
// Unknown capabilities default to ADMIN — the conservative choice that
// keeps a new gate from accidentally being callable in USER mode before
// someone audits it.
func MinModeFor(capability string) Mode {
	switch capability {
	// ELEVATED — destructive + authority-changing.
	case
		"cluster:delete",
		"bucket:delete",
		"key:delete",
		"host:manage_users",
		"host:manage_policies",
		"policy:edit_matrix",
		"policy:assign_role",
		"cluster:edit_layout":
		return ModeElevated
	// USER — data plane + harmless reads.
	case
		"objects:list",
		"objects:get",
		"objects:put",
		"objects:delete",
		"objects:share_create",
		"objects:share_revoke",
		"bucket:view":
		return ModeUser
	default:
		// Default ADMIN: cluster:edit, cluster:test, cluster:view_layout,
		// bucket:create, bucket:edit_alias, bucket:set_quota,
		// key:create, key:edit_permissions, key:view,
		// host:manage_signup_mode, host:manage_drivers, host:manage_org_caps,
		// policy:view_matrix, and anything future cycles add until
		// explicitly classified.
		return ModeAdmin
	}
}

// Includes reports whether m grants every privilege of `other`.
// ELEVATED includes ADMIN and USER; ADMIN includes USER; USER includes
// only itself. Used by the gate as `current.Includes(required)`.
func (m Mode) Includes(other Mode) bool {
	switch m {
	case ModeElevated:
		// ELEVATED is the top of the lattice — includes all three.
		return other == ModeElevated || other == ModeAdmin || other == ModeUser
	case ModeAdmin:
		// ADMIN includes itself + USER but not ELEVATED.
		return other == ModeAdmin || other == ModeUser
	case ModeUser:
		// USER only includes itself.
		return other == ModeUser
	}
	return false
}

// String returns the lowercase mode token used in JWT claims and audit
// records.
func (m Mode) String() string { return string(m) }
