// Package api: OIDC sudo-style elevation (ADR-0003, cycle v1.2.0c).
//
// Wires the OIDC `prompt=login` re-authentication flow that lets an
// OIDC-only user step their session up from USER → ADMIN or
// ADMIN → ELEVATED without ever having a local password. Companion to
// auth_elevate.go (which handles local-password elevation) — the
// dispatcher there pivots OIDC users to /api/v1/auth/elevate/oidc/start
// via {requires_oidc: true, start_url: "..."} so the FE only needs to
// branch on a single boolean.
//
// Flow:
//
//  1. FE POSTs /api/v1/auth/elevate/oidc/start with {target_mode}.
//     We mint fresh state + nonce, stash {target_mode, user_id, nonce,
//     created_at} in an in-process map keyed by state (5min TTL), and
//     return {redirect_url} pointing at the IdP's authorize endpoint
//     with `prompt=<configured>` + `max_age=0`.
//
//  2. Browser follows redirect_url → IdP re-prompts → IdP redirects
//     back to /api/v1/auth/elevate/oidc/callback?state=...&code=...
//
//  3. Callback: validate state (CSRF + retrieve target_mode), exchange
//     code, verify ID token, confirm auth_time within last 60s (proves
//     this was a FRESH login, not a cached session). On pass: mint a
//     new JWT cookie at the target mode with the right TTL, 302 to
//     "/?elevated=<mode>" so the FE can pop a success toast + close
//     any modal.
//
// State store: in-memory map. Cleaned up on each insert (sweep expired
// entries first) so the map never grows unbounded; entries also expire
// implicitly via the 5min TTL when looked up. Trade-off vs the
// existing /auth/oidc/start cookie approach: the elevation flow
// originates from an already-authenticated session, so we can key the
// state by the user_id and require the callback to come from the same
// session. The cookie approach would also work but mixing it with the
// session JWT in the callback complicates things — keeping this in
// memory is simpler and the data is genuinely ephemeral.

package api

import (
	"encoding/json"
	"net/http"
	stdsync "sync"
	"time"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/auth/policy"
)

// oidcElevationStateTTL is how long a /auth/elevate/oidc/start state
// token stays valid. 5 minutes matches the existing OIDC login state
// cookie and is comfortably longer than a normal IdP roundtrip while
// keeping the attack window short.
const oidcElevationStateTTL = 5 * time.Minute

// oidcElevationAuthTimeFreshness is the maximum age of the IdP's
// auth_time claim that the callback will accept. 60s gives the
// browser + IdP enough breathing room while still rejecting any
// session whose auth_time predates the start of THIS elevation
// request. Belt-and-braces alongside `prompt=login` + `max_age=0`.
const oidcElevationAuthTimeFreshness = 60 * time.Second

// oidcElevationCallbackPath is the relative path the FE redirects to
// after a successful elevation callback. The trailing `?elevated=<mode>`
// is appended by the handler so the FE can pop a toast / close modals.
const oidcElevationCallbackPath = "/"

// oidcElevationStateEntry is the value stored in the state map.
type oidcElevationStateEntry struct {
	TargetMode policy.Mode
	UserID     string
	Nonce      string
	CreatedAt  time.Time
}

// oidcElevationStateStore is the in-memory state map shared across
// /auth/elevate/oidc/start (writer) and /auth/elevate/oidc/callback
// (reader/deleter). Goroutine-safe.
type oidcElevationStateStore struct {
	mu      stdsync.Mutex
	entries map[string]oidcElevationStateEntry
}

// newOIDCElevationStateStore returns a ready-to-use store.
func newOIDCElevationStateStore() *oidcElevationStateStore {
	return &oidcElevationStateStore{entries: make(map[string]oidcElevationStateEntry)}
}

// put inserts an entry and opportunistically sweeps any that have
// passed their TTL. Sweeping on every insert keeps the map size
// bounded by "concurrent in-flight elevations" without needing a
// background goroutine.
func (s *oidcElevationStateStore) put(state string, entry oidcElevationStateEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := nowFunc()
	for k, v := range s.entries {
		if now.Sub(v.CreatedAt) > oidcElevationStateTTL {
			delete(s.entries, k)
		}
	}
	s.entries[state] = entry
}

// take fetches + removes an entry. Returns (entry, true) if present
// AND within TTL; (zero, false) otherwise. The remove-on-fetch is
// deliberate: state tokens are single-use.
func (s *oidcElevationStateStore) take(state string) (oidcElevationStateEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[state]
	if !ok {
		return oidcElevationStateEntry{}, false
	}
	delete(s.entries, state)
	if nowFunc().Sub(entry.CreatedAt) > oidcElevationStateTTL {
		return oidcElevationStateEntry{}, false
	}
	return entry, true
}

// elevateOIDCStartRequest is the POST body shape for /auth/elevate/oidc/start.
type elevateOIDCStartRequest struct {
	TargetMode string `json:"target_mode"`
}

// elevateOIDCStartResponse is the 200 body shape.
type elevateOIDCStartResponse struct {
	RedirectURL string `json:"redirect_url"`
}

// elevateOIDCStartHandler handles POST /api/v1/auth/elevate/oidc/start.
//
// Mints state + nonce, stashes the binding, and returns the IdP's
// authorize URL with `prompt=<configured>` + `max_age=0`. The FE is
// expected to full-page-navigate (window.location.href = redirect_url)
// rather than open a new window so the callback lands in the same
// session.
func (s *Server) elevateOIDCStartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok || claims == nil || claims.UserID == "" {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	if s.oidc == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable,
			"OIDC_NOT_CONFIGURED",
			"OIDC is not configured on this server; contact your administrator to enable elevation.")
		return
	}

	var req elevateOIDCStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	target := policy.Mode(req.TargetMode)
	if target != policy.ModeAdmin && target != policy.ModeElevated {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_TARGET_MODE",
			"target_mode must be \"admin\" or \"elevated\"")
		return
	}

	current := currentMode(r)
	if current == policy.ModeUser && target == policy.ModeElevated {
		writeError(w, http.StatusBadRequest, "INVALID_TARGET_MODE",
			"Cannot elevate directly from user to elevated; elevate to admin first.",
			map[string]any{
				"current_mode":  string(current),
				"target_mode":   string(target),
				"required_path": []string{"admin", "elevated"},
			})
		return
	}

	state, err := randomHex(16)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL", "Failed to generate state")
		return
	}
	nonce, err := randomHex(16)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL", "Failed to generate nonce")
		return
	}

	s.ensureOIDCElevationStore().put(state, oidcElevationStateEntry{
		TargetMode: target,
		UserID:     claims.UserID,
		Nonce:      nonce,
		CreatedAt:  nowFunc(),
	})

	redirectURL := s.oidc.ElevationAuthCodeURL(state, nonce, s.cfg.OIDC.ElevationPrompt)

	s.audit.Log(audit.Event{
		Time:      nowFunc().UTC(),
		Actor:     claims.UserID,
		ActorRole: string(current),
		Action:    "auth:elevate_oidc_start",
		Resource:  "mode:" + string(target),
		Detail:    "issued OIDC elevation redirect (prompt=" + s.cfg.OIDC.ElevationPrompt + ")",
		Result:    audit.ResultSuccess,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	})

	writeJSON(w, http.StatusOK, elevateOIDCStartResponse{RedirectURL: redirectURL})
}

// elevateOIDCCallbackHandler handles GET /api/v1/auth/elevate/oidc/callback.
//
// Validates state (CSRF + retrieves target_mode), exchanges the code,
// verifies the new ID token + auth_time freshness, mints a fresh JWT
// cookie at the target mode, then 302s the browser to "/?elevated=<mode>"
// so the FE can pop a success toast + close any open modal.
//
// On any failure we redirect to "/?elevation_error=<code>" so the FE
// surfaces the error in-context (a 4xx JSON response is invisible to
// a full-page-nav browser).
func (s *Server) elevateOIDCCallbackHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	if s.oidc == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable,
			"OIDC_NOT_CONFIGURED",
			"OIDC is not configured on this server.")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok || claims == nil || claims.UserID == "" {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	queryState := r.URL.Query().Get("state")
	if queryState == "" {
		writeErrorSimple(w, http.StatusBadRequest, "OIDC_STATE_MISSING", "Missing state parameter")
		return
	}

	entry, ok := s.ensureOIDCElevationStore().take(queryState)
	if !ok {
		writeErrorSimple(w, http.StatusBadRequest, "OIDC_STATE_INVALID",
			"OIDC elevation state is missing or expired")
		return
	}

	// Same-session check: the callback must arrive in the session
	// that started the flow. Without this, an attacker who somehow
	// got their hands on the state could redirect their own browser
	// to the callback and have THEIR session elevated.
	if entry.UserID != claims.UserID {
		s.audit.Log(audit.Event{
			Time:      nowFunc().UTC(),
			Actor:     claims.UserID,
			ActorRole: string(currentMode(r)),
			Action:    "auth:elevate_oidc_session_mismatch",
			Resource:  "mode:" + string(entry.TargetMode),
			Detail:    "callback session user differs from start session user",
			Result:    audit.ResultFailure,
			IP:        clientIP(r),
			UserAgent: r.UserAgent(),
		})
		writeErrorSimple(w, http.StatusBadRequest, "OIDC_STATE_INVALID",
			"OIDC elevation state does not match the current session")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		writeErrorSimple(w, http.StatusBadRequest, "OIDC_CODE_MISSING", "Missing authorization code")
		return
	}

	tok, err := s.oidc.Exchange(r.Context(), code)
	if err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "OIDC_EXCHANGE_FAILED",
			"Failed to exchange authorization code")
		return
	}

	rawIDToken, err := auth.IDTokenFromOAuth2(tok)
	if err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "OIDC_ID_TOKEN_MISSING",
			"Provider response missing id_token")
		return
	}

	_, authTime, err := s.oidc.VerifyIDTokenWithAuthTime(r.Context(), rawIDToken, entry.Nonce)
	if err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "OIDC_ID_TOKEN_INVALID",
			"ID token verification failed")
		return
	}

	// Freshness: the IdP MUST have re-authenticated the user; we use
	// auth_time within `oidcElevationAuthTimeFreshness` of now as the
	// proof. If the IdP returned an old auth_time (i.e. it ignored
	// max_age=0 and reused a cached session) we reject with 401.
	now := nowFunc()
	if authTime <= 0 || now.Unix()-authTime > int64(oidcElevationAuthTimeFreshness.Seconds()) {
		s.audit.Log(audit.Event{
			Time:      now.UTC(),
			Actor:     claims.UserID,
			ActorRole: string(currentMode(r)),
			Action:    "auth:elevate_oidc_stale",
			Resource:  "mode:" + string(entry.TargetMode),
			Detail:    "auth_time stale or missing — IdP did not perform a fresh login",
			Result:    audit.ResultFailure,
			IP:        clientIP(r),
			UserAgent: r.UserAgent(),
		})
		writeErrorSimple(w, http.StatusUnauthorized, "OIDC_AUTH_TIME_STALE",
			"OIDC re-authentication was not fresh enough; the IdP returned a cached session.")
		return
	}

	// Mint the elevated cookie. Same TTL ladder as the password path.
	var ttl time.Duration
	switch entry.TargetMode {
	case policy.ModeAdmin:
		ttl = adminModeTTL
	case policy.ModeElevated:
		ttl = elevatedModeTTL
	}

	sessionTTL := s.cfg.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = 24 * time.Hour
	}

	modeExpiresAt := now.Add(ttl).Unix()

	token, err := auth.IssueTokenWithMode(
		s.cfg.JWT.Secret,
		claims.UserID,
		claims.Role,
		claims.UIAdmin,
		string(entry.TargetMode),
		modeExpiresAt,
		sessionTTL,
	)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "TOKEN_ISSUE",
			"Failed to issue session token")
		return
	}

	auth.SetSessionCookie(w, token, sessionTTL)

	s.audit.Log(audit.Event{
		Time:      now.UTC(),
		Actor:     claims.UserID,
		ActorRole: string(entry.TargetMode),
		Action:    "auth:elevate_oidc_success",
		Resource:  "mode:" + string(entry.TargetMode),
		Detail:    "OIDC re-auth fresh; elevated to " + string(entry.TargetMode),
		Result:    audit.ResultSuccess,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	})

	http.Redirect(w, r, oidcElevationCallbackPath+"?elevated="+string(entry.TargetMode), http.StatusFound)
}

// ensureOIDCElevationStore lazily allocates the state map. The map is
// kept on the Server (not as a package var) so tests get an isolated
// instance per *Server and concurrent test goroutines never collide.
func (s *Server) ensureOIDCElevationStore() *oidcElevationStateStore {
	s.oidcElevMu.Lock()
	defer s.oidcElevMu.Unlock()
	if s.oidcElevState == nil {
		s.oidcElevState = newOIDCElevationStateStore()
	}
	return s.oidcElevState
}
