// Package api: /admin/policies matrix editor handlers (ADR-0001
// cycle v0.9.0g).
//
// The four endpoints in this file power the matrix editor UI:
//
//   GET    /api/v1/admin/policies                  view matrix
//   POST   /api/v1/admin/policies/roles            upsert role
//   DELETE /api/v1/admin/policies/roles/{id}       delete role
//   POST   /api/v1/admin/policies/assignments      assign role
//   DELETE /api/v1/admin/policies/assignments      unassign role
//
// Each is gated by a `policy:*` capability so a Host Admin who has
// been granted `policy:view_matrix` but not `policy:edit_matrix` can
// browse the matrix without being able to change it. Per ADR-0001
// the matrix itself is just JSON in policies.json; the handlers are
// thin shims around the Enforcer interface.
package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mattjackson/basement/internal/auth/policy"
)

// capabilityDTO is the wire shape for a single capability row. ID +
// human description suffice for the UI's reference table.
type capabilityDTO struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// policiesResponse is the shape returned by GET /admin/policies. The
// UI uses `capabilities` to render the reference pane, `roles` for
// the role editor cards, and `assignments` for the assignments table.
type policiesResponse struct {
	Capabilities []capabilityDTO         `json:"capabilities"`
	Roles        []policy.Role           `json:"roles"`
	Assignments  []policy.RoleAssignment `json:"assignments"`
}

// listPoliciesHandler implements GET /api/v1/admin/policies.
// Gated on policy:view_matrix @ host:*.
func (s *Server) listPoliciesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
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

	caps := make([]capabilityDTO, 0, len(policy.Registry))
	for id, desc := range policy.Registry {
		caps = append(caps, capabilityDTO{ID: id, Description: desc})
	}
	// Stable sort by ID so the UI doesn't reorder each render.
	sort.Slice(caps, func(i, j int) bool { return caps[i].ID < caps[j].ID })

	roles := s.policy.Roles()
	if roles == nil {
		roles = []policy.Role{}
	}
	assignments := s.policy.Assignments()
	if assignments == nil {
		assignments = []policy.RoleAssignment{}
	}

	writeJSON(w, http.StatusOK, policiesResponse{
		Capabilities: caps,
		Roles:        roles,
		Assignments:  assignments,
	})
}

// upsertRoleHandler implements POST /api/v1/admin/policies/roles.
// Gated on policy:edit_matrix @ host:*.
//
// Body shape mirrors policy.Role. The Seed flag in the body is
// ignored — the enforcer preserves it on existing rows and refuses
// to promote new rows to seed.
func (s *Server) upsertRoleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	if _, ok := s.requireCapability(w, r, "policy:edit_matrix", "host:*"); !ok {
		return
	}

	if s.policy == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "POLICY_NOT_WIRED",
			"Policy subsystem is not configured on this deployment.")
		return
	}

	var role policy.Role
	if err := json.NewDecoder(r.Body).Decode(&role); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	role.ID = strings.TrimSpace(role.ID)
	role.Label = strings.TrimSpace(role.Label)
	if role.ID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Role id is required")
		return
	}
	if role.Capabilities == nil {
		role.Capabilities = []string{}
	}

	if err := s.policy.UpsertRole(role); err != nil {
		s.auditFailure(r, "policy:role_upsert", resourceRole(role.ID), err)
		// UpsertRole returns descriptive errors for unknown capabilities
		// and malformed expressions — surface them rather than swallow.
		writeErrorSimple(w, http.StatusBadRequest, "ROLE_INVALID", err.Error())
		return
	}

	s.auditSuccess(r, "policy:role_upsert", resourceRole(role.ID))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(role)
}

// deleteRoleHandler implements DELETE /api/v1/admin/policies/roles/{id}.
// Gated on policy:edit_matrix @ host:*. Refuses seed roles with 409.
func (s *Server) deleteRoleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "DELETE required")
		return
	}

	if _, ok := s.requireCapability(w, r, "policy:edit_matrix", "host:*"); !ok {
		return
	}

	if s.policy == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "POLICY_NOT_WIRED",
			"Policy subsystem is not configured on this deployment.")
		return
	}

	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Role id is required")
		return
	}

	if err := s.policy.DeleteRole(id); err != nil {
		s.auditFailure(r, "policy:role_delete", resourceRole(id), err)
		msg := err.Error()
		switch {
		case strings.Contains(msg, "seed role"):
			writeErrorSimple(w, http.StatusConflict, "ROLE_SEED",
				"Seed roles cannot be deleted (only edited).")
		case strings.Contains(msg, "not found"):
			writeErrorSimple(w, http.StatusNotFound, "ROLE_NOT_FOUND", msg)
		default:
			writeErrorSimple(w, http.StatusInternalServerError, "ROLE_DELETE_FAILED", msg)
		}
		return
	}

	s.auditSuccess(r, "policy:role_delete", resourceRole(id))
	w.WriteHeader(http.StatusNoContent)
}

// assignRoleHandler implements POST /api/v1/admin/policies/assignments.
// Gated on policy:assign_role @ host:*. Body is policy.RoleAssignment.
func (s *Server) assignRoleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	if _, ok := s.requireCapability(w, r, "policy:assign_role", "host:*"); !ok {
		return
	}

	if s.policy == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "POLICY_NOT_WIRED",
			"Policy subsystem is not configured on this deployment.")
		return
	}

	var a policy.RoleAssignment
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	a.UserID = strings.TrimSpace(a.UserID)
	a.RoleID = strings.TrimSpace(a.RoleID)
	a.Scope = strings.TrimSpace(a.Scope)
	if a.UserID == "" || a.RoleID == "" || a.Scope == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			"userId, roleId and scope are all required")
		return
	}

	if err := s.policy.AssignRole(a); err != nil {
		s.auditFailure(r, "policy:assign", resourceAssignment(a.UserID, a.RoleID, a.Scope), err)
		msg := err.Error()
		if strings.Contains(msg, "does not exist") {
			writeErrorSimple(w, http.StatusBadRequest, "ROLE_NOT_FOUND", msg)
			return
		}
		writeErrorSimple(w, http.StatusInternalServerError, "ASSIGN_FAILED", msg)
		return
	}

	s.auditSuccess(r, "policy:assign", resourceAssignment(a.UserID, a.RoleID, a.Scope))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(a)
}

// unassignRoleHandler implements DELETE /api/v1/admin/policies/assignments.
// Gated on policy:assign_role @ host:*. Query params: userId, roleId, scope.
//
// Query params (vs body for DELETE) so the same composite key is in
// the URL — easier to log + reproduce + cache-bust.
func (s *Server) unassignRoleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "DELETE required")
		return
	}

	if _, ok := s.requireCapability(w, r, "policy:assign_role", "host:*"); !ok {
		return
	}

	if s.policy == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "POLICY_NOT_WIRED",
			"Policy subsystem is not configured on this deployment.")
		return
	}

	userID := strings.TrimSpace(r.URL.Query().Get("userId"))
	roleID := strings.TrimSpace(r.URL.Query().Get("roleId"))
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if userID == "" || roleID == "" || scope == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			"userId, roleId and scope query params are all required")
		return
	}

	if err := s.policy.UnassignRole(userID, roleID, scope); err != nil {
		s.auditFailure(r, "policy:unassign", resourceAssignment(userID, roleID, scope), err)
		writeErrorSimple(w, http.StatusInternalServerError, "UNASSIGN_FAILED", err.Error())
		return
	}

	s.auditSuccess(r, "policy:unassign", resourceAssignment(userID, roleID, scope))
	w.WriteHeader(http.StatusNoContent)
}
