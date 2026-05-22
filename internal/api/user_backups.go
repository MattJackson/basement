// Package api: user-persona backup endpoints (v1.5.0a).
//
// Backups promote the v0.8.x sync engine into a scheduled, recurring
// concern. Every handler here writes to a backup.Backups store and
// notifies the in-memory backup.Scheduler so cron entries stay in
// sync with disk state. The actual object-copy work is delegated to
// the existing sync engine through the runner wired in
// server.SetBackupScheduler.
//
// Authorization: identical pattern to user_syncs.go — handlers
// require the JWT auth middleware (mounted under userG in server.go)
// and additionally check OwnerUserID == claims.UserID for any
// mutation on a specific record. Visibility tests use 404 instead of
// 403 so the existence of other users' backups never leaks.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/backup"
)

// userBackupCreateRequest is the wire shape for POST /user/backups.
// Field names mirror UserSyncCreateRequest so the wizard can reuse
// its region/bucket picker components.
type userBackupCreateRequest struct {
	Name        string `json:"name"`
	SrcRegionID string `json:"srcRegionId"`
	SrcBucket   string `json:"srcBucket"`
	SrcPrefix   string `json:"srcPrefix,omitempty"`
	DstRegionID string `json:"dstRegionId"`
	DstBucket   string `json:"dstBucket"`
	DstPrefix   string `json:"dstPrefix,omitempty"`
	Schedule    string `json:"schedule"`
	Disabled    bool   `json:"disabled,omitempty"`
}

// validateBackupRequest returns the first user-visible reason a
// create / update body should be rejected, or "" if everything looks
// well-formed. We deliberately keep this string-shaped (rather than
// typed errors) because the API surface only ever needs to render
// the message inline in the wizard.
func validateBackupRequest(req userBackupCreateRequest) (code, msg string) {
	if strings.TrimSpace(req.Name) == "" {
		return "INVALID_NAME", "Name is required"
	}
	if req.SrcRegionID == "" || req.SrcBucket == "" {
		return "INVALID_SOURCE", "Source region and bucket are required"
	}
	if req.DstRegionID == "" || req.DstBucket == "" {
		return "INVALID_DESTINATION", "Destination region and bucket are required"
	}
	if req.Schedule == "" {
		return "INVALID_SCHEDULE", "Schedule is required (use \"manual\" for run-on-demand only)"
	}
	return "", ""
}

// userCreateBackupHandler handles POST /api/v1/user/backups.
func (s *Server) userCreateBackupHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}
	if s.backups == nil || s.backupSched == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "BACKUPS_NOT_WIRED", "Backup subsystem is not enabled on this server")
		return
	}

	var req userBackupCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}
	if code, msg := validateBackupRequest(req); code != "" {
		writeErrorSimple(w, http.StatusBadRequest, code, msg)
		return
	}

	// Validate the cron expression eagerly so an operator can't save
	// a backup with a typo and only discover the breakage when the
	// scheduled time comes around. Scheduler.Add re-validates on its
	// own path (no need for a separate parser exposure on the public
	// API) — we just attempt a dry-run Add against a zero-value
	// Backup to surface the parser error in a 400.
	if req.Schedule != backup.ScheduleManual {
		if err := s.backupSched.Add(backup.Backup{ID: "__dryrun__", Schedule: req.Schedule}); err != nil {
			s.backupSched.Remove("__dryrun__")
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_SCHEDULE", err.Error())
			return
		}
		// Drop the dry-run entry — the real one is added below
		// after the store persists the record.
		s.backupSched.Remove("__dryrun__")
	}

	b := backup.Backup{
		OwnerUserID: claims.UserID,
		Name:        strings.TrimSpace(req.Name),
		SrcRegionID: req.SrcRegionID,
		SrcBucket:   req.SrcBucket,
		SrcPrefix:   req.SrcPrefix,
		DstRegionID: req.DstRegionID,
		DstBucket:   req.DstBucket,
		DstPrefix:   req.DstPrefix,
		Schedule:    req.Schedule,
		Disabled:    req.Disabled,
	}
	created, err := s.backups.Create(r.Context(), b)
	if err != nil {
		s.auditFailure(r, "backup:create", resourceBackup(""), err)
		writeErrorSimple(w, http.StatusInternalServerError, "BACKUP_STORE_ERROR", "Failed to save backup")
		return
	}
	if err := s.backupSched.Add(created); err != nil {
		// Store succeeded but cron registration failed — log and
		// surface a partial success: the backup exists, but won't
		// fire automatically until the next reschedule attempt.
		s.logger.Warn("backup:create scheduled registration failed",
			"backupId", created.ID, "schedule", created.Schedule, "error", err)
	}
	s.auditSuccess(r, "backup:create", resourceBackup(created.ID))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(created)
}

// userListBackupsHandler handles GET /api/v1/user/backups.
func (s *Server) userListBackupsHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}
	if s.backups == nil {
		// Empty list is the safe degraded response — clients render
		// "no backups" rather than a 5xx, and the operator sees the
		// missing-wiring warning in the server logs at boot.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]backup.Backup{})
		return
	}
	rows, err := s.backups.ListForUser(r.Context(), claims.UserID)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "BACKUP_STORE_ERROR", "Failed to list backups")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}

// userGetBackupHandler handles GET /api/v1/user/backups/{id}.
func (s *Server) userGetBackupHandler(w http.ResponseWriter, r *http.Request) {
	b, ok := s.loadOwnedBackup(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(b)
}

// userUpdateBackupHandler handles PUT /api/v1/user/backups/{id}.
//
// Treats the request body as a full replacement of the mutable
// fields (Name, src/dst, schedule, Disabled). Identity / history
// fields are preserved by the store. After persisting we tell the
// scheduler to rebuild the cron entry from the new schedule.
func (s *Server) userUpdateBackupHandler(w http.ResponseWriter, r *http.Request) {
	existing, ok := s.loadOwnedBackup(w, r)
	if !ok {
		return
	}

	var req userBackupCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}
	if code, msg := validateBackupRequest(req); code != "" {
		writeErrorSimple(w, http.StatusBadRequest, code, msg)
		return
	}
	if req.Schedule != backup.ScheduleManual {
		if err := s.backupSched.Add(backup.Backup{ID: "__dryrun__", Schedule: req.Schedule}); err != nil {
			s.backupSched.Remove("__dryrun__")
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_SCHEDULE", err.Error())
			return
		}
		s.backupSched.Remove("__dryrun__")
	}

	patch := backup.Backup{
		Name:        strings.TrimSpace(req.Name),
		SrcRegionID: req.SrcRegionID,
		SrcBucket:   req.SrcBucket,
		SrcPrefix:   req.SrcPrefix,
		DstRegionID: req.DstRegionID,
		DstBucket:   req.DstBucket,
		DstPrefix:   req.DstPrefix,
		Schedule:    req.Schedule,
		Disabled:    req.Disabled,
	}
	updated, err := s.backups.Update(r.Context(), existing.ID, patch)
	if err != nil {
		s.auditFailure(r, "backup:update", resourceBackup(existing.ID), err)
		writeErrorSimple(w, http.StatusInternalServerError, "BACKUP_STORE_ERROR", "Failed to update backup")
		return
	}
	if err := s.backupSched.Reschedule(r.Context(), updated.ID); err != nil {
		s.logger.Warn("backup:update reschedule failed",
			"backupId", updated.ID, "error", err)
	}
	s.auditSuccess(r, "backup:update", resourceBackup(updated.ID))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

// userDeleteBackupHandler handles DELETE /api/v1/user/backups/{id}.
func (s *Server) userDeleteBackupHandler(w http.ResponseWriter, r *http.Request) {
	existing, ok := s.loadOwnedBackup(w, r)
	if !ok {
		return
	}
	if err := s.backups.Delete(r.Context(), existing.ID); err != nil {
		s.auditFailure(r, "backup:delete", resourceBackup(existing.ID), err)
		writeErrorSimple(w, http.StatusInternalServerError, "BACKUP_STORE_ERROR", "Failed to delete backup")
		return
	}
	s.backupSched.Remove(existing.ID)
	s.auditSuccess(r, "backup:delete", resourceBackup(existing.ID))
	w.WriteHeader(http.StatusNoContent)
}

// userRunBackupHandler handles POST /api/v1/user/backups/{id}/run.
//
// Kicks off a one-shot run via the scheduler, BYPASSING the cron
// entry. Returns 202 immediately and lets the run finish in the
// background; the result is later visible via GET /backups/{id}.
//
// We require the backup not be Disabled because the contract is
// "I want to copy now": a disabled backup signals the operator
// doesn't want it firing, scheduled or otherwise. They can update
// to Disabled=false first if they really want to run it.
func (s *Server) userRunBackupHandler(w http.ResponseWriter, r *http.Request) {
	existing, ok := s.loadOwnedBackup(w, r)
	if !ok {
		return
	}
	if existing.Disabled {
		writeErrorSimple(w, http.StatusBadRequest, "BACKUP_DISABLED", "Backup is disabled — re-enable it before running")
		return
	}
	if s.backupSched == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "BACKUPS_NOT_WIRED", "Backup subsystem is not enabled on this server")
		return
	}

	s.auditSuccess(r, "backup:run_start", resourceBackup(existing.ID))
	// Detach: a backup run can take minutes; the HTTP timeout is
	// 15s. We use a fresh context.Background so a client disconnect
	// doesn't kill the copy.
	go func(id string) {
		ctx := context.Background()
		if err := s.backupSched.Trigger(ctx, id); err != nil {
			s.logger.Error("backup ad-hoc run failed", "backupId", id, "error", err)
			// Note: the result-recording path inside Trigger
			// already wrote a failure entry into history, so we
			// don't need an additional audit failure event here.
			return
		}
		// best-effort: emit a run_complete audit event with a
		// pared-down resource. We don't have the *http.Request
		// anymore (detached), so use the lightweight Log path
		// directly.
		s.auditEmit(r, "backup:run_complete", resourceBackup(id), "success", "")
	}(existing.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"id":     existing.ID,
		"status": "queued",
	})
}

// loadOwnedBackup is the shared "load + ownership check" used by
// every per-record handler. Returns the Backup + true on success;
// on any failure path it writes the appropriate response and
// returns false, signalling the caller to return.
//
// 404 on either missing OR not-owner so the existence of other
// users' backups never leaks — same convention as user_regions.go
// and user_syncs.go.
func (s *Server) loadOwnedBackup(w http.ResponseWriter, r *http.Request) (backup.Backup, bool) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return backup.Backup{}, false
	}
	if s.backups == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "BACKUPS_NOT_WIRED", "Backup subsystem is not enabled on this server")
		return backup.Backup{}, false
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_ID", "Backup ID required")
		return backup.Backup{}, false
	}
	b, err := s.backups.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, backup.ErrNotFound) {
			writeErrorSimple(w, http.StatusNotFound, "BACKUP_NOT_FOUND", "Backup not found")
			return backup.Backup{}, false
		}
		writeErrorSimple(w, http.StatusInternalServerError, "BACKUP_STORE_ERROR", err.Error())
		return backup.Backup{}, false
	}
	if b.OwnerUserID != claims.UserID {
		// 404 instead of 403 to avoid leaking existence (see
		// user_regions.go header).
		writeErrorSimple(w, http.StatusNotFound, "BACKUP_NOT_FOUND", "Backup not found")
		return backup.Backup{}, false
	}
	return b, true
}

// resourceBackup builds the audit Resource string for backup actions.
// Kept here rather than in audit_helpers.go so the audit_helpers file
// doesn't need to know about the optional backup subsystem — the
// scheme is the same domain:id shape used by every other resource.
func resourceBackup(id string) string { return "backup:" + id }
