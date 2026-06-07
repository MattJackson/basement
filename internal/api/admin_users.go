package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
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
	Token    string `json:"token"`
	Password string `json:"password"`
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
//
// Per ADR-0001 v0.9.0f: gated on host:manage_users at "host:*".
func (s *Server) createUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	if _, ok := s.requireCapability(w, r, "host:manage_users", "host:*"); !ok {
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

	// Invite-only mode: persist a redeemable invite instead of creating
	// the account up front.
	//
	// SECURITY (r10/r11): the previous inline branch (a) created the
	// user record FIRST and minted the token afterward — a token-gen
	// failure left an orphan account with no password and no invite;
	// (b) returned the bcrypt HashedToken on the wire (needless
	// credential-hash exposure); and (c) never persisted the token to
	// store.Invites, so the returned plaintext could never be redeemed
	// (inviteRedeemHandler verifies against the store only). We now
	// route through the canonical persisted-invite path: the token is
	// stored bcrypt-hashed, the plaintext is returned exactly ONCE
	// (mirroring shares + createInvitePersistedHandler), the hash never
	// leaves the server, and no user is pre-created — the account is
	// minted at redeem time from the invite Label.
	if req.InviteOnly {
		if s.store.Invites() == nil {
			writeErrorSimple(w, http.StatusServiceUnavailable, "INVITES_NOT_WIRED",
				"Invite store is not configured on this deployment.")
			return
		}

		claims, _ := auth.FromContext(r.Context())
		createdBy := ""
		if claims != nil {
			createdBy = claims.UserID
		}

		// Label carries the desired username; the redeem path
		// sanitizes it into the new account's login.
		inv, plain, err := s.store.Invites().Create(req.Username, createdBy, 7*24*time.Hour)
		if err != nil {
			s.auditFailure(r, "invite:create", "invite:"+req.Username, err)
			writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create invite")
			return
		}

		s.auditSuccess(r, "invite:create", "invite:"+inv.ID)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"user": UserResponse{Username: req.Username, Role: "user"},
			"invite": createInviteResponse{
				Invite: toInvitePublic(inv),
				Token:  plain, // plaintext, returned once; hash stays server-side
			},
		})
		return
	}

	// Direct (non-invite) creation: a password is required.
	if req.Password == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Password required for non-invite user")
		return
	}
	hashStr, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to hash password")
		return
	}

	user := store.User{
		ID:           uuid.New().String(),
		Username:     req.Username,
		Role:         "user",
		UIAdmin:      false,
		Email:        req.Email,
		Name:         req.Name,
		PasswordHash: hashStr,
		Created:      time.Now(),
	}

	if err := s.store.CreateUser(user); err != nil {
		s.auditFailure(r, "user:create", resourceUser(user.Username), err)
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create user")
		return
	}

	s.auditSuccess(r, "user:create", resourceUser(user.Username))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(UserResponse{
		Username: user.Username,
		Role:     user.Role,
		UIAdmin:  false,
	})
}

// deleteUserHandler handles DELETE /api/v1/admin/users/{id}.
// Deletes a user for UI Admin only.
//
// Per ADR-0001 v0.9.0f: gated on host:manage_users at "host:*".
func (s *Server) deleteUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "DELETE required")
		return
	}

	if _, ok := s.requireCapability(w, r, "host:manage_users", "host:*"); !ok {
		return
	}

	// The route is DELETE /admin/users/{id}, so the identifier is a
	// PATH param — not a query param (the previous ?id= read always
	// landed empty under chi and 400'd, leaving the endpoint
	// permanently broken). The FE addresses users by username (the
	// only field /admin/users surfaces), so accept either the store
	// UUID or the username and resolve to the canonical store ID
	// before deleting — store.DeleteUser matches on the UUID ID.
	param := chi.URLParam(r, "id")
	if param == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "User ID is required")
		return
	}

	id := param
	if _, err := s.store.UserByID(param); err != nil {
		// Not a known store UUID — try resolving it as a username.
		if u, uerr := s.store.UserByUsername(param); uerr == nil {
			id = u.ID
		}
		// If neither resolves, fall through with the raw param; the
		// DeleteUser call below returns not-found and we 404.
	}

	if err := s.store.DeleteUser(id); err != nil {
		s.auditFailure(r, "user:delete", resourceUser(param), err)
		writeErrorSimple(w, http.StatusNotFound, "USER_NOT_FOUND", "User not found")
		return
	}

	s.auditSuccess(r, "user:delete", resourceUser(param))
	w.WriteHeader(http.StatusNoContent)
}
