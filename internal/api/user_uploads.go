package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mattjackson/basement/internal/driver"
)

// userInitMultipartUploadHandler handles POST /api/v1/user/clusters/{cid}/buckets/{bid}/multipart/init.
func (s *Server) userInitMultipartUploadHandler(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		Key         string `json:"key"`
		ContentType string `json:"contentType"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "invalid request body")
		return
	}

	if req.Key == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "key required in request body")
		return
	}

	drv, err := s.reg.For(r.Context(), cid)
	if err != nil {
		writeErrorSimple(w, http.StatusNotFound, "CLUSTER_NOT_FOUND", "Connection not found")
		return
	}

	upload, err := drv.CreateMultipart(r.Context(), bid, req.Key, req.ContentType)
	if err != nil {
		writeDriverError(w, "CreateMultipart", err)
		return
	}

	writeJSON(w, http.StatusOK, upload)
}

// userPresignUploadPartHandler handles POST /api/v1/user/clusters/{cid}/buckets/{bid}/multipart/{uploadId}/part/{partNum}/presign.
func (s *Server) userPresignUploadPartHandler(w http.ResponseWriter, r *http.Request) {
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

	uploadId := chi.URLParam(r, "uploadId")
	if uploadId == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "multipart upload id required")
		return
	}

	partNumStr := chi.URLParam(r, "partNum")
	if partNumStr == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "part number required")
		return
	}

	partNum, err := strconv.Atoi(partNumStr)
	if err != nil || partNum < 1 || partNum > 10000 {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "part number must be between 1 and 10000")
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

	multipartUpload := driver.MultipartUpload{
		UploadID: uploadId,
		Bucket:   bid,
	}

	presign, err := drv.PresignUploadPart(r.Context(), multipartUpload, partNum)
	if err != nil {
		writeDriverError(w, "PresignUploadPart", err)
		return
	}

	writeJSON(w, http.StatusOK, presign)
}

// userCompleteMultipartUploadHandler handles POST /api/v1/user/clusters/{cid}/buckets/{bid}/multipart/{uploadId}/complete.
func (s *Server) userCompleteMultipartUploadHandler(w http.ResponseWriter, r *http.Request) {
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

	uploadId := chi.URLParam(r, "uploadId")
	if uploadId == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "multipart upload id required")
		return
	}

	var req struct {
		Parts []struct {
			PartNumber int    `json:"partNumber"`
			ETag       string `json:"etag"`
		} `json:"parts"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "invalid request body")
		return
	}

	completedParts := make([]driver.CompletedPart, len(req.Parts))
	for i, p := range req.Parts {
		if p.PartNumber < 1 || p.PartNumber > 10000 {
			writeErrorSimple(w, http.StatusBadRequest, "INVALID", "part number must be between 1 and 10000")
			return
		}
		completedParts[i] = driver.CompletedPart{
			PartNumber: p.PartNumber,
			ETag:       p.ETag,
		}
	}

	drv, err := s.reg.For(r.Context(), cid)
	if err != nil {
		writeErrorSimple(w, http.StatusNotFound, "CLUSTER_NOT_FOUND", "Connection not found")
		return
	}

	multipartUpload := driver.MultipartUpload{
		UploadID: uploadId,
		Bucket:   bid,
	}

	if err := drv.CompleteMultipart(r.Context(), multipartUpload, completedParts); err != nil {
		writeDriverError(w, "CompleteMultipart", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// userAbortMultipartUploadHandler handles DELETE /api/v1/user/clusters/{cid}/buckets/{bid}/multipart/{uploadId}.
func (s *Server) userAbortMultipartUploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "DELETE required")
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

	uploadId := chi.URLParam(r, "uploadId")
	if uploadId == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "multipart upload id required")
		return
	}

	drv, err := s.reg.For(r.Context(), cid)
	if err != nil {
		writeErrorSimple(w, http.StatusNotFound, "CLUSTER_NOT_FOUND", "Connection not found")
		return
	}

	multipartUpload := driver.MultipartUpload{
		UploadID: uploadId,
		Bucket:   bid,
	}

	if err := drv.AbortMultipart(r.Context(), multipartUpload); err != nil {
		writeDriverError(w, "AbortMultipart", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
