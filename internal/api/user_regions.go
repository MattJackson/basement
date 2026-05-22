// Package api: user-persona region keychain endpoints (ADR-0002,
// cycle v1.1.0b; legacy /user/clusters tree retired in v1.1.0e).
//
// Every endpoint here lives under /api/v1/user/regions/*. The legacy
// per-bucket Connect-a-bucket flow (/api/v1/user/buckets/connect and
// /api/v1/user/clusters/*) was deleted in cycle v1.1.0e — the
// region-keychain model is the only user-tier path now.
//
// Security model: the region's S3 key IS the permission. basement
// stops inventing per-bucket access — backends already enforce key
// grants, so the API layer just verifies the caller owns the region
// (userID match) and signs every downstream S3 op with the region's
// key. There is no requireCapability gate on this tree; the audit
// trail for unauthorized access comes from the 404-on-wrong-owner
// pattern (we return 404 instead of 403 so we never leak the existence
// of other users' regions).
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// userRegionResponse is the wire shape for a UserRegion — strips the
// encrypted secret so it never leaves the server. The plaintext secret
// is never returned anywhere; the user only ever PUTs it, not GETs.
type userRegionResponse struct {
	ID          string    `json:"id"`
	UserID      string    `json:"userId"`
	Alias       string    `json:"alias"`
	Endpoint    string    `json:"endpoint"`
	Region      string    `json:"region"`
	AccessKeyID string    `json:"accessKeyId"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	LastUsedAt  time.Time `json:"lastUsedAt,omitempty"`
}

// toRegionResponse converts a store.UserRegion to the wire shape,
// dropping SecretKeyEnc. Centralised so a future field add (e.g.
// Notes) updates one place.
func toRegionResponse(r store.UserRegion) userRegionResponse {
	return userRegionResponse{
		ID:          r.ID,
		UserID:      r.UserID,
		Alias:       r.Alias,
		Endpoint:    r.Endpoint,
		Region:      r.Region,
		AccessKeyID: r.AccessKeyID,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
		LastUsedAt:  r.LastUsedAt,
	}
}

// regionsStore pulls the per-user keychain off the server, returning
// nil if it hasn't been wired. Every handler nil-checks and emits 503
// REGIONS_NOT_WIRED — same shape as the existing GRANTS_NOT_WIRED
// for the v0.9.0e flow.
func (s *Server) regionsStore() store.UserRegions {
	if s.store == nil {
		return nil
	}
	return s.store.UserRegions()
}

// requireOwnedRegion looks up a UserRegion by regionId, returning it
// only if the caller's userID matches. On any mismatch (not found OR
// owned by someone else), returns a 404 — never 403 — so the API never
// leaks the existence of another user's regions.
//
// Returns (region, userID, ok). ok==false means the response has
// already been written; the caller should just return.
func (s *Server) requireOwnedRegion(w http.ResponseWriter, r *http.Request) (store.UserRegion, string, bool) {
	regions := s.regionsStore()
	if regions == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "REGIONS_NOT_WIRED",
			"Region keychain store is not configured on this deployment.")
		return store.UserRegion{}, "", false
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return store.UserRegion{}, "", false
	}

	regionID := chi.URLParam(r, "regionId")
	if regionID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "regionId is required")
		return store.UserRegion{}, "", false
	}

	region, err := regions.Get(r.Context(), regionID)
	if err != nil || region.UserID != claims.UserID {
		// 404 covers both "doesn't exist" and "exists but isn't yours"
		// to avoid leaking the existence of other users' regions.
		writeErrorSimple(w, http.StatusNotFound, "REGION_NOT_FOUND", "Region not found")
		return store.UserRegion{}, "", false
	}

	return region, claims.UserID, true
}

// regionDriver builds a per-region driver from a UserRegion. The secret
// is decrypted on this path only — kept narrow so audit greps for
// "Decrypt(" land on a small surface.
func (s *Server) regionDriver(r *http.Request, region store.UserRegion) (driver.Driver, error) {
	regions := s.regionsStore()
	if regions == nil {
		return nil, errors.New("regions store not wired")
	}
	if s.reg == nil {
		return nil, errors.New("driver registry not wired")
	}

	secret, err := regions.Decrypt(region)
	if err != nil {
		return nil, err
	}

	return s.reg.ForUserRegion(r.Context(), region.Endpoint, region.AccessKeyID, secret, region.Region)
}

// userRegionCreateRequest is the body shape for POST /user/regions.
type userRegionCreateRequest struct {
	Alias       string `json:"alias"`
	Endpoint    string `json:"endpoint"`
	AccessKeyID string `json:"accessKeyId"`
	SecretKey   string `json:"secretKey"`
	Region      string `json:"region,omitempty"`
}

// userCreateRegionHandler implements POST /api/v1/user/regions.
//
//   - 503 REGIONS_NOT_WIRED if the keychain store is unavailable
//   - 400 INVALID_REQUEST for missing required fields
//   - 400 INVALID_ENDPOINT if the endpoint URL is unparseable / missing
//     scheme / missing host (NormalizeEndpoint is the source of truth)
//   - 409 DUPLICATE_REGION if a region for (userID, endpoint) already
//     exists (alias is NOT part of the uniqueness key — see ADR-0002)
//   - 201 with the stored record (minus the secret) on success
func (s *Server) userCreateRegionHandler(w http.ResponseWriter, r *http.Request) {
	regions := s.regionsStore()
	if regions == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "REGIONS_NOT_WIRED",
			"Region keychain store is not configured on this deployment.")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	var req userRegionCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	req.Alias = strings.TrimSpace(req.Alias)
	req.Endpoint = strings.TrimSpace(req.Endpoint)
	req.AccessKeyID = strings.TrimSpace(req.AccessKeyID)
	req.SecretKey = strings.TrimSpace(req.SecretKey)
	req.Region = strings.TrimSpace(req.Region)

	if req.Alias == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "alias is required")
		return
	}
	if req.Endpoint == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "endpoint is required")
		return
	}
	if req.AccessKeyID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "accessKeyId is required")
		return
	}
	if req.SecretKey == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "secretKey is required")
		return
	}

	// Light URL precheck — the store's NormalizeEndpoint will catch
	// scheme/host issues too, but we surface a typed INVALID_ENDPOINT
	// so the UI can render a targeted error.
	if u, err := url.Parse(req.Endpoint); err != nil || u.Scheme == "" || u.Host == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_ENDPOINT",
			"endpoint must be a full URL with scheme and host")
		return
	}

	in := store.UserRegion{
		UserID:       claims.UserID,
		Alias:        req.Alias,
		Endpoint:     req.Endpoint,
		Region:       req.Region,
		AccessKeyID:  req.AccessKeyID,
		SecretKeyEnc: []byte(req.SecretKey), // store.Create encrypts immediately
	}

	created, err := regions.Create(r.Context(), in)
	if err != nil {
		// Pre-creation the resource doesn't have an ID yet — use the
		// raw endpoint so the failure row remains greppable. Success
		// rows below use the actual region ID per the cycle spec.
		failResource := "region:" + req.Endpoint

		if errors.Is(err, store.ErrUserRegionDuplicate) {
			s.auditFailure(r, "region:create", failResource, err)
			writeErrorSimple(w, http.StatusConflict, "DUPLICATE_REGION",
				"You already have a region for this endpoint.")
			return
		}
		// NormalizeEndpoint surfaces its own errors here (e.g. bad
		// scheme) — bubble as 400 INVALID_ENDPOINT so the UI can
		// render the same error path.
		if strings.Contains(err.Error(), "endpoint") || strings.Contains(err.Error(), "scheme") || strings.Contains(err.Error(), "host") {
			s.auditFailure(r, "region:create", failResource, err)
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_ENDPOINT", err.Error())
			return
		}
		s.auditFailure(r, "region:create", failResource, err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_CREATE_FAILED",
			"Failed to create region: "+err.Error())
		return
	}

	s.auditSuccess(r, "region:create", "region:"+created.ID)

	writeJSON(w, http.StatusCreated, toRegionResponse(created))
}

// userListRegionsHandler implements GET /api/v1/user/regions.
//
// Returns the caller's UserRegions, never anyone else's. Empty list
// (not 404) when the user has no regions yet.
func (s *Server) userListRegionsHandler(w http.ResponseWriter, r *http.Request) {
	regions := s.regionsStore()
	if regions == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "REGIONS_NOT_WIRED",
			"Region keychain store is not configured on this deployment.")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	list, err := regions.ListForUser(r.Context(), claims.UserID)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR",
			"Failed to list regions: "+err.Error())
		return
	}

	out := make([]userRegionResponse, 0, len(list))
	for _, r := range list {
		out = append(out, toRegionResponse(r))
	}
	writeJSON(w, http.StatusOK, out)
}

// userGetRegionHandler implements GET /api/v1/user/regions/{regionId}.
// 404 on not-yours-or-not-found per the security model.
func (s *Server) userGetRegionHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toRegionResponse(region))
}

// userDeleteRegionHandler implements DELETE /api/v1/user/regions/{regionId}.
// Deletes the record AND evicts the per-region driver cache so a
// subsequent re-create with rotated creds doesn't reuse the old client.
func (s *Server) userDeleteRegionHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}

	regions := s.regionsStore() // guaranteed non-nil by requireOwnedRegion

	if err := regions.Delete(r.Context(), region.ID); err != nil {
		s.auditFailure(r, "region:delete", "region:"+region.ID, err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DELETE_FAILED",
			"Failed to delete region: "+err.Error())
		return
	}

	if s.reg != nil {
		s.reg.InvalidateUserRegion(region.Endpoint, region.AccessKeyID)
	}

	s.auditSuccess(r, "region:delete", "region:"+region.ID)

	w.WriteHeader(http.StatusNoContent)
}

// userListRegionBucketsHandler implements GET
// /api/v1/user/regions/{regionId}/buckets — signs ListBuckets with the
// region's key and returns the live backend response. Bumps
// LastUsedAt as a side-effect.
//
// No audit entry per the cycle spec: object-tier ops are covered by
// the driver/audit chain already at the cluster admin layer, and
// recording every per-user list would balloon the audit log without
// adding accountability the user couldn't get from their own access
// key on the backend.
func (s *Server) userListRegionBucketsHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		if errors.Is(err, driver.ErrUnsupported) {
			writeErrorSimple(w, http.StatusServiceUnavailable, "REGIONS_NOT_WIRED",
				"Region driver subsystem is not configured on this deployment.")
			return
		}
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED",
			"Failed to build region driver: "+err.Error())
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

	// Best-effort LastUsedAt bump. A persistence failure on the touch
	// is logged inside the store layer and not surfaced to the user —
	// the data they came for has already been signed and returned.
	if regions := s.regionsStore(); regions != nil {
		_ = regions.TouchLastUsed(r.Context(), region.ID)
	}

	writeJSON(w, http.StatusOK, buckets)
}

// userListRegionBucketObjectsHandler implements GET
// /api/v1/user/regions/{regionId}/buckets/{bid}/objects.
func (s *Server) userListRegionBucketObjectsHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}

	bid := chi.URLParam(r, "bid")
	if bid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "bucket id required")
		return
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED",
			"Failed to build region driver: "+err.Error())
		return
	}

	prefix := r.URL.Query().Get("prefix")
	token := r.URL.Query().Get("token")
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	page, err := drv.ListObjects(r.Context(), bid, prefix, token, limit)
	if err != nil {
		writeDriverError(w, "ListObjects", err)
		return
	}
	if page.Objects == nil {
		page.Objects = []driver.ObjectInfo{}
	}
	if page.Prefixes == nil {
		page.Prefixes = []string{}
	}
	writeJSON(w, http.StatusOK, page)
}

// presignTTL parses the ttl query param, applying a 1h default and a
// 24h cap to keep presigned URLs from outliving their usefulness.
func presignTTL(r *http.Request) time.Duration {
	ttl := 3600 * time.Second
	if v := r.URL.Query().Get("ttl"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ttl = time.Duration(n) * time.Second
		}
	}
	const maxTTL = 86400 * time.Second
	if ttl > maxTTL {
		ttl = maxTTL
	}
	return ttl
}

// userPresignGetRegionObjectHandler implements GET
// /api/v1/user/regions/{regionId}/buckets/{bid}/objects/{key}/presign-get.
func (s *Server) userPresignGetRegionObjectHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	key := chi.URLParam(r, "key")
	if bid == "" || key == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "bucket id and key required")
		return
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	presign, err := drv.PresignGet(r.Context(), bid, key, presignTTL(r))
	if err != nil {
		writeDriverError(w, "PresignGet", err)
		return
	}
	writeJSON(w, http.StatusOK, presign)
}

// userPresignPutRegionObjectHandler implements POST
// /api/v1/user/regions/{regionId}/buckets/{bid}/objects/{key}/presign-put.
// Body may optionally contain {"contentType": "..."} — used by the
// uploader to pre-bind the Content-Type into the signed URL.
func (s *Server) userPresignPutRegionObjectHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	key := chi.URLParam(r, "key")
	if bid == "" || key == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "bucket id and key required")
		return
	}

	var body struct {
		ContentType string `json:"contentType"`
	}
	// Body is optional — ignore decode errors on empty body.
	_ = json.NewDecoder(r.Body).Decode(&body)

	drv, err := s.regionDriver(r, region)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	presign, err := drv.PresignPut(r.Context(), bid, key, presignTTL(r), body.ContentType)
	if err != nil {
		writeDriverError(w, "PresignPut", err)
		return
	}
	writeJSON(w, http.StatusOK, presign)
}

// userInitRegionMultipartHandler implements POST
// /api/v1/user/regions/{regionId}/buckets/{bid}/multipart/init.
func (s *Server) userInitRegionMultipartHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	if bid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "bucket id required")
		return
	}

	var req struct {
		Key         string `json:"key"`
		ContentType string `json:"contentType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}
	if req.Key == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "key required in request body")
		return
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	upload, err := drv.CreateMultipart(r.Context(), bid, req.Key, req.ContentType)
	if err != nil {
		writeDriverError(w, "CreateMultipart", err)
		return
	}
	writeJSON(w, http.StatusOK, upload)
}

// userCompleteRegionMultipartHandler implements POST
// /api/v1/user/regions/{regionId}/buckets/{bid}/multipart/{uploadId}/complete.
func (s *Server) userCompleteRegionMultipartHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	uploadID := chi.URLParam(r, "uploadId")
	if bid == "" || uploadID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "bucket id and uploadId required")
		return
	}

	var req struct {
		Parts []struct {
			PartNumber int    `json:"partNumber"`
			ETag       string `json:"etag"`
		} `json:"parts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid request body")
		return
	}

	parts := make([]driver.CompletedPart, len(req.Parts))
	for i, p := range req.Parts {
		if p.PartNumber < 1 || p.PartNumber > 10000 {
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
				"part number must be between 1 and 10000")
			return
		}
		parts[i] = driver.CompletedPart{PartNumber: p.PartNumber, ETag: p.ETag}
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	if err := drv.CompleteMultipart(r.Context(), driver.MultipartUpload{UploadID: uploadID, Bucket: bid}, parts); err != nil {
		writeDriverError(w, "CompleteMultipart", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// userAbortRegionMultipartHandler implements DELETE
// /api/v1/user/regions/{regionId}/buckets/{bid}/multipart/{uploadId}.
func (s *Server) userAbortRegionMultipartHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	uploadID := chi.URLParam(r, "uploadId")
	if bid == "" || uploadID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "bucket id and uploadId required")
		return
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	if err := drv.AbortMultipart(r.Context(), driver.MultipartUpload{UploadID: uploadID, Bucket: bid}); err != nil {
		writeDriverError(w, "AbortMultipart", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
