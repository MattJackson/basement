package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/driver"
)

const opDeleteKey = "delete:key"

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

// createKeyHandler handles POST /admin/clusters/{cid}/keys.
//
// Per ADR-0001 v0.9.0f: gated on key:create at "key:{cid}:*".
//
// v0.9.0m: routes through the connection-scoped Registry so multi-cluster
// operators hitting /admin/clusters/cluster-B/keys mint into cluster B,
// not the default. Falls back to s.drv when no cid (legacy) or when the
// registry is unwired (older tests that pass nil for reg).
//
// IMPORTANT: the driver's CreateKey response includes SecretAccessKey
// — Garage returns it exactly once at creation and never again. The
// handler is a pass-through: it does NOT log or persist the secret;
// it lives in the response body and that's it. basement's DB has
// no field for it. Drop a copy in the response, render once in the UI
// (shown-once dialog), and the operator owns its lifecycle from there.
func (s *Server) createKeyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	cid := chi.URLParam(r, "cid")
	if cid != "" {
		if _, ok := s.requireCapability(w, r, "key:create", "key:"+cid+":*"); !ok {
			return
		}
	}

	// Pick the per-cluster driver when possible; fall back to s.drv
	// when no registry / no cid. Mirrors listKeysByClusterHandler so
	// the create + list pair stay consistent for any given cid.
	drv := s.drv
	if cid != "" && s.reg != nil {
		d, err := s.reg.For(r.Context(), cid)
		if err != nil {
			writeRegistryForError(w, err)
			return
		}
		drv = d
	}

	var spec driver.KeySpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", "invalid request body", nil)
		return
	}

	// Garage allows empty / duplicate names at the storage layer (each
	// key has its own GK... id), but operator-facing UX needs both.
	if ve := validateName("name", spec.Name, nil, ""); ve != nil {
		writeValidationError(w, ve)
		return
	}
	if existing, listErr := drv.ListKeys(r.Context()); listErr == nil {
		if ve := requireUniqueName("name", spec.Name, existing, func(k driver.Key) []string {
			return []string{k.Name}
		}); ve != nil {
			writeValidationError(w, ve)
			return
		}
	}

	key, err := drv.CreateKey(r.Context(), spec)
	if err != nil {
		writeDriverError(w, "CreateKey", err)
		return
	}

	// v0.9.0m: response carries secretAccessKey verbatim when the
	// driver populated it (Garage v1/v2 both do on /v1/key resp.
	// /v2/CreateKey). Never log it server-side — pass through only.
	writeJSON(w, http.StatusCreated, key)
}

// updateKeyHandler handles PATCH /admin/clusters/{cid}/keys/{id}.
// Supports updating bucketsPermissions (required) and name (returns 501 if only name is set).
//
// Per ADR-0001 v0.9.0f: gated on key:edit_permissions at "key:{cid}:{id}".
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

	cid := chi.URLParam(r, "cid")
	if cid != "" {
		if _, ok := s.requireCapability(w, r, "key:edit_permissions", "key:"+cid+":"+id); !ok {
			return
		}
	}

	var body struct {
		Name               *string                   `json:"name,omitempty"`
		BucketsPermissions *[]driver.BucketPermission `json:"bucketsPermissions,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", "invalid request body", nil)
		return
	}

	// Handle permissions update first (if provided)
	if body.BucketsPermissions != nil {
		if err := s.drv.UpdateKeyPermissions(r.Context(), id, *body.BucketsPermissions); err != nil {
			writeDriverError(w, "UpdateKeyPermissions", err)
			return
		}
	} else if body.Name != nil {
		// OPEN: Rename not supported by driver interface yet.
		// Per task T2.38b, when only name is set (no permissions), return 501 Not Implemented.
		// The rename functionality will be added in a future prompt via UpdateKey to the driver interface.
		writeError(w, http.StatusNotImplemented, "RENAME_NOT_SUPPORTED",
			"Renaming keys is not yet supported. This feature will be available in a future update.", nil)
		return
	}

	key, err := s.drv.GetKey(r.Context(), id)
	if err != nil {
		writeDriverError(w, "GetKey", err)
		return
	}

	writeJSON(w, http.StatusOK, key)
}

// armDeleteKeyHandler handles POST /admin/clusters/{cid}/keys/{id}/_arm-delete.
// Issues a short-lived HMAC token bound to {keyID, requester} that
// the matching DELETE must present via X-Confirm-Delete. Two-phase
// arm/fire pattern — no single curl can destroy a key.
//
// Per ADR-0001 v0.9.0f: gated on key:delete at "key:{cid}:{id}".
func (s *Server) armDeleteKeyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "key id required")
		return
	}

	cid := chi.URLParam(r, "cid")
	if cid != "" {
		if _, ok := s.requireCapability(w, r, "key:delete", "key:"+cid+":"+id); !ok {
			return
		}
	}

	// Confirm the key exists before issuing a token. Avoids handing
	// out tokens for nonexistent IDs and surfaces 404 cleanly.
	if _, err := s.drv.GetKey(r.Context(), id); err != nil {
		writeDriverError(w, "GetKey", err)
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "SESSION_REQUIRED", "Session required")
		return
	}

	token := auth.MintConfirmToken(s.cfg.JWT.Secret, opDeleteKey, id, claims.UserID, confirmDeleteTTL)
	writeJSON(w, http.StatusOK, map[string]any{
		"token":            token,
		"expiresInSeconds": int(confirmDeleteTTL.Seconds()),
	})
}

// deleteKeyHandler handles DELETE /admin/clusters/{cid}/keys/{id}.
//
// Requires X-Confirm-Delete header carrying a token previously minted
// by POST /admin/clusters/{cid}/keys/{id}/_arm-delete. Token is
// HMAC-bound to the (key id, user) pair and expires in 60s, so
// curl-by-hand is two-step and a single leaked URL/path cannot destroy.
//
// Per ADR-0001 v0.9.0f: gated on key:delete at "key:{cid}:{id}".
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

	cid := chi.URLParam(r, "cid")
	if cid != "" {
		if _, ok := s.requireCapability(w, r, "key:delete", "key:"+cid+":"+id); !ok {
			return
		}
	}

	confirm := r.Header.Get("X-Confirm-Delete")
	if confirm == "" {
		writeErrorSimple(w, http.StatusBadRequest, "CONFIRMATION_REQUIRED",
			"X-Confirm-Delete header required. POST /admin/keys/{id}/_arm-delete first to obtain a token.")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "SESSION_REQUIRED", "Session required")
		return
	}

	if err := auth.VerifyConfirmToken(s.cfg.JWT.Secret, confirm, opDeleteKey, id, claims.UserID); err != nil {
		switch {
		case errors.Is(err, auth.ErrConfirmMismatch):
			writeErrorSimple(w, http.StatusBadRequest, "CONFIRMATION_MISMATCH",
				"Token does not match this key or user. Re-arm with POST /admin/keys/{id}/_arm-delete.")
		default:
			writeErrorSimple(w, http.StatusBadRequest, "CONFIRMATION_INVALID",
				"Token invalid or expired. Re-arm with POST /admin/keys/{id}/_arm-delete.")
		}
		return
	}

	if err := s.drv.DeleteKey(r.Context(), id); err != nil {
		writeDriverError(w, "DeleteKey", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Access key deleted"})
}
