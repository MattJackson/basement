package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/driver"
)

// userListClustersHandler handles GET /api/v1/user/clusters.
//
// Returns the connections the calling user has any grant on, plus any
// connections they own (e.g. user-added buckets via the v0.9.0e flow).
// UI-Admin / role=admin callers continue to see every connection so
// matthew's existing browsing flow on basement.pq.io stays intact.
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
//
// Per ADR-0001 v0.9.0f: returns ONLY the buckets the calling user has
// a BucketGrant for on this cluster — NOT the full cluster bucket list
// any more. The previous semantics (list all + filter via the legacy
// grants table) leaked bucket names to users who shouldn't see them.
//
// Each entry is constructed from the BucketGrant alone (id only, no
// size / object count) because the per-user S3 key may not have
// permission to call GetBucket on every grant in one fan-out. Detail
// for a single bucket is loaded lazily by the bucket-detail handler.
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

	claims, ok := auth.FromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	// Admin users (role=admin) keep the legacy full-cluster view —
	// matthew's existing "browse every bucket as host admin" flow on
	// basement.pq.io must not regress when v0.9.0f's capability gates
	// land. Non-admins get the grant-filtered view.
	if claims.Role == "admin" {
		drv, err := s.reg.For(r.Context(), cid)
		if err != nil {
			writeRegistryForError(w, err)
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
		writeJSON(w, http.StatusOK, buckets)
		return
	}

	if s.store == nil || s.store.CredGrants() == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "GRANTS_NOT_WIRED",
			"Credential-grant store is not configured on this deployment.")
		return
	}

	grants, err := s.store.CredGrants().ListForUser(r.Context(), claims.UserID)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to list grants")
		return
	}

	out := []driver.Bucket{}
	for _, g := range grants {
		if g.ConnectionID != cid {
			continue
		}
		out = append(out, driver.Bucket{
			ID:      g.BucketID,
			Aliases: []string{g.BucketID},
		})
	}

	writeJSON(w, http.StatusOK, out)
}

// userGetClusterHandler handles GET /api/v1/user/clusters/{cid}.
//
// Keeps the legacy any-grant-on-this-cluster check so the cluster
// metadata page works for users who have at least one bucket grant
// there. Cluster admin/edit ops live on /admin/* and run their own
// capability gates.
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
		writeRegistryForError(w, err)
		return
	}

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
//
// Per ADR-0001 v0.9.0f: requires bucket:view on bucket:{cid}:{bid},
// then asks the per-user S3 client for the bucket detail so audit
// logs attribute the GetBucket call to the user's key.
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

	userID, ok := s.requireCapability(w, r, "bucket:view", scopeBucket(cid, bid))
	if !ok {
		return
	}

	// UIAdmin / role=admin can fall back to the cluster driver — the
	// legacy admin browsing flow doesn't require a per-bucket grant.
	// Non-admins always go through the grant-driver path.
	claims, _ := auth.FromContext(r.Context())
	if claims != nil && claims.Role == "admin" {
		drv, err := s.reg.For(r.Context(), cid)
		if err != nil {
			writeRegistryForError(w, err)
			return
		}
		bucket, err := drv.GetBucket(r.Context(), bid)
		if err != nil {
			writeDriverError(w, "GetBucket", err)
			return
		}
		writeJSON(w, http.StatusOK, bucket)
		return
	}

	drv, err := s.userGrantDriver(r.Context(), userID, cid, bid)
	if err != nil {
		if errors.Is(err, ErrNoGrant) {
			writeNoGrant(w)
			return
		}
		writeGrantInternalError(w, err)
		return
	}

	bucket, err := drv.GetBucket(r.Context(), bid)
	if err != nil {
		writeDriverError(w, "GetBucket", err)
		return
	}

	writeJSON(w, http.StatusOK, bucket)
}
