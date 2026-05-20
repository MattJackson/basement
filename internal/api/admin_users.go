package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/store"
)

// ListAllUsersRequest represents the request for listing users.
type ListAllUsersRequest struct {
	Search string `json:"search"` // optional username search
}

// InviteUserRequest represents the request for creating a user with invite.
type InviteUserRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password,omitempty"`
	Email      string `json:"email,omitempty"`
	Name       string `json:"name,omitempty"`
	InviteOnly bool   `json:"inviteOnly"` // if true, creates invite token instead of direct account
}

// InviteRedeemRequest represents the request for redeeming an invite.
type InviteRedeemRequest struct {
	Token  string `json:"token"`
	Password string `json:"password"`
}

// UserInvite represents an invite token with expiration.
type UserInvite struct {
	Token     string    `json:"token"`      // hashed for storage
	HashedToken string   `json:"hashedToken,omitempty"` // for response (we send plain, store hash)
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// listAllUsersHandler handles GET /api/v1/admin/users.
// Returns all users for UI Admin only.
func (s *Server) listAllUsersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	users := s.store.Users()

	// Pre-allocate so encoding gives [] not null when no users exist
	// (frontend crashes trying to .map() on null).
	result := make([]UserResponse, 0, len(users)+1)

	// Synthesize the env-seeded admin (matthew) as a user entry — it
	// authenticates from cfg.Admin.User / cfg.Admin.Hash, not from
	// users.json, so it wouldn't otherwise appear on /admin/users.
	loadAdminCreds(s.cfg)
	if adminUser != "" {
		result = append(result, UserResponse{
			Username: adminUser,
			Role:     "admin",
			UIAdmin:  true,
		})
	}

	for _, u := range users {
		if u.Username == adminUser {
			continue // already synthesized above
		}
		result = append(result, UserResponse{
			Username: u.Username,
			Role:     u.Role,
			UIAdmin:  u.UIAdmin,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// createUserHandler handles POST /api/v1/admin/users.
// Creates a new user for UI Admin only. Supports invite mode.
func (s *Server) createUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	var req InviteUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Username is required")
		return
	}

	// Check if username already exists
	_, err := s.store.UserByUsername(req.Username)
	if err == nil {
		writeErrorSimple(w, http.StatusConflict, "USERNAME_TAKEN", "Username already exists")
		return
	}

	// Create user
	user := store.User{
		ID:           uuid.New().String(),
		Username:     req.Username,
		Role:         "user",
		UIAdmin:      false,
		Email:        req.Email,
		Name:         req.Name,
		Created:      time.Now(),
	}

	if !req.InviteOnly && req.Password != "" {
		hashStr, err := auth.HashPassword(req.Password)
		if err != nil {
			writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to hash password")
			return
		}
		user.PasswordHash = hashStr
	} else if !req.InviteOnly {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Password required for non-invite user")
		return
	}

	if err := s.store.CreateUser(user); err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create user")
		return
	}

	resp := UserResponse{
		Username:  user.Username,
		Role:      user.Role,
		UIAdmin:   false,
	}

	if req.InviteOnly {
		// Generate invite token
		tokenBytes := make([]byte, 32)
		if _, err := rand.Read(tokenBytes); err != nil {
			writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to generate token")
			return
		}
		token := hex.EncodeToString(tokenBytes)

		// Hash for storage
		hashedToken, err := auth.HashPassword(token)
		if err != nil {
			writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to hash token")
			return
		}

		invite := UserInvite{
			Token:       token, // Return plain token to user
			HashedToken: hashedToken,
			Username:    req.Username,
			ExpiresAt:   time.Now().Add(7 * 24 * time.Hour), // 7 days
		}

		resp.UIAdmin = false

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"user":     resp,
			"invite":   invite,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// deleteUserHandler handles DELETE /api/v1/admin/users/{id}.
// Deletes a user for UI Admin only.
func (s *Server) deleteUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "DELETE required")
		return
	}

	id := r.URL.Query().Get("id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "User ID is required")
		return
	}

	if err := s.store.DeleteUser(id); err != nil {
		writeErrorSimple(w, http.StatusNotFound, "USER_NOT_FOUND", "User not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
