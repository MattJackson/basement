package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mattjackson/basement/internal/audit"
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
//
// Mode + ModeExpiresAt (ADR-0003, v1.2.0b) ride along on /auth/me so the
// frontend can hydrate its sudo-state machine on first render without a
// separate roundtrip. The login handler still returns the legacy shape
// (mode is always "user" right after login, expiry zero); meHandler is
// the one place that needs the live values because the gate's grace-
// window logic can downgrade a pre-v1.2 cookie to ADMIN on the wire.
type UserResponse struct {
	Username       string        `json:"username"`
	Role           string        `json:"role"`
	UIAdmin        bool          `json:"uiAdmin,omitempty"`
	Mode           string        `json:"mode,omitempty"`
	ModeExpiresAt  int64         `json:"modeExpiresAt,omitempty"`
	ActiveRole     *auth.ActiveRole `json:"activeRole,omitempty"`
	AvailableRoles []auth.AvailableRole `json:"availableRoles,omitempty"`
	// OIDCUser is true when this account was provisioned via OIDC
	// (no local password). The FE branches its elevation modal on
	// this — OIDC-only users see an "Elevate via SSO" button that
	// kicks off /auth/elevate/oidc/start instead of the password
	// form. ADR-0003 v1.2.0c.
	OIDCUser bool `json:"oidcUser,omitempty"`
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
		// Per v1.0.0c: record auth failures so a privilege-escalation
		// attempt — repeated wrong-password storms from one IP, or a
		// surge across many usernames — surfaces in the audit view.
		// Actor stays empty because by definition no JWT issued yet;
		// the submitted username goes into Detail (NOT Actor) so the
		// schema stays consistent: Actor = "authenticated user ID."
		s.audit.Log(audit.Event{
			Action:    "auth:login",
			Resource:  resourceUser(req.Username),
			Result:    audit.ResultFailure,
			Detail:    "invalid credentials",
			IP:        clientIP(r),
			UserAgent: r.UserAgent(),
		})

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

// computeAvailableRoles computes the list of roles a user is eligible for.
// Returns []AvailableRole with labels in display order: User first, then
// Cluster Admin options (one per cluster grant), then UI Admin if applicable.
func (s *Server) computeAvailableRoles(claims *auth.Claims) []auth.AvailableRole {
	if claims == nil || claims.UserID == "" {
		return nil
	}

	var roles []auth.AvailableRole
	clusterAdminSet := make(map[string]bool)

	// User is always available
	roles = append(roles, auth.AvailableRole{Kind: "user", Label: "User"})

	// Collect cluster admin grants (explicit and implicit via UIAdmin)
	if s.policy != nil && claims.UserID != "" {
		assignments := s.policy.AssignmentsFor(claims.UserID)
		for _, a := range assignments {
			if strings.HasPrefix(a.RoleID, "cluster_admin") {
				if a.Scope == "cluster:*" {
					// Wildcard: enumerate all clusters via connection list if available
					if s.conns != nil {
						conns, err := s.conns.List(context.Background())
						if err == nil && conns != nil {
							for _, conn := range conns {
								label := conn.Label
								if label == "" {
									label = conn.ID
								}
								key := "cluster-admin:" + conn.ID
								if !clusterAdminSet[key] {
									roles = append(roles, auth.AvailableRole{
										Kind:    "cluster-admin",
										Cluster: conn.ID,
										Label:   "Cluster Admin: " + label,
									})
									clusterAdminSet[key] = true
								}
							}
						}
					}
				} else if strings.HasPrefix(a.Scope, "cluster:") {
					cid := strings.TrimPrefix(a.Scope, "cluster:")
					if cid != "" {
						key := "cluster-admin:" + cid
						if !clusterAdminSet[key] {
							roles = append(roles, auth.AvailableRole{
								Kind:    "cluster-admin",
								Cluster: cid,
								Label:   "Cluster Admin: " + cid,
							})
							clusterAdminSet[key] = true
						}
					}
				}
			}
		}
	}

	// UI Admin gets implicit cluster admin on all clusters (if not already explicit)
	if claims.UIAdmin && s.conns != nil {
		conns, err := s.conns.List(context.Background())
		if err == nil && conns != nil {
			for _, conn := range conns {
				label := conn.Label
				if label == "" {
					label = conn.ID
				}
				key := "cluster-admin:" + conn.ID
				if !clusterAdminSet[key] {
					roles = append(roles, auth.AvailableRole{
						Kind:    "cluster-admin",
						Cluster: conn.ID,
						Label:   "Cluster Admin: " + label,
					})
					clusterAdminSet[key] = true
				}
			}
		}
	}

	// UI Admin role is always available if claims.UIAdmin == true
	if claims.UIAdmin {
		roles = append(roles, auth.AvailableRole{Kind: "ui-admin", Label: "UI Admin"})
	}

	return roles
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

	// Mode comes through currentMode() so a pre-v1.2 cookie inside the
	// 7-day grace window shows up as "admin" — matching what the gate
	// actually enforces. Without this, the FE pill would render USER
	// while the backend honoured ADMIN, and the operator would see
	// admin pages they couldn't access via the UI.
	mode := currentMode(r)

	// Provide default active role if nil (for tests and unconfigured servers)
	activeRole := claims.ActiveRole
	if activeRole == nil {
		activeRole = &auth.ActiveRole{Kind: "user"}
	}

	resp := UserResponse{
		Username:       claims.UserID,
		Role:           claims.Role,
		UIAdmin:        claims.UIAdmin,
		Mode:           string(mode),
		ModeExpiresAt:  claims.ModeExpiresAt,
		ActiveRole:     activeRole,
		AvailableRoles: s.computeAvailableRoles(claims),
	}

	// OIDCUser flag: a user with an empty PasswordHash + non-empty
	// Provider is OIDC-only and must elevate through the OIDC
	// challenge flow. The env-seeded admin (claims.UserID ==
	// adminUser) always has a local password, so we never look that
	// one up. Store may be nil in some test paths — leave the flag
	// at false in that case.
	loadAdminCreds(s.cfg)
	if claims.UserID != adminUser && s.store != nil {
		if u, err := s.store.UserByUsername(claims.UserID); err == nil {
			if u.PasswordHash == "" && u.Provider != "" {
				resp.OIDCUser = true
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// logoutElevationHandler handles POST /api/v1/auth/logout-elevation
// (ADR-0003 v1.2.0b "drop privileges"). Issues a new session cookie
// with mode=user, ModeExpiresAt=0; the rest of the JWT (user id, role,
// uiAdmin, session expiry) is preserved.
//
// This is NOT a full logout — the session cookie stays valid, just
// downgraded to USER mode. The FE wires this to the "X / Drop" button
// next to the persona-pill countdown so the operator can instantly
// shed admin privileges without re-logging-in to come back up.
func (s *Server) logoutElevationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok || claims == nil || claims.UserID == "" {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	current := currentMode(r)

	sessionTTL := s.cfg.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = 24 * time.Hour
	}

	token, err := auth.IssueTokenWithMode(
		s.cfg.JWT.Secret,
		claims.UserID,
		claims.Role,
		claims.UIAdmin,
		"user",
		0,
		sessionTTL,
	)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "TOKEN_ISSUE", "Failed to issue session token")
		return
	}

	auth.SetSessionCookie(w, token, sessionTTL)

	s.audit.Log(audit.Event{
		Time:      time.Now().UTC(),
		Actor:     claims.UserID,
		ActorRole: "user",
		Action:    "auth:drop_privileges",
		Resource:  "mode:user",
		Detail:    "dropped privileges from " + string(current) + " to user",
		Result:    audit.ResultSuccess,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"mode":            "user",
		"mode_expires_at": int64(0),
	})
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

// ActiveRoleRequest represents the payload for PUT /api/v1/auth/active-role.
type ActiveRoleRequest struct {
	Kind    string `json:"kind"`
	Cluster string `json:"cluster,omitempty"` // only required when Kind=="cluster-admin"
}

// activeRoleHandler handles PUT /api/v1/auth/active-role.
// Validates user is eligible for the requested role, returns 423 LOCKED if elevation needed,
// persists activeRole in session cookie on success.
func (s *Server) activeRoleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "PUT required")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok || claims == nil || claims.UserID == "" {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	var req ActiveRoleRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	// Validate kind is one of the three supported roles
	validKinds := map[string]bool{"user": true, "cluster-admin": true, "ui-admin": true}
	if !validKinds[req.Kind] {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_ROLE_KIND", "Invalid role kind")
		return
	}

	// Determine if elevation is required for this role switch
	requiresElevation := false
	elevationPrompt := ""

	switch req.Kind {
	case "user":
		// Dropping to user mode is always free - no elevation needed
		requiresElevation = false

	case "cluster-admin":
		// Cluster admin does NOT require sudo-style elevation: the cluster_admin
		// grant itself is the authorization. The grant check below is what gates
		// the switch. Only UI Admin (platform-wide super-admin) needs re-auth.

		// Validate cluster parameter is provided for cluster-admin kind
		if req.Cluster == "" {
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Cluster ID required for cluster-admin role")
			return
		}

		// Verify user has cluster_admin capability on this specific cluster.
		// UI Admins implicitly admin every cluster (matches computeAvailableRoles).
		if !claims.UIAdmin && s.policy != nil {
			hasGrant := false
			assignments := s.policy.AssignmentsFor(claims.UserID)
			for _, a := range assignments {
				if strings.HasPrefix(a.RoleID, "cluster_admin") &&
					(a.Scope == "cluster:*" || a.Scope == "cluster:"+req.Cluster) {
					hasGrant = true
					break
				}
			}
			if !hasGrant {
				writeErrorSimple(w, http.StatusForbidden, "FORBIDDEN", "User not eligible for cluster admin role on this cluster")
				return
			}
		}

	case "ui-admin":
		// UI Admin requires elevation if not already elevated AND user is uiAdmin
		if !claims.UIAdmin {
			writeErrorSimple(w, http.StatusForbidden, "FORBIDDEN", "User is not a UI admin")
			return
		}
		if claims.Mode != "admin" && claims.Mode != "elevated" {
			requiresElevation = true
			elevationPrompt = "Switching to UI admin requires admin re-authentication."
		}
	}

	// If elevation required but user not elevated, return 423 LOCKED
	if requiresElevation && claims.Mode != "admin" && claims.Mode != "elevated" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusLocked)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"requires_elevation": true,
			"prompt":             elevationPrompt,
		})
		return
	}

	// Build the new active role
	newActiveRole := &auth.ActiveRole{Kind: req.Kind}
	if req.Kind == "cluster-admin" {
		newActiveRole.Cluster = req.Cluster
	}

	// Issue a new session cookie with updated activeRole
	sessionTTL := s.cfg.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = 24 * time.Hour
	}

	token, err := auth.IssueTokenWithActiveRole(
		s.cfg.JWT.Secret,
		claims.UserID,
		claims.Role,
		claims.UIAdmin,
		claims.Mode,
		claims.ModeExpiresAt,
		sessionTTL,
		newActiveRole,
	)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "TOKEN_ISSUE", "Failed to issue session token")
		return
	}

	auth.SetSessionCookie(w, token, sessionTTL)

	s.audit.Log(audit.Event{
		Time:      time.Now().UTC(),
		Actor:     claims.UserID,
		ActorRole: "user",
		Action:    "auth:switch_role",
		Resource:  "active-role:" + req.Kind,
		Detail: func() string {
			detail := "Switched active role to " + req.Kind
			if newActiveRole.Cluster != "" {
				detail += " cluster=" + newActiveRole.Cluster
			}
			return detail
		}(),
		Result:    audit.ResultSuccess,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	})

	resp := UserResponse{
		Username:       claims.UserID,
		Role:           claims.Role,
		UIAdmin:        claims.UIAdmin,
		Mode:           string(currentMode(r)),
		ModeExpiresAt:  claims.ModeExpiresAt,
		ActiveRole:     newActiveRole,
		AvailableRoles: s.computeAvailableRoles(claims),
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, http.StatusOK, resp)
}
