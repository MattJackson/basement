// Package api: user-tier bucket Object Lock + per-version retention +
// legal-hold endpoints (v1.10.0c).
//
// Sits under /api/v1/user/regions/{regionId}/buckets/{bid}/object-lock
// and .../o/{key}/retention + .../o/{key}/legal-hold. Shares the
// requireOwnedRegion + regionDriver plumbing with the rest of the
// user-region surface. Capability gate: drivers that report
// ObjectLockSupport()==false return 501 NOT_SUPPORTED before any
// backend call.
//
// Audit events surfaced here:
//   - bucket:object_lock_enabled         (enabling Object Lock on a bucket)
//   - bucket:object_lock_default_retention_set
//   - object:retention_set / _extended / _reduced
//   - object:legal_hold_set / _released
//
// Failures emit auditFailure for every action so an operator can grep
// "who tried to bypass a compliance retention on bucket foo and got a
// 409".
//
// Object Lock layers on top of versioning per the S3 spec — the FE
// gates the settings card on versioning being enabled. The API layer
// does NOT re-check that gate per request; S3 itself rejects Object
// Lock writes on unversioned buckets and surfaces the error verbatim
// via writeDriverError. (Re-checking would race a concurrent
// versioning suspend and add a round-trip on the hot path.)

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/driver"
)

// objectLockConfigResponse is the wire shape for GET object-lock.
// Includes the capability flag inline so the FE can decide whether
// to render the settings card without a second round trip.
type objectLockConfigResponse struct {
	Enabled          bool                          `json:"enabled"`
	DefaultRetention *driver.ObjectLockRetention   `json:"defaultRetention,omitempty"`
	Supported        bool                          `json:"supported"`
}

// objectLockConfigRequest is the body shape for PUT object-lock.
// Enabled is the only required field — DefaultRetention is optional
// and clears existing default retention when omitted.
type objectLockConfigRequest struct {
	Enabled          bool                          `json:"enabled"`
	DefaultRetention *driver.ObjectLockRetention   `json:"defaultRetention,omitempty"`
}

// objectRetentionResponse wraps a single per-version retention with
// a top-level `retention` key — keeping the wire shape consistent
// with the request body and trivially extensible for future fields
// (e.g. lastModifiedBy).
type objectRetentionResponse struct {
	Retention *driver.ObjectLockRetention `json:"retention,omitempty"`
}

// objectLegalHoldResponse is the wire shape for GET / PUT legal hold.
type objectLegalHoldResponse struct {
	On bool `json:"on"`
}

// userGetBucketObjectLockHandler implements GET
// /api/v1/user/regions/{regionId}/buckets/{bid}/object-lock.
//
// Returns the current Object Lock config plus the per-driver
// capability flag. Unsupported drivers (Garage v1/v2 today) return
// supported=false + enabled=false without hitting the backend, so
// the FE can hide the settings card without a 501 error banner.
func (s *Server) userGetBucketObjectLockHandler(w http.ResponseWriter, r *http.Request) {
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
		s.auditFailure(r, "bucket:object_lock_get", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	if !drv.ObjectLockSupport() {
		// Mirror the versioning GET pattern — surface
		// supported=false rather than 501 so the FE can render the
		// off-state UI without an error banner. Mutating paths
		// still 501 for direct callers.
		writeJSON(w, http.StatusOK, objectLockConfigResponse{
			Enabled:   false,
			Supported: false,
		})
		return
	}

	cfg, err := drv.GetObjectLockConfig(r.Context(), bid)
	if err != nil {
		s.auditFailure(r, "bucket:object_lock_get", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		writeDriverError(w, "GetObjectLockConfig", err)
		return
	}

	resp := objectLockConfigResponse{
		Supported: true,
	}
	if cfg != nil {
		resp.Enabled = cfg.Enabled
		resp.DefaultRetention = cfg.DefaultRetention
	}
	writeJSON(w, http.StatusOK, resp)
}

// userPutBucketObjectLockHandler implements PUT
// /api/v1/user/regions/{regionId}/buckets/{bid}/object-lock.
//
// Body: {"enabled": true, "defaultRetention": {...}?}. Enabled must
// be true — S3 has no contract for turning Object Lock OFF once on,
// and the driver also rejects this defensively.
//
// Two audit events fire here:
//   - bucket:object_lock_enabled (always when this PUT succeeds)
//   - bucket:object_lock_default_retention_set (when the body carries
//     a non-nil defaultRetention block)
//
// Drivers without Object Lock support return 501 NOT_SUPPORTED.
func (s *Server) userPutBucketObjectLockHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	if bid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "bucket id required")
		return
	}

	var req objectLockConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}

	if !req.Enabled {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			"Object Lock cannot be disabled once enabled on a bucket")
		return
	}

	// Validate default retention shape up front so a malformed body
	// surfaces as 400 rather than a 501-or-500 down at the driver.
	if req.DefaultRetention != nil {
		if req.DefaultRetention.Mode != driver.ObjectLockGovernance &&
			req.DefaultRetention.Mode != driver.ObjectLockCompliance {
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
				`defaultRetention.mode must be "GOVERNANCE" or "COMPLIANCE"`)
			return
		}
		if req.DefaultRetention.RetainUntilDate.IsZero() {
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
				"defaultRetention.retainUntilDate required")
			return
		}
		if req.DefaultRetention.RetainUntilDate.Before(time.Now()) {
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
				"defaultRetention.retainUntilDate must be in the future")
			return
		}
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		s.auditFailure(r, "bucket:object_lock_enabled", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	if !drv.ObjectLockSupport() {
		s.auditFailureDetail(r, "bucket:object_lock_enabled",
			regionObjectResource(region, bid),
			"driver does not support Object Lock")
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED",
			"This backend driver does not support Object Lock.",
			map[string]interface{}{"capability": "objectLock"})
		return
	}

	cfg := driver.ObjectLockConfig{
		Enabled:          req.Enabled,
		DefaultRetention: req.DefaultRetention,
	}
	if err := drv.PutObjectLockConfig(r.Context(), bid, cfg); err != nil {
		s.auditFailure(r, "bucket:object_lock_enabled", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		if errors.Is(err, driver.ErrUnsupported) {
			writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", err.Error(),
				map[string]interface{}{"capability": "objectLock"})
			return
		}
		writeDriverError(w, "PutObjectLockConfig", err)
		return
	}

	s.auditEmit(r, "bucket:object_lock_enabled", regionObjectResource(region, bid),
		audit.ResultSuccess, regionAuditDetail(region))
	if req.DefaultRetention != nil {
		s.auditEmit(r, "bucket:object_lock_default_retention_set",
			regionObjectResource(region, bid),
			audit.ResultSuccess,
			regionAuditDetail(region)+",mode="+string(req.DefaultRetention.Mode))
	}

	writeJSON(w, http.StatusOK, objectLockConfigResponse{
		Enabled:          true,
		DefaultRetention: req.DefaultRetention,
		Supported:        true,
	})
}

// userGetObjectRetentionHandler implements GET
// /api/v1/user/regions/{regionId}/buckets/{bid}/o/{key}/retention?versionId=...
//
// versionId is required — the per-object Object Lock surface always
// pins to a specific version row. Returns {retention: null} when
// the object has no retention set rather than a 404 so the FE can
// render the "no retention" state without an error banner.
func (s *Server) userGetObjectRetentionHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	key, _ := url.PathUnescape(chi.URLParam(r, "key"))
	versionID := r.URL.Query().Get("versionId")
	if bid == "" || key == "" || versionID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			"bucket id, key, and versionId required")
		return
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		s.auditFailure(r, "object:retention_get", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	if !drv.ObjectLockSupport() {
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED",
			"This backend driver does not support Object Lock.",
			map[string]interface{}{"capability": "objectLock"})
		return
	}

	ret, err := drv.GetObjectRetention(r.Context(), bid, key, versionID)
	if err != nil {
		s.auditFailure(r, "object:retention_get", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		writeDriverError(w, "GetObjectRetention", err)
		return
	}

	writeJSON(w, http.StatusOK, objectRetentionResponse{Retention: ret})
}

// userPutObjectRetentionHandler implements PUT
// /api/v1/user/regions/{regionId}/buckets/{bid}/o/{key}/retention?versionId=...&bypassGovernance=true|false
//
// Body: {"mode": "GOVERNANCE"|"COMPLIANCE", "retainUntilDate": "..."}
//
// The audit action is chosen based on the requested vs prior state:
//   - object:retention_set when no prior retention existed
//   - object:retention_extended when extending (new date > old date)
//   - object:retention_reduced when reducing (new date < old date)
//
// We GET the existing retention before the PUT to decide the audit
// action. This adds one round-trip on the write path but the audit
// distinction is more valuable than the saved RTT for compliance.
// (S3 itself is the authoritative gate on whether the change is
// allowed — we don't pre-decide that.)
func (s *Server) userPutObjectRetentionHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	key, _ := url.PathUnescape(chi.URLParam(r, "key"))
	versionID := r.URL.Query().Get("versionId")
	bypassGovernance := r.URL.Query().Get("bypassGovernance") == "true"
	if bid == "" || key == "" || versionID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			"bucket id, key, and versionId required")
		return
	}

	var ret driver.ObjectLockRetention
	if err := json.NewDecoder(r.Body).Decode(&ret); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}
	if ret.Mode != driver.ObjectLockGovernance && ret.Mode != driver.ObjectLockCompliance {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			`mode must be "GOVERNANCE" or "COMPLIANCE"`)
		return
	}
	if ret.RetainUntilDate.IsZero() {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			"retainUntilDate required")
		return
	}
	if ret.RetainUntilDate.Before(time.Now()) {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			"retainUntilDate must be in the future")
		return
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		s.auditFailure(r, "object:retention_set", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	if !drv.ObjectLockSupport() {
		s.auditFailureDetail(r, "object:retention_set", regionObjectResource(region, bid),
			"driver does not support Object Lock")
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED",
			"This backend driver does not support Object Lock.",
			map[string]interface{}{"capability": "objectLock"})
		return
	}

	// Determine the audit action by comparing against the existing
	// retention. A backend error on this probe is non-fatal — fall
	// back to retention_set so the PUT path still runs.
	action := "object:retention_set"
	if prior, perr := drv.GetObjectRetention(r.Context(), bid, key, versionID); perr == nil && prior != nil {
		if ret.RetainUntilDate.After(prior.RetainUntilDate) {
			action = "object:retention_extended"
		} else if ret.RetainUntilDate.Before(prior.RetainUntilDate) {
			action = "object:retention_reduced"
		} else {
			// Same date — count as a re-assert/set.
			action = "object:retention_set"
		}
	}

	if err := drv.PutObjectRetention(r.Context(), bid, key, versionID, ret, bypassGovernance); err != nil {
		s.auditFailure(r, action, regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		if errors.Is(err, driver.ErrUnsupported) {
			writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", err.Error(),
				map[string]interface{}{"capability": "objectLock"})
			return
		}
		writeDriverError(w, "PutObjectRetention", err)
		return
	}

	detail := regionAuditDetail(region) +
		",versionId=" + versionID +
		",mode=" + string(ret.Mode)
	if bypassGovernance {
		detail += ",bypassGovernance=true"
	}
	s.auditEmit(r, action, regionObjectResource(region, bid),
		audit.ResultSuccess, detail)

	writeJSON(w, http.StatusOK, objectRetentionResponse{Retention: &ret})
}

// userGetObjectLegalHoldHandler implements GET
// /api/v1/user/regions/{regionId}/buckets/{bid}/o/{key}/legal-hold?versionId=...
//
// versionId is required — same per-version pinning as retention.
// Returns {on: false} when no legal hold has been set rather than
// a 404, so the FE can render the toggle without special-casing.
func (s *Server) userGetObjectLegalHoldHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	key, _ := url.PathUnescape(chi.URLParam(r, "key"))
	versionID := r.URL.Query().Get("versionId")
	if bid == "" || key == "" || versionID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			"bucket id, key, and versionId required")
		return
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		s.auditFailure(r, "object:legal_hold_get", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	if !drv.ObjectLockSupport() {
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED",
			"This backend driver does not support Object Lock.",
			map[string]interface{}{"capability": "objectLock"})
		return
	}

	on, err := drv.GetObjectLegalHold(r.Context(), bid, key, versionID)
	if err != nil {
		s.auditFailure(r, "object:legal_hold_get", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		writeDriverError(w, "GetObjectLegalHold", err)
		return
	}

	writeJSON(w, http.StatusOK, objectLegalHoldResponse{On: on})
}

// userPutObjectLegalHoldHandler implements PUT
// /api/v1/user/regions/{regionId}/buckets/{bid}/o/{key}/legal-hold?versionId=...
//
// Body: {"on": true|false}. Toggles the legal-hold flag. The audit
// action is chosen by which direction the toggle moved:
//   - object:legal_hold_set when going off → on
//   - object:legal_hold_released when going on → off
//
// We pre-fetch the existing state to decide the action — the audit
// distinction matters for compliance forensics.
func (s *Server) userPutObjectLegalHoldHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	key, _ := url.PathUnescape(chi.URLParam(r, "key"))
	versionID := r.URL.Query().Get("versionId")
	if bid == "" || key == "" || versionID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			"bucket id, key, and versionId required")
		return
	}

	var body objectLegalHoldResponse
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		s.auditFailure(r, "object:legal_hold_set", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	if !drv.ObjectLockSupport() {
		s.auditFailureDetail(r, "object:legal_hold_set", regionObjectResource(region, bid),
			"driver does not support Object Lock")
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED",
			"This backend driver does not support Object Lock.",
			map[string]interface{}{"capability": "objectLock"})
		return
	}

	// Choose action by toggle direction. A backend error on the
	// probe falls back to the simpler "set" action so the PUT still
	// runs and gets audited.
	action := "object:legal_hold_set"
	if !body.On {
		action = "object:legal_hold_released"
	}
	if prior, perr := drv.GetObjectLegalHold(r.Context(), bid, key, versionID); perr == nil {
		if prior && !body.On {
			action = "object:legal_hold_released"
		} else if !prior && body.On {
			action = "object:legal_hold_set"
		}
	}

	if err := drv.PutObjectLegalHold(r.Context(), bid, key, versionID, body.On); err != nil {
		s.auditFailure(r, action, regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		if errors.Is(err, driver.ErrUnsupported) {
			writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", err.Error(),
				map[string]interface{}{"capability": "objectLock"})
			return
		}
		writeDriverError(w, "PutObjectLegalHold", err)
		return
	}

	s.auditEmit(r, action, regionObjectResource(region, bid),
		audit.ResultSuccess,
		regionAuditDetail(region)+",versionId="+versionID)

	writeJSON(w, http.StatusOK, objectLegalHoldResponse{On: body.On})
}
