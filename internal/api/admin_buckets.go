package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mattjackson/basement/internal/driver"
)

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

// deleteBucketHandler handles DELETE /admin/buckets/{id}.
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

	if err := s.drv.DeleteBucket(r.Context(), id); err != nil {
		writeDriverError(w, "DeleteBucket", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Bucket deleted"})
}
