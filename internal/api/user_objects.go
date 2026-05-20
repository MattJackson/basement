package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mattjackson/basement/internal/driver"
)

// userListClusterBucketObjectsHandler handles GET /api/v1/user/clusters/{cid}/buckets/{bid}/objects.
func (s *Server) userListClusterBucketObjectsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	cid := chi.URLParam(r, "cid")
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id required")
		return
	}

	bid := chi.URLParam(r, "bid")
	if bid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "bucket id required")
		return
	}

	drv, err := s.reg.For(r.Context(), cid)
	if err != nil {
		writeErrorSimple(w, http.StatusNotFound, "CLUSTER_NOT_FOUND", "Connection not found")
		return
	}

	prefix := r.URL.Query().Get("prefix")
	token := r.URL.Query().Get("token")
	limitStr := r.URL.Query().Get("limit")

	limit := 100 // default
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	page, err := drv.ListObjects(r.Context(), bid, prefix, token, limit)
	if err != nil {
		writeDriverError(w, "ListObjects", err)
		return
	}

	// For matthew (UIAdmin), check rolls through directly.
	// TODO: In future cycles, intersect with user's bucket grant.
	if page.Objects == nil {
		page.Objects = []driver.ObjectInfo{}
	}
	if page.Prefixes == nil {
		page.Prefixes = []string{}
	}

	writeJSON(w, http.StatusOK, page)
}

// userStatClusterBucketObjectHandler handles GET /api/v1/user/clusters/{cid}/buckets/{bid}/objects/{key+}/stat.
func (s *Server) userStatClusterBucketObjectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	cid := chi.URLParam(r, "cid")
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id required")
		return
	}

	bid := chi.URLParam(r, "bid")
	if bid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "bucket id required")
		return
	}

	key := chi.URLParam(r, "key")
	if key == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "object key required")
		return
	}

	drv, err := s.reg.For(r.Context(), cid)
	if err != nil {
		writeErrorSimple(w, http.StatusNotFound, "CLUSTER_NOT_FOUND", "Connection not found")
		return
	}

	obj, err := drv.StatObject(r.Context(), bid, key)
	if err != nil {
		writeDriverError(w, "StatObject", err)
		return
	}

	writeJSON(w, http.StatusOK, obj)
}

// userPresignGetClusterBucketObjectHandler handles POST /api/v1/user/clusters/{cid}/buckets/{bid}/objects/{key+}/presign-get.
func (s *Server) userPresignGetClusterBucketObjectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	cid := chi.URLParam(r, "cid")
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id required")
		return
	}

	bid := chi.URLParam(r, "bid")
	if bid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "bucket id required")
		return
	}

	key := chi.URLParam(r, "key")
	if key == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "object key required")
		return
	}

	ttlStr := r.URL.Query().Get("ttl")
	ttl := 3600 * time.Second // default 1 hour
	if ttlStr != "" {
		if parsed, err := strconv.Atoi(ttlStr); err == nil && parsed > 0 {
			ttl = time.Duration(parsed) * time.Second
		}
	}

	// Enforce max TTL of 86400s (24 hours).
	maxTtl := 86400 * time.Second
	if ttl > maxTtl {
		ttl = maxTtl
	}

	drv, err := s.reg.For(r.Context(), cid)
	if err != nil {
		writeErrorSimple(w, http.StatusNotFound, "CLUSTER_NOT_FOUND", "Connection not found")
		return
	}

	presign, err := drv.PresignGet(r.Context(), bid, key, ttl)
	if err != nil {
		writeDriverError(w, "PresignGet", err)
		return
	}

	writeJSON(w, http.StatusOK, presign)
}
