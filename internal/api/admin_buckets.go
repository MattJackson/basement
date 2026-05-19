package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/driver"
)

// confirmDeleteTTL bounds how long an armed delete token stays valid.
// Short enough that an armed-but-not-fired token aging out forces an
// explicit re-arm; long enough to absorb network jitter between the
// /_arm-delete POST and the DELETE.
const confirmDeleteTTL = 60 * time.Second

const opDeleteBucket = "delete:bucket"

// listBucketsHandler handles GET /api/v1/admin/buckets.
// Calls driver.ListBuckets and returns JSON []Bucket per OpenAPI schema.
func (s *Server) listBucketsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	buckets, err := s.drv.ListBuckets(r.Context())
	if err != nil {
		writeDriverError(w, "ListBuckets", err)
		return
	}

	if buckets == nil {
		buckets = []driver.Bucket{}
	}

	writeJSON(w, http.StatusOK, buckets)
}

// getBucketHandler handles GET /admin/buckets/{id}.
func (s *Server) getBucketHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "bucket id required")
		return
	}

	bucket, err := s.drv.GetBucket(r.Context(), id)
	if err != nil {
		writeDriverError(w, "GetBucket", err)
		return
	}

	writeJSON(w, http.StatusOK, bucket)
}

// createBucketHandler handles POST /admin/buckets.
func (s *Server) createBucketHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	var spec driver.BucketSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", "invalid request body", nil)
		return
	}

	bucket, err := s.drv.CreateBucket(r.Context(), spec)
	if err != nil {
		writeDriverError(w, "CreateBucket", err)
		return
	}

	writeJSON(w, http.StatusCreated, bucket)
}

// updateBucketHandler handles PATCH /admin/buckets/{id}.
func (s *Server) updateBucketHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "PATCH required")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "bucket id required")
		return
	}

	var update driver.BucketUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", "invalid request body", nil)
		return
	}

	bucket, err := s.drv.UpdateBucket(r.Context(), id, update)
	if err != nil {
		writeDriverError(w, "UpdateBucket", err)
		return
	}

	writeJSON(w, http.StatusOK, bucket)
}

// armDeleteBucketHandler handles POST /admin/buckets/{id}/_arm-delete.
// Issues a short-lived HMAC token bound to {bucketID, requester} that
// the matching DELETE must present via X-Confirm-Delete. Two-phase
// arm/fire pattern — no single curl can destroy a bucket.
func (s *Server) armDeleteBucketHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "bucket id required")
		return
	}

	// Confirm the bucket exists before issuing a token. Avoids handing
	// out tokens for nonexistent IDs and surfaces 404 cleanly.
	if _, err := s.drv.GetBucket(r.Context(), id); err != nil {
		writeDriverError(w, "GetBucket", err)
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "SESSION_REQUIRED", "Session required")
		return
	}

	token := auth.MintConfirmToken(s.cfg.JWT.Secret, opDeleteBucket, id, claims.UserID, confirmDeleteTTL)
	writeJSON(w, http.StatusOK, map[string]any{
		"token":            token,
		"expiresInSeconds": int(confirmDeleteTTL.Seconds()),
	})
}

// deleteBucketHandler handles DELETE /admin/buckets/{id}.
//
// Requires X-Confirm-Delete header carrying a token previously minted
// by POST /admin/buckets/{id}/_arm-delete. Token is HMAC-bound to the
// (bucket id, user) pair and expires in 60s, so curl-by-hand is
// two-step and a single leaked URL/path cannot destroy.
func (s *Server) deleteBucketHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "DELETE required")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "bucket id required")
		return
	}

	confirm := r.Header.Get("X-Confirm-Delete")
	if confirm == "" {
		writeErrorSimple(w, http.StatusBadRequest, "CONFIRMATION_REQUIRED",
			"X-Confirm-Delete header required. POST /admin/buckets/{id}/_arm-delete first to obtain a token.")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "SESSION_REQUIRED", "Session required")
		return
	}

	if err := auth.VerifyConfirmToken(s.cfg.JWT.Secret, confirm, opDeleteBucket, id, claims.UserID); err != nil {
		switch {
		case errors.Is(err, auth.ErrConfirmMismatch):
			writeErrorSimple(w, http.StatusBadRequest, "CONFIRMATION_MISMATCH",
				"Token does not match this bucket or user. Re-arm with POST /admin/buckets/{id}/_arm-delete.")
		default:
			writeErrorSimple(w, http.StatusBadRequest, "CONFIRMATION_INVALID",
				"Token invalid or expired. Re-arm with POST /admin/buckets/{id}/_arm-delete.")
		}
		return
	}

	if err := s.drv.DeleteBucket(r.Context(), id); err != nil {
		writeDriverError(w, "DeleteBucket", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Bucket deleted"})
}
