// Package api: user-persona region keychain endpoints (ADR-0002,
// cycle v1.1.0b).
//
// Every endpoint here lives under /api/v1/user/regions/*. They are the
// sole user-tier S3 surface after ADR-0002 v1.1.0e retired the legacy
// per-bucket Connect-a-bucket flow.
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
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// isUserKeyRejected returns true if the driver error indicates the
// supplied access key was rejected by the backend (revoked, rotated,
// or wrong). Used by every region handler to convert a generic 500
// INTERNAL into a typed 401 USER_KEY_REJECTED so the UI can render
// an actionable "delete this key + add a fresh one" prompt instead of
// just "internal error".
//
// v1.3.0a.1: detection works off driver.Error.Message because the
// per-driver S3 wrappers (internal/drivers/{aws_s3,garage,...}/s3.go)
// collapse every transport error into Err=ErrInvalid with Message =
// underlying err.Error(). The AWS SDK formats those as
// `api error <Code>: <text>` (e.g. "api error InvalidAccessKeyId: The
// AWS Access Key Id you provided does not exist in our records.") so
// a substring scan over the canonical codes is the cheapest reliable
// signal without changing the driver wrap surface.
func isUserKeyRejected(err error) bool {
	if err == nil {
		return false
	}
	var derr *driver.Error
	if !errors.As(err, &derr) {
		return false
	}
	msg := derr.Message
	// Canonical S3 / Garage / MinIO auth-rejection codes. SignatureDoesNotMatch
	// also covers a stale-key edge where the secret on disk is wrong.
	for _, code := range []string{
		"InvalidAccessKeyId",
		"SignatureDoesNotMatch",
		"AccessDenied",
		"Forbidden",
		"InvalidSignature",
	} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

// writeUserKeyRejected emits the standard 401 USER_KEY_REJECTED
// response with the region context the FE needs to render its
// "delete this region" call-to-action. Centralised so every handler
// surfaces the same payload shape.
func writeUserKeyRejected(w http.ResponseWriter, region store.UserRegion) {
	writeError(w, http.StatusUnauthorized, "USER_KEY_REJECTED",
		"Your access key was rejected by the backend. The key may have been revoked, rotated, or never existed. Delete this key from /files/keys and add a fresh one.",
		map[string]interface{}{
			"regionId":    region.ID,
			"alias":       region.Alias,
			"endpoint":    region.Endpoint,
			"accessKeyId": region.AccessKeyID,
		})
}

// userRegionResponse is the wire shape for a UserRegion — strips the
// encrypted secret so it never leaves the server. The plaintext secret
// is never returned anywhere; the user only ever PUTs it, not GETs.
//
// v1.3.0c: AddressingStyle ("path" | "virtual_host") rides on the wire
// so the FE can render a per-key "via path-style" / "via virtual-host"
// subtitle on the keys list. Always non-empty after this cycle thanks
// to the store's applyReadDefaults — every UserRegion is observed with
// an explicit style even when the on-disk JSON predates the field.
type userRegionResponse struct {
	ID              string    `json:"id"`
	UserID          string    `json:"userId"`
	Alias           string    `json:"alias"`
	Endpoint        string    `json:"endpoint"`
	Region          string    `json:"region"`
	AccessKeyID     string    `json:"accessKeyId"`
	AddressingStyle string    `json:"addressingStyle"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
	LastUsedAt      time.Time `json:"lastUsedAt,omitempty"`
}

// toRegionResponse converts a store.UserRegion to the wire shape,
// dropping SecretKeyEnc. Centralised so a future field add (e.g.
// Notes) updates one place.
func toRegionResponse(r store.UserRegion) userRegionResponse {
	return userRegionResponse{
		ID:              r.ID,
		UserID:          r.UserID,
		Alias:           r.Alias,
		Endpoint:        r.Endpoint,
		Region:          r.Region,
		AccessKeyID:     r.AccessKeyID,
		AddressingStyle: r.AddressingStyle,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
		LastUsedAt:      r.LastUsedAt,
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

	return s.reg.ForUserRegion(r.Context(), region.Endpoint, region.AccessKeyID, secret, region.Region, region.AddressingStyle)
}

// userRegionCreateRequest is the body shape for POST /user/regions.
//
// v1.3.0c: AddressingStyle is optional — empty / missing defaults to
// "path" (current behaviour for every UserRegion shipped before this
// cycle). "virtual_host" opts the region into virtual-host addressing;
// the store coalesces any other value back to "path" defensively.
type userRegionCreateRequest struct {
	Alias           string `json:"alias"`
	Endpoint        string `json:"endpoint"`
	AccessKeyID     string `json:"accessKeyId"`
	SecretKey       string `json:"secretKey"`
	Region          string `json:"region,omitempty"`
	AddressingStyle string `json:"addressingStyle,omitempty"`
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
		UserID:          claims.UserID,
		Alias:           req.Alias,
		Endpoint:        req.Endpoint,
		Region:          req.Region,
		AccessKeyID:     req.AccessKeyID,
		AddressingStyle: req.AddressingStyle,    // store normalizes "" / unknown → "path"
		SecretKeyEnc:    []byte(req.SecretKey), // store.Create encrypts immediately
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
				"You already have a key with this alias at this endpoint. Pick a different alias to add another key for the same endpoint.")
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

// userRegionsBulkRequest is the body shape for POST /user/regions/bulk
// (v1.3.0d). Each row mirrors userRegionCreateRequest. Per-row errors
// don't abort the rest — the response payload reports which indices
// succeeded and which failed, with a typed error code per row.
type userRegionsBulkRequest struct {
	Regions []userRegionCreateRequest `json:"regions"`
}

// userRegionsBulkRowError is one per-row failure in the bulk-create
// response. Index is the original position in the request array so the
// UI can correlate it with the operator's input row.
type userRegionsBulkRowError struct {
	Index    int    `json:"index"`
	Alias    string `json:"alias,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Error    string `json:"error"`
	Message  string `json:"message"`
}

// userRegionsBulkResponse is the wire shape for the bulk create.
// Created mirrors a /user/regions GET result, errors carries per-row
// failures. Caller renders a per-row status table off these two
// arrays.
type userRegionsBulkResponse struct {
	Created []userRegionResponse      `json:"created"`
	Errors  []userRegionsBulkRowError `json:"errors"`
}

// userBulkCreateRegionsHandler implements POST /api/v1/user/regions/bulk
// (v1.3.0d).
//
// Body shape: {regions: [{alias, endpoint, accessKeyId, secretKey,
// region, addressingStyle}, ...]}. Each row is validated + created
// independently; a per-row failure (duplicate alias, malformed
// endpoint, store error) is reported in the response's errors array
// and does NOT abort the rest of the batch. Always returns 200 with
// the full result envelope (created + errors), even when every row
// failed — the FE renders a status table off the envelope so a
// blanket non-2xx would just collapse useful per-row context.
//
// Gated only by USER-tier auth (no requireCapability): per the cycle
// constraint, every authenticated user can bulk-add keys to their own
// keychain. The created rows are owned by the calling claims.UserID.
func (s *Server) userBulkCreateRegionsHandler(w http.ResponseWriter, r *http.Request) {
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

	var req userRegionsBulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}
	if len(req.Regions) == 0 {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "regions array is required and must be non-empty")
		return
	}
	// Defensive cap so a runaway client can't post a million-row body.
	const maxBulkRows = 200
	if len(req.Regions) > maxBulkRows {
		writeErrorSimple(w, http.StatusBadRequest, "TOO_MANY_REGIONS",
			"bulk import accepts at most 200 rows per request")
		return
	}

	resp := userRegionsBulkResponse{
		Created: make([]userRegionResponse, 0, len(req.Regions)),
		Errors:  make([]userRegionsBulkRowError, 0),
	}

	for i, row := range req.Regions {
		row.Alias = strings.TrimSpace(row.Alias)
		row.Endpoint = strings.TrimSpace(row.Endpoint)
		row.AccessKeyID = strings.TrimSpace(row.AccessKeyID)
		row.SecretKey = strings.TrimSpace(row.SecretKey)
		row.Region = strings.TrimSpace(row.Region)

		// Per-row required-fields check. Returns INVALID_REQUEST for
		// any missing field — the FE pre-validates client-side too but
		// this is the canonical wire-level check.
		var missing string
		switch {
		case row.Alias == "":
			missing = "alias"
		case row.Endpoint == "":
			missing = "endpoint"
		case row.AccessKeyID == "":
			missing = "accessKeyId"
		case row.SecretKey == "":
			missing = "secretKey"
		}
		if missing != "" {
			resp.Errors = append(resp.Errors, userRegionsBulkRowError{
				Index:    i,
				Alias:    row.Alias,
				Endpoint: row.Endpoint,
				Error:    "INVALID_REQUEST",
				Message:  missing + " is required",
			})
			continue
		}

		// Light URL precheck — same as the single-create handler.
		if u, err := url.Parse(row.Endpoint); err != nil || u.Scheme == "" || u.Host == "" {
			resp.Errors = append(resp.Errors, userRegionsBulkRowError{
				Index:    i,
				Alias:    row.Alias,
				Endpoint: row.Endpoint,
				Error:    "INVALID_ENDPOINT",
				Message:  "endpoint must be a full URL with scheme and host",
			})
			continue
		}

		in := store.UserRegion{
			UserID:          claims.UserID,
			Alias:           row.Alias,
			Endpoint:        row.Endpoint,
			Region:          row.Region,
			AccessKeyID:     row.AccessKeyID,
			AddressingStyle: row.AddressingStyle,
			SecretKeyEnc:    []byte(row.SecretKey),
		}

		created, err := regions.Create(r.Context(), in)
		if err != nil {
			failResource := "region:" + row.Endpoint
			if errors.Is(err, store.ErrUserRegionDuplicate) {
				s.auditFailure(r, "region:bulk_create", failResource, err)
				resp.Errors = append(resp.Errors, userRegionsBulkRowError{
					Index:    i,
					Alias:    row.Alias,
					Endpoint: row.Endpoint,
					Error:    "DUPLICATE_REGION",
					Message:  "A key with this alias already exists at this endpoint.",
				})
				continue
			}
			// NormalizeEndpoint failures from the store land here too.
			if strings.Contains(err.Error(), "endpoint") || strings.Contains(err.Error(), "scheme") || strings.Contains(err.Error(), "host") {
				s.auditFailure(r, "region:bulk_create", failResource, err)
				resp.Errors = append(resp.Errors, userRegionsBulkRowError{
					Index:    i,
					Alias:    row.Alias,
					Endpoint: row.Endpoint,
					Error:    "INVALID_ENDPOINT",
					Message:  err.Error(),
				})
				continue
			}
			s.auditFailure(r, "region:bulk_create", failResource, err)
			resp.Errors = append(resp.Errors, userRegionsBulkRowError{
				Index:    i,
				Alias:    row.Alias,
				Endpoint: row.Endpoint,
				Error:    "REGION_CREATE_FAILED",
				Message:  err.Error(),
			})
			continue
		}

		s.auditSuccess(r, "region:bulk_create", "region:"+created.ID)
		resp.Created = append(resp.Created, toRegionResponse(created))
	}

	writeJSON(w, http.StatusOK, resp)
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

// userRotateRegionHandler implements POST
// /api/v1/user/regions/{regionId}/rotate (v1.3.0c).
//
// Body: {accessKeyId, secretKey}. Updates ONLY those two fields in
// place — alias / endpoint / region / addressingStyle / lastUsedAt
// history remain untouched. Returns the updated UserRegion (sans
// secret) on success.
//
// Side effects:
//   - audit emits "region:rotate" success/failure rows with
//     Resource=region:{id}:{host} (same shape as region:list_buckets
//     so an operator filtering on host gets every region-tier event
//     touching that endpoint).
//   - InvalidateUserRegion evicts the cached S3 client for the OLD
//     (endpoint, accessKeyId) tuple so the next ForUserRegion build
//     picks up the fresh creds. We invalidate BOTH the old and new
//     access-key cache keys because the new key may have been used
//     before (replay of a rotated key).
//
// Wrong-owner / unknown-region collapse to 404 via requireOwnedRegion,
// matching the rest of the user-region surface (never leak existence
// of other users' regions).
func (s *Server) userRotateRegionHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}

	var req struct {
		AccessKeyID string `json:"accessKeyId"`
		SecretKey   string `json:"secretKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	req.AccessKeyID = strings.TrimSpace(req.AccessKeyID)
	req.SecretKey = strings.TrimSpace(req.SecretKey)

	if req.AccessKeyID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "accessKeyId is required")
		return
	}
	if req.SecretKey == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "secretKey is required")
		return
	}

	regions := s.regionsStore() // guaranteed non-nil by requireOwnedRegion

	// store.Update interprets a non-empty SecretKeyEnc as PLAINTEXT
	// bytes per its Create-style convention (the store layer encrypts
	// before persisting). The patch is partial — leaving Alias/Region/
	// Endpoint/AddressingStyle untouched means the existing values are
	// preserved verbatim.
	patch := store.UserRegion{
		AccessKeyID:  req.AccessKeyID,
		SecretKeyEnc: []byte(req.SecretKey),
	}

	updated, err := regions.Update(r.Context(), region.ID, patch)
	if err != nil {
		s.auditFailure(r, "region:rotate", regionListResource(region), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_ROTATE_FAILED",
			"Failed to rotate key: "+err.Error())
		return
	}

	// Evict the OLD cache entry so the next ForUserRegion call rebuilds
	// with the new creds. If the new accessKey was ever cached (e.g.
	// re-uploading a previously rotated key), evict that too so the
	// fresh secret is honoured rather than served from a stale cached
	// driver.
	if s.reg != nil {
		s.reg.InvalidateUserRegion(region.Endpoint, region.AccessKeyID)
		if updated.AccessKeyID != region.AccessKeyID {
			s.reg.InvalidateUserRegion(updated.Endpoint, updated.AccessKeyID)
		}
	}

	s.auditSuccess(r, "region:rotate", regionListResource(updated))

	writeJSON(w, http.StatusOK, toRegionResponse(updated))
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
// ADR-0002 v1.1.0d — Garage's S3 data-plane endpoint does NOT
// implement ListBuckets (returns 404), so for a Garage region the
// user-key driver returns an empty list. Bridge that gap by looking
// up an admin Connection at the same endpoint, calling ListBuckets
// against the admin API with admin_token, then filtering the result
// down to buckets the user's S3 key can actually reach. AWS S3 +
// MinIO implement S3 ListBuckets natively, so for those drivers the
// region-tier path returns whatever the user's key sees and the
// bridge is skipped.
//
// v1.1.0g: emit a region:list_buckets audit event on success so the
// "who listed what region with which key" trail is complete. Failures
// surface as auditFailure with the driver error as detail. The Detail
// field includes accessKey=... so operator forensics can correlate a
// basement caller with a backend access log line. Object-tier
// reads (list/presign-get) still go un-audited per the v1.1.0b spec —
// the cycle scope was the high-touch region ops only.
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
		s.auditFailure(r, "region:list_buckets", regionListResource(region), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED",
			"Failed to build region driver: "+err.Error())
		return
	}

	// Garage admin bridge: when the matched admin Connection is a
	// Garage backend, prefer its admin ListBuckets and intersect with
	// what the user's key can reach. Returns nil if no bridge applies
	// (no matching Connection, or the matched one is a non-Garage
	// driver where the user-key ListBuckets already works).
	buckets, bridgeAttempted, bridgeErr := s.garageRegionBucketsBridge(r, region, drv)
	if bridgeErr != nil {
		// The bridge picked up an admin Connection but the admin call
		// failed — surface that rather than silently fall through, so
		// the operator can fix the cluster wiring.
		s.auditFailure(r, "region:list_buckets", regionListResource(region), bridgeErr)
		// v1.3.0a.1: bridge errors flow through the admin driver, but
		// the per-bucket access probe inside the bridge uses the user
		// key — if the key is revoked at the backend the probe surfaces
		// AccessDenied here too. Convert to 401 USER_KEY_REJECTED so
		// the FE can prompt for re-keying instead of "internal error".
		if isUserKeyRejected(bridgeErr) {
			writeUserKeyRejected(w, region)
			return
		}
		writeDriverError(w, "ListBuckets", bridgeErr)
		return
	}
	if !bridgeAttempted {
		// No admin bridge applies — use the user-key driver directly.
		buckets, err = drv.ListBuckets(r.Context())
		if err != nil {
			s.auditFailure(r, "region:list_buckets", regionListResource(region), err)
			if isUserKeyRejected(err) {
				writeUserKeyRejected(w, region)
				return
			}
			writeDriverError(w, "ListBuckets", err)
			return
		}
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

	// Audit: record the successful list with the key used. Detail
	// carries accessKey=... so an operator correlating a backend
	// access log entry can map it back to a basement actor.
	s.auditEmit(r, "region:list_buckets", regionListResource(region), audit.ResultSuccess,
		"accessKey="+region.AccessKeyID)

	// v1.4.0a: wrap the bucket list with a capability flag the FE uses
	// to decide whether to render the Size + Objects columns. Garage
	// v1's user-region path can't surface counters; hiding the columns
	// is cleaner than rendering rows of em-dashes.
	writeJSON(w, http.StatusOK, userRegionBucketListResponse{
		Buckets:                 buckets,
		PerBucketStatsAvailable: drv.PerBucketStatsAvailable(),
	})
}

// userRegionBucketListResponse is the wire shape returned by GET
// /api/v1/user/regions/{regionId}/buckets. v1.4.0a wraps the prior
// raw []Bucket response so the FE can read a per-driver capability
// flag (PerBucketStatsAvailable) alongside the list without a second
// round trip. Field order on the wire: buckets first (the main
// payload), capability flag second (the meta).
type userRegionBucketListResponse struct {
	Buckets                 []driver.Bucket `json:"buckets"`
	PerBucketStatsAvailable bool            `json:"perBucketStatsAvailable"`
}

// regionListResource builds the canonical Resource string for
// region:list_buckets audit events. Shape: region:{id}:{host} — the
// host suffix is the part of the canonical endpoint after the scheme,
// so the audit log is greppable by hostname without the operator
// having to cross-reference region IDs.
func regionListResource(r store.UserRegion) string {
	host := r.Endpoint
	if idx := strings.Index(host, "://"); idx >= 0 {
		host = host[idx+3:]
	}
	return "region:" + r.ID + ":" + host
}

// garageRegionBucketsBridge looks for an admin Connection whose
// canonical s3_endpoint matches the user region's endpoint AND whose
// driver is a Garage variant. When found, lists buckets via the admin
// driver (admin_token in hand) and filters the result to those the
// user's S3 key can actually reach.
//
// Returns:
//   - buckets, true, nil — bridge applied successfully
//   - nil, true, err — bridge applied but the admin call failed (caller
//     surfaces the error rather than masking it)
//   - nil, false, nil — no bridge applies; caller should use the
//     user-key driver's ListBuckets directly
//
// For AWS S3 + MinIO the user-key ListBuckets returns a real list
// already, so the bridge is skipped even if an admin Connection at
// the same endpoint exists.
func (s *Server) garageRegionBucketsBridge(r *http.Request, region store.UserRegion, userDrv driver.Driver) ([]driver.Bucket, bool, error) {
	if s.conns == nil || s.reg == nil {
		return nil, false, nil
	}

	conns, err := s.conns.List(r.Context())
	if err != nil {
		// Don't fail the user's request because the admin-conns
		// lookup hiccuped — fall through and let the user driver
		// answer.
		s.logger.Warn("garage bridge: failed to list admin connections", "error", err.Error())
		return nil, false, nil
	}

	target := region.Endpoint // already canonical per store.NormalizeEndpoint
	var match *store.Connection
	for i := range conns {
		c := &conns[i]
		raw := connectionS3Endpoint(*c)
		if raw == "" {
			continue
		}
		canon, err := store.NormalizeEndpoint(raw)
		if err != nil {
			continue
		}
		if canon != target {
			continue
		}
		// Only Garage drivers need the bridge — for aws-s3/minio the
		// user's own ListBuckets returns a real list.
		if c.Driver != store.DriverGarage && c.Driver != store.DriverGarageV1 {
			continue
		}
		match = c
		break
	}

	if match == nil {
		s.logger.Warn("garage region: no admin bridge for endpoint",
			"endpoint", target, "userId", region.UserID, "regionId", region.ID)
		return nil, false, nil
	}

	adminDrv, err := s.reg.For(r.Context(), match.ID)
	if err != nil {
		s.logger.Warn("garage bridge: build admin driver failed",
			"connId", match.ID, "error", err.Error())
		return nil, true, err
	}

	adminBuckets, err := adminDrv.ListBuckets(r.Context())
	if err != nil {
		s.logger.Warn("garage bridge: admin ListBuckets failed",
			"connId", match.ID, "error", err.Error())
		return nil, true, err
	}

	// Intersect with what the user's S3 key can reach. We use
	// ListObjects(limit=1) instead of HeadBucket (which isn't on the
	// Driver interface) — it exercises the same access check via the
	// user's signed S3 client. 403 / 404 / NoSuchBucket / AccessDenied
	// errors mean "drop"; anything else means "keep" (we err on the
	// side of showing the bucket rather than hiding it on a transient
	// network blip).
	out := make([]driver.Bucket, 0, len(adminBuckets))
	for _, b := range adminBuckets {
		// Probe by the first alias for Garage (S3 paths route by
		// alias, not by the 32-byte internal ID).
		probe := b.ID
		if len(b.Aliases) > 0 {
			probe = b.Aliases[0]
		}
		if !regionKeyCanAccessBucket(r.Context(), userDrv, probe) {
			continue
		}
		out = append(out, b)
	}
	return out, true, nil
}

// connectionS3Endpoint returns the S3 endpoint URL configured on a
// Connection record, honouring the per-driver config-key convention:
// Garage variants use "s3_endpoint"; aws-s3 + minio use "endpoint".
// Returns empty string when neither key is present.
func connectionS3Endpoint(c store.Connection) string {
	if v := strings.TrimSpace(c.Config["s3_endpoint"]); v != "" {
		return v
	}
	if v := strings.TrimSpace(c.Config["endpoint"]); v != "" {
		return v
	}
	return ""
}

// regionKeyCanAccessBucket returns true when the user's signed S3
// client can reach the bucket. Implemented via ListObjects(limit=1)
// — the cheapest call that exercises the bucket-level access check
// without depending on a HeadBucket method that isn't on the Driver
// interface. driver.ErrPermissionDenied / driver.ErrNotFound (the
// 403/404 cases) drop the bucket; other errors are conservatively
// treated as "keep" so a transient blip doesn't hide a bucket the
// user legitimately owns.
func regionKeyCanAccessBucket(ctx context.Context, userDrv driver.Driver, bucket string) bool {
	if userDrv == nil || bucket == "" {
		return false
	}
	// delimiter="" + limit=1 — the cheapest call that exercises
	// bucket-level access without paying to enumerate sub-folders.
	_, err := userDrv.ListObjects(ctx, bucket, "", "", "", 1)
	if err == nil {
		return true
	}
	if errors.Is(err, driver.ErrPermissionDenied) || errors.Is(err, driver.ErrNotFound) {
		return false
	}
	return true
}

// regionObjectResource builds the canonical Resource string for
// object-tier region audit events. Shape: region:{regionId}:{bucketID}
// per the v1.1.0h spec — bucket-scoped so an operator filtering the
// audit log on a bucket name finds every region-tier object op that
// touched it. The accessKey lands in Detail (not Resource) so the
// resource cardinality stays bounded by (region × bucket).
func regionObjectResource(r store.UserRegion, bucketID string) string {
	return "region:" + r.ID + ":" + bucketID
}

// regionAuditDetail formats the audit Detail string used by every
// object-tier region handler: accessKey=<id>. Centralised so a future
// addition (e.g. partCount=) lands in one place.
func regionAuditDetail(r store.UserRegion) string {
	return "accessKey=" + r.AccessKeyID
}

// userListRegionBucketObjectsHandler implements GET
// /api/v1/user/regions/{regionId}/buckets/{bid}/objects — prefix +
// continuation-token + limit query params, same shape as the retired
// cluster-tier list-objects endpoint.
//
// v1.1.0h: emits region:list_objects audit events on both success and
// failure. Resource = region:{regionId}:{bucketID}; Detail carries the
// access key used to sign the call (per v1.1.0g pattern). Reads ARE
// audited at the region tier because the operator needs the trail —
// "who read what bucket, with which key" — to investigate suspicious
// activity against backend access logs.
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
		s.auditFailure(r, "region:list_objects", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED",
			"Failed to build region driver: "+err.Error())
		return
	}

	prefix := r.URL.Query().Get("prefix")
	token := r.URL.Query().Get("token")
	// delimiter defaults to "/" so the bucket browser sees folder
	// rows + only-this-level files (v1.3.0c.1 folder-nav fix).
	// Callers that want a flat recursive dump (scripts, sync
	// preview) opt out with ?delimiter= (empty value).
	delimiter := "/"
	if v, ok := r.URL.Query()["delimiter"]; ok && len(v) > 0 {
		delimiter = v[0]
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	page, err := drv.ListObjects(r.Context(), bid, prefix, token, delimiter, limit)
	if err != nil {
		s.auditFailure(r, "region:list_objects", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		writeDriverError(w, "ListObjects", err)
		return
	}
	if page.Objects == nil {
		page.Objects = []driver.ObjectInfo{}
	}
	if page.CommonPrefixes == nil {
		page.CommonPrefixes = []string{}
	}

	s.auditEmit(r, "region:list_objects", regionObjectResource(region, bid),
		audit.ResultSuccess, regionAuditDetail(region))

	writeJSON(w, http.StatusOK, page)
}

// presignTTL parses the ttl query param, applying a 1h default + 24h
// cap (carried over from the retired cluster-tier presign handlers).
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
//
// v1.1.0h: audits as region:presign_get. The presigned URL is the
// effective grant — once handed to a browser it can pull the object
// independent of basement — so the audit trail matters more here
// than for a direct GET that would only ever run inside the server.
func (s *Server) userPresignGetRegionObjectHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	key, _ := url.PathUnescape(chi.URLParam(r, "key"))
	if bid == "" || key == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "bucket id and key required")
		return
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		s.auditFailure(r, "region:presign_get", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	presign, err := drv.PresignGet(r.Context(), bid, key, presignTTL(r))
	if err != nil {
		s.auditFailure(r, "region:presign_get", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		writeDriverError(w, "PresignGet", err)
		return
	}

	s.auditEmit(r, "region:presign_get", regionObjectResource(region, bid),
		audit.ResultSuccess, regionAuditDetail(region))

	writeJSON(w, http.StatusOK, presign)
}

// userPresignPutRegionObjectHandler implements POST
// /api/v1/user/regions/{regionId}/buckets/{bid}/objects/{key}/presign-put.
// Body may optionally contain {"contentType": "..."} — used by the
// uploader to pre-bind the Content-Type into the signed URL.
//
// v1.1.0h: audits as region:presign_put. Mirrors presign_get — the
// signed URL is the effective grant for the upload, so the operator
// needs to see who minted it.
func (s *Server) userPresignPutRegionObjectHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	key, _ := url.PathUnescape(chi.URLParam(r, "key"))
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
		s.auditFailure(r, "region:presign_put", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	presign, err := drv.PresignPut(r.Context(), bid, key, presignTTL(r), body.ContentType)
	if err != nil {
		s.auditFailure(r, "region:presign_put", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		writeDriverError(w, "PresignPut", err)
		return
	}

	s.auditEmit(r, "region:presign_put", regionObjectResource(region, bid),
		audit.ResultSuccess, regionAuditDetail(region))

	writeJSON(w, http.StatusOK, presign)
}

// userInitRegionMultipartHandler implements POST
// /api/v1/user/regions/{regionId}/buckets/{bid}/multipart/init.
//
// v1.1.0h: audits as region:multipart_init.
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
		s.auditFailure(r, "region:multipart_init", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	upload, err := drv.CreateMultipart(r.Context(), bid, req.Key, req.ContentType)
	if err != nil {
		s.auditFailure(r, "region:multipart_init", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		writeDriverError(w, "CreateMultipart", err)
		return
	}

	s.auditEmit(r, "region:multipart_init", regionObjectResource(region, bid),
		audit.ResultSuccess, regionAuditDetail(region))

	writeJSON(w, http.StatusOK, upload)
}

// userCompleteRegionMultipartHandler implements POST
// /api/v1/user/regions/{regionId}/buckets/{bid}/multipart/{uploadId}/complete.
//
// v1.1.0h: audits as region:multipart_complete.
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
		s.auditFailure(r, "region:multipart_complete", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	if err := drv.CompleteMultipart(r.Context(), driver.MultipartUpload{UploadID: uploadID, Bucket: bid}, parts); err != nil {
		s.auditFailure(r, "region:multipart_complete", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		writeDriverError(w, "CompleteMultipart", err)
		return
	}

	s.auditEmit(r, "region:multipart_complete", regionObjectResource(region, bid),
		audit.ResultSuccess, regionAuditDetail(region))

	w.WriteHeader(http.StatusNoContent)
}

// userAbortRegionMultipartHandler implements DELETE
// /api/v1/user/regions/{regionId}/buckets/{bid}/multipart/{uploadId}.
//
// v1.1.0h: audits as region:multipart_abort.
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
		s.auditFailure(r, "region:multipart_abort", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	if err := drv.AbortMultipart(r.Context(), driver.MultipartUpload{UploadID: uploadID, Bucket: bid}); err != nil {
		s.auditFailure(r, "region:multipart_abort", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		writeDriverError(w, "AbortMultipart", err)
		return
	}

	s.auditEmit(r, "region:multipart_abort", regionObjectResource(region, bid),
		audit.ResultSuccess, regionAuditDetail(region))

	w.WriteHeader(http.StatusNoContent)
}

// userPresignRegionUploadPartHandler implements POST
// /api/v1/user/regions/{regionId}/buckets/{bid}/multipart/{uploadId}/part/{partNum}/presign.
//
// Added in v1.1.0c (FE region rewrite) because the upload flow uploads
// each part directly from the browser to the backend via a presigned
// PUT — without this handler, multipart uploads under the region tier
// would have no way to sign individual parts.
//
// v1.1.0h: audits as region:multipart_part. Each presigned-part URL is
// the effective per-part upload grant, so it's audited like presign_get
// + presign_put rather than coalesced with the parent upload.
func (s *Server) userPresignRegionUploadPartHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	uploadID := chi.URLParam(r, "uploadId")
	partNumStr := chi.URLParam(r, "partNum")
	if bid == "" || uploadID == "" || partNumStr == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "bucket id, uploadId, and partNum required")
		return
	}

	partNum, err := strconv.Atoi(partNumStr)
	if err != nil || partNum < 1 || partNum > 10000 {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "part number must be between 1 and 10000")
		return
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		s.auditFailure(r, "region:multipart_part", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	presign, err := drv.PresignUploadPart(r.Context(), driver.MultipartUpload{UploadID: uploadID, Bucket: bid}, partNum)
	if err != nil {
		s.auditFailure(r, "region:multipart_part", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		writeDriverError(w, "PresignUploadPart", err)
		return
	}

	s.auditEmit(r, "region:multipart_part", regionObjectResource(region, bid),
		audit.ResultSuccess, regionAuditDetail(region))

	writeJSON(w, http.StatusOK, presign)
}

// userDeleteRegionObjectHandler implements DELETE
// /api/v1/user/regions/{regionId}/buckets/{bid}/objects/{key}.
//
// Added in v1.1.0c so the object browser's delete button can wire to a
// region-tier endpoint. The region's S3 key is the permission — the
// backend rejects the DELETE if the key lacks objects:delete on the
// bucket, surfacing as a 4xx from the driver.
//
// v1.1.0h: audits as region:delete_object. Destructive, so the trail
// matters most here of any object-tier op.
func (s *Server) userDeleteRegionObjectHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	key, _ := url.PathUnescape(chi.URLParam(r, "key"))
	if bid == "" || key == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "bucket id and key required")
		return
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		s.auditFailure(r, "region:delete_object", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	if err := drv.DeleteObject(r.Context(), bid, key); err != nil {
		s.auditFailure(r, "region:delete_object", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		writeDriverError(w, "DeleteObject", err)
		return
	}

	s.auditEmit(r, "region:delete_object", regionObjectResource(region, bid),
		audit.ResultSuccess, regionAuditDetail(region))

	w.WriteHeader(http.StatusNoContent)
}
