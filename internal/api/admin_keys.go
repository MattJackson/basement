package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mattjackson/basement/internal/driver"
)

// listKeysHandler handles GET /api/v1/admin/keys.
// Calls driver.ListKeys and returns JSON []Key per OpenAPI schema.
func (s *Server) listKeysHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	keys, err := s.drv.ListKeys(r.Context())
	if err != nil {
		writeDriverError(w, "ListKeys", err)
		return
	}

	if keys == nil {
		keys = []driver.Key{}
	}

	writeJSON(w, http.StatusOK, keys)
}

// getKeyHandler handles GET /admin/keys/{id}.
func (s *Server) getKeyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "key id required")
		return
	}

	key, err := s.drv.GetKey(r.Context(), id)
	if err != nil {
		writeDriverError(w, "GetKey", err)
		return
	}

	writeJSON(w, http.StatusOK, key)
}

// createKeyHandler handles POST /admin/keys.
func (s *Server) createKeyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	var spec driver.KeySpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", "invalid request body", nil)
		return
	}

	key, err := s.drv.CreateKey(r.Context(), spec)
	if err != nil {
		writeDriverError(w, "CreateKey", err)
		return
	}

	writeJSON(w, http.StatusCreated, key)
}

// updateKeyHandler handles PATCH /admin/keys/{id}.
func (s *Server) updateKeyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "PATCH required")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "key id required")
		return
	}

	var perms []driver.BucketPermission
	if err := json.NewDecoder(r.Body).Decode(&perms); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", "invalid request body", nil)
		return
	}

	if err := s.drv.UpdateKeyPermissions(r.Context(), id, perms); err != nil {
		writeDriverError(w, "UpdateKeyPermissions", err)
		return
	}

	key, err := s.drv.GetKey(r.Context(), id)
	if err != nil {
		writeDriverError(w, "GetKey", err)
		return
	}

	writeJSON(w, http.StatusOK, key)
}

// deleteKeyHandler handles DELETE /admin/keys/{id}.
func (s *Server) deleteKeyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "DELETE required")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "key id required")
		return
	}

	if err := s.drv.DeleteKey(r.Context(), id); err != nil {
		writeDriverError(w, "DeleteKey", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Access key deleted"})
}
