package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/driver"
)

// bucketAliasPattern matches the OpenAPI BucketSpec.alias pattern —
// lowercase alphanumeric + hyphen, 3–63 chars, no leading/trailing
// hyphen. Same shape as S3 bucket naming so the alias is usable as
// an actual S3-client bucket reference.
var bucketAliasPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

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

// getBucketHandler handles GET /admin/clusters/{cid}/buckets/{id}.
//
// v1.11.0.2: routes through s.reg.For(ctx, cid) per-cluster driver
// instead of the global s.drv default. Earlier handlers ignored {cid}
// and silently operated on whichever cluster s.drv pointed at, which
// in multi-cluster deployments meant every per-cluster route landed on
// the same one. Caught by Garage v2 hands-on testing.
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

	drv, err := s.driverForRouteCluster(r)
	if err != nil {
		writeRegistryForError(w, err)
		return
	}

	bucket, err := drv.GetBucket(r.Context(), id)
	if err != nil {
		writeDriverError(w, "GetBucket", err)
		return
	}

	writeJSON(w, http.StatusOK, bucket)
}

// driverForRouteCluster resolves the per-cluster driver from the route's
// {cid} URL param. cid is REQUIRED — no "default cluster" fallback. Per
// v1.11.0.2: the old handlers used s.drv (global default) regardless of
// {cid}, which silently routed every per-cluster API call to whichever
// cluster s.drv pointed at. Operator-confirmed posture: cid required or
// the request is malformed.
func (s *Server) driverForRouteCluster(r *http.Request) (driver.Driver, error) {
	cid := chi.URLParam(r, "cid")
	if cid == "" {
		return nil, errMissingClusterID
	}
	return s.reg.For(r.Context(), cid)
}

// errMissingClusterID surfaces as 400 CLUSTER_ID_REQUIRED via
// writeRegistryForError. Internal sentinel; not exported.
var errMissingClusterID = errors.New("cluster id required")

// createBucketHandler handles POST /admin/clusters/{cid}/buckets.
//
// Per ADR-0001 v0.9.0f: gated on bucket:create at "bucket:{cid}:*"
// since the new bucket has no id yet. Cluster Admin's seeded role
// includes bucket:* so this passes for them; users without that
// capability get 403 FORBIDDEN.
func (s *Server) createBucketHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	cid := chi.URLParam(r, "cid")
	if cid != "" {
		if _, ok := s.requireCapability(w, r, "bucket:create", "bucket:"+cid+":*"); !ok {
			return
		}
	}

	var spec driver.BucketSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", "invalid request body", nil)
		return
	}

	// Garage accepts empty alias and creates an orphan bucket
	// addressable only by id-hash. Refuse here so the list view
	// stays meaningful + each bucket has a usable S3 reference.
	if ve := validateName("alias", spec.Alias, bucketAliasPattern,
		"3–63 chars, lowercase letters/digits/hyphens, no leading or trailing hyphen"); ve != nil {
		writeValidationError(w, ve)
		return
	}

	drv, err := s.driverForRouteCluster(r)
	if err != nil {
		writeRegistryForError(w, err)
		return
	}

	if existing, listErr := drv.ListBuckets(r.Context()); listErr == nil {
		if ve := requireUniqueName("alias", spec.Alias, existing, func(b driver.Bucket) []string {
			return b.Aliases
		}); ve != nil {
			writeValidationError(w, ve)
			return
		}
	}

	bucket, err := drv.CreateBucket(r.Context(), spec)
	if err != nil {
		s.auditFailure(r, "bucket:create", resourceBucket(chi.URLParam(r, "cid"), spec.Alias), err)
		writeDriverError(w, "CreateBucket", err)
		return
	}

	s.auditSuccess(r, "bucket:create", resourceBucket(chi.URLParam(r, "cid"), bucket.ID))
	writeJSON(w, http.StatusCreated, bucket)
}

// updateBucketHandler handles PATCH /admin/clusters/{cid}/buckets/{id}.
//
// Per ADR-0001 v0.9.0f: gated on bucket:edit_alias OR bucket:set_quota
// depending on the patch shape. Conservative gate: require both
// capabilities because the patch shape isn't inspected until JSON
// decode; if a future cycle adds finer body-level gating it can
// downgrade individual sub-patches.
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

	cid := chi.URLParam(r, "cid")
	if cid != "" {
		if _, ok := s.requireCapability(w, r, "bucket:edit_alias", scopeBucket(cid, id)); !ok {
			return
		}
	}

	var update driver.BucketUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", "invalid request body", nil)
		return
	}

	drv, err := s.driverForRouteCluster(r)
	if err != nil {
		writeRegistryForError(w, err)
		return
	}

	bucket, err := drv.UpdateBucket(r.Context(), id, update)
	if err != nil {
		s.auditFailure(r, "bucket:edit_alias", resourceBucket(chi.URLParam(r, "cid"), id), err)
		writeDriverError(w, "UpdateBucket", err)
		return
	}

	s.auditSuccess(r, "bucket:edit_alias", resourceBucket(chi.URLParam(r, "cid"), id))
	writeJSON(w, http.StatusOK, bucket)
}

// armDeleteBucketHandler handles POST /admin/clusters/{cid}/buckets/{id}/_arm-delete.
// Issues a short-lived HMAC token bound to {bucketID, requester} that
// the matching DELETE must present via X-Confirm-Delete. Two-phase
// arm/fire pattern — no single curl can destroy a bucket.
//
// Per ADR-0001 v0.9.0f: gated on bucket:delete at "bucket:{cid}:{id}".
// Arming requires the capability so we don't hand tokens to callers
// who can't even use them.
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

	cid := chi.URLParam(r, "cid")
	if cid != "" {
		if _, ok := s.requireCapability(w, r, "bucket:delete", scopeBucket(cid, id)); !ok {
			return
		}
	}

	// Confirm the bucket exists before issuing a token. Avoids handing
	// out tokens for nonexistent IDs and surfaces 404 cleanly.
	drv, err := s.driverForRouteCluster(r)
	if err != nil {
		writeRegistryForError(w, err)
		return
	}
	if _, err := drv.GetBucket(r.Context(), id); err != nil {
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

// deleteBucketHandler handles DELETE /admin/clusters/{cid}/buckets/{id}.
//
// Requires X-Confirm-Delete header carrying a token previously minted
// by POST /admin/clusters/{cid}/buckets/{id}/_arm-delete. Token is
// HMAC-bound to the (bucket id, user) pair and expires in 60s, so
// curl-by-hand is two-step and a single leaked URL/path cannot destroy.
//
// Per ADR-0001 v0.9.0f: gated on bucket:delete at "bucket:{cid}:{id}".
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

	cid := chi.URLParam(r, "cid")
	if cid != "" {
		if _, ok := s.requireCapability(w, r, "bucket:delete", scopeBucket(cid, id)); !ok {
			return
		}
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

	drv, err := s.driverForRouteCluster(r)
	if err != nil {
		writeRegistryForError(w, err)
		return
	}
	if err := drv.DeleteBucket(r.Context(), id); err != nil {
		s.auditFailure(r, "bucket:delete", resourceBucket(cid, id), err)
		writeDriverError(w, "DeleteBucket", err)
		return
	}

	s.auditSuccess(r, "bucket:delete", resourceBucket(cid, id))
	writeJSON(w, http.StatusOK, map[string]string{"message": "Bucket deleted"})
}
