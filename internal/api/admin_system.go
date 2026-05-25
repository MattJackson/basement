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

// orgCapabilitiesPatch mirrors store.OrgCapabilities but with every
// field as a pointer so we can tell "the client omitted this" apart
// from "the client sent the zero value". HTTP PATCH semantics: only
// fields present in the request body are applied to the existing
// record; unsent fields are preserved.
//
// v1.13.39: introduced after the operator hit silent-data-loss when
// the Skins card saved {activeSkin, userOverridableSkin,
// allowedUserSkins} — the old decode-into-full-struct path zeroed
// SignupMode, AllowUserBackends, OIDCOnly, AdminSessionTTLSec, and
// Gateways on every Skins-card save. Operators noticed settings
// silently reverting after touching an unrelated card.
type orgCapabilitiesPatch struct {
	SignupMode          *string                `json:"signupMode,omitempty"`
	EnabledDrivers      *[]string              `json:"enabledDrivers,omitempty"`
	AllowUserBackends   *bool                  `json:"allowUserBackends,omitempty"`
	UserBackendDrivers  *[]string              `json:"userBackendDrivers,omitempty"`
	OIDCOnly            *bool                  `json:"oidcOnly,omitempty"`
	AdminSessionTTLSec  *int                   `json:"adminSessionTtlSec,omitempty"`
	Gateways            *store.GatewaySettings `json:"gateways,omitempty"`
	OnboardingCompleted *bool                  `json:"onboardingCompleted,omitempty"`
	ActiveSkin          *string                `json:"activeSkin,omitempty"`
	UserOverridableSkin *bool                  `json:"userOverridableSkin,omitempty"`
	AllowedUserSkins    *[]string              `json:"allowedUserSkins,omitempty"`
}

// updateOrgCapabilitiesHandler handles PATCH /api/v1/admin/system.
// Updates OrgCapabilities for UI Admin only. Atomic write.
//
// Per ADR-0001 v0.9.0f: gated on host:manage_org_caps at "host:*".
// v1.13.1: validates AllowedUserSkins against installed skins; rejects 400 if any
// name doesn't match an installed skin (unless list is empty = all).
// v1.13.39: true PATCH semantics — merges sent fields onto the current
// record so saving one card doesn't wipe other cards' values.
func (s *Server) updateOrgCapabilitiesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "PATCH required")
		return
	}

	if _, ok := s.requireCapability(w, r, "host:manage_org_caps", "host:*"); !ok {
		return
	}

	var patch orgCapabilitiesPatch
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	// Start from the current on-disk record so every field the client
	// didn't send keeps its existing value.
	caps := s.store.OrgCapabilities().Get()

	if patch.SignupMode != nil {
		caps.SignupMode = *patch.SignupMode
	}
	if patch.EnabledDrivers != nil {
		caps.EnabledDrivers = *patch.EnabledDrivers
	}
	if patch.AllowUserBackends != nil {
		caps.AllowUserBackends = *patch.AllowUserBackends
	}
	if patch.UserBackendDrivers != nil {
		caps.UserBackendDrivers = *patch.UserBackendDrivers
	}
	if patch.OIDCOnly != nil {
		caps.OIDCOnly = *patch.OIDCOnly
	}
	if patch.AdminSessionTTLSec != nil {
		caps.AdminSessionTTLSec = *patch.AdminSessionTTLSec
	}
	if patch.Gateways != nil {
		caps.Gateways = *patch.Gateways
	}
	if patch.OnboardingCompleted != nil {
		caps.OnboardingCompleted = *patch.OnboardingCompleted
	}
	if patch.ActiveSkin != nil {
		caps.ActiveSkin = *patch.ActiveSkin
	}
	if patch.UserOverridableSkin != nil {
		caps.UserOverridableSkin = *patch.UserOverridableSkin
	}
	if patch.AllowedUserSkins != nil {
		caps.AllowedUserSkins = *patch.AllowedUserSkins
	}

	// v1.13.1: validate AllowedUserSkins against installed skins —
	// run after the merge so the check uses the effective post-merge
	// value (the client may have left the field unset; in that case
	// we re-validate the existing on-disk list, which is a cheap
	// extra guard against drift).
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
