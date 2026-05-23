// Package api: user-tier bucket default server-side encryption
// endpoints (v1.10.0d).
//
// Sits under /api/v1/user/regions/{regionId}/buckets/{bid}/encryption.
// Shares the requireOwnedRegion + regionDriver plumbing with the rest
// of the user-region surface. Capability gate: drivers that report
// SSESupport()=(false, false) return 501 NOT_SUPPORTED on mutating
// paths, and supported flags on the GET so the FE can hide the card.
//
// Audit events surfaced here:
//   - bucket:encryption_enabled        (PUT on a never-configured bucket)
//   - bucket:encryption_disabled       (DELETE)
//   - bucket:encryption_algorithm_changed (PUT changes Algorithm)
//   - bucket:encryption_kms_key_changed   (PUT changes KMSKeyID)
//
// Failures emit auditFailure for every action so an operator can grep
// "who tried to set SSE-KMS on bucket foo and got a 501".
//
// Per-axis capability gating: the API rejects algorithm=AES256 when
// SSE-S3 is unsupported and algorithm=aws:kms when SSE-KMS is
// unsupported, independent of the SSESupport() overall flag. This
// lets a backend with KES misconfigured advertise (true, false) and
// have the API surface the right error per request.

package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/driver"
)

// bucketEncryptionResponse is the wire shape for GET encryption.
// Includes the two capability flags inline so the FE can decide
// whether to render the SSE-S3 toggle, the SSE-KMS toggle, both, or
// hide the card entirely without a second round trip.
type bucketEncryptionResponse struct {
	Enabled      bool                `json:"enabled"`
	Algorithm    driver.SSEAlgorithm `json:"algorithm,omitempty"`
	KMSKeyID     string              `json:"kmsKeyId,omitempty"`
	BucketKey    bool                `json:"bucketKey,omitempty"`
	SupportedS3  bool                `json:"supportedS3"`
	SupportedKMS bool                `json:"supportedKms"`
}

// bucketEncryptionRequest is the body shape for PUT encryption.
// Mirrors the driver.BucketEncryption shape without the SupportedX
// flags (those are derived state, not input).
type bucketEncryptionRequest struct {
	Algorithm driver.SSEAlgorithm `json:"algorithm"`
	KMSKeyID  string              `json:"kmsKeyId,omitempty"`
	BucketKey bool                `json:"bucketKey,omitempty"`
}

// userGetBucketEncryptionHandler implements GET
// /api/v1/user/regions/{regionId}/buckets/{bid}/encryption.
//
// Returns the current encryption config plus the per-driver capability
// flags. Unsupported drivers (Garage v1/v2 today) return
// supportedS3=false + supportedKms=false + enabled=false without
// hitting the backend, so the FE can hide the settings card without
// a 501 error banner.
func (s *Server) userGetBucketEncryptionHandler(w http.ResponseWriter, r *http.Request) {
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
		s.auditFailure(r, "bucket:encryption_get", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	supS3, supKMS := drv.SSESupport()
	if !supS3 && !supKMS {
		// Mirror the Object Lock GET pattern — surface
		// supportedX=false rather than 501 so the FE can render the
		// off-state UI without an error banner. Mutating paths still
		// 501 for direct callers.
		writeJSON(w, http.StatusOK, bucketEncryptionResponse{
			Enabled:      false,
			SupportedS3:  false,
			SupportedKMS: false,
		})
		return
	}

	enc, err := drv.GetBucketEncryption(r.Context(), bid)
	if err != nil {
		s.auditFailure(r, "bucket:encryption_get", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		writeDriverError(w, "GetBucketEncryption", err)
		return
	}

	resp := bucketEncryptionResponse{
		SupportedS3:  supS3,
		SupportedKMS: supKMS,
	}
	if enc != nil {
		resp.Enabled = enc.Enabled
		resp.Algorithm = enc.Algorithm
		resp.KMSKeyID = enc.KMSKeyID
		resp.BucketKey = enc.BucketKey
	}
	writeJSON(w, http.StatusOK, resp)
}

// userPutBucketEncryptionHandler implements PUT
// /api/v1/user/regions/{regionId}/buckets/{bid}/encryption.
//
// Body: {"algorithm": "AES256"|"aws:kms", "kmsKeyId": "...", "bucketKey": bool}.
//
// Three audit events fire here depending on the state transition:
//   - bucket:encryption_enabled (was disabled → now enabled)
//   - bucket:encryption_algorithm_changed (was enabled, algorithm differs)
//   - bucket:encryption_kms_key_changed (was enabled with SSE-KMS, key differs)
//
// We pre-fetch the existing config to decide which actions to emit —
// the audit distinction matters for compliance forensics (a switch
// from AES256 to KMS is meaningfully different from a fresh enable).
//
// Drivers without SSE support, or without the specific axis the
// request asks for, return 501 NOT_SUPPORTED with a capability hint.
func (s *Server) userPutBucketEncryptionHandler(w http.ResponseWriter, r *http.Request) {
	region, _, ok := s.requireOwnedRegion(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	if bid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "bucket id required")
		return
	}

	var req bucketEncryptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
		return
	}

	if req.Algorithm != driver.SSEAlgorithmAES256 &&
		req.Algorithm != driver.SSEAlgorithmKMS {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			`algorithm must be "AES256" or "aws:kms"`)
		return
	}
	if req.Algorithm == driver.SSEAlgorithmKMS && req.KMSKeyID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			"kmsKeyId required when algorithm is aws:kms")
		return
	}

	drv, err := s.regionDriver(r, region)
	if err != nil {
		s.auditFailure(r, "bucket:encryption_enabled", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	supS3, supKMS := drv.SSESupport()
	// Per-axis capability gate. A backend with (true, false) accepts
	// SSE-S3 requests but rejects SSE-KMS — surface the right
	// capability hint per axis.
	if !supS3 && !supKMS {
		s.auditFailureDetail(r, "bucket:encryption_enabled",
			regionObjectResource(region, bid),
			"driver does not support server-side encryption")
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED",
			"This backend driver does not support server-side encryption.",
			map[string]interface{}{"capability": "sse"})
		return
	}
	if req.Algorithm == driver.SSEAlgorithmAES256 && !supS3 {
		s.auditFailureDetail(r, "bucket:encryption_enabled",
			regionObjectResource(region, bid),
			"driver does not support SSE-S3")
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED",
			"This backend driver does not support SSE-S3 (AES256).",
			map[string]interface{}{"capability": "sseS3"})
		return
	}
	if req.Algorithm == driver.SSEAlgorithmKMS && !supKMS {
		s.auditFailureDetail(r, "bucket:encryption_enabled",
			regionObjectResource(region, bid),
			"driver does not support SSE-KMS")
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED",
			"This backend driver does not support SSE-KMS.",
			map[string]interface{}{"capability": "sseKms"})
		return
	}

	// Pre-fetch existing config to decide audit actions. A backend
	// error on this probe is non-fatal — fall back to "enabled" so
	// the PUT path still runs and gets at least one audit event.
	priorEnabled := false
	priorAlgorithm := driver.SSEAlgorithm("")
	priorKMSKeyID := ""
	if prior, perr := drv.GetBucketEncryption(r.Context(), bid); perr == nil && prior != nil {
		priorEnabled = prior.Enabled
		priorAlgorithm = prior.Algorithm
		priorKMSKeyID = prior.KMSKeyID
	}

	enc := driver.BucketEncryption{
		Enabled:   true,
		Algorithm: req.Algorithm,
		KMSKeyID:  req.KMSKeyID,
		BucketKey: req.BucketKey,
	}
	if err := drv.PutBucketEncryption(r.Context(), bid, enc); err != nil {
		s.auditFailure(r, "bucket:encryption_enabled", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		if errors.Is(err, driver.ErrUnsupported) {
			writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", err.Error(),
				map[string]interface{}{"capability": "sse"})
			return
		}
		writeDriverError(w, "PutBucketEncryption", err)
		return
	}

	// Audit emission — at minimum one event fires per successful PUT
	// so a compliance auditor can grep "encryption changed on bucket".
	baseDetail := regionAuditDetail(region) + ",algorithm=" + string(enc.Algorithm)
	if !priorEnabled {
		s.auditEmit(r, "bucket:encryption_enabled",
			regionObjectResource(region, bid),
			audit.ResultSuccess, baseDetail)
	} else {
		if priorAlgorithm != enc.Algorithm {
			s.auditEmit(r, "bucket:encryption_algorithm_changed",
				regionObjectResource(region, bid),
				audit.ResultSuccess,
				baseDetail+",previousAlgorithm="+string(priorAlgorithm))
		}
		if enc.Algorithm == driver.SSEAlgorithmKMS && priorKMSKeyID != enc.KMSKeyID {
			s.auditEmit(r, "bucket:encryption_kms_key_changed",
				regionObjectResource(region, bid),
				audit.ResultSuccess,
				baseDetail+",kmsKeyId="+enc.KMSKeyID)
		}
		// If neither algorithm nor key changed, still emit a generic
		// "enabled" re-assert so the audit trail captures the
		// operator's intent verbatim.
		if priorAlgorithm == enc.Algorithm &&
			(enc.Algorithm != driver.SSEAlgorithmKMS || priorKMSKeyID == enc.KMSKeyID) {
			s.auditEmit(r, "bucket:encryption_enabled",
				regionObjectResource(region, bid),
				audit.ResultSuccess, baseDetail)
		}
	}

	writeJSON(w, http.StatusOK, bucketEncryptionResponse{
		Enabled:      true,
		Algorithm:    enc.Algorithm,
		KMSKeyID:     enc.KMSKeyID,
		BucketKey:    enc.BucketKey,
		SupportedS3:  supS3,
		SupportedKMS: supKMS,
	})
}

// userDeleteBucketEncryptionHandler implements DELETE
// /api/v1/user/regions/{regionId}/buckets/{bid}/encryption.
//
// Removes the bucket-level default encryption configuration entirely
// — new objects after the DELETE land unencrypted unless the client
// supplies per-request SSE headers. Idempotent on already-empty
// buckets (the driver swallows the "never configured" sentinel).
//
// Drivers without SSE support return 501 NOT_SUPPORTED.
func (s *Server) userDeleteBucketEncryptionHandler(w http.ResponseWriter, r *http.Request) {
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
		s.auditFailure(r, "bucket:encryption_disabled", regionObjectResource(region, bid), err)
		writeErrorSimple(w, http.StatusInternalServerError, "REGION_DRIVER_FAILED", err.Error())
		return
	}

	supS3, supKMS := drv.SSESupport()
	if !supS3 && !supKMS {
		s.auditFailureDetail(r, "bucket:encryption_disabled",
			regionObjectResource(region, bid),
			"driver does not support server-side encryption")
		writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED",
			"This backend driver does not support server-side encryption.",
			map[string]interface{}{"capability": "sse"})
		return
	}

	if err := drv.DeleteBucketEncryption(r.Context(), bid); err != nil {
		s.auditFailure(r, "bucket:encryption_disabled", regionObjectResource(region, bid), err)
		if isUserKeyRejected(err) {
			writeUserKeyRejected(w, region)
			return
		}
		if errors.Is(err, driver.ErrUnsupported) {
			writeError(w, http.StatusNotImplemented, "NOT_SUPPORTED", err.Error(),
				map[string]interface{}{"capability": "sse"})
			return
		}
		writeDriverError(w, "DeleteBucketEncryption", err)
		return
	}

	s.auditEmit(r, "bucket:encryption_disabled", regionObjectResource(region, bid),
		audit.ResultSuccess, regionAuditDetail(region))

	writeJSON(w, http.StatusOK, bucketEncryptionResponse{
		Enabled:      false,
		SupportedS3:  supS3,
		SupportedKMS: supKMS,
	})
}
