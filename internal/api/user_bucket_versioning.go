// Package api: user-tier bucket versioning + object-version endpoints
// (v1.10.0a).
//
// Sits under /api/v1/user/regions/{regionId}/buckets/{bid}/... and
// shares the requireOwnedRegion + regionDriver plumbing with the rest
// of the user-region surface. Capability gate: drivers that report
// VersioningSupport()==false return 501 NOT_SUPPORTED before any
// backend call, so the FE can render a "your backend doesn't support
// versioning" message without paying for a round trip.
//
// Audit events surfaced here:
//   - bucket:versioning_get    (read; success only at debug volume)
//   - bucket:versioning_enabled
//   - bucket:versioning_suspended
//   - object:version_list      (success only)
//   - object:version_get       (success only)
//   - object:version_delete    (destructive — always audited)
//
// Failures emit auditFailure for every action so an operator can grep
// "matthew tried to suspend versioning on bucket foo and got a 403."

package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/driver"
)

// versioningStatusResponse is the wire shape for GET versioning.
// Includes the capability flag inline so the FE can decide whether
// to render the toggle without a second round trip.
type versioningStatusResponse struct {
	Status     driver.VersioningStatus `json:"status"`
	Supported  bool                    `json:"supported"`
}

// versioningStatusRequest is the body shape for PUT versioning.
// "enabled" / "suspended" are the only accepted values — disabled
// is observable but not settable (S3 contract — once enabled, the
// only off state is suspended).
type versioningStatusRequest struct {
	Status string `json:"status"`
}

// objectVersionsResponse is the wire shape for GET object versions.
// nextVersionIDMarker is empty when the listing is not truncated.
type objectVersionsResponse struct {
	Versions            []driver.ObjectVersion `json:"versions"`
	NextVersionIDMarker string                 `json:"nextVersionIdMarker,omitempty"`
}

// userGetBucketVersioningHandler implements GET
// /api/v1/user/regions/{regionId}/buckets/{bid}/versioning.
//
// Returns the current versioning status plus the per-driver
// capability flag. Unsupported drivers (Garage v1/v2 today) return
// supported=false + status="disabled" without hitting the backend,
// so the FE can hide the toggle without a 501.
func (s *Server) userGetBucketVersioningHandler(w http.ResponseWriter, r *http.Request) {
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
		s.auditFailure(r, "bucket:versioning_get", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	if !drv.VersioningSupport() {
		// Don't 501 the read — surface the disabled+unsupported pair
		// so the FE can render the off-state UI without an error
		// banner. The mutating endpoints DO 501 so a direct caller
		// can't accidentally toggle on a driver that doesn't
		// implement the rest of the surface.
		writeJSON(w, http.StatusOK, versioningStatusResponse{
			Status:    driver.VersioningDisabled,
			Supported: false,
		})
		return
	}

	status, err := drv.GetVersioningStatus(r.Context(), bid)
	if err != nil {
		s.auditFailure(r, "bucket:versioning_get", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		writeDriverError(w, "GetVersioningStatus", err)
		return
	}

	writeJSON(w, http.StatusOK, versioningStatusResponse{
		Status:    status,
		Supported: true,
	})
}

// userPutBucketVersioningHandler implements PUT
// /api/v1/user/regions/{regionId}/buckets/{bid}/versioning.
//
// Body: {"status": "enabled" | "suspended"}. Drivers that don't
// support versioning return 501 NOT_SUPPORTED. Audit emits one of
// bucket:versioning_enabled / bucket:versioning_suspended depending
// on which transition was requested.
func (s *Server) userPutBucketVersioningHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	if bid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "bucket id required")
		return
	}

	var req versioningStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}

	var action string
	switch req.Status {
	case string(driver.VersioningEnabled):
		action = "bucket:versioning_enabled"
	case string(driver.VersioningSuspended):
		action = "bucket:versioning_suspended"
	default:
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			`status must be "enabled" or "suspended"`)
		return
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		s.auditFailure(r, action, regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	if !drv.VersioningSupport() {
		s.auditFailureDetail(r, action, regionObjectResource(region, bid),
			"driver does not support versioning")
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED",
			"This backend driver does not support bucket versioning.",
			map[string]interface{}{"capability": "versioning"})
		return
	}

	if req.Status == string(driver.VersioningEnabled) {
		err = drv.EnableVersioning(r.Context(), bid)
	} else {
		err = drv.SuspendVersioning(r.Context(), bid)
	}
	if err != nil {
		s.auditFailure(r, action, regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		// ErrUnsupported through the wrapper still maps to 501 via
		// writeDriverError, covering the (defensive) case where the
		// driver advertised supported=true but a specific call still
		// surfaces unsupported.
		writeDriverError(w, "PutVersioning", err)
		return
	}

	s.auditEmit(r, action, regionObjectResource(region, bid),
		audit.ResultSuccess, regionAuditDetail(region))

	// Echo the new state back so the FE can update its store without
	// a follow-up GET.
	writeJSON(w, http.StatusOK, versioningStatusResponse{
		Status:    driver.VersioningStatus(req.Status),
		Supported: true,
	})
}

// userListObjectVersionsHandler implements GET
// /api/v1/user/regions/{regionId}/buckets/{bid}/o/{key}/versions.
//
// The {key} path param is URL-encoded by the caller; we decode once
// here. The list is filtered to versions of THIS specific key (via
// prefix=key on the S3 call); the FE renders the result as a
// version-history table for that single object.
//
// Pagination uses an opaque marker (?marker=); empty for the first
// call. The response's nextVersionIdMarker is non-empty when the
// listing was truncated.
func (s *Server) userListObjectVersionsHandler(w http.ResponseWriter, r *http.Request) {
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
		s.auditFailure(r, "object:version_list", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	if !drv.VersioningSupport() {
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED",
			"This backend driver does not support object versioning.",
			map[string]interface{}{"capability": "versioning"})
		return
	}

	marker := r.URL.Query().Get("marker")
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	// prefix=key narrows the backend list to versions of THIS key.
	// The driver may still return adjacent keys that share the prefix
	// (e.g. "foo" + "foobar"), so we filter exact matches below.
	raw, next, err := drv.ListObjectVersions(r.Context(), bid, key, marker, limit)
	if err != nil {
		s.auditFailure(r, "object:version_list", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		writeDriverError(w, "ListObjectVersions", err)
		return
	}

	versions := make([]driver.ObjectVersion, 0, len(raw))
	for _, v := range raw {
		if v.Key == key {
			versions = append(versions, v)
		}
	}

	s.auditEmit(r, "object:version_list", regionObjectResource(region, bid),
		audit.ResultSuccess, regionAuditDetail(region))

	writeJSON(w, http.StatusOK, objectVersionsResponse{
		Versions:            versions,
		NextVersionIDMarker: next,
	})
}

// userGetObjectVersionHandler implements GET
// /api/v1/user/regions/{regionId}/buckets/{bid}/o/{key}/versions/{versionID}.
//
// Streams the specified version back to the caller. Mirrors the
// admin object-stream path: forwards Content-Type + Content-Length
// from the backend so the browser can render or download cleanly.
func (s *Server) userGetObjectVersionHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	key, _ := url.PathUnescape(chi.URLParam(r, "key"))
	versionID := chi.URLParam(r, "versionId")
	if bid == "" || key == "" || versionID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			"bucket id, key, and versionId required")
		return
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		s.auditFailure(r, "object:version_get", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	if !drv.VersioningSupport() {
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED",
			"This backend driver does not support object versioning.",
			map[string]interface{}{"capability": "versioning"})
		return
	}

	result, err := drv.GetObjectVersion(r.Context(), bid, key, versionID)
	if err != nil {
		s.auditFailure(r, "object:version_get", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		writeDriverError(w, "GetObjectVersion", err)
		return
	}
	defer func() {
		if result.Body != nil {
			_ = result.Body.Close()
		}
	}()

	s.auditEmit(r, "object:version_get", regionObjectResource(region, bid),
		audit.ResultSuccess, regionAuditDetail(region))

	if result.ContentType != "" {
		w.Header().Set("Content-Type", result.ContentType)
	}
	if result.ContentLength > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(result.ContentLength, 10))
	}
	if result.ETag != "" {
		w.Header().Set("ETag", result.ETag)
	}
	w.WriteHeader(http.StatusOK)
	if result.Body != nil {
		_, _ = io.Copy(w, result.Body)
	}
}

// userDeleteObjectVersionHandler implements DELETE
// /api/v1/user/regions/{regionId}/buckets/{bid}/o/{key}/versions/{versionID}.
//
// Permanently removes a single version row. Distinct from a regular
// DeleteObject (which inserts a delete marker on a versioned bucket).
// Always audited — this is one of the most destructive ops in the
// versioning surface and a forensic trail is essential.
func (s *Server) userDeleteObjectVersionHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	key, _ := url.PathUnescape(chi.URLParam(r, "key"))
	versionID := chi.URLParam(r, "versionId")
	if bid == "" || key == "" || versionID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			"bucket id, key, and versionId required")
		return
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		s.auditFailure(r, "object:version_delete", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	if !drv.VersioningSupport() {
		s.auditFailureDetail(r, "object:version_delete", regionObjectResource(region, bid),
			"driver does not support versioning")
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED",
			"This backend driver does not support object versioning.",
			map[string]interface{}{"capability": "versioning"})
		return
	}

	if err := drv.DeleteObjectVersion(r.Context(), bid, key, versionID); err != nil {
		s.auditFailure(r, "object:version_delete", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		// Defensive: drv.VersioningSupport() may have returned true
		// while the method itself errors with ErrUnsupported. The
		// writeDriverError mapping handles both shapes.
		if errors.Is(err, driver.ErrUnsupported) {
			writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", err.Error(),
				map[string]interface{}{"capability": "versioning"})
			return
		}
		writeDriverError(w, "DeleteObjectVersion", err)
		return
	}

	s.auditEmit(r, "object:version_delete", regionObjectResource(region, bid),
		audit.ResultSuccess, regionAuditDetail(region)+",versionId="+versionID)

	w.WriteHeader(http.StatusNoContent)
}
