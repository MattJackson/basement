// Package policy implements the RBAC capability registry, role/assignment
// types, and enforcer interface described in ADR-0001
// (docs/adr/0001-rbac-three-tier-creds.md).
//
// This file defines the compiled-in capability registry. Adding a
// capability requires a code change because something has to *implement*
// it — but which roles get which capabilities is data, stored in
// policies.json and edited by Host Admin via /admin/policies.
//
// Capability IDs use the form "domain:verb". Roles may use the shorthand
// "domain:*" in their capability lists; Expand() unfolds that to every
// verb in the registered domain.
package policy

import (
	"fmt"
	"sort"
	"strings"
)

// Registry maps capability ID -> human description. The set of valid
// capability IDs the enforcer accepts. Roles referencing an unknown
// capability fail validation at write time + warn at read time.
//
// Initial vocabulary seeded per ADR-0001's "Capability vocabulary" table.
var Registry = map[string]string{
	// Host (basement platform)
	"host:manage_users":       "Create, edit, delete basement user accounts",
	"host:manage_signup_mode": "Toggle signup mode (open/invite-only/closed)",
	"host:manage_drivers":     "Enable / disable backend drivers basement surfaces",
	"host:manage_org_caps":    "Edit basement-wide org capabilities flags",
	"host:manage_policies":    "Edit the RBAC role/permission matrix and assignments",

	// Cluster
	"cluster:create":      "Register a new cluster (admin URL + admin token)",
	"cluster:edit":        "Edit a cluster's label, color, endpoints, admin token",
	"cluster:delete":      "Remove a cluster from basement",
	"cluster:test":        "Run connectivity test against a cluster",
	"cluster:view_layout": "View a cluster's storage layout",
	"cluster:edit_layout": "Edit a cluster's storage layout (zones, nodes, capacities)",

	// Bucket
	"bucket:create":     "Create a new bucket on a cluster",
	"bucket:edit_alias": "Rename a bucket alias",
	"bucket:set_quota":  "Set or change a bucket's quota",
	"bucket:delete":     "Delete a bucket",
	"bucket:view":       "View bucket metadata (size, object count, quotas)",

	// Key
	"key:create":           "Create a new access key on a cluster",
	"key:edit_permissions": "Edit which buckets / permissions an access key has",
	"key:delete":           "Delete an access key",
	"key:view":             "View access key metadata",

	// Objects (data plane)
	"objects:list":          "List objects within a bucket",
	"objects:get":           "Download / read objects",
	"objects:put":           "Upload / write objects",
	"objects:delete":        "Delete objects",
	"objects:share_create":  "Create a share link for an object or prefix",
	"objects:share_revoke":  "Revoke a previously created share link",

	// Policy (the matrix itself)
	"policy:view_matrix": "View the role/permission matrix and assignments",
	"policy:edit_matrix": "Edit roles and their capabilities",
	"policy:assign_role": "Assign a role to a user at a given scope",
}

// Validate returns nil iff capID is a registered capability.
// "*:*" and "domain:*" shorthand are NOT accepted here — they're only
// valid as entries in a Role's capability list (which Expand() unfolds).
// Validate is for checking concrete (leaf) capability IDs.
func Validate(capID string) error {
	if capID == "" {
		return fmt.Errorf("policy: empty capability ID")
	}
	if _, ok := Registry[capID]; !ok {
		return fmt.Errorf("policy: unknown capability %q", capID)
	}
	return nil
}

// Expand takes a capability expression as it may appear in a Role's
// capability list and returns the concrete leaf capabilities it covers.
//
//   - "*:*"          -> every capability in the registry
//   - "domain:*"     -> every registered capability whose ID starts with "domain:"
//   - "domain:verb"  -> []string{"domain:verb"} if registered, else empty
//
// The returned slice is sorted for deterministic output and is always
// non-nil (possibly empty). Unknown leaf IDs are dropped silently — call
// ValidateRoleCapabilities to surface errors at write time.
func Expand(capExpr string) []string {
	out := []string{}
	if capExpr == "" {
		return out
	}

	if capExpr == "*:*" {
		for id := range Registry {
			out = append(out, id)
		}
		sort.Strings(out)
		return out
	}

	if strings.HasSuffix(capExpr, ":*") {
		domain := strings.TrimSuffix(capExpr, ":*")
		prefix := domain + ":"
		for id := range Registry {
			if strings.HasPrefix(id, prefix) {
				out = append(out, id)
			}
		}
		sort.Strings(out)
		return out
	}

	if _, ok := Registry[capExpr]; ok {
		out = append(out, capExpr)
	}
	return out
}

// ValidateRoleCapabilities checks every entry in a Role's capability
// list. Wildcard forms ("*:*", "domain:*") are accepted iff at least one
// matching capability exists in the registry. Leaf IDs must be present
// in the registry.
func ValidateRoleCapabilities(caps []string) error {
	for _, c := range caps {
		if c == "*:*" {
			continue
		}
		if strings.HasSuffix(c, ":*") {
			domain := strings.TrimSuffix(c, ":*")
			if domain == "" {
				return fmt.Errorf("policy: malformed capability expression %q", c)
			}
			// At least one capability must exist in this domain.
			prefix := domain + ":"
			found := false
			for id := range Registry {
				if strings.HasPrefix(id, prefix) {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("policy: capability expression %q matches no registered capability", c)
			}
			continue
		}
		if err := Validate(c); err != nil {
			return err
		}
	}
	return nil
}
