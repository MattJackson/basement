package policy

// Role is a named bundle of capabilities. Per ADR-0001 "Flexibility:
// role/permission matrix", roles + capabilities are data, not code.
// The three tiers shipped at v0.9.x (host_admin / cluster_admin /
// bucket_user) are seed presets — rows in the matrix the operator can
// edit, not enum values in Go.
type Role struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// Capabilities may include "domain:*" shorthand and the reserved
	// "*:*" superuser. Expand() unfolds these to leaf capability IDs at
	// enforcement time.
	Capabilities []string `json:"capabilities"`
	// Seed marks built-in roles. Seed roles can be edited but cannot be
	// deleted — prevents accidental lockout.
	Seed bool `json:"seed"`
	// Deprecated marks roles that still exist for back-compat but whose
	// capabilities no longer have effect at the gates they used to cover.
	// ADR-0002 (v1.1.0f) introduces this for `bucket_user`: the
	// `objects:*` capabilities on bucket-scoped assignments are inert
	// post-v1.1.0e because user-tier bucket access now flows through the
	// region keychain's S3 key, NOT through basement's policy gates.
	// The UI uses this flag to render a badge + banner + assignment
	// guardrail; the enforcer itself doesn't change behaviour based on
	// it. Existing assignments stay valid + deletable.
	Deprecated bool `json:"deprecated,omitempty"`
}

// RoleAssignment binds a user to a role at a given scope. A user with no
// assignments has zero capabilities — secure default.
//
// Scope grammar (URI-style, pattern-matched by the enforcer):
//
//   host:*                   basement platform — singular
//   cluster:*                every cluster
//   cluster:{cid}            one specific cluster
//   bucket:{cid}:*           every bucket on a cluster
//   bucket:{cid}:{bid}       one specific bucket
//   key:{cid}:*              every key on a cluster
//   key:{cid}:{kid}          one specific key
type RoleAssignment struct {
	UserID string `json:"userId"`
	RoleID string `json:"roleId"`
	Scope  string `json:"scope"`
}

// policyFile is the on-disk shape of policies.json — a single file the
// operator backs up alongside everything else.
type policyFile struct {
	Roles       []Role           `json:"roles"`
	Assignments []RoleAssignment `json:"assignments"`
}
