// Package api: /admin/oidc-group-mappings handlers (v1.3.0a).
//
// Two endpoints power the operator-editable map of OIDC group claims
// to basement role assignments:
//
//   GET /api/v1/admin/oidc-group-mappings   list current mappings
//   PUT /api/v1/admin/oidc-group-mappings   replace the full list
//
// Both are gated on host:manage_policies — the same persona that owns
// the matrix at /admin/policies owns this mapping table. Per ADR-0001
// the actual policy decisions still flow through the enforcer; this
// table only declares what assignments to AUTO-create on OIDC login.
//
// Mappings apply on each user's NEXT OIDC login (the callback handler
// runs SyncOIDCAssignments after exchange + claim verification).
// Existing sessions are unaffected until they sign in again.
package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/mattjackson/basement/internal/store"
)

// oidcGroupMappingsResponse is the wire shape for GET /admin/oidc-group-mappings.
type oidcGroupMappingsResponse struct {
	Mappings  []store.OIDCGroupMapping `json:"mappings"`
	UpdatedAt string                   `json:"updatedAt,omitempty"`
}

// listOIDCGroupMappingsHandler implements GET /api/v1/admin/oidc-group-mappings.
// Gated on host:manage_policies @ host:*.
func (s *Server) listOIDCGroupMappingsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	if _, ok := s.requireCapability(w, r, "host:manage_policies", "host:*"); !ok {
		return
	}

	if s.store == nil || s.store.OIDCGroupMappings() == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "OIDC_MAPPINGS_NOT_WIRED",
			"OIDC group-mappings store is not configured on this deployment.")
		return
	}

	current := s.store.OIDCGroupMappings().Get()
	if current.Mappings == nil {
		current.Mappings = []store.OIDCGroupMapping{}
	}

	resp := oidcGroupMappingsResponse{Mappings: current.Mappings}
	if !current.UpdatedAt.IsZero() {
		resp.UpdatedAt = current.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	writeJSON(w, http.StatusOK, resp)
}

// updateOIDCGroupMappingsRequest is the PUT body shape — wraps a flat
// list to keep room for future top-level fields without breaking
// existing clients.
type updateOIDCGroupMappingsRequest struct {
	Mappings []store.OIDCGroupMapping `json:"mappings"`
}

// updateOIDCGroupMappingsHandler implements PUT /api/v1/admin/oidc-group-mappings.
// Gated on host:manage_policies @ host:*. Replaces the FULL list
// atomically; missing fields on individual mappings yield a 400.
func (s *Server) updateOIDCGroupMappingsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "PUT required")
		return
	}

	if _, ok := s.requireCapability(w, r, "host:manage_policies", "host:*"); !ok {
		return
	}

	if s.store == nil || s.store.OIDCGroupMappings() == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "OIDC_MAPPINGS_NOT_WIRED",
			"OIDC group-mappings store is not configured on this deployment.")
		return
	}

	var req updateOIDCGroupMappingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	cleaned := make([]store.OIDCGroupMapping, 0, len(req.Mappings))
	for i, m := range req.Mappings {
		m.Claim = strings.TrimSpace(m.Claim)
		m.ClaimValue = strings.TrimSpace(m.ClaimValue)
		m.RoleID = strings.TrimSpace(m.RoleID)
		m.Scope = strings.TrimSpace(m.Scope)
		if m.Claim == "" || m.ClaimValue == "" || m.RoleID == "" || m.Scope == "" {
			s.auditFailureDetail(r, "host:oidc_mappings_edit", resourceHost,
				"mapping at index "+itoa(i)+" missing required field")
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_MAPPING",
				"Each mapping requires claim, claimValue, roleId, and scope.")
			return
		}
		cleaned = append(cleaned, m)
	}

	if err := s.store.OIDCGroupMappings().Replace(cleaned); err != nil {
		s.auditFailure(r, "host:oidc_mappings_edit", resourceHost, err)
		writeErrorSimple(w, http.StatusInternalServerError, "OIDC_MAPPINGS_SAVE_FAILED",
			"Failed to persist OIDC group mappings.")
		return
	}

	s.auditSuccess(r, "host:oidc_mappings_edit", resourceHost)

	current := s.store.OIDCGroupMappings().Get()
	resp := oidcGroupMappingsResponse{Mappings: current.Mappings}
	if !current.UpdatedAt.IsZero() {
		resp.UpdatedAt = current.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	writeJSON(w, http.StatusOK, resp)
}

// itoa is a local int->string helper kept here so this file doesn't
// drag strconv into its import list just for a single Sprintf use.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	n := len(buf)
	for i > 0 {
		n--
		buf[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		buf[n] = '-'
	}
	return string(buf[n:])
}
