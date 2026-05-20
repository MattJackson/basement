package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

const (
	shareAuthCookieNamePrefix = "__Host-share_"
	shareAuthCookieTTL        = time.Hour
)

// shareCookieValue returns a deterministic HMAC-SHA256 over the
// (token, passwordHash) pair signed with the server's JWT secret.
// Storing this in the cookie instead of the raw password means even
// XSS / extension access can't recover the password. The HMAC
// auto-invalidates if the share's password is rotated (new hash
// → new HMAC). Constant-time compared on verify.
func (s *Server) shareCookieValue(token, passwordHash string) string {
	h := hmac.New(sha256.New, s.cfg.JWT.Secret)
	h.Write([]byte(token + ":" + passwordHash))
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Server) verifyShareCookie(token, passwordHash, cookieValue string) bool {
	expected := s.shareCookieValue(token, passwordHash)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(cookieValue)) == 1
}

// shareInfoResponse is the response for /api/v1/share/{token}/info.
type shareInfoResponse struct {
	RequiresPassword bool   `json:"requiresPassword"`
	Expired          bool   `json:"expired"`
	Revoked          bool   `json:"revoked"`
	IsDirectory      bool   `json:"isDirectory"`
	DisplayName      string `json:"displayName,omitempty"`
}

// shareAuthRequest is the request body for /api/v1/share/{token}/auth.
type shareAuthRequest struct {
	Password string `json:"password"`
}

// shareListResponse is the response for /api/v1/share/{token}/list.
type shareListResponse struct {
	Objects          []driver.ObjectInfo `json:"objects"`
	NextContinuation string              `json:"nextContinuation,omitempty"`
	IsTruncated      bool                `json:"isTruncated"`
	Prefixes         []string            `json:"prefixes,omitempty"`
}

// shareGetResponse is the response for /api/v1/share/{token}/get.
type shareGetResponse struct {
	URL string `json:"url"`
}

// validateShareBasic checks that a share exists, is not revoked, and is not expired.
func (s *Server) validateShareBasic(token string) (store.Share, error) {
	if s.store == nil {
		return store.Share{}, fmt.Errorf("share not found: %s", token)
	}

	sh, err := s.store.Share(token)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return store.Share{}, fmt.Errorf("share not found: %s", token)
		}
		if strings.Contains(err.Error(), "revoked") {
			return store.Share{}, fmt.Errorf("share revoked: %s", token)
		}
		return store.Share{}, err
	}

	_, err = s.store.IsExpired(token)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return store.Share{}, err
	}

	return sh, nil
}

// shareInfoHandler handles GET /api/v1/share/{token}/info.
func (s *Server) shareInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	token := chi.URLParam(r, "token")
	if token == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Token required")
		return
	}

	sh, err := s.validateShareBasic(token)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErrorSimple(w, http.StatusNotFound, "SHARE_NOT_FOUND", "Share not found")
			return
		}
		if strings.Contains(err.Error(), "revoked") {
			writeErrorSimple(w, http.StatusGone, "SHARE_REVOKED", "Share has been revoked")
			return
		}
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to retrieve share")
		return
	}

	expired, _ := s.store.IsExpired(token)

	isDir := sh.Prefix != "" || (sh.Key == "")

	resp := shareInfoResponse{
		RequiresPassword: sh.PasswordHash != "",
		Expired:          expired,
		Revoked:          sh.Revoked,
		IsDirectory:      isDir,
	}

	if sh.Prefix != "" {
		resp.DisplayName = sh.Prefix
	} else if sh.Key != "" {
		// Extract filename from key for display
		parts := strings.Split(sh.Key, "/")
		if len(parts) > 0 {
			resp.DisplayName = parts[len(parts)-1]
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// shareAuthHandler handles POST /api/v1/share/{token}/auth.
func (s *Server) shareAuthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	token := chi.URLParam(r, "token")
	if token == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Token required")
		return
	}

	sh, err := s.validateShareBasic(token)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErrorSimple(w, http.StatusNotFound, "SHARE_NOT_FOUND", "Share not found")
			return
		}
		if strings.Contains(err.Error(), "revoked") {
			writeErrorSimple(w, http.StatusGone, "SHARE_REVOKED", "Share has been revoked")
			return
		}
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to retrieve share")
		return
	}

	var req shareAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}

	// Check if password is required
	if sh.PasswordHash == "" {
		writeErrorSimple(w, http.StatusBadRequest, "NO_PASSWORD_REQUIRED", "No password required for this share")
		return
	}

	// Verify password
	err = s.store.VerifyPassword(token, req.Password)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "revoked") {
			writeErrorSimple(w, http.StatusNotFound, "SHARE_NOT_FOUND", "Share not found")
			return
		}
		writeErrorSimple(w, http.StatusUnauthorized, "INVALID_PASSWORD", "Invalid password")
		return
	}

	// Set scoped cookie — HMAC over (token, passwordHash) signed with
	// the server's JWT secret. Never store the raw password in the
	// cookie (security: XSS / extension reading the cookie would
	// otherwise recover the password).
	http.SetCookie(w, &http.Cookie{
		Name:     shareAuthCookieNamePrefix + token,
		Value:    s.shareCookieValue(token, sh.PasswordHash),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(shareAuthCookieTTL),
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Authenticated",
	})
}

// shareListHandler handles GET /api/v1/share/{token}/list.
func (s *Server) shareListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	token := chi.URLParam(r, "token")
	if token == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Token required")
		return
	}

	sh, err := s.validateShareBasic(token)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErrorSimple(w, http.StatusNotFound, "SHARE_NOT_FOUND", "Share not found")
			return
		}
		if strings.Contains(err.Error(), "revoked") {
			writeErrorSimple(w, http.StatusGone, "SHARE_REVOKED", "Share has been revoked")
			return
		}
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to retrieve share")
		return
	}

	expired, err := s.store.IsExpired(token)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to check expiration")
		return
	}
	if expired {
		writeErrorSimple(w, http.StatusGone, "SHARE_EXPIRED", "Share has expired")
		return
	}

	// Check password requirement
	if sh.PasswordHash != "" {
		cookie, err := r.Cookie(shareAuthCookieNamePrefix + token)
		if err != nil || cookie.Value == "" {
			writeErrorSimple(w, http.StatusUnauthorized, "SHARE_PASSWORD_REQUIRED", "Password required")
			return
		}

		// Verify HMAC cookie signature (see shareCookieValue).
		if !s.verifyShareCookie(token, sh.PasswordHash, cookie.Value) {
			writeErrorSimple(w, http.StatusUnauthorized, "INVALID_PASSWORD", "Invalid password")
			return
		}
	}

	// Check download limit
	if sh.DownloadLimit != nil && sh.DownloadsUsed >= *sh.DownloadLimit {
		writeErrorSimple(w, http.StatusGone, "SHARE_LIMIT_REACHED", "Download limit reached")
		return
	}

	// Object shares cannot be listed
	if sh.Key != "" {
		writeErrorSimple(w, http.StatusNotFound, "SHARE_IS_SINGLE_OBJECT", "Share is for a single object")
		return
	}

	prefix := r.URL.Query().Get("prefix")
	
	// Build the full prefix for listing
	fullPrefix := sh.Prefix + prefix
	if !strings.HasSuffix(fullPrefix, "/") && prefix != "" {
		fullPrefix += "/"
	}

	// Get driver and list objects
	drv, err := s.reg.For(r.Context(), sh.ConnectionID)
	if err != nil {
		writeErrorSimple(w, http.StatusNotFound, "CLUSTER_NOT_FOUND", "Connection not found")
		return
	}

	page, err := drv.ListObjects(r.Context(), sh.BucketID, fullPrefix, "", 100)
	if err != nil {
		var de *driver.Error
		if errors.As(err, &de) {
			writeDriverError(w, "ListObjects", err)
			return
		}
		writeErrorSimple(w, http.StatusInternalServerError, "DRIVER_ERROR", "Failed to list objects")
		return
	}

	// Security: ensure all returned objects are under the share's prefix
	cleanedObjects := []driver.ObjectInfo{}
	for _, obj := range page.Objects {
		if strings.HasPrefix(obj.Key, sh.Prefix) {
			cleanedObjects = append(cleanedObjects, obj)
		}
	}

	resp := shareListResponse{
		Objects:       cleanedObjects,
		IsTruncated:   page.IsTruncated,
		Prefixes:      page.Prefixes,
	}
	if page.NextContinuation != "" {
		resp.NextContinuation = page.NextContinuation
	}

	writeJSON(w, http.StatusOK, resp)
}

// shareGetHandler handles GET /api/v1/share/{token}/get.
func (s *Server) shareGetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	token := chi.URLParam(r, "token")
	if token == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Token required")
		return
	}

	sh, err := s.validateShareBasic(token)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeErrorSimple(w, http.StatusNotFound, "SHARE_NOT_FOUND", "Share not found")
			return
		}
		if strings.Contains(err.Error(), "revoked") {
			writeErrorSimple(w, http.StatusGone, "SHARE_REVOKED", "Share has been revoked")
			return
		}
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to retrieve share")
		return
	}

	expired, err := s.store.IsExpired(token)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to check expiration")
		return
	}
	if expired {
		writeErrorSimple(w, http.StatusGone, "SHARE_EXPIRED", "Share has expired")
		return
	}

	// Check download limit before incrementing
	if sh.DownloadLimit != nil && sh.DownloadsUsed >= *sh.DownloadLimit {
		writeErrorSimple(w, http.StatusGone, "SHARE_LIMIT_REACHED", "Download limit reached")
		return
	}

	// Check password requirement
	if sh.PasswordHash != "" {
		cookie, err := r.Cookie(shareAuthCookieNamePrefix + token)
		if err != nil || cookie.Value == "" {
			writeErrorSimple(w, http.StatusUnauthorized, "SHARE_PASSWORD_REQUIRED", "Password required")
			return
		}

		// Verify HMAC cookie signature (see shareCookieValue).
		if !s.verifyShareCookie(token, sh.PasswordHash, cookie.Value) {
			writeErrorSimple(w, http.StatusUnauthorized, "INVALID_PASSWORD", "Invalid password")
			return
		}
	}

	var key string
	if sh.Key != "" {
		// Object share - use the stored key
		key = sh.Key
	} else {
		// Prefix share - get key from query parameter
		key = r.URL.Query().Get("key")
		if key == "" {
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Key required for prefix shares")
			return
		}

		// Security: validate key is under the share's prefix
		expectedPrefix := sh.Prefix
		if !strings.HasSuffix(expectedPrefix, "/") && key != "" {
			expectedPrefix += "/"
		}
		
		if !strings.HasPrefix(key, expectedPrefix) {
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Key not under share prefix")
			return
		}

		// Additional security: prevent path traversal
		cleanKey := cleanPath(key)
		if cleanKey != key {
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid key path")
			return
		}
	}

	// Get driver and create presigned URL
	drv, err := s.reg.For(r.Context(), sh.ConnectionID)
	if err != nil {
		writeErrorSimple(w, http.StatusNotFound, "CLUSTER_NOT_FOUND", "Connection not found")
		return
	}

	presignURL, err := drv.PresignGet(r.Context(), sh.BucketID, key, 3600*time.Second)
	if err != nil {
		var de *driver.Error
		if errors.As(err, &de) {
			writeDriverError(w, "PresignGet", err)
			return
		}
		writeErrorSimple(w, http.StatusInternalServerError, "DRIVER_ERROR", "Failed to generate presigned URL")
		return
	}

	// Increment download counter
	err = s.store.IncrementDownloads(token)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "revoked") {
			writeErrorSimple(w, http.StatusNotFound, "SHARE_NOT_FOUND", "Share not found")
			return
		}
		if strings.Contains(err.Error(), "limit reached") {
			writeErrorSimple(w, http.StatusGone, "SHARE_LIMIT_REACHED", "Download limit reached")
			return
		}
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to increment download counter")
		return
	}

	// Redirect to presigned URL
	http.Redirect(w, r, presignURL.URL, http.StatusFound)
}

// cleanPath removes path traversal attempts.
func cleanPath(path string) string {
	// Use strings.ReplaceAll to remove any .. patterns
	result := strings.ReplaceAll(path, "..", "")
	
	// Check for encoded variants
	result = strings.ReplaceAll(result, "%2e%2e", "")
	result = strings.ReplaceAll(result, "%2E%2E", "")
	
	return result
}
