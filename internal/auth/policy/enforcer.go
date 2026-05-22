package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// ReasoningStep is one line in the human-readable explanation a
// `CanWithReason` call returns. The simulator UI renders these in
// order so an operator answering "why can/can't user X do Y at Z?"
// gets a step-by-step trail back to either the matching role (allow
// path) or the reason the request fell through (deny path).
//
// Step is the short headline ("user has assignment", "capability not
// covered"); Detail is the supporting evidence ("matthew →
// host_admin@host:*"). The simulator renders Step bold and Detail in
// muted text.
type ReasoningStep struct {
	Step   string `json:"step"`
	Detail string `json:"detail"`
}

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

	// CanWithReason returns the same bool as Can plus the matching
	// assignments (empty on deny) and a sequenced explanation suitable
	// for the policy simulator UI. The reasoning records every
	// assignment considered: which ones were dropped (and why) and
	// which one finally satisfied the request. POLICY.SIM (v0.9.0j) is
	// the sole caller — production hot paths stay on Can() which skips
	// the bookkeeping.
	CanWithReason(userID, capability, scope string) (bool, []RoleAssignment, []ReasoningStep)

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
	// exists. When an existing manual assignment (Source != "oidc")
	// matches the triple, the existing Source wins — calling AssignRole
	// with Source="oidc" on top of a manual row leaves it manual so an
	// operator's explicit grant can't be silently downgraded into a
	// revocable auto-assignment.
	AssignRole(a RoleAssignment) error

	// UnassignRole removes the (userId, roleId, scope) triple if present.
	// Removing an absent assignment is a no-op (not an error).
	UnassignRole(userID, roleID, scope string) error

	// SyncOIDCAssignments reconciles the user's Source="oidc" assignments
	// against `wanted` (v1.3.0a):
	//
	//   - Every triple in `wanted` not already present gets added with
	//     Source="oidc"+AutoAssigned=true.
	//   - Every existing Source="oidc" assignment for the user whose
	//     (RoleID, Scope) is NOT in `wanted` gets revoked.
	//   - Manually-assigned rows (Source != "oidc") for the same user are
	//     never touched, even if they overlap with `wanted`.
	//
	// Returns the assignments that were added + revoked so the caller
	// (the OIDC callback) can emit one audit event per change.
	SyncOIDCAssignments(userID string, wanted []RoleAssignment) (added, revoked []RoleAssignment, err error)

	// SeedEnvAdmin ensures the env-seeded admin (cfg.Admin.User) has the
	// blanket assignments that keep them functional on first boot of a
	// policy-aware deployment: host_admin on host:*, host_admin on "*"
	// (superuser scope), cluster_admin on cluster:*. Idempotent —
	// called from main.go at boot. Returning early on empty username
	// keeps tests that don't supply one working with the legacy
	// seedDefaults() pair.
	//
	// Per ADR-0001 + v0.9.0f prompt: matthew's existing usage of
	// basement.pq.io must not break when capability gates land, so the
	// runtime grants him the blanket roles up front. Operators
	// can revoke or narrow these via /admin/policies later.
	//
	// ADR-0002 (v1.1.0f) note: the legacy `bucket_user @ bucket:*` seed
	// is no longer created on fresh deployments — bucket-level user
	// access flows through the region keychain's S3 key, not policy.
	// Existing bucket_user assignments survive untouched (back-compat).
	SeedEnvAdmin(username string) error
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
	allowed, _, _ := e.CanWithReason(userID, capability, scope)
	return allowed
}

// CanWithReason walks the same loop as Can but records every
// assignment it considered. Returns the final bool, the assignments
// that contributed to the decision (the single granting assignment
// on allow; every same-user assignment that was considered on deny),
// and a sequenced reasoning trail. POLICY.SIM (v0.9.0j) is the only
// caller — production hot paths use Can() which discards the trail.
func (e *fileEnforcer) CanWithReason(userID, capability, scope string) (bool, []RoleAssignment, []ReasoningStep) {
	steps := []ReasoningStep{}
	matches := []RoleAssignment{}

	if userID == "" || capability == "" || scope == "" {
		steps = append(steps, ReasoningStep{
			Step:   "invalid request",
			Detail: "userId, capability, and scope are all required",
		})
		return false, matches, steps
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	rolesByID := map[string]*Role{}
	for i := range e.roles {
		r := &e.roles[i]
		rolesByID[r.ID] = r
	}

	// Surface the user's full assignment set up front so deny paths
	// can show "we considered N assignments for this user" without a
	// second walk of the slice.
	userAssignments := []RoleAssignment{}
	for _, a := range e.assignments {
		if a.UserID == userID {
			userAssignments = append(userAssignments, a)
		}
	}

	if len(userAssignments) == 0 {
		steps = append(steps, ReasoningStep{
			Step:   "no assignments",
			Detail: fmt.Sprintf("user %q has zero role assignments — secure default is deny", userID),
		})
		return false, matches, steps
	}

	steps = append(steps, ReasoningStep{
		Step: "user has assignments",
		Detail: fmt.Sprintf("user %q has %d assignment(s); checking each against capability %q at scope %q",
			userID, len(userAssignments), capability, scope),
	})

	for _, a := range userAssignments {
		role, ok := rolesByID[a.RoleID]
		if !ok {
			// Dangling assignment (role was deleted out from under it).
			// Treat as no grant — caller should clean up.
			steps = append(steps, ReasoningStep{
				Step:   "skip: dangling assignment",
				Detail: fmt.Sprintf("%s @ %s references role %q which no longer exists", a.UserID, a.Scope, a.RoleID),
			})
			continue
		}

		if !ScopeMatches(a.Scope, scope) {
			steps = append(steps, ReasoningStep{
				Step: "skip: scope mismatch",
				Detail: fmt.Sprintf("assignment %s @ %s does not cover requested scope %s",
					a.RoleID, a.Scope, scope),
			})
			continue
		}

		// Scope matches. Check the role's capabilities.
		matchingExpr := ""
		for _, capExpr := range role.Capabilities {
			for _, leaf := range Expand(capExpr) {
				if leaf == capability {
					matchingExpr = capExpr
					break
				}
			}
			if matchingExpr != "" {
				break
			}
		}

		if matchingExpr == "" {
			steps = append(steps, ReasoningStep{
				Step: "skip: capability not in role",
				Detail: fmt.Sprintf("scope matched (%s covers %s) but role %q does not grant %s",
					a.Scope, scope, a.RoleID, capability),
			})
			continue
		}

		// Granted.
		steps = append(steps, ReasoningStep{
			Step: "scope matches",
			Detail: fmt.Sprintf("assignment %s @ %s covers requested scope %s",
				a.RoleID, a.Scope, scope),
		})
		if matchingExpr == capability {
			steps = append(steps, ReasoningStep{
				Step: "capability granted",
				Detail: fmt.Sprintf("role %q lists %s directly", a.RoleID, capability),
			})
		} else {
			steps = append(steps, ReasoningStep{
				Step: "capability granted via wildcard",
				Detail: fmt.Sprintf("role %q lists %s which expands to include %s",
					a.RoleID, matchingExpr, capability),
			})
		}
		matches = append(matches, a)
		return true, matches, steps
	}

	steps = append(steps, ReasoningStep{
		Step:   "deny",
		Detail: fmt.Sprintf("no assignment for %q grants %s at scope %s", userID, capability, scope),
	})
	return false, matches, steps
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
			// Preserve Deprecated flag too — once a role is sunsetted
			// (ADR-0002 bucket_user), an operator editing its label or
			// capabilities can't accidentally un-deprecate it. Removing
			// the deprecation is a code change, not a UI action.
			r.Deprecated = existing.Deprecated
			e.roles[i] = r
			return e.saveLocked()
		}
	}

	// New role — strip any incoming Seed=true; only seeding-at-construction
	// may create seed roles. Same for Deprecated — only code marks roles
	// deprecated, never the UI / API.
	r.Seed = false
	r.Deprecated = false
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
			// Manual-source assignments are never silently downgraded
			// to OIDC-source on re-add: the existing row wins. This
			// protects an operator's explicit grant from being turned
			// into a revocable auto-assignment if the same triple
			// happens to appear in OIDC sync output.
			return nil // idempotent
		}
	}

	// Normalise: an empty Source defaults to "manual" so audit + UI
	// can rely on a non-empty value everywhere. AutoAssigned is
	// derived from Source so the two never disagree on disk.
	if a.Source == "" {
		a.Source = "manual"
	}
	a.AutoAssigned = a.Source == "oidc"
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

// SyncOIDCAssignments is the per-login reconcile step for OIDC-driven
// role assignments (v1.3.0a). It walks the user's current Source="oidc"
// rows and adjusts the persisted set so it exactly matches `wanted`:
//
//   - Every triple in wanted that is not already present gets appended
//     with Source="oidc" + AutoAssigned=true.
//   - Every persisted Source="oidc" row whose (RoleID, Scope) is NOT
//     in wanted gets dropped.
//   - Manual rows (Source != "oidc") are never read or modified — even
//     if a wanted triple overlaps with a manual row, the operator's
//     explicit grant survives untouched (no duplicate, no downgrade).
//
// One saveLocked at the end keeps the JSON file consistent under
// concurrent reads; the caller can audit-log the returned added/revoked
// slices without holding any of the enforcer's mutex state.
func (e *fileEnforcer) SyncOIDCAssignments(userID string, wanted []RoleAssignment) (added, revoked []RoleAssignment, err error) {
	if userID == "" {
		return nil, nil, fmt.Errorf("policy: SyncOIDCAssignments requires userId")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Build a quick lookup of valid role IDs so we don't persist an
	// auto-assignment referencing a role that doesn't exist (operator
	// deleted the role between mapping save + this login).
	validRole := map[string]bool{}
	for _, r := range e.roles {
		validRole[r.ID] = true
	}

	// Wanted set, keyed by RoleID+Scope, filtered to valid roles.
	type key struct{ roleID, scope string }
	wantedSet := map[key]RoleAssignment{}
	for _, w := range wanted {
		if w.RoleID == "" || w.Scope == "" {
			continue
		}
		if !validRole[w.RoleID] {
			continue
		}
		wantedSet[key{w.RoleID, w.Scope}] = RoleAssignment{
			UserID:       userID,
			RoleID:       w.RoleID,
			Scope:        w.Scope,
			Source:       "oidc",
			AutoAssigned: true,
		}
	}

	// Walk the existing assignments. For this user's OIDC rows: keep
	// those still wanted (and remove from wantedSet so we don't re-add),
	// drop those no longer wanted. For everything else (other users
	// + this user's manual rows): keep verbatim.
	kept := e.assignments[:0]
	for _, a := range e.assignments {
		if a.UserID != userID || a.Source != "oidc" {
			kept = append(kept, a)
			// A manual row for this user that overlaps with a wanted
			// triple satisfies the operator's intent — drop the wanted
			// entry so we don't add a duplicate OIDC row alongside it.
			if a.UserID == userID && a.Source != "oidc" {
				delete(wantedSet, key{a.RoleID, a.Scope})
			}
			continue
		}
		k := key{a.RoleID, a.Scope}
		if _, ok := wantedSet[k]; ok {
			// Still wanted — keep + ensure flag fields stay normalised.
			a.Source = "oidc"
			a.AutoAssigned = true
			kept = append(kept, a)
			delete(wantedSet, k)
			continue
		}
		// No longer wanted — revoke.
		revoked = append(revoked, a)
	}

	// Anything left in wantedSet is a new assignment to add.
	for _, w := range wantedSet {
		kept = append(kept, w)
		added = append(added, w)
	}

	e.assignments = kept

	if len(added) == 0 && len(revoked) == 0 {
		return added, revoked, nil
	}
	if err := e.saveLocked(); err != nil {
		return nil, nil, fmt.Errorf("policy: persisting OIDC sync: %w", err)
	}
	return added, revoked, nil
}

// SeedEnvAdmin grants the env-seeded admin (BASEMENT_ADMIN_USER) the
// blanket assignments that keep matthew's existing flow on
// basement.pq.io working when v0.9.0f's capability gates land.
//
// v0.9.0m.1 adds host_admin @ "*" (true superuser scope) on top of the
// per-domain seeds. ScopeMatches("*", anything) returns true, so this
// covers every future gate at any scope domain (key:cid:*, bucket:cid:*,
// objects:cid:bid:*, etc.) — including domains added in cycles that
// weren't anticipated when v0.9.0f shipped its seed list.
//
// The pre-v0.9.0m.1 seed (host:*, cluster:*, bucket:*) ONLY covered the
// three named domains; new gates like key:create @ key:cid:* (added
// v0.9.0f but never live-verified end-to-end) silently blocked the
// env-admin because no seeded assignment scope matched the key: domain.
// The * scope is the explicit "owns everything" assignment per ADR-0001
// and matches host_admin's *:* capability list.
//
// All four assignments are independently idempotent via AssignRole so
// re-running on each boot — including for operators upgrading from
// pre-v0.9.0m.1 — safely adds the missing * row without touching the
// existing per-domain rows.
func (e *fileEnforcer) SeedEnvAdmin(username string) error {
	if username == "" {
		return nil
	}

	// ADR-0002 (v1.1.0f): no longer seed bucket_user @ bucket:*. The
	// role is deprecated — bucket-level user access flows through the
	// region keychain's S3 key, not basement policy. Existing
	// deployments that already have the assignment keep it (back-compat
	// + cleanup-on-demand); new deployments get nothing for it. Removing
	// the wants entry is the whole change: AssignRole was already
	// idempotent, so existing matthew@bucket_user@bucket:* rows survive
	// untouched.
	wants := []RoleAssignment{
		{UserID: username, RoleID: "host_admin", Scope: "host:*"},
		// v0.9.0m.1: superuser scope. Covers every domain — key:*,
		// bucket:*, objects:*, anything future cycles add — so a new
		// gate per cycle doesn't silently lock out the env-admin.
		{UserID: username, RoleID: "host_admin", Scope: "*"},
		{UserID: username, RoleID: "cluster_admin", Scope: "cluster:*"},
	}

	for _, a := range wants {
		if err := e.AssignRole(a); err != nil {
			return fmt.Errorf("policy: seeding env-admin %q with %s/%s: %w", username, a.RoleID, a.Scope, err)
		}
	}
	return nil
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
			Description: "Deprecated as of ADR-0002 (v1.1.0f): user-tier bucket access is now controlled by the S3 key attached to a UserRegion, not by basement policy. New assignments have no effect; existing assignments remain for back-compat and deletable for cleanup.",
			Capabilities: []string{
				"bucket:view",
				"objects:list",
				"objects:get",
				"objects:put",
				"objects:share_create",
				"objects:share_revoke",
			},
			Seed:       true,
			Deprecated: true,
		},
	}

	// Only matthew gets a seed assignment — the env-seeded admin. Any
	// other admin (OIDC, signup) is up to the operator to grant explicitly.
	assignments := []RoleAssignment{
		{UserID: "matthew", RoleID: "host_admin", Scope: "host:*"},
	}

	return roles, assignments
}
