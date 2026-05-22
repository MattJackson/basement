// Package api: per-cluster admin assignment listing handler
// (v1.3.0e CLUSTER.ADMINS).
//
// /admin/policies is the global matrix editor. A common operator
// task — "show me everyone who has admin rights on THIS cluster" —
// is awkward there because they have to filter the global assignments
// table by scope substring. This handler is the convenience read for
// the cluster detail page: returns the assignments scoped to one
// cluster, plus any wildcard assignments (`cluster:*`, `*`) that
// confer admin authority over it.
//
// Read-only: writes still go through the global
// /admin/policies/assignments endpoints (assignRole / unassignRole).
// Same `policy:view_matrix` gate as the global GET — anyone allowed
// to see assignments can see this scoped view.
package api

import (
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"

	"github.com/mattjackson/basement/internal/auth/policy"
)

// clusterAdminAssignmentDTO is the wire shape for a single row on the
// /admin/clusters/{cid}/admins surface. Mirrors policy.RoleAssignment
// with two additions:
//
//   - DisplayName: joined from the user store so the UI doesn't have
//     to do a second round-trip per row.
//   - Inherited: true when the row matches because of a wildcard scope
//     (`cluster:*` or `*`) rather than this cluster's exact scope.
//     The UI uses this to render an "inherited from global" badge and
//     disable the Remove button — removing wildcards belongs in
//     /admin/policies, not here.
type clusterAdminAssignmentDTO struct {
	UserID      string `json:"userId"`
	DisplayName string `json:"displayName,omitempty"`
	RoleID      string `json:"roleId"`
	Scope       string `json:"scope"`
	Source      string `json:"source,omitempty"`
	Inherited   bool   `json:"inherited"`
}

// clusterAdminsResponse is the response envelope. Wrapping the slice
// keeps the door open for future fields (e.g. effective capabilities,
// last-modified) without breaking the FE consumer.
type clusterAdminsResponse struct {
	Assignments []clusterAdminAssignmentDTO `json:"assignments"`
}

// listClusterAdminsHandler implements GET /api/v1/admin/clusters/{cid}/admins.
// Gated on policy:view_matrix @ host:*.
//
// Returns the union of:
//
//   - Assignments whose Scope is exactly `cluster:{cid}` (manual or oidc).
//   - Assignments whose Scope is `cluster:*` (or `*` superuser) —
//     marked Inherited=true so the UI gates the Remove button.
//
// Bucket-scoped assignments (`bucket:{cid}:*`) are NOT included even
// though they technically grant authority over THAT cluster's buckets
// — the cycle prompt scopes this view to "cluster admins" (the
// cluster_admin tier), not the broader notion of "anyone who can
// touch anything inside the cluster".
func (s *Server) listClusterAdminsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	cid := chi.URLParam(r, "cid")
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id required")
		return
	}

	if _, ok := s.requireCapability(w, r, "policy:view_matrix", "host:*"); !ok {
		return
	}

	if s.policy == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "POLICY_NOT_WIRED",
			"Policy subsystem is not configured on this deployment.")
		return
	}

	// Confirm the cluster exists so a stale link returns 404 instead
	// of an empty 200 (which the FE would happily render as "no
	// admins on a cluster that doesn't exist").
	if s.conns != nil {
		if _, err := s.conns.Get(r.Context(), cid); err != nil {
			writeRegistryForError(w, err)
			return
		}
	}

	wantScope := "cluster:" + cid

	all := s.policy.Assignments()
	rows := make([]clusterAdminAssignmentDTO, 0, len(all))
	for _, a := range all {
		if !policy.ScopeMatches(a.Scope, wantScope) {
			continue
		}
		// Inherited rows are everything that matches via wildcard
		// rather than exact scope. ScopeMatches() handles the
		// matching; the UI just needs the boolean to gate the
		// Remove button + render the badge.
		inherited := a.Scope != wantScope
		rows = append(rows, clusterAdminAssignmentDTO{
			UserID:      a.UserID,
			DisplayName: s.lookupDisplayName(a.UserID),
			RoleID:      a.RoleID,
			Scope:       a.Scope,
			Source:      a.Source,
			Inherited:   inherited,
		})
	}

	// Stable sort: inherited rows last (they're context, not the
	// primary action), then by userId for predictable rendering.
	// Stable so equal-priority rows keep the enforcer's order.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Inherited != rows[j].Inherited {
			return !rows[i].Inherited
		}
		if rows[i].UserID != rows[j].UserID {
			return rows[i].UserID < rows[j].UserID
		}
		return rows[i].RoleID < rows[j].RoleID
	})

	writeJSON(w, http.StatusOK, clusterAdminsResponse{Assignments: rows})
}

// lookupDisplayName resolves a UserID (username) to the human-friendly
// Name field on the user record, falling back to the username itself
// when the user isn't in the store (e.g. the env-seeded admin, or a
// freshly-assigned username that hasn't redeemed an invite yet).
// Empty string is returned when the store is unavailable in test
// fixtures — the FE treats empty DisplayName as "use the UserID".
func (s *Server) lookupDisplayName(userID string) string {
	if s.store == nil {
		return ""
	}
	u, err := s.store.UserByUsername(userID)
	if err != nil {
		return ""
	}
	return u.Name
}
