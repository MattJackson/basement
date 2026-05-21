package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Enforcer is the policy decision point. UI gating and API handlers ask
// Can() / Capabilities(); /admin/policies handlers manage Roles +
// Assignments via the mutation methods.
//
// Per ADR-0001 the UI must use capability checks only — no driver-name
// checks, no role-name checks.
type Enforcer interface {
	// Can returns true iff userID is assigned a role whose capabilities
	// (after Expand) include `capability`, at an assignment scope that
	// matches the requested scope per ScopeMatches().
	Can(userID, capability, scope string) bool

	// Capabilities returns the unique, sorted list of leaf capabilities
	// userID holds at the given scope. Intended for UI gating: "should I
	// render the delete-bucket button at this bucket's scope?".
	Capabilities(userID, scope string) []string

	// AssignmentsFor returns every assignment for the given user, in
	// stable order (by RoleID then Scope).
	AssignmentsFor(userID string) []RoleAssignment

	// Roles returns a defensive copy of every role known to the enforcer.
	Roles() []Role

	// Assignments returns a defensive copy of every assignment.
	Assignments() []RoleAssignment

	// UpsertRole inserts or replaces a role by ID. Validates capabilities
	// before persisting. Seed flag on an incoming role is preserved iff a
	// role with that ID already exists and is Seed — operators cannot
	// promote arbitrary roles to seed by editing, and editing a seed role
	// keeps it seeded.
	UpsertRole(r Role) error

	// DeleteRole removes a role by ID. Refuses if the role is Seed (would
	// allow accidental lockout) or unknown. Also removes any assignments
	// referencing that role.
	DeleteRole(id string) error

	// AssignRole adds an assignment. Idempotent: re-adding the same
	// (userId, roleId, scope) triple is a no-op. Validates that the role
	// exists.
	AssignRole(a RoleAssignment) error

	// UnassignRole removes the (userId, roleId, scope) triple if present.
	// Removing an absent assignment is a no-op (not an error).
	UnassignRole(userID, roleID, scope string) error
}

// fileEnforcer is the file-backed implementation. policies.json lives at
// {dataDir}/policies.json; cache is RWMutex-guarded; saves are atomic
// (tmp + rename, matching internal/sync/store.go's pattern).
type fileEnforcer struct {
	path string

	mu          sync.RWMutex
	roles       []Role
	assignments []RoleAssignment
}

// Open opens (or creates + seeds) the policy store at dataDir/policies.json.
// Seeds the three built-in roles + matthew's host_admin assignment if the
// file doesn't exist or has zero roles (per ADR-0001 — prevents lockout
// on basement.pq.io's existing deployment).
func Open(dataDir string) (Enforcer, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("policy: creating data dir: %w", err)
	}

	e := &fileEnforcer{
		path: filepath.Join(dataDir, "policies.json"),
	}

	if err := e.load(); err != nil {
		return nil, err
	}

	if len(e.roles) == 0 {
		e.roles, e.assignments = seedDefaults()
		if err := e.saveLocked(); err != nil {
			return nil, fmt.Errorf("policy: seeding defaults: %w", err)
		}
	}

	return e, nil
}

// load reads policies.json into the cache. Missing file is fine — caller
// will seed defaults next.
func (e *fileEnforcer) load() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	data, err := os.ReadFile(e.path)
	if err != nil {
		if os.IsNotExist(err) {
			e.roles = nil
			e.assignments = nil
			return nil
		}
		return fmt.Errorf("policy: reading %s: %w", e.path, err)
	}
	if len(data) == 0 {
		e.roles = nil
		e.assignments = nil
		return nil
	}

	var pf policyFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return fmt.Errorf("policy: parsing %s: %w", e.path, err)
	}
	e.roles = pf.Roles
	e.assignments = pf.Assignments
	return nil
}

// saveLocked persists the cache. Caller must hold e.mu (write).
func (e *fileEnforcer) saveLocked() error {
	pf := policyFile{
		Roles:       e.roles,
		Assignments: e.assignments,
	}
	if pf.Roles == nil {
		pf.Roles = []Role{}
	}
	if pf.Assignments == nil {
		pf.Assignments = []RoleAssignment{}
	}

	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return fmt.Errorf("policy: marshaling: %w", err)
	}
	data = append(data, '\n')

	tmp := e.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("policy: writing tmp file: %w", err)
	}

	f, err := os.OpenFile(tmp, os.O_RDONLY|os.O_SYNC, 0644)
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("policy: opening tmp for fsync: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("policy: fsyncing tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("policy: closing tmp: %w", err)
	}

	if err := os.Rename(tmp, e.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("policy: renaming tmp to final: %w", err)
	}
	return nil
}

// --- Read side -------------------------------------------------------------

func (e *fileEnforcer) Can(userID, capability, scope string) bool {
	if userID == "" || capability == "" || scope == "" {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()

	rolesByID := map[string]*Role{}
	for i := range e.roles {
		r := &e.roles[i]
		rolesByID[r.ID] = r
	}

	for _, a := range e.assignments {
		if a.UserID != userID {
			continue
		}
		if !ScopeMatches(a.Scope, scope) {
			continue
		}
		role, ok := rolesByID[a.RoleID]
		if !ok {
			// Dangling assignment (role was deleted out from under it).
			// Treat as no grant — caller should clean up.
			continue
		}
		for _, capExpr := range role.Capabilities {
			for _, leaf := range Expand(capExpr) {
				if leaf == capability {
					return true
				}
			}
		}
	}
	return false
}

func (e *fileEnforcer) Capabilities(userID, scope string) []string {
	if userID == "" || scope == "" {
		return []string{}
	}
	e.mu.RLock()
	defer e.mu.RUnlock()

	rolesByID := map[string]*Role{}
	for i := range e.roles {
		r := &e.roles[i]
		rolesByID[r.ID] = r
	}

	seen := map[string]struct{}{}
	for _, a := range e.assignments {
		if a.UserID != userID {
			continue
		}
		if !ScopeMatches(a.Scope, scope) {
			continue
		}
		role, ok := rolesByID[a.RoleID]
		if !ok {
			continue
		}
		for _, capExpr := range role.Capabilities {
			for _, leaf := range Expand(capExpr) {
				seen[leaf] = struct{}{}
			}
		}
	}

	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func (e *fileEnforcer) AssignmentsFor(userID string) []RoleAssignment {
	e.mu.RLock()
	defer e.mu.RUnlock()

	out := []RoleAssignment{}
	for _, a := range e.assignments {
		if a.UserID == userID {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RoleID != out[j].RoleID {
			return out[i].RoleID < out[j].RoleID
		}
		return out[i].Scope < out[j].Scope
	})
	return out
}

func (e *fileEnforcer) Roles() []Role {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]Role, len(e.roles))
	for i, r := range e.roles {
		// Defensive deep-ish copy of slice header so callers can't mutate
		// our cache via the returned Capabilities slice.
		caps := make([]string, len(r.Capabilities))
		copy(caps, r.Capabilities)
		r.Capabilities = caps
		out[i] = r
	}
	return out
}

func (e *fileEnforcer) Assignments() []RoleAssignment {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]RoleAssignment, len(e.assignments))
	copy(out, e.assignments)
	return out
}

// --- Write side ------------------------------------------------------------

func (e *fileEnforcer) UpsertRole(r Role) error {
	if r.ID == "" {
		return fmt.Errorf("policy: role ID required")
	}
	if err := ValidateRoleCapabilities(r.Capabilities); err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for i, existing := range e.roles {
		if existing.ID == r.ID {
			// Preserve Seed flag from the existing record — caller can't
			// promote nor demote seed status via UpsertRole.
			r.Seed = existing.Seed
			e.roles[i] = r
			return e.saveLocked()
		}
	}

	// New role — strip any incoming Seed=true; only seeding-at-construction
	// may create seed roles.
	r.Seed = false
	e.roles = append(e.roles, r)
	return e.saveLocked()
}

func (e *fileEnforcer) DeleteRole(id string) error {
	if id == "" {
		return fmt.Errorf("policy: role ID required")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	idx := -1
	for i, r := range e.roles {
		if r.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("policy: role %q not found", id)
	}
	if e.roles[idx].Seed {
		return fmt.Errorf("policy: role %q is a seed role and cannot be deleted", id)
	}

	e.roles = append(e.roles[:idx], e.roles[idx+1:]...)

	// Drop any dangling assignments to this role.
	filtered := e.assignments[:0]
	for _, a := range e.assignments {
		if a.RoleID != id {
			filtered = append(filtered, a)
		}
	}
	e.assignments = filtered

	return e.saveLocked()
}

func (e *fileEnforcer) AssignRole(a RoleAssignment) error {
	if a.UserID == "" || a.RoleID == "" || a.Scope == "" {
		return fmt.Errorf("policy: assignment requires userId, roleId, and scope")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	roleExists := false
	for _, r := range e.roles {
		if r.ID == a.RoleID {
			roleExists = true
			break
		}
	}
	if !roleExists {
		return fmt.Errorf("policy: role %q does not exist", a.RoleID)
	}

	for _, existing := range e.assignments {
		if existing.UserID == a.UserID && existing.RoleID == a.RoleID && existing.Scope == a.Scope {
			return nil // idempotent
		}
	}

	e.assignments = append(e.assignments, a)
	return e.saveLocked()
}

func (e *fileEnforcer) UnassignRole(userID, roleID, scope string) error {
	if userID == "" || roleID == "" || scope == "" {
		return fmt.Errorf("policy: unassign requires userId, roleId, and scope")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	changed := false
	filtered := e.assignments[:0]
	for _, a := range e.assignments {
		if a.UserID == userID && a.RoleID == roleID && a.Scope == scope {
			changed = true
			continue
		}
		filtered = append(filtered, a)
	}
	e.assignments = filtered

	if !changed {
		return nil
	}
	return e.saveLocked()
}

// --- Scope matching --------------------------------------------------------

// ScopeMatches reports whether an assignment scope covers a requested
// scope. Both use the URI-style "domain:part:part" grammar; wildcard is
// the literal trailing "*" (not regex).
//
// Examples:
//
//	ScopeMatches("host:*",            "host:*")            -> true
//	ScopeMatches("*",                 "bucket:abc:lsi")    -> true
//	ScopeMatches("bucket:*",          "bucket:abc:lsi")    -> true
//	ScopeMatches("bucket:abc:*",      "bucket:abc:lsi")    -> true
//	ScopeMatches("bucket:abc:lsi",    "bucket:abc:lsi")    -> true
//	ScopeMatches("bucket:xyz:*",      "bucket:abc:lsi")    -> false
//	ScopeMatches("cluster:abc",       "bucket:abc:lsi")    -> false  (different domain)
//
// Per ADR-0001 wildcards are explicit, not implicit — a role with
// cluster:abc does NOT auto-cover its buckets.
func ScopeMatches(assignmentScope, requestedScope string) bool {
	if assignmentScope == "" || requestedScope == "" {
		return false
	}
	// Bare "*" matches anything (superuser scope).
	if assignmentScope == "*" {
		return true
	}
	if assignmentScope == requestedScope {
		return true
	}

	// Trailing "*" means "any continuation at or below this prefix".
	// "bucket:*" matches "bucket:abc" and "bucket:abc:lsi".
	// "bucket:abc:*" matches "bucket:abc:lsi" but NOT "bucket:xyz:lsi".
	if len(assignmentScope) >= 2 && assignmentScope[len(assignmentScope)-2:] == ":*" {
		prefix := assignmentScope[:len(assignmentScope)-1] // keep trailing ':'
		if len(requestedScope) >= len(prefix) && requestedScope[:len(prefix)] == prefix {
			return true
		}
	}

	return false
}

// --- Seed defaults ---------------------------------------------------------

// seedDefaults returns the three built-in roles + one assignment
// (matthew -> host_admin) per ADR-0001 + this cycle's prompt. The
// matthew assignment exists so basement.pq.io doesn't lock matthew out
// of his own deployment on first policy-aware boot.
func seedDefaults() ([]Role, []RoleAssignment) {
	roles := []Role{
		{
			ID:           "host_admin",
			Label:        "Host Admin",
			Description:  "Full control over basement itself and every cluster, bucket, key, object, and policy.",
			Capabilities: []string{"*:*"},
			Seed:         true,
		},
		{
			ID:          "cluster_admin",
			Label:       "Cluster Admin",
			Description: "Manages buckets, keys, layout on a cluster they're assigned to.",
			Capabilities: []string{
				"cluster:edit",
				"cluster:test",
				"cluster:view_layout",
				"cluster:edit_layout",
				"bucket:*",
				"key:*",
				"objects:list",
			},
			Seed: true,
		},
		{
			ID:          "bucket_user",
			Label:       "Bucket User",
			Description: "Reads, writes, and shares objects in a bucket they're granted access to.",
			Capabilities: []string{
				"bucket:view",
				"objects:list",
				"objects:get",
				"objects:put",
				"objects:share_create",
				"objects:share_revoke",
			},
			Seed: true,
		},
	}

	// Only matthew gets a seed assignment — the env-seeded admin. Any
	// other admin (OIDC, signup) is up to the operator to grant explicitly.
	assignments := []RoleAssignment{
		{UserID: "matthew", RoleID: "host_admin", Scope: "host:*"},
	}

	return roles, assignments
}
