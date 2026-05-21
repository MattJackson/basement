package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

const opDeleteCluster = "delete:cluster"

// TestClusterResult represents the response from testing a cluster connection.
type TestClusterResult struct {
	Ok      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// listClustersHandler handles GET /api/v1/admin/clusters.
func (s *Server) listClustersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	conns, err := s.conns.List(r.Context())
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to list connections")
		return
	}

	if conns == nil {
		conns = []store.Connection{}
	}

	writeJSON(w, http.StatusOK, conns)
}

// createClusterHandler handles POST /api/v1/admin/clusters.
//
// Per ADR-0001 v0.9.0f: gated on cluster:create at "cluster:*"
// (creating a new cluster has no prior cid to scope to). The legacy
// admin-role middleware still applies as a coarse defense-in-depth
// layer per the prompt.
func (s *Server) createClusterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	if _, ok := s.requireCapability(w, r, "cluster:create", "cluster:*"); !ok {
		return
	}

	var spec store.Connection
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", "invalid request body", nil)
		return
	}

	// Validate label
	if ve := validateName("label", spec.Label, nil, ""); ve != nil {
		writeValidationError(w, ve)
		return
	}

	// Validate driver is supported
	if !supportedDriver(spec.Driver) {
		writeError(w, http.StatusBadRequest, "INVALID_DRIVER", "Unsupported driver. Supported drivers: garage, garage-v1, aws-s3", nil)
		return
	}

	// Require non-empty config
	if len(spec.Config) == 0 {
		writeError(w, http.StatusBadRequest, "CONFIG_REQUIRED", "Connection config map must be non-empty", nil)
		return
	}

	// Check label uniqueness using validation helper
	existingConns, listErr := s.conns.List(r.Context())
	if listErr != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to list connections for duplicate check")
		return
	}

	if ve := requireUniqueName("label", spec.Label, existingConns, func(c store.Connection) []string {
		return []string{c.Label}
	}); ve != nil {
		writeValidationError(w, ve)
		return
	}

	conn, err := s.conns.Create(r.Context(), spec)
	if err != nil {
		s.auditFailure(r, "cluster:create", "cluster:"+spec.Label, err)
		writeErrorSimple(w, http.StatusBadRequest, "CREATE_FAILED", "Failed to create connection")
		return
	}

	s.auditSuccess(r, "cluster:create", resourceCluster(conn.ID))
	writeJSON(w, http.StatusCreated, conn)
}

// getClusterHandler handles GET /api/v1/admin/clusters/{cid}.
func (s *Server) getClusterHandler(w http.ResponseWriter, r *http.Request) {
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

	writeJSON(w, http.StatusOK, conn)
}

// updateClusterHandler handles PATCH /api/v1/admin/clusters/{cid}.
//
// Per ADR-0001 v0.9.0f: gated on cluster:edit at "cluster:{cid}".
func (s *Server) updateClusterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "PATCH required")
		return
	}

	cid := chi.URLParam(r, "cid")
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id required")
		return
	}

	if _, ok := s.requireCapability(w, r, "cluster:edit", scopeCluster(cid)); !ok {
		return
	}

	var patch store.Connection
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", "invalid request body", nil)
		return
	}

	// Validate label if provided
	if patch.Label != "" {
		if ve := validateName("label", patch.Label, nil, ""); ve != nil {
			writeValidationError(w, ve)
			return
		}

		// Check uniqueness (excluding self) — the comment lied; the
		// freshman passed the full list including the cluster being
		// edited, so a no-op label change failed with DUPLICATE_LABEL.
		existingConns, listErr := s.conns.List(r.Context())
		if listErr != nil {
			writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to list connections for duplicate check")
			return
		}
		otherConns := make([]store.Connection, 0, len(existingConns))
		for _, c := range existingConns {
			if c.ID != cid {
				otherConns = append(otherConns, c)
			}
		}

		if ve := requireUniqueName("label", patch.Label, otherConns, func(c store.Connection) []string {
			return []string{c.Label}
		}); ve != nil {
			writeValidationError(w, ve)
			return
		}
	}

	// Validate driver if provided
	if patch.Driver != "" && !supportedDriver(patch.Driver) {
		writeError(w, http.StatusBadRequest, "INVALID_DRIVER", "Unsupported driver. Supported drivers: garage, garage-v1, aws-s3", nil)
		return
	}

	conn, err := s.conns.Update(r.Context(), cid, patch)
	if err != nil {
		s.auditFailure(r, "cluster:edit", resourceCluster(cid), err)
		// Map the real store error to the right code+status; the
		// old freshman code returned 404 CLUSTER_NOT_FOUND for any
		// error from Update, swallowing duplicate-label, unsupported-
		// driver, and disk-write failures. Operator saw the wrong
		// error message after a successful Edit Cluster save.
		msg := err.Error()
		switch {
		case strings.Contains(msg, "connection not found"):
			writeRegistryForError(w, err)
		case strings.Contains(msg, "duplicate label"):
			writeErrorSimple(w, http.StatusConflict, "DUPLICATE_LABEL", msg)
		case strings.Contains(msg, "unsupported driver"):
			writeErrorSimple(w, http.StatusBadRequest, "INVALID_DRIVER", msg)
		case strings.Contains(msg, "persisting"):
			writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", msg)
		default:
			writeErrorSimple(w, http.StatusInternalServerError, "UPDATE_FAILED", msg)
		}
		return
	}

	// Invalidate cached driver instance — the registry caches a
	// built driver per connection ID; without this, the cached
	// instance keeps its old config (e.g. missing s3_endpoint) and
	// every subsequent call uses stale credentials. Operator hit
	// this immediately after Edit Cluster save in v0.8.0d.25.
	if s.reg != nil {
		s.reg.Invalidate(cid)
	}

	s.auditSuccess(r, "cluster:edit", resourceCluster(cid))
	writeJSON(w, http.StatusOK, conn)
}

// armDeleteClusterHandler handles POST /api/v1/admin/clusters/{cid}/_arm-delete.
//
// Per ADR-0001 v0.9.0f: gated on cluster:delete at "cluster:{cid}".
// Both the arm and the DELETE check the capability — arming without
// the right capability should not even hand out a token.
func (s *Server) armDeleteClusterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	cid := chi.URLParam(r, "cid")
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id required")
		return
	}

	if _, ok := s.requireCapability(w, r, "cluster:delete", scopeCluster(cid)); !ok {
		return
	}

	// Confirm the connection exists before issuing a token.
	if _, err := s.conns.Get(r.Context(), cid); err != nil {
		writeRegistryForError(w, err)
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "SESSION_REQUIRED", "Session required")
		return
	}

	token := auth.MintConfirmToken(s.cfg.JWT.Secret, opDeleteCluster, cid, claims.UserID, confirmDeleteTTL)
	writeJSON(w, http.StatusOK, map[string]any{
		"token":            token,
		"expiresInSeconds": int(confirmDeleteTTL.Seconds()),
	})
}

// deleteClusterHandler handles DELETE /api/v1/admin/clusters/{cid}.
//
// Per ADR-0001 v0.9.0f: gated on cluster:delete at "cluster:{cid}".
func (s *Server) deleteClusterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "DELETE required")
		return
	}

	cid := chi.URLParam(r, "cid")
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id required")
		return
	}

	if _, ok := s.requireCapability(w, r, "cluster:delete", scopeCluster(cid)); !ok {
		return
	}

	confirm := r.Header.Get("X-Confirm-Delete")
	if confirm == "" {
		writeErrorSimple(w, http.StatusBadRequest, "CONFIRMATION_REQUIRED",
			"X-Confirm-Delete header required. POST /admin/clusters/{cid}/_arm-delete first to obtain a token.")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeErrorSimple(w, http.StatusUnauthorized, "SESSION_REQUIRED", "Session required")
		return
	}

	if err := auth.VerifyConfirmToken(s.cfg.JWT.Secret, confirm, opDeleteCluster, cid, claims.UserID); err != nil {
		switch {
		case errors.Is(err, auth.ErrConfirmMismatch):
			writeErrorSimple(w, http.StatusBadRequest, "CONFIRMATION_MISMATCH",
				"Token does not match this cluster or user. Re-arm with POST /admin/clusters/{cid}/_arm-delete.")
		default:
			writeErrorSimple(w, http.StatusBadRequest, "CONFIRMATION_INVALID",
				"Token invalid or expired. Re-arm with POST /admin/clusters/{cid}/_arm-delete.")
		}
		return
	}

	if err := s.conns.Delete(r.Context(), cid); err != nil {
		s.auditFailure(r, "cluster:delete", resourceCluster(cid), err)
		writeRegistryForError(w, err)
		return
	}

	if s.reg != nil {
		s.reg.Invalidate(cid)
	}

	s.auditSuccess(r, "cluster:delete", resourceCluster(cid))
	writeJSON(w, http.StatusOK, map[string]string{"message": "Cluster deleted"})
}

// testClusterHandler handles POST /api/v1/admin/clusters/{cid}/_test.
//
// Per ADR-0001 v0.9.0f: gated on cluster:test at "cluster:{cid}".
func (s *Server) testClusterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	cid := chi.URLParam(r, "cid")
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id required")
		return
	}

	if _, ok := s.requireCapability(w, r, "cluster:test", scopeCluster(cid)); !ok {
		return
	}

	drv, err := s.reg.For(r.Context(), cid)
	if err != nil {
		writeRegistryForError(w, err)
		return
	}

	report, err := drv.HealthCheck(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, TestClusterResult{
			Ok:      false,
			Message: "Health check failed: " + err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, TestClusterResult{
		Ok:      report.Status == "healthy",
		Message: report.Status,
	})
}

// supportedDriver checks if a driver name is in the list of supported drivers.
func supportedDriver(name string) bool {
	return store.SupportedDrivers[name]
}

// AggregatedBucket represents a bucket from a specific connection with error info.
type AggregatedBucket struct {
	driver.Bucket
	ConnectionID string `json:"connectionId"`
}

// AggregatedBucketError represents an error from a specific connection during fan-out.
type AggregatedBucketError struct {
	ConnectionID string `json:"connectionId"`
	Message      string `json:"message"`
}

// AggregatedBucketsResponse is the response for cross-cluster bucket listing.
type AggregatedBucketsResponse struct {
	Buckets []AggregatedBucket   `json:"buckets"`
	Errors  []AggregatedBucketError `json:"errors,omitempty"`
}

// listAllBucketsHandler handles GET /api/v1/admin/buckets (cross-cluster aggregate).
func (s *Server) listAllBucketsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	conns, err := s.conns.List(r.Context())
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to list connections")
		return
	}

	type result struct {
		bucket driver.Bucket
		connID string
		err    error
	}

	results := make([]result, 0, len(conns))
	mu := sync.Mutex{}
	var wg sync.WaitGroup
	concurrency := 5

	sem := make(chan struct{}, concurrency)

	for _, conn := range conns {
		wg.Add(1)
		go func(conn store.Connection) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Per-cluster deadline so an unreachable cluster doesn't
			// block the aggregated response for the full per-call
			// driver timeout (10s). 3s is well above a healthy
			// Garage admin call but tight enough that operators see
			// the rest of their clusters' data immediately.
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()

			drv, err := s.reg.For(ctx, conn.ID)
			if err != nil {
				mu.Lock()
				results = append(results, result{connID: conn.ID, err: fmt.Errorf("building driver for %s: %w", conn.ID, err)})
				mu.Unlock()
				return
			}

			buckets, err := drv.ListBuckets(ctx)
			if err != nil {
				mu.Lock()
				results = append(results, result{connID: conn.ID, err: fmt.Errorf("listing buckets for %s: %w", conn.ID, err)})
				mu.Unlock()
				return
			}

			for _, b := range buckets {
				mu.Lock()
				results = append(results, result{bucket: b, connID: conn.ID})
				mu.Unlock()
			}
		}(conn)
	}

	wg.Wait()

	resp := AggregatedBucketsResponse{
		Buckets: make([]AggregatedBucket, 0),
		Errors:  make([]AggregatedBucketError, 0),
	}

	for _, r := range results {
		if r.err != nil {
			resp.Errors = append(resp.Errors, AggregatedBucketError{
				ConnectionID: r.connID,
				Message:      r.err.Error(),
			})
		} else {
			resp.Buckets = append(resp.Buckets, AggregatedBucket{
				Bucket:       r.bucket,
				ConnectionID: r.connID,
			})
		}
	}

writeJSON(w, http.StatusOK, resp)
}

// listBucketsByClusterHandler handles GET /admin/clusters/{cid}/buckets (connection-scoped).
func (s *Server) listBucketsByClusterHandler(w http.ResponseWriter, r *http.Request) {
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
}

// listKeysByClusterHandler handles GET /admin/clusters/{cid}/keys (connection-scoped).
func (s *Server) listKeysByClusterHandler(w http.ResponseWriter, r *http.Request) {
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
		writeRegistryForError(w, err)
		return
	}

	keys, err := drv.ListKeys(r.Context())
	if err != nil {
		writeDriverError(w, "ListKeys", err)
		return
	}

	if keys == nil {
		keys = []driver.Key{}
	}

	writeJSON(w, http.StatusOK, keys)
}


// AggregatedKey represents a key from a specific connection with error info.
type AggregatedKey struct {
	driver.Key
	ConnectionID string `json:"connectionId"`
}

// AggregatedKeyError represents an error from a specific connection during fan-out.
type AggregatedKeyError struct {
	ConnectionID string `json:"connectionId"`
	Message      string `json:"message"`
}

// AggregatedKeysResponse is the response for cross-cluster key listing.
type AggregatedKeysResponse struct {
	Keys   []AggregatedKey       `json:"keys"`
	Errors []AggregatedKeyError  `json:"errors,omitempty"`
}

// listAllKeysHandler handles GET /api/v1/admin/keys (cross-cluster aggregate).
func (s *Server) listAllKeysHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	conns, err := s.conns.List(r.Context())
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to list connections")
		return
	}

	type result struct {
		key  driver.Key
		connID string
		err  error
	}

	results := make([]result, 0, len(conns))
	mu := sync.Mutex{}
	var wg sync.WaitGroup
	concurrency := 5

	sem := make(chan struct{}, concurrency)

	for _, conn := range conns {
		wg.Add(1)
		go func(conn store.Connection) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Per-cluster 3s deadline so an unreachable cluster
			// doesn't block the aggregated keys list.
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()

			drv, err := s.reg.For(ctx, conn.ID)
			if err != nil {
				mu.Lock()
				results = append(results, result{connID: conn.ID, err: fmt.Errorf("building driver for %s: %w", conn.ID, err)})
				mu.Unlock()
				return
			}

			keys, err := drv.ListKeys(ctx)
			if err != nil {
				mu.Lock()
				results = append(results, result{connID: conn.ID, err: fmt.Errorf("listing keys for %s: %w", conn.ID, err)})
				mu.Unlock()
				return
			}

			for _, k := range keys {
				mu.Lock()
				results = append(results, result{key: k, connID: conn.ID})
				mu.Unlock()
			}
		}(conn)
	}

	wg.Wait()

	resp := AggregatedKeysResponse{
		Keys:   make([]AggregatedKey, 0),
		Errors: make([]AggregatedKeyError, 0),
	}

	for _, r := range results {
		if r.err != nil {
			resp.Errors = append(resp.Errors, AggregatedKeyError{
				ConnectionID: r.connID,
				Message:      r.err.Error(),
			})
		} else {
			resp.Keys = append(resp.Keys, AggregatedKey{
				Key:        r.key,
				ConnectionID: r.connID,
			})
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

