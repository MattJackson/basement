package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mattjackson/basement/internal/driver"
)

// userListClustersHandler handles GET /api/v1/user/clusters.
func (s *Server) userListClustersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	visibleConnIDs := s.userVisibleConnections(r.Context())
	if visibleConnIDs == nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to list connections")
		return
	}

	allConns, err := s.conns.List(r.Context())
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to list connections")
		return
	}

	filtered := []interface{}{}
	for _, conn := range allConns {
		for _, id := range visibleConnIDs {
			if conn.ID == id || s.userOwnsConnection(r.Context(), conn.ID) {
				filtered = append(filtered, conn)
				break
			}
		}
	}

	writeJSON(w, http.StatusOK, filtered)
}

// userListClusterBucketsHandler handles GET /api/v1/user/clusters/{cid}/buckets.
func (s *Server) userListClusterBucketsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	cid := chi.URLParam(r, "cid")
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id required")
		return
	}

	drv, err := s.reg.For(r.Context(), cid)
	if err != nil {
		writeErrorSimple(w, http.StatusNotFound, "CLUSTER_NOT_FOUND", "Connection not found")
		return
	}

	buckets, err := drv.ListBuckets(r.Context())
	if err != nil {
		writeDriverError(w, "ListBuckets", err)
		return
	}

	if buckets == nil {
		buckets = []driver.Bucket{}
	}

	// Filter by user grants.
	visibleBucketIDs := s.userVisibleBuckets(r.Context(), cid)
	filtered := []interface{}{}
	for _, bucket := range buckets {
		for _, id := range visibleBucketIDs {
			if bucket.ID == id {
				filtered = append(filtered, bucket)
				break
			}
		}
	}

	writeJSON(w, http.StatusOK, filtered)
}

// userGetClusterHandler handles GET /api/v1/user/clusters/{cid}.
func (s *Server) userGetClusterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	cid := chi.URLParam(r, "cid")
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id required")
		return
	}

	conn, err := s.conns.Get(r.Context(), cid)
	if err != nil {
		writeErrorSimple(w, http.StatusNotFound, "CLUSTER_NOT_FOUND", "Connection not found")
		return
	}

	// Check if user has any grant on this cluster or owns it.
	visibleConnIDs := s.userVisibleConnections(r.Context())
	if visibleConnIDs == nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to list connections")
		return
	}

	hasGrant := false
	for _, id := range visibleConnIDs {
		if conn.ID == id {
			hasGrant = true
			break
		}
	}

	if !hasGrant && !s.userOwnsConnection(r.Context(), cid) {
		writeErrorSimple(w, http.StatusForbidden, "FORBIDDEN", "User does not have access to this cluster")
		return
	}

	writeJSON(w, http.StatusOK, conn)
}

// userGetClusterBucketHandler handles GET /api/v1/user/clusters/{cid}/buckets/{bid}.
func (s *Server) userGetClusterBucketHandler(w http.ResponseWriter, r *http.Request) {
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

	bucket, err := drv.GetBucket(r.Context(), bid)
	if err != nil {
		writeDriverError(w, "GetBucket", err)
		return
	}

	// Check if user has grant on this bucket.
	visibleBucketIDs := s.userVisibleBuckets(r.Context(), cid)
	hasGrant := false
	for _, id := range visibleBucketIDs {
		if bid == id {
			hasGrant = true
			break
		}
	}

	if !hasGrant && !s.userOwnsConnection(r.Context(), cid) {
		writeErrorSimple(w, http.StatusForbidden, "FORBIDDEN", "User does not have access to this bucket")
		return
	}

	writeJSON(w, http.StatusOK, bucket)
}
