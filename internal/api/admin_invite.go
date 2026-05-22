package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/store"
)

// Invite endpoints — v1.3.0d.
//
// Three admin-side surfaces (gated by host:manage_users):
//   - GET    /admin/invites          → list pending invites
//   - POST   /admin/invites          → create a new invite (returns plaintext once)
//   - POST   /admin/invites/{id}/rotate → replace token + extend expiry
//   - DELETE /admin/invites/{id}     → revoke
//
// Plus the existing public surface:
//   - POST /invites/{token}/redeem   → exchange plaintext for a new user
//
// Tokens live in store.Invites (invites.json). The plaintext is never
// persisted: Create + Rotate return it exactly once, the store keeps
// only a bcrypt hash + the last-4 characters (for UI display).

// invitePublicResponse is the wire shape for an Invite minus the
// bcrypt hash and other server-side bookkeeping. The plaintext token
// rides along on Create + Rotate exactly once; subsequent list /
// get calls see only the last 4 chars.
type invitePublicResponse struct {
	ID         string    `json:"id"`
	Label      string    `json:"label,omitempty"`
	TokenLast4 string    `json:"tokenLast4"`
	CreatedBy  string    `json:"createdBy,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	Expired    bool      `json:"expired"`
}

func toInvitePublic(inv store.Invite) invitePublicResponse {
	return invitePublicResponse{
		ID:         inv.ID,
		Label:      inv.Label,
		TokenLast4: inv.TokenLast4,
		CreatedBy:  inv.CreatedBy,
		CreatedAt:  inv.CreatedAt,
		ExpiresAt:  inv.ExpiresAt,
		Expired:    time.Now().UTC().After(inv.ExpiresAt),
	}
}

// createInviteRequest is the body for POST /admin/invites.
// Label is optional ("wife", "father", "Alice from accounting"); TTL
// (in seconds) is optional and falls back to DefaultInviteTTL.
type createInviteRequest struct {
	Label    string `json:"label,omitempty"`
	TTLHours int    `json:"ttlHours,omitempty"` // hours; 0 → default
}

type createInviteResponse struct {
	Invite invitePublicResponse `json:"invite"`
	Token  string               `json:"token"` // PLAINTEXT, only sent once
}

// listInvitesHandler handles GET /api/v1/admin/invites.
func (s *Server) listInvitesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	if _, ok := s.requireCapability(w, r, "host:manage_users", "host:*"); !ok {
		return
	}

	if s.store == nil || s.store.Invites() == nil {
		writeJSON(w, http.StatusOK, []invitePublicResponse{})
		return
	}

	rows, err := s.store.Invites().List()
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list invites")
		return
	}

	out := make([]invitePublicResponse, 0, len(rows))
	for _, inv := range rows {
		out = append(out, toInvitePublic(inv))
	}
	writeJSON(w, http.StatusOK, out)
}

// createInvitePersistedHandler handles POST /api/v1/admin/invites.
// Persists a fresh invite token in the store and returns the plaintext
// exactly once. Distinct from createUserHandler's inline-invite path:
// this handler is the canonical way to mint a redemption URL.
func (s *Server) createInvitePersistedHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	if _, ok := s.requireCapability(w, r, "host:manage_users", "host:*"); !ok {
		return
	}

	if s.store == nil || s.store.Invites() == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "INVITES_NOT_WIRED",
			"Invite store is not configured on this deployment.")
		return
	}

	var req createInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	req.Label = strings.TrimSpace(req.Label)

	// Caller-supplied TTL in hours; clamp to a reasonable upper bound
	// (90 days). 0 / negative falls through to the store's default.
	var ttl time.Duration
	if req.TTLHours > 0 {
		ttl = time.Duration(req.TTLHours) * time.Hour
		const maxTTL = 90 * 24 * time.Hour
		if ttl > maxTTL {
			ttl = maxTTL
		}
	}

	claims, _ := auth.FromContext(r.Context())
	createdBy := ""
	if claims != nil {
		createdBy = claims.UserID
	}

	inv, plain, err := s.store.Invites().Create(req.Label, createdBy, ttl)
	if err != nil {
		s.auditFailure(r, "invite:create", "invite:"+req.Label, err)
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create invite")
		return
	}

	s.auditSuccess(r, "invite:create", "invite:"+inv.ID)

	writeJSON(w, http.StatusCreated, createInviteResponse{
		Invite: toInvitePublic(inv),
		Token:  plain,
	})
}

// revokeInviteHandler handles DELETE /api/v1/admin/invites/{id}.
func (s *Server) revokeInviteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "DELETE required")
		return
	}

	if _, ok := s.requireCapability(w, r, "host:manage_users", "host:*"); !ok {
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "invite id required")
		return
	}

	if s.store == nil || s.store.Invites() == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "INVITES_NOT_WIRED",
			"Invite store is not configured on this deployment.")
		return
	}

	if err := s.store.Invites().Revoke(id); err != nil {
		if errors.Is(err, store.ErrInviteNotFound) {
			s.auditFailure(r, "invite:revoke", "invite:"+id, err)
			writeErrorSimple(w, http.StatusNotFound, "INVITE_NOT_FOUND", "Invite not found")
			return
		}
		s.auditFailure(r, "invite:revoke", "invite:"+id, err)
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to revoke invite")
		return
	}

	s.auditSuccess(r, "invite:revoke", "invite:"+id)
	w.WriteHeader(http.StatusNoContent)
}

// rotateInviteHandler handles POST /api/v1/admin/invites/{id}/rotate.
// Replaces the token + refreshes the expiry, returning the new
// plaintext exactly once. Useful when the original token leaked or
// expired and the operator still wants the invitee to redeem under
// the same Label / CreatedBy attribution.
func (s *Server) rotateInviteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	if _, ok := s.requireCapability(w, r, "host:manage_users", "host:*"); !ok {
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "invite id required")
		return
	}

	if s.store == nil || s.store.Invites() == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "INVITES_NOT_WIRED",
			"Invite store is not configured on this deployment.")
		return
	}

	var req createInviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		// Body is optional for rotate — ignore parse errors so a
		// caller can POST with no body and get default-TTL behaviour.
		_ = err
	}

	var ttl time.Duration
	if req.TTLHours > 0 {
		ttl = time.Duration(req.TTLHours) * time.Hour
		const maxTTL = 90 * 24 * time.Hour
		if ttl > maxTTL {
			ttl = maxTTL
		}
	}

	inv, plain, err := s.store.Invites().Rotate(id, ttl)
	if err != nil {
		if errors.Is(err, store.ErrInviteNotFound) {
			s.auditFailure(r, "invite:rotate", "invite:"+id, err)
			writeErrorSimple(w, http.StatusNotFound, "INVITE_NOT_FOUND", "Invite not found")
			return
		}
		s.auditFailure(r, "invite:rotate", "invite:"+id, err)
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to rotate invite")
		return
	}

	s.auditSuccess(r, "invite:rotate", "invite:"+inv.ID)

	writeJSON(w, http.StatusOK, createInviteResponse{
		Invite: toInvitePublic(inv),
		Token:  plain,
	})
}

// inviteRedeemHandler handles POST /api/v1/invites/{token}/redeem.
// Public endpoint that exchanges an invite token for a user account.
//
// v1.3.0d: token is now verified against the persistent invite store
// (bcrypt-hashed on disk). Plaintext travels in the path param the
// admin sent the invitee; redemption is one-shot — the row disappears
// from the store on success and on expiry.
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

	if s.store == nil || s.store.Invites() == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "INVITES_NOT_WIRED",
			"Invite store is not configured on this deployment.")
		return
	}

	inv, err := s.store.Invites().Redeem(tokenStr)
	if err != nil {
		// Collapse "not found" and "expired" to the same wire-shape
		// error so the public endpoint never leaks which one it was.
		// Admin-side rotate / list panel surfaces the precise reason.
		writeErrorSimple(w, http.StatusUnauthorized, "INVALID_OR_EXPIRED_TOKEN",
			"This invite token is invalid or has expired. Ask the operator for a new one.")
		return
	}

	// Build the username from the invite Label when possible (sanitized
	// to lowercase + alnum / dash) — gives the operator's note a chance
	// to become a recognisable login. Falls back to user-<uuid-prefix>
	// if Label is blank or sanitization strips everything.
	userID := uuid.New().String()
	username := sanitizeInviteUsername(inv.Label)
	if username == "" {
		username = "user-" + userID[:8]
	}
	// Avoid collision with an existing user.
	if _, err := s.store.UserByUsername(username); err == nil {
		username = username + "-" + userID[:6]
	}

	hashStr, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to hash password")
		return
	}

	user := store.User{
		ID:           userID,
		Username:     username,
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
		Username: user.Username,
		Role:     "user",
		UIAdmin:  false,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// sanitizeInviteUsername lowercases the label, replaces whitespace
// with dashes, and drops everything outside [a-z0-9-]. Length capped
// at 32 chars. Returns "" if the result is empty after sanitization
// (caller falls back to user-<uuid-prefix>).
func sanitizeInviteUsername(label string) string {
	s := strings.ToLower(strings.TrimSpace(label))
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
			out = append(out, c)
		case c >= '0' && c <= '9':
			out = append(out, c)
		case c == '-':
			out = append(out, c)
		case c == ' ' || c == '_':
			out = append(out, '-')
		}
		if len(out) >= 32 {
			break
		}
	}
	// Trim leading / trailing dashes for a cleaner login.
	result := strings.Trim(string(out), "-")
	return result
}
