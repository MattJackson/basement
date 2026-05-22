// Package policy: sudo-style mode model (ADR-0003 + v1.3.0a.4 amendment).
//
// Each session is in one of two modes — USER or ADMIN — and each
// capability declares the minimum mode required to exercise it. The
// gate (internal/api/policy_gates.go) checks both that the user HOLDS
// the capability via their role assignments AND that their current
// session mode meets the capability's minimum.
//
// Mode is orthogonal to roles: a Host Admin holding `cluster:delete` in
// USER mode still gets 403 ELEVATION_REQUIRED until they re-auth and
// bump to ADMIN. Roles answer "are you allowed to do this?"; mode
// answers "have you proven you intended to right now?".
//
// History: v1.2.0 introduced three modes (USER / ADMIN / ELEVATED).
// The v1.3.0a.4 amendment collapsed ELEVATED into ADMIN — the extra
// sub-mode added cognitive overhead without buying real protection;
// the per-elevation TTL is the safety, not a sub-mode. ModeElevated is
// kept as a type-level alias for one release cycle so any caller still
// referencing it compiles + behaves identically to ModeAdmin.
package policy

// Mode is one of "user" | "admin" per the v1.3.0a.4 amendment to
// ADR-0003. USER is the default after login; the operator must
// explicitly re-authenticate to elevate to ADMIN.
type Mode string

const (
	// ModeUser is the read-mostly default. Covers UserRegion access
	// + share/sync ops + bucket viewing. Always allowed once logged
	// in; never requires elevation.
	ModeUser Mode = "user"
	// ModeAdmin grants every admin capability — destructive ops
	// included. Reached by re-entering the password (or completing a
	// fresh OIDC challenge) from USER. TTL is operator-configured via
	// OrgCapabilities.AdminSessionTTLSec (default 15 min; range
	// 60s – 24h). When the TTL elapses the session falls back to USER.
	ModeAdmin Mode = "admin"
	// ModeElevated is a v1.2-era alias for ModeAdmin retained for one
	// release cycle so any caller still typing the constant compiles
	// without change. New code should use ModeAdmin directly. Cookies
	// with mode="elevated" are silently migrated to mode="admin" on
	// read in currentMode() so an in-flight v1.2 session doesn't have
	// to re-log-in across the upgrade.
	ModeElevated Mode = ModeAdmin
)

// MinModeFor returns the minimum session mode required to exercise the
// given capability. Per the ADR-0003 v1.3.0a.4 amendment the mapping is:
//
//   - USER: data-plane + read-mostly ops
//     (objects:list/get/put/delete, objects:share_create/revoke,
//     bucket:view)
//
//   - ADMIN: everything else, including destructive + authority-changing
//     ops (cluster:delete, bucket:delete, key:delete, host:manage_*,
//     policy:edit_matrix, policy:assign_role, cluster:edit_layout,
//     cluster:edit, cluster:test, cluster:view_layout, bucket:create,
//     bucket:edit_alias, bucket:set_quota, key:create,
//     key:edit_permissions, key:view, policy:view_matrix, etc.)
//
// Unknown capabilities default to ADMIN — the conservative choice that
// keeps a new gate from accidentally being callable in USER mode before
// someone audits it.
func MinModeFor(capability string) Mode {
	switch capability {
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
		// Default ADMIN: every admin capability, destructive or not.
		// The v1.3.0a.4 amendment removed the ELEVATED sub-tier; the
		// TTL is the safety, not the mode bump.
		return ModeAdmin
	}
}

// Includes reports whether m grants every privilege of `other`. With
// the v1.3.0a.4 two-mode model the lattice is simply ADMIN >= USER;
// USER includes only itself. Used by the gate as
// `current.Includes(required)`.
func (m Mode) Includes(other Mode) bool {
	switch m {
	case ModeAdmin:
		// ADMIN includes itself + USER.
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
