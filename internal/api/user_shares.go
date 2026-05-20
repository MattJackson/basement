package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/store"
)

// UserShareCreateRequest represents the request body for creating a share.
type UserShareCreateRequest struct {
	ConnectionID  string `json:"connectionId"`
	BucketID      string `json:"bucketId"`
	Prefix        string `json:"prefix,omitempty"`
	Key           string `json:"key,omitempty"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
	DownloadLimit *int       `json:"downloadLimit,omitempty"`
	Password      string   `json:"password,omitempty"`
}

// userCreateShareHandler handles POST /api/v1/user/shares.
func (s *Server) userCreateShareHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	var req UserShareCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}

	// Validate: at least one of prefix or key must be set, but not both.
	hasPrefix := req.Prefix != ""
	hasKey := req.Key != ""
	if !hasPrefix && !hasKey {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Either prefix or key must be specified")
		return
	}
	if hasPrefix && hasKey {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Cannot specify both prefix and key")
		return
	}

	// Validate connection exists and user has access.
	if _, err := s.conns.Get(r.Context(), req.ConnectionID); err != nil {
		writeRegistryForError(w, err)
		return
	}

	visibleConnIDs := s.userVisibleConnections(r.Context())
	if visibleConnIDs == nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to validate access")
		return
	}

	hasAccess := false
	for _, id := range visibleConnIDs {
		if req.ConnectionID == id || s.userOwnsConnection(r.Context(), req.ConnectionID) {
			hasAccess = true
			break
		}
	}

	if !hasAccess {
		writeErrorSimple(w, http.StatusForbidden, "FORBIDDEN", "User does not have access to this cluster")
		return
	}

	// Validate bucket exists and user has access (skip if no driver registry).
	if s.reg != nil {
		drv, err := s.reg.For(r.Context(), req.ConnectionID)
		if err != nil {
			writeRegistryForError(w, err)
			return
		}

		if _, err := drv.GetBucket(r.Context(), req.BucketID); err != nil {
			writeDriverError(w, "GetBucket", err)
			return
		}
	}

	visibleBucketIDs := s.userVisibleBuckets(r.Context(), req.ConnectionID)
	hasBucketAccess := false
	for _, id := range visibleBucketIDs {
		if req.BucketID == id {
			hasBucketAccess = true
			break
		}
	}

	if !hasBucketAccess && !s.userOwnsConnection(r.Context(), req.ConnectionID) {
		writeErrorSimple(w, http.StatusForbidden, "FORBIDDEN", "User does not have access to this bucket")
		return
	}

	// Validate expiration time if provided.
	if req.ExpiresAt != nil && req.ExpiresAt.Before(time.Now()) {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Expiration time must be in the future")
		return
	}

	// Validate download limit if provided.
	if req.DownloadLimit != nil && *req.DownloadLimit < 1 {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Download limit must be at least 1")
		return
	}

	// Create the share record.
	share := store.Share{
		ConnectionID:  req.ConnectionID,
		BucketID:      req.BucketID,
		Prefix:        req.Prefix,
		Key:           req.Key,
		OwnerUserID:   claims.UserID,
		ExpiresAt:     req.ExpiresAt,
		DownloadLimit: req.DownloadLimit,
		DownloadsUsed: 0,
		Revoked:       false,
	}

	if req.Password != "" {
		passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to hash password")
			return
		}
		share.PasswordHash = string(passwordHash)
	}

	if s.store == nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_NOT_AVAILABLE", "Store not available")
		return
	}

	if err := s.store.CreateShare(share); err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to create share")
		return
	}

	// Return the share with plaintext token (only time it's returned).
	writeJSON(w, http.StatusCreated, share)
}

// userListSharesHandler handles GET /api/v1/user/shares.
func (s *Server) userListSharesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	if s.store == nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_NOT_AVAILABLE", "Store not available")
		return
	}

	shares := s.store.SharesByUser(claims.UserID)

	// Fetch bucket info for display.
	result := make([]map[string]interface{}, len(shares))
	for i, share := range shares {
		bucketInfo := map[string]string{}
		
		// Try to get bucket alias if user has access (skip if no registry).
		if s.reg != nil {
			if _, err := s.conns.Get(r.Context(), share.ConnectionID); err == nil {
				if drv, _ := s.reg.For(r.Context(), share.ConnectionID); drv != nil {
					if bucket, err := drv.GetBucket(r.Context(), share.BucketID); err == nil {
						if len(bucket.Aliases) > 0 {
							bucketInfo["alias"] = bucket.Aliases[0]
						} else {
							bucketInfo["alias"] = share.BucketID
						}
					}
				}
			}
		}

		displayPath := ""
		if share.Key != "" {
			displayPath = share.Key
		} else if share.Prefix != "" {
			displayPath = share.Prefix
		}

		result[i] = map[string]interface{}{
			"token":          share.Token,
			"connectionId":   share.ConnectionID,
			"bucketId":       share.BucketID,
			"bucketAlias":    bucketInfo["alias"],
			"bucketLabel":    bucketInfo["label"],
			"path":           displayPath,
			"isPrefixShare":  share.Prefix != "",
			"createdAt":      share.CreatedAt.UTC().Format(time.RFC3339),
			"expiresAt":      formatTimeOrNull(share.ExpiresAt),
			"downloadLimit":  share.DownloadLimit,
			"downloadsUsed":  share.DownloadsUsed,
			"hasPassword":    share.PasswordHash != "",
			"isRevoked":      share.Revoked,
		}
	}

	writeJSON(w, http.StatusOK, result)
}

// userRevokeShareHandler handles DELETE /api/v1/user/shares/{token}.
func (s *Server) userRevokeShareHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "DELETE required")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	token := chi.URLParam(r, "token")
	if token == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Token required")
		return
	}

	if s.store == nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_NOT_AVAILABLE", "Store not available")
		return
	}

	// Verify ownership before revoking.
	existingShare, err := s.store.Share(token)
	if err != nil {
		if err.Error() == "share not found: "+token || err.Error() == "share revoked: "+token {
			writeErrorSimple(w, http.StatusNotFound, "SHARE_NOT_FOUND", "Share not found")
			return
		}
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to retrieve share")
		return
	}

	if existingShare.OwnerUserID != claims.UserID {
		writeErrorSimple(w, http.StatusForbidden, "FORBIDDEN", "Cannot revoke shares owned by others")
		return
	}

	if err := s.store.RevokeShare(token); err != nil {
		if err.Error() == "share not found: "+token {
			writeErrorSimple(w, http.StatusNotFound, "SHARE_NOT_FOUND", "Share not found")
			return
		}
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to revoke share")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Share link revoked",
	})
}

// hashPassword hashes a plaintext password using bcrypt.
func hashPassword(password string) (string, error) {
	if password == "" {
		return "", nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// formatTimeOrNull formats a time pointer to RFC3339 or returns null.
func formatTimeOrNull(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}
