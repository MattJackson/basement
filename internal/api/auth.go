package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/driver"
)

// LoginRequest represents the login payload.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// UserResponse represents a user profile response.
type UserResponse struct {
	Username  string `json:"username"`
	Role      string `json:"role"`
	UIAdmin   bool   `json:"uiAdmin,omitempty"`
}

var adminCredsOnce sync.Once
var adminUser, adminHash string

// loadAdminCreds loads admin credentials from config once at first login attempt.
func loadAdminCreds(cfg *config.Config) {
	adminCredsOnce.Do(func() {
		adminUser = cfg.Admin.User
		adminHash = cfg.Admin.PasswordHash
	})
}

// loginHandler handles POST /api/v1/auth/login.
// Parses JSON {username, password}, verifies against config admin creds,
// issues JWT via auth.IssueToken, sets cookie via auth.SetSessionCookie,
// returns 200 with {username, role: "admin"}.
func (s *Server) loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	var req LoginRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	// Validate required fields
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Username and password are required")
		return
	}

	loadAdminCreds(s.cfg)

	// Constant-time comparison for both username and password to prevent timing attacks.
	// Per anti-fab gates: use subtle.ConstantTimeCompare for username too, not just bcrypt.
	usernameMatch := subtleConstantTimeString(req.Username, adminUser)
	passwordMatch := auth.VerifyPassword(adminHash, req.Password)

	if !usernameMatch || !passwordMatch {
		// Return same error for both cases to prevent enumeration.
		writeErrorSimple(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid credentials")
		return
	}

	// Success: issue JWT with 24h or cfg.SessionTTL TTL.
	ttl := s.cfg.SessionTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	token, err := auth.IssueToken(s.cfg.JWT.Secret, adminUser, "admin", true, ttl)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "TOKEN_ISSUE", "Failed to issue session token")
		return
	}

	auth.SetSessionCookie(w, token, ttl)

	resp := UserResponse{
		Username: adminUser,
		Role:     "admin",
		UIAdmin:  true,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// logoutHandler handles POST /api/v1/auth/logout.
// Clears cookie, returns 204.
func (s *Server) logoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	auth.ClearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// meHandler handles GET /api/v1/auth/me.
// Returns current claims as {username, role, uiAdmin} from auth.FromContext.
// 401 if no claims (middleware should have caught this — meHandler treats
// missing claims as a programming error and 500s).
func (s *Server) meHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok {
		// Programming error: middleware should have caught missing claims.
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "No session claims in context")
		return
	}

	resp := UserResponse{
		Username: claims.UserID,
		Role:     claims.Role,
		UIAdmin:  claims.UIAdmin,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// subtleConstantTimeString compares two strings in constant time.
func subtleConstantTimeString(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	aBytes := []byte(a)
	bBytes := []byte(b)
	return subtleConstantTimeCompare(aBytes, bBytes) == 0
}

// subtleConstantTimeCompare is a simplified constant-time byte comparison.
func subtleConstantTimeCompare(a, b []byte) int {
	if len(a) != len(b) {
		return 1
	}
	var diff uint8
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return int(diff)
}

// capabilitiesHandler handles GET /api/v1/capabilities.
// Calls drv.Capabilities(ctx) and JSON-encodes the result.
// If driver returns ErrUnsupported, passes through uniform error shape
// with code: "DRIVER_UNSUPPORTED", HTTP 501.
func (s *Server) capabilitiesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	caps, err := s.drv.Capabilities(r.Context())
	if err != nil {
		if errors.Is(err, driver.ErrUnsupported) {
			writeErrorSimple(w, http.StatusNotImplemented, "DRIVER_UNSUPPORTED", err.Error())
			return
		}
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(caps)
}

// getOrgCapabilitiesHandler handles GET /api/v1/auth/org-capabilities.
// Returns user-visible subset of OrgCapabilities: allowUserBackends, userBackendDrivers.
func (s *Server) getCurrentOrgCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	caps := s.store.OrgCapabilities().Get()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"allowUserBackends":  caps.AllowUserBackends,
		"userBackendDrivers": caps.UserBackendDrivers,
	})
}
