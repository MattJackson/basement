package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mattjackson/basement/internal/store"
)

// getOrgCapabilitiesHandler handles GET /api/v1/admin/system.
// Returns OrgCapabilities for UI Admin only.
func (s *Server) getOrgCapabilitiesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	caps := s.store.OrgCapabilities().Get()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(caps)
}

// updateOrgCapabilitiesHandler handles PATCH /api/v1/admin/system.
	// Updates OrgCapabilities for UI Admin only. Atomic write.
	//
	// Per ADR-0001 v0.9.0f: gated on host:manage_org_caps at "host:*".
	// v1.13.1: validates AllowedUserSkins against installed skins; rejects 400 if any
	// name doesn't match an installed skin (unless list is empty = all).
	func (s *Server) updateOrgCapabilitiesHandler(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "PATCH required")
			return
		}

		if _, ok := s.requireCapability(w, r, "host:manage_org_caps", "host:*"); !ok {
			return
		}

		var caps store.OrgCapabilities
		if err := json.NewDecoder(r.Body).Decode(&caps); err != nil {
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
			return
		}

		// v1.13.1: validate AllowedUserSkins against installed skins
		if len(caps.AllowedUserSkins) > 0 {
			allSkins := s.skins.All()
			skinSet := make(map[string]bool, len(allSkins))
			for _, sk := range allSkins {
				skinSet[sk.Name] = true
			}
			for _, skinName := range caps.AllowedUserSkins {
				if !skinSet[skinName] {
					writeErrorSimple(w, http.StatusBadRequest, "INVALID_SKIN_NAME",
						fmt.Sprintf("AllowedUserSkins contains unknown skin: %s", skinName))
					return
				}
			}
		}

		if err := s.store.OrgCapabilities().Update(caps); err != nil {
			s.auditFailure(r, "host:org_caps_edit", resourceHost, err)
			writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update capabilities")
			return
		}

		s.auditSuccess(r, "host:org_caps_edit", resourceHost)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(caps)
	}
