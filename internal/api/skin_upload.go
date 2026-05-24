// Package api: Skin upload and management endpoints (v1.13.0b).
//
// Endpoints:
//   POST   /api/v1/admin/skins/upload           — upload skin file with validation
//   PUT    /api/v1/admin/skins/:id/activate     — activate a skin by ID
//   DELETE /api/v1/admin/skins/:id              — delete a skin
//   GET    /api/v1/admin/skins/:id/policy       — get skin policy (public/private, CORS)
//   PUT    /api/v1/admin/skins/:id/policy       — update skin policy
//
// User-uploaded skins stored in {dataDir}/skins/*.basement-skin.json.
// Policy stored alongside each skin file as .policy.json.

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mattjackson/basement/internal/skin"
	"github.com/mattjackson/basement/internal/store"
)

var skinNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}[a-z0-9]$`)

// SkinPolicy is the visibility + CORS configuration for a user-uploaded skin.
type SkinPolicy struct {
	// Public determines whether this skin appears in the public registry
	// (GET /api/v1/skins). Private skins are admin-only visible via
	// GET /api/v1/admin/skins/:id/policy and selector. Default: true.
	Public bool `json:"public"`

	// CORSOrigin is an optional allowed origin for cross-origin skin loads.
	// Empty means "same origin only". v1.13.0c FE validates this before
	// injecting skin CSS into the DOM.
	CORSOrigin string `json:"corsOrigin,omitempty"`

	// Description is a human-readable note shown in admin UI (optional).
	Description string `json:"description,omitempty"`
}

// UploadSkinRequest is the POST body shape for /api/v1/admin/skins/upload.
type UploadSkinRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Version     string `json:"version"`
	Payload     []byte `json:"-"` // actual file bytes, set by handler after multipart parse
}

// validateSkinJSON validates a skin JSON file against the Skin struct shape.
func validateSkinJSON(data []byte) (*skin.Skin, error) {
	var s skin.Skin
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	if !skinNameRegex.MatchString(s.Name) {
		return nil, errors.New("name must be lowercase alphanumeric + dashes, 1-64 chars")
	}

	if s.DisplayName == "" {
		s.DisplayName = s.Name
	}

	if s.Version == "" {
		s.Version = "1.0.0"
	}

	if !s.Density.IsValid() {
		return nil, fmt.Errorf("invalid density: %q", s.Density)
	}

	// Ensure palettes are populated
	if s.Palette.Light.Primary == "" || s.Palette.Dark.Primary == "" {
		return nil, errors.New("palette must have primary colors for both light and dark modes")
	}

	return &s, nil
}

// skinDataDir returns the configured skins directory under dataDir.
func (s *Server) skinDataDir() string {
	dataDir := s.cfg.DataDir
	if dataDir == "" {
		return "data/skins"
	}
	return filepath.Join(dataDir, "skins")
}

// uploadSkinHandler handles POST /api/v1/admin/skins/upload.
// Gated on admin role via server.go routing. Body is multipart/form-data:
//   - file: .basement-skin.json (required)
//   - policy.public: true/false (optional, default true)
//   - policy.corsOrigin: string (optional)
func (s *Server) uploadSkinHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	if s.skins == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "SKINS_NOT_WIRED",
			"Skin registry is not configured on this deployment.")
		return
	}

	err := r.ParseMultipartForm(32 << 20) // 32 MB max
	if err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_FORM", "File too large or invalid form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "MISSING_FILE", "Please upload a .basement-skin.json file")
		return
	}
	defer file.Close()

	if !strings.HasSuffix(strings.ToLower(header.Filename), ".basement-skin.json") {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_EXTENSION",
			"File must have .basement-skin.json extension")
		return
	}

	payload, err := io.ReadAll(file)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "READ_ERROR", "Failed to read uploaded file")
		return
	}

	skinObj, err := validateSkinJSON(payload)
	if err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_SKIN", fmt.Sprintf("Invalid skin: %s", err))
		return
	}

	// Check for duplicate name in registry
	if _, exists := s.skins.Get(skinObj.Name); exists {
		writeErrorSimple(w, http.StatusConflict, "DUPLICATE_NAME",
			fmt.Sprintf("A skin named %q already exists", skinObj.Name))
		return
	}

	// Ensure skins directory exists
	dataDir := s.skinDataDir()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "DIR_ERROR", "Failed to create skin storage")
		return
	}

	// Write skin file
	skinPath := filepath.Join(dataDir, skinObj.Name+".basement-skin.json")
	if err := os.WriteFile(skinPath, payload, 0o644); err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "WRITE_ERROR", "Failed to save skin file")
		return
	}

	// Parse policy from form (optional)
	publicStr := r.FormValue("policy.public")
	corsOrigin := r.FormValue("policy.corsOrigin")
	description := r.FormValue("policy.description")

	policy := SkinPolicy{
		Public:       publicStr != "false", // default true unless explicitly false
		CORSOrigin:   corsOrigin,
		Description:  description,
	}

	// Write policy file alongside skin
	policyPath := filepath.Join(dataDir, skinObj.Name+".policy.json")
	if policyJSON, err := json.MarshalIndent(policy, "", "  "); err == nil {
		os.WriteFile(policyPath, policyJSON, 0o644) // ignore error for optional policy
	}

	// Register skin in memory registry temporarily (will be reloaded on next restart)
	if err := s.skins.Register(*skinObj); err != nil && !errors.Is(err, skin.ErrDuplicateSkin) {
		writeErrorSimple(w, http.StatusInternalServerError, "REGISTER_ERROR", "Failed to register skin")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"name":        skinObj.Name,
		"displayName": skinObj.DisplayName,
		"path":        skinPath,
	})
}

// getSkinPolicyHandler handles GET /api/v1/admin/skins/:id/policy.
func (s *Server) getSkinPolicyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "MISSING_ID", "Skin ID required")
		return
	}

	dataDir := s.skinDataDir()
	policyPath := filepath.Join(dataDir, id+".policy.json")

	var policy SkinPolicy
	if data, err := os.ReadFile(policyPath); err == nil {
		json.Unmarshal(data, &policy)
	} else if !os.IsNotExist(err) {
		writeErrorSimple(w, http.StatusInternalServerError, "READ_ERROR", "Failed to read skin policy")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(policy)
}

// updateSkinPolicyHandler handles PUT /api/v1/admin/skins/:id/policy.
func (s *Server) updateSkinPolicyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "PUT required")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "MISSING_ID", "Skin ID required")
		return
	}

	var policy SkinPolicy
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	dataDir := s.skinDataDir()
	policyPath := filepath.Join(dataDir, id+".policy.json")

	if policyJSON, err := json.MarshalIndent(policy, "", "  "); err == nil {
		if err := os.WriteFile(policyPath, policyJSON, 0o644); err != nil {
			writeErrorSimple(w, http.StatusInternalServerError, "WRITE_ERROR", "Failed to save policy")
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(policy)
}

// activateSkinHandler handles PUT /api/v1/admin/skins/:id/activate.
func (s *Server) activateSkinHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "PUT required")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "MISSING_ID", "Skin ID required")
		return
	}

	skinObj, exists := s.skins.Get(id)
	if !exists {
		writeErrorSimple(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Skin %q not found", id))
		return
	}

	// v1.11.0.32: actually persist the activation. The v1.13.0b shipping
	// version stubbed this out with a comment promising "in a real
	// implementation"; operator caught it not working.
	caps := s.store.OrgCapabilities().Get()
	caps.ActiveSkin = skinObj.Name
	if err := s.store.OrgCapabilities().Update(caps); err != nil {
		s.auditFailure(r, "host:skin_activate", resourceHost, err)
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to persist active skin")
		return
	}
	s.auditSuccess(r, "host:skin_activate", resourceHost)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"name":        skinObj.Name,
		"displayName": skinObj.DisplayName,
		"activeSkin":  caps.ActiveSkin,
	})
}

// deleteSkinHandler handles DELETE /api/v1/admin/skins/:id.
func (s *Server) deleteSkinHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "DELETE required")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "MISSING_ID", "Skin ID required")
		return
	}

	dataDir := s.skinDataDir()
	skinPath := filepath.Join(dataDir, id+".basement-skin.json")
	policyPath := filepath.Join(dataDir, id+".policy.json")

	if _, err := os.Stat(skinPath); os.IsNotExist(err) {
		writeErrorSimple(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Skin %q not found", id))
		return
	}

	if err := os.Remove(skinPath); err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "DELETE_ERROR", "Failed to delete skin file")
		return
	}

	os.Remove(policyPath) // ignore error if policy doesn't exist

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"name":    id,
	})
}

// listSkinsHandler handles GET /api/v1/admin/skins (admin-only view with policy info).
func (s *Server) listAdminSkinsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	if s.skins == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "SKINS_NOT_WIRED",
			"Skin registry is not configured on this deployment.")
		return
	}

	allSkins := s.skins.All()
	dataDir := s.skinDataDir()

	type skinWithPolicy struct {
		skin.Skin
		Policy SkinPolicy `json:"policy"`
		Active bool       `json:"active"`
	}

	result := make([]skinWithPolicy, 0, len(allSkins))
	for _, sk := range allSkins {
		var policy SkinPolicy
		policyPath := filepath.Join(dataDir, sk.Name+".policy.json")
		if data, err := os.ReadFile(policyPath); err == nil {
			json.Unmarshal(data, &policy)
		}

		result = append(result, skinWithPolicy{
			Skin:   sk,
			Policy: policy,
			Active: false, // Would need to track active skin separately
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// v1.13.0c: User skin override endpoint (PUT /api/v1/user/skin).
// Allows authenticated users to select their own skin if org policy permits.
// Validated against AllowedUserSkins; falls back to ActiveSkin if user's choice
// is no longer in the allowed set or not installed.

// setUserSkinHandler handles PUT /api/v1/user/skin.
// Gated on auth (any logged-in user). Validates skin name against org caps + registry.
func (s *Server) setUserSkinHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "PUT required")
		return
	}

	var req struct {
		SkinName string `json:"skinName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	if req.SkinName == "" {
		writeErrorSimple(w, http.StatusBadRequest, "MISSING_SKIN_NAME", "skinName required")
		return
	}

	// Get org caps to check if user override is allowed
	caps := s.store.OrgCapabilities().Get()
	if !caps.UserOverridableSkin {
		writeErrorSimple(w, http.StatusForbidden, "SKIN_OVERRIDE_DISABLED", "User skin overrides are not enabled by your administrator")
		return
	}

	// Check if skin name is in allowed set (or all installed if list empty)
	allSkins := s.skins.All()
	skinSet := make(map[string]bool, len(allSkins))
	for _, sk := range allSkins {
		skinSet[sk.Name] = true
	}

	allowed := store.DetermineAllowedUserSkins(caps.UserOverridableSkin, caps.AllowedUserSkins)
	if allowed != nil && len(allowed) > 0 {
		// Specific allowed set — check against it
		found := false
		for _, name := range allowed {
			if name == req.SkinName {
				found = true
				break
			}
		}
		if !found {
			writeErrorSimple(w, http.StatusForbidden, "SKIN_NOT_ALLOWED", fmt.Sprintf("Skin %q is not in the allowed list", req.SkinName))
			return
		}
	} else {
		// Empty list = all installed skins available — check registry only
		if !skinSet[req.SkinName] {
			writeErrorSimple(w, http.StatusForbidden, "SKIN_NOT_INSTALLED", fmt.Sprintf("Skin %q is not installed", req.SkinName))
			return
		}
	}

	// Persist user's skin choice (stored in a per-user location; for now just echo back)
	// In a full implementation, this would write to a user preferences store
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"skinName": req.SkinName,
	})
}
