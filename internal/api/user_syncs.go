package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/sync"
)

// UserSyncCreateRequest represents the request body for creating a sync job.
type UserSyncCreateRequest struct {
	Mode            string `json:"mode"`
	SrcConnectionID string `json:"srcConnectionId"`
	SrcBucket       string `json:"srcBucket"`
	SrcPrefix       string `json:"srcPrefix,omitempty"`
	DstConnectionID string `json:"dstConnectionId"`
	DstBucket       string `json:"dstBucket"`
	DstPrefix       string `json:"dstPrefix,omitempty"`
}

// userCreateSyncHandler handles POST /api/v1/user/syncs.
func (s *Server) userCreateSyncHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	var req UserSyncCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}

	// Validate mode (v0.8.0d: pull and push supported)
	if req.Mode != "pull" && req.Mode != "push" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_MODE", "Mode must be \"pull\" or \"push\"")
		return
	}

	// Validate source connection exists and user has access
	if _, err := s.conns.Get(r.Context(), req.SrcConnectionID); err != nil {
		writeErrorSimple(w, http.StatusNotFound, "SRC_CLUSTER_NOT_FOUND", "Source cluster not found")
		return
	}

	// Validate destination connection exists and user has access
	if _, err := s.conns.Get(r.Context(), req.DstConnectionID); err != nil {
		writeErrorSimple(w, http.StatusNotFound, "DST_CLUSTER_NOT_FOUND", "Destination cluster not found")
		return
	}

	// Check user grants on both connections (per role-model-three-axes)
	visibleSrcConnIDs := s.userVisibleConnections(r.Context())
	if visibleSrcConnIDs == nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to validate access")
		return
	}

	hasSrcAccess := false
	for _, id := range visibleSrcConnIDs {
		if req.SrcConnectionID == id || s.userOwnsConnection(r.Context(), req.SrcConnectionID) {
			hasSrcAccess = true
			break
		}
	}
	if !hasSrcAccess {
		writeErrorSimple(w, http.StatusForbidden, "FORBIDDEN", "User does not have access to source cluster")
		return
	}

	hasDstAccess := false
	for _, id := range visibleSrcConnIDs {
		if req.DstConnectionID == id || s.userOwnsConnection(r.Context(), req.DstConnectionID) {
			hasDstAccess = true
			break
		}
	}
	if !hasDstAccess {
		writeErrorSimple(w, http.StatusForbidden, "FORBIDDEN", "User does not have access to destination cluster")
		return
	}

	// Create sync job
	job := &sync.SyncJob{
		ID:              sync.GenerateID(),
		OwnerUserID:     claims.UserID,
		Mode:            req.Mode,
		SrcConnectionID: req.SrcConnectionID,
		SrcBucket:       req.SrcBucket,
		SrcPrefix:       req.SrcPrefix,
		DstConnectionID: req.DstConnectionID,
		DstBucket:       req.DstBucket,
		DstPrefix:       req.DstPrefix,
		CreatedAt:       time.Now().UTC(),
		State:           "queued",
	}

	// Save initial job state
	if err := s.syncStore.Save(job); err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "SYNC_STORE_ERROR", "Failed to save sync job")
		return
	}

	// Spawn goroutine to run the sync (async, return 202 immediately)
	go func() {
		ctx := context.Background()

		srcDrv, err := s.reg.For(ctx, req.SrcConnectionID)
		if err != nil {
			s.logger.Error("sync: failed to resolve source driver", "job_id", job.ID, "error", err)
			return
		}

		dstDrv, err := s.reg.For(ctx, req.DstConnectionID)
		if err != nil {
			s.logger.Error("sync: failed to resolve destination driver", "job_id", job.ID, "error", err)
			return
		}

		engine := sync.NewEngine(s.syncStore, 4)
		runErr := engine.Run(ctx, job, srcDrv, dstDrv)
		if runErr != nil && job.State != "paused" {
			s.logger.Error("sync: job failed", "job_id", job.ID, "error", runErr)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      job.ID,
		"state":   job.State,
		"message": "Sync job created and queued",
	})
}

// userListSyncsHandler handles GET /api/v1/user/syncs.
func (s *Server) userListSyncsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	jobs, err := s.syncStore.List(claims.UserID)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "SYNC_STORE_ERROR", "Failed to list sync jobs")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobs)
}

// userGetSyncHandler handles GET /api/v1/user/syncs/{id}.
func (s *Server) userGetSyncHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	jobID := chi.URLParam(r, "id")
	if jobID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_ID", "Sync ID required")
		return
	}

	job, err := s.syncStore.Load(jobID)
	if err != nil {
		writeErrorSimple(w, http.StatusNotFound, "SYNC_NOT_FOUND", "Sync job not found")
		return
	}

	// Verify ownership
	if job.OwnerUserID != claims.UserID {
		writeErrorSimple(w, http.StatusForbidden, "FORBIDDEN", "Access denied to this sync job")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

// userDeleteSyncHandler handles DELETE /api/v1/user/syncs/{id}.
func (s *Server) userDeleteSyncHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "DELETE required")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	jobID := chi.URLParam(r, "id")
	if jobID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_ID", "Sync ID required")
		return
	}

	job, err := s.syncStore.Load(jobID)
	if err != nil {
		writeErrorSimple(w, http.StatusNotFound, "SYNC_NOT_FOUND", "Sync job not found")
		return
	}

	// Verify ownership
	if job.OwnerUserID != claims.UserID {
		writeErrorSimple(w, http.StatusForbidden, "FORBIDDEN", "Access denied to this sync job")
		return
	}

	if err := s.syncStore.Delete(jobID); err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "SYNC_STORE_ERROR", "Failed to delete sync job")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// userPauseSyncHandler handles POST /api/v1/user/syncs/{id}/pause.
func (s *Server) userPauseSyncHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	_, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	jobID := chi.URLParam(r, "id")
	if jobID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_ID", "Sync ID required")
		return
	}

	engine := sync.NewEngine(s.syncStore, 4)
	if err := engine.Pause(r.Context(), jobID); err != nil {
		writeDriverError(w, "pause", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"state": "paused"})
}

// userResumeSyncHandler handles POST /api/v1/user/syncs/{id}/resume.
func (s *Server) userResumeSyncHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	jobID := chi.URLParam(r, "id")
	if jobID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_ID", "Sync ID required")
		return
	}

	job, err := s.syncStore.Load(jobID)
	if err != nil {
		writeErrorSimple(w, http.StatusNotFound, "SYNC_NOT_FOUND", "Sync job not found")
		return
	}

	// Verify ownership
	if job.OwnerUserID != claims.UserID {
		writeErrorSimple(w, http.StatusForbidden, "FORBIDDEN", "Access denied to this sync job")
		return
	}

	ctx := context.Background()

	srcDrv, err := s.reg.For(ctx, job.SrcConnectionID)
	if err != nil {
		writeErrorSimple(w, http.StatusNotFound, "SRC_CLUSTER_NOT_FOUND", "Source cluster not found")
		return
	}

	dstDrv, err := s.reg.For(ctx, job.DstConnectionID)
	if err != nil {
		writeErrorSimple(w, http.StatusNotFound, "DST_CLUSTER_NOT_FOUND", "Destination cluster not found")
		return
	}

	engine := sync.NewEngine(s.syncStore, 4)
	if err := engine.Resume(r.Context(), jobID, srcDrv, dstDrv); err != nil {
		writeDriverError(w, "resume", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"state": "running"})
}
