package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/store"
)

// inviteRedeemHandler handles POST /api/v1/invites/{token}/redeem.
// Public endpoint that exchanges an invite token for a user account.
func (s *Server) inviteRedeemHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	tokenStr := r.PathValue("token")
	if tokenStr == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Invite token is required")
		return
	}

	var req InviteRedeemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	req.Password = strings.TrimSpace(req.Password)
	if req.Password == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Password is required")
		return
	}

	// TODO: Store invite tokens in a separate invites.json file with expiration
	// For now, this is a placeholder that accepts any valid token format
	// In production, you'd validate against stored hashed tokens

	// Generate new user ID
	userID := uuid.New().String()

	// Hash password
	hashStr, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to hash password")
		return
	}

	user := store.User{
		ID:           userID,
		Username:     "user-" + userID[:8], // auto-generated username from invite
		PasswordHash: hashStr,
		Role:         "user",
		UIAdmin:      false,
		Created:      time.Now(),
	}

	if err := s.store.CreateUser(user); err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create user")
		return
	}

	ttl := s.cfg.SessionTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	token, err := auth.IssueToken(s.cfg.JWT.Secret, userID, "user", false, ttl)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "TOKEN_ISSUE", "Failed to issue session token")
		return
	}

	auth.SetSessionCookie(w, token, ttl)

	resp := UserResponse{
		Username:  user.Username,
		Role:      "user",
		UIAdmin:   false,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
