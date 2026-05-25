// Package api: per-cluster envelope encryption (CSK) HTTP surface
// (v1.12.0a / ADR-0007).
//
// Five endpoints wire the clustersecret manager to the FE:
//
//	POST   /api/v1/admin/clusters/{cid}/unlock          {password}
//	POST   /api/v1/admin/clusters/{cid}/lock
//	GET    /api/v1/admin/clusters/{cid}/lock-status
//	POST   /api/v1/admin/clusters/{cid}/admins          {adminUserId,password}
//	DELETE /api/v1/admin/clusters/{cid}/admins/{adminUserId}
//
// Everything is gated on cluster:edit at scopeCluster(cid) — the same
// capability that already governs "change cluster config" is what
// governs "manage the secret that protects that config". A
// future cycle may split this into a dedicated cluster:csk_* gate; for
// v1.12.0a one capability keeps the matrix from growing for what is
// already a tightly-scoped permission.
//
// Any other endpoint that needs to decrypt a stored secret uses
// requireUnlocked(w, r, cid) at the start of the handler — that helper
// returns 423 LOCKED with {cluster_id, hint} when the cluster's CSK is
// not in memory, which the FE intercepts to present the unlock modal.
//
// Audit events emitted by this file:
//
//	cluster:csk_first_admin_bootstrapped
//	cluster:csk_unlocked
//	cluster:csk_locked
//	cluster:csk_admin_added
//	cluster:csk_admin_removed
//	cluster:csk_migrated  (emitted from the migration helper, not here)
//
// CSK plaintext, passwords, and wrapping keys never appear in audit
// fields per ADR-0007's hard constraints.

package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/clustersecret"
)

// SetClusterSecrets wires the clustersecret manager into the server.
// Production main.go always supplies a non-nil manager backed by
// internal/clustersecret.NewFileStore({dataDir}). Tests that don't care
// about the CSK surface leave this unset; handlers return 503
// CLUSTER_SECRETS_NOT_WIRED so misconfigured deploys surface clearly.
//
// MUST be called before Start in production.
func (s *Server) SetClusterSecrets(m *clustersecret.ClusterSecretManager) {
	s.clusterSecrets = m
}

// requireClusterSecrets short-circuits with 503 when the manager isn't
// wired. Centralised so every handler in this file shares one nil-check.
func (s *Server) requireClusterSecrets(w http.ResponseWriter) bool {
	if s.clusterSecrets == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "CLUSTER_SECRETS_NOT_WIRED",
			"Cluster secret manager is not configured on this deployment.")
		return false
	}
	return true
}

// requireUnlocked is the gate every secret-touching handler outside
// this file should call. Returns true when the cluster's CSK is in
// memory; otherwise writes 423 LOCKED + structured body and returns
// false. The FE intercepts 423 and pops the unlock modal.
//
// Safe to call when the manager isn't wired — falls through to true
// (legacy behaviour) so handlers added before SetClusterSecrets is
// part of every deploy don't suddenly require it. Once every deploy
// wires the manager, callers can rely on a non-nil manager being a
// hard precondition.
func (s *Server) requireUnlocked(w http.ResponseWriter, cid string) bool {
	if s.clusterSecrets == nil {
		return true
	}
	if s.clusterSecrets.IsUnlocked(cid) {
		return true
	}
	writeError(w, http.StatusLocked, "LOCKED",
		"Cluster is locked; unlock with cluster admin password to proceed.",
		map[string]any{
			"cluster_id": cid,
			"hint":       "POST /api/v1/admin/clusters/{cid}/unlock with {password}",
		})
	return false
}

// unlockClusterHandler handles POST /admin/clusters/{cid}/unlock.
//
// Body: {password: string}. Empty password or bad JSON → 400.
// Wrong password → 401 INVALID_PASSWORD (deliberately not 403 so the
// FE can distinguish "your password is wrong" from "you don't have
// the cluster:edit capability"). No-admins → 404 NO_CSK_ADMIN (the
// cluster never had CSK enabled; FE may offer "set up encryption").
//
// Side effects on success:
//   - Cluster's CSK is cached in memory until Lock / restart.
//   - audit event cluster:csk_unlocked.
//   - If the cluster's Connection record still carries a legacy
//     JWT-encrypted admin_token (ConfigEnc populated, set by v1.0.0a),
//     it's migrated to CSK encryption on the way out. The migration
//     never blocks the response — failure logs + leaves the legacy
//     blob in place for the next unlock retry.
func (s *Server) unlockClusterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}
	cid := chi.URLParam(r, "cid")
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id required")
		return
	}
	if _, ok := s.requireCapability(w, r, "cluster:edit", scopeCluster(cid)); !ok {
		return
	}
	if !s.requireClusterSecrets(w) {
		return
	}

	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", "invalid request body", nil)
		return
	}
	if body.Password == "" {
		writeError(w, http.StatusBadRequest, "INVALID", "password required", nil)
		return
	}

	// Confirm the cluster exists before doing Argon2id work — saves
	// 100ms of CPU on a typo'd cid and keeps the error surface aligned
	// with the rest of /admin/clusters/{cid}/*.
	if _, err := s.conns.Get(r.Context(), cid); err != nil {
		writeRegistryForError(w, err)
		return
	}

	if err := s.clusterSecrets.Unlock(cid, body.Password); err != nil {
		switch {
		case errors.Is(err, clustersecret.ErrInvalidPassword):
			s.auditFailureDetail(r, "cluster:csk_unlocked", resourceCluster(cid), "invalid password")
			writeError(w, http.StatusUnauthorized, "INVALID_PASSWORD", "Wrong password.", nil)
		case errors.Is(err, clustersecret.ErrNoWrappedCSK):
			writeError(w, http.StatusNotFound, "NO_CSK_ADMIN",
				"Cluster has no CSK admin yet — call POST /admins with the first admin's password to bootstrap.",
				map[string]any{"cluster_id": cid})
		default:
			writeErrorSimple(w, http.StatusInternalServerError, "UNLOCK_FAILED", err.Error())
		}
		return
	}

	// Successful unlock — try the lazy admin_token migration. Never
	// block the response on this; logging + leaving the legacy blob
	// in place is fine for the next unlock to retry.
	if migrated, err := s.maybeMigrateLegacyClusterSecret(r, cid); err != nil {
		s.logger.Warn("cluster CSK migration failed; legacy ciphertext remains in place",
			"cluster_id", cid, "error", err)
	} else if migrated {
		s.auditSuccess(r, "cluster:csk_migrated", resourceCluster(cid))
	}

	s.auditSuccess(r, "cluster:csk_unlocked", resourceCluster(cid))
	writeJSON(w, http.StatusOK, map[string]any{"unlocked": true})
}

// lockClusterHandler handles POST /admin/clusters/{cid}/lock.
//
// Idempotent: locking an already-locked cluster returns 204 just like
// the first lock. No body, no parameters beyond cid.
func (s *Server) lockClusterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}
	cid := chi.URLParam(r, "cid")
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id required")
		return
	}
	if _, ok := s.requireCapability(w, r, "cluster:edit", scopeCluster(cid)); !ok {
		return
	}
	if !s.requireClusterSecrets(w) {
		return
	}

	s.clusterSecrets.Lock(cid)
	s.auditSuccess(r, "cluster:csk_locked", resourceCluster(cid))
	w.WriteHeader(http.StatusNoContent)
}

// lockStatusResponse is the GET /lock-status payload.
//
// requiresMigration is true when the cluster's Connection record still
// carries a legacy v1.0.0a JWT-encrypted ConfigEnc that the next unlock
// will migrate to CSK. The FE surfaces it as a one-line banner so the
// operator knows the bootstrap unlock will do extra work.
type lockStatusResponse struct {
	Unlocked          bool     `json:"unlocked"`
	HasCSK            bool     `json:"hasCsk"`
	RequiresMigration bool     `json:"requiresMigration"`
	Admins            []string `json:"admins"`
}

// lockStatusHandler handles GET /admin/clusters/{cid}/lock-status.
//
// Lightweight — no Argon2id work. The FE polls this on the cluster
// detail page to refresh the lock-state badge.
func (s *Server) lockStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}
	cid := chi.URLParam(r, "cid")
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id required")
		return
	}
	// Reading lock-status uses the broader cluster:test gate (anyone
	// who can probe the cluster's health can see its lock-state). This
	// keeps the cluster detail page available to viewers who can't
	// otherwise mutate the cluster.
	if _, ok := s.requireCapability(w, r, "cluster:test", scopeCluster(cid)); !ok {
		return
	}
	if !s.requireClusterSecrets(w) {
		return
	}

	hasCSK, err := s.clusterSecrets.HasAdmins(cid)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}
	admins, err := s.clusterSecrets.ListAdmins(cid)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}
	if admins == nil {
		admins = []string{}
	}

	requiresMigration, err := s.clusterRequiresLegacyMigration(r, cid)
	if err != nil {
		// Migration check failures aren't fatal for the status read;
		// log + report false so the FE doesn't dead-lock on a stuck
		// banner. The next unlock attempt will surface the real issue.
		s.logger.Warn("lock-status: legacy migration check failed",
			"cluster_id", cid, "error", err)
		requiresMigration = false
	}

	writeJSON(w, http.StatusOK, lockStatusResponse{
		Unlocked:          s.clusterSecrets.IsUnlocked(cid),
		HasCSK:            hasCSK,
		RequiresMigration: requiresMigration,
		Admins:            admins,
	})
}

// addAdminHandler handles POST /admin/clusters/{cid}/admins.
//
// Body: {adminUserId: string, password: string}.
//
// First-admin bootstrap: when the cluster has zero existing wrappedCSK
// records, this call creates the CSK and wraps it under the supplied
// admin's password. The cluster is unlocked on return so the caller
// can immediately use it.
//
// Subsequent admin: requires the cluster to already be unlocked (the
// in-memory CSK is wrapped under the new admin's password). 423 LOCKED
// if not unlocked.
func (s *Server) addAdminHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}
	cid := chi.URLParam(r, "cid")
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id required")
		return
	}
	if _, ok := s.requireCapability(w, r, "cluster:edit", scopeCluster(cid)); !ok {
		return
	}
	if !s.requireClusterSecrets(w) {
		return
	}

	var body struct {
		AdminUserID string `json:"adminUserId"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", "invalid request body", nil)
		return
	}
	if body.AdminUserID == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "INVALID",
			"adminUserId and password are both required", nil)
		return
	}

	if _, err := s.conns.Get(r.Context(), cid); err != nil {
		writeRegistryForError(w, err)
		return
	}

	hasAdmins, err := s.clusterSecrets.HasAdmins(cid)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}

	if !hasAdmins {
		// First-admin bootstrap path: generate a fresh CSK + cache it.
		if err := s.clusterSecrets.BootstrapFirstAdmin(cid, body.AdminUserID, body.Password); err != nil {
			s.auditFailureDetail(r, "cluster:csk_first_admin_bootstrapped",
				resourceCluster(cid), err.Error())
			writeErrorSimple(w, http.StatusInternalServerError, "BOOTSTRAP_FAILED", err.Error())
			return
		}
		s.auditSuccess(r, "cluster:csk_first_admin_bootstrapped", resourceCluster(cid))
		writeJSON(w, http.StatusCreated, map[string]any{
			"bootstrap": true,
			"unlocked":  true,
			"adminId":   body.AdminUserID,
		})
		return
	}

	// Subsequent admin — must be unlocked already.
	if !s.requireUnlocked(w, cid) {
		return
	}

	if err := s.clusterSecrets.AddAdmin(cid, body.AdminUserID, body.Password); err != nil {
		switch {
		case errors.Is(err, clustersecret.ErrAdminAlreadyExists):
			writeError(w, http.StatusConflict, "ADMIN_ALREADY_EXISTS",
				"An admin with this user ID already holds a wrappedCSK; remove first to rotate.",
				map[string]any{"adminUserId": body.AdminUserID})
		case errors.Is(err, clustersecret.ErrLocked):
			// Race: someone locked between requireUnlocked + AddAdmin.
			writeError(w, http.StatusLocked, "LOCKED",
				"Cluster locked between gate and write; unlock and retry.",
				map[string]any{"cluster_id": cid})
		default:
			s.auditFailureDetail(r, "cluster:csk_admin_added", resourceCluster(cid), err.Error())
			writeErrorSimple(w, http.StatusInternalServerError, "ADD_ADMIN_FAILED", err.Error())
		}
		return
	}

	s.auditSuccess(r, "cluster:csk_admin_added", resourceCluster(cid))
	writeJSON(w, http.StatusCreated, map[string]any{
		"bootstrap": false,
		"adminId":   body.AdminUserID,
	})
}

// removeAdminHandler handles DELETE /admin/clusters/{cid}/admins/{adminUserId}.
//
// Removing the last admin while the cluster is currently unlocked is
// allowed — the in-memory CSK still works for the process lifetime,
// but a restart leaves no path back. Caller is responsible for warning
// the operator (FE confirms before sending the DELETE).
func (s *Server) removeAdminHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "DELETE required")
		return
	}
	cid := chi.URLParam(r, "cid")
	adminUserID := chi.URLParam(r, "adminUserId")
	if cid == "" || adminUserID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID",
			"cluster id and admin user id required")
		return
	}
	if _, ok := s.requireCapability(w, r, "cluster:edit", scopeCluster(cid)); !ok {
		return
	}
	if !s.requireClusterSecrets(w) {
		return
	}

	if err := s.clusterSecrets.RemoveAdmin(cid, adminUserID); err != nil {
		s.auditFailureDetail(r, "cluster:csk_admin_removed",
			resourceCluster(cid)+":"+adminUserID, err.Error())
		writeErrorSimple(w, http.StatusInternalServerError, "REMOVE_ADMIN_FAILED", err.Error())
		return
	}

	s.auditSuccess(r, "cluster:csk_admin_removed",
		resourceCluster(cid)+":"+adminUserID)
	w.WriteHeader(http.StatusNoContent)
}

// ─── migration helpers ──────────────────────────────────────────────

// maybeMigrateLegacyClusterSecret re-encrypts the cluster's
// JWT-encrypted ConfigEnc (v1.0.0a) under the CSK and persists the
// result via store.Connections.SwapClusterSecret (v1.12.0b /
// ADR-0007). Idempotent:
//
//   - Connection has no legacy ConfigEnc → no-op (nothing to migrate).
//   - Connection already carries a ConfigEncCSK that matches the
//     freshly re-encrypted value → no-op via SwapClusterSecret's
//     bytes-equal guard (a concurrent unlock raced and won).
//   - Cluster is locked when called → ErrLocked from the manager,
//     surfaced as an error the caller logs but doesn't fail on.
//
// Safety: the legacy ConfigEnc is NEVER mutated. The CSK-encrypted
// v2.0.0-beta.2: This function was removed — legacy JWT-encrypted credentials
	// are no longer supported. Clusters with ConfigEnc but no ConfigEncCSK are
	// dropped on boot per [[v2_clean_break]]. Returns (migrated, error) where
	// migrated is always false and there is nothing to migrate.
	func (s *Server) maybeMigrateLegacyClusterSecret(r *http.Request, cid string) (bool, error) {
		// Legacy migration removed in v2.0.0-beta.2 — no-op for API compat.
		return false, nil
	}

// clusterRequiresLegacyMigration reports whether the cluster's
// Connection still carries a legacy JWT-encrypted ConfigEnc with no
// CSK parallel yet — i.e. the next successful unlock will run the
// v1.12.0b migration. The /lock-status handler surfaces this so the
// FE can render a "first unlock will migrate" banner.
//
// Returns false in the no-conns and no-legacy cases; never errors on
// a missing cluster (the status handler converts a get-failure into
// "migration not required" rather than surfacing a stuck banner).
func (s *Server) clusterRequiresLegacyMigration(r *http.Request, cid string) (bool, error) {
	if s.conns == nil {
		return false, nil
	}
	conn, err := s.conns.Get(r.Context(), cid)
	if err != nil {
		return false, err
	}
	// Migration is needed when there IS a legacy blob AND there is
	// no CSK parallel yet. Once ConfigEncCSK is populated the bridge
	// has been crossed (even if ConfigEnc stays around as a safety
	// fallback until the future cycle that retires it).
	return len(conn.ConfigEnc) > 0 && len(conn.ConfigEncCSK) == 0, nil
}

// requireCapabilityCallerID is a thin wrapper that returns the caller's
// userID after a capability gate without short-circuiting the response.
// Used by handlers that want both the gate and the userID for audit /
// downstream calls. Not currently used by the CSK handlers but exposed
// so future code (e.g. attribute-bootstrap-to-caller) can attribute
// the action to claims.UserID rather than the supplied adminUserId.
func (s *Server) requireCapabilityCallerID(r *http.Request) string {
	if claims, ok := auth.FromContext(r.Context()); ok && claims != nil {
		return claims.UserID
	}
	return ""
}
