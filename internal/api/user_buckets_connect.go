// Package api: user-persona "Add bucket access" endpoint (ADR-0001
// cycle v0.9.0e).
//
// POST /api/v1/user/buckets/connect: the user supplies their own S3
// credentials for a single bucket they're entitled to. basement:
//
//  1. Resolves or creates a Connection record pointing at the bucket's
//     S3 endpoint (garage-v1 driver for now).
//  2. Stores the user's S3 access_key + secret as a BucketGrant
//     (encrypted at rest).
//  3. Mints a bucket_user RoleAssignment so the policy enforcer lets
//     the user see + use this bucket.
//
// NO admin_token is involved — this is the user tier. Per ADR-0001 the
// runtime (v0.9.0f, next cycle) will look up this Grant per-request and
// sign S3 calls with the user's key, so backend audit logs attribute
// activity to the right identity instead of the cluster's shared key.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/store"
)

// userBucketsConnectRequest is the body shape for POST
// /api/v1/user/buckets/connect. Field names are camelCase to match the
// rest of the user-tier API (see also user_clusters_create.go).
type userBucketsConnectRequest struct {
	Alias       string `json:"alias"`
	S3Endpoint  string `json:"s3Endpoint"`
	AccessKeyID string `json:"accessKeyId"`
	SecretKey   string `json:"secretKey"`
	Region      string `json:"region,omitempty"`
}

// userBucketsConnectResponse is what the handler returns on success.
// connectionId identifies the (possibly newly-created) Connection that
// fronts the S3 endpoint; bucketId is the alias the user supplied —
// for Garage that's the bucket key for ListBuckets purposes (a later
// cycle may resolve it via the driver and replace it with the real
// bucket UUID).
type userBucketsConnectResponse struct {
	ConnectionID string `json:"connectionId"`
	BucketID     string `json:"bucketId"`
	Alias        string `json:"alias"`
}

// userBucketsConnectHandler implements POST /api/v1/user/buckets/connect.
//
// 503 POLICY_NOT_WIRED / GRANTS_NOT_WIRED when subsystems aren't up
// (matches the prompt's guidance to gate).
// 400 INVALID_REQUEST for missing or malformed fields.
// 409 GRANT_DUPLICATE when a grant for (user, connection, alias) exists.
// 201 with userBucketsConnectResponse on success.
func (s *Server) userBucketsConnectHandler(w http.ResponseWriter, r *http.Request) {
	// Subsystem gates — the prompt mandates 503 if either is nil.
	if s.policy == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "POLICY_NOT_WIRED",
			"Policy subsystem is not configured on this deployment.")
		return
	}
	if s.store == nil || s.store.CredGrants() == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "GRANTS_NOT_WIRED",
			"Credential-grant store is not configured on this deployment.")
		return
	}

	claims, ok := auth.FromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	var req userBucketsConnectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	req.Alias = strings.TrimSpace(req.Alias)
	req.S3Endpoint = strings.TrimSpace(req.S3Endpoint)
	req.AccessKeyID = strings.TrimSpace(req.AccessKeyID)
	req.SecretKey = strings.TrimSpace(req.SecretKey)
	req.Region = strings.TrimSpace(req.Region)
	if req.Region == "" {
		req.Region = "us-east-1"
	}

	if req.Alias == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "alias is required")
		return
	}
	if req.S3Endpoint == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "s3Endpoint is required")
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

	// Light URL validation — must parse + have a scheme + host. Lets
	// the upstream Garage HEAD probe still catch unreachable hosts
	// later without blocking on DNS here.
	if u, err := url.Parse(req.S3Endpoint); err != nil || u.Scheme == "" || u.Host == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			"s3Endpoint must be a full URL with scheme and host")
		return
	}

	// Resolve or create the Connection that holds the S3 endpoint
	// metadata. Per the cycle prompt the driver is garage-v1 for now;
	// later cycles can broaden this when other backends grow user-tier
	// grant flows.
	conn, err := s.findOrCreateUserConnection(r.Context(), claims.UserID, req)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "CONNECTION_ERROR",
			"Failed to resolve connection: "+err.Error())
		return
	}

	// Mint the BucketGrant. Per ADR-0001 the alias IS the bucket ID
	// for ListBuckets purposes on Garage today — a later cycle can
	// resolve the alias to the real Garage bucket UUID via the driver
	// once the user-tier driver path lands.
	grantIn := store.BucketGrantInput{
		UserID:       claims.UserID,
		ConnectionID: conn.ID,
		BucketID:     req.Alias,
		AccessKeyID:  req.AccessKeyID,
		SecretKey:    req.SecretKey,
	}
	if _, err := s.store.CredGrants().Create(r.Context(), grantIn); err != nil {
		if errors.Is(err, store.ErrBucketGrantDuplicate) {
			writeErrorSimple(w, http.StatusConflict, "GRANT_DUPLICATE",
				"You already have a grant for this bucket on this endpoint.")
			return
		}
		writeErrorSimple(w, http.StatusInternalServerError, "GRANT_CREATE_FAILED",
			"Failed to store grant: "+err.Error())
		return
	}

	// Mint the bucket_user RoleAssignment so policy.Can returns true
	// for objects:list / objects:get / etc. on this bucket scope.
	// AssignRole is idempotent so a stale assignment from a deleted-
	// then-recreated grant doesn't error.
	scope := fmt.Sprintf("bucket:%s:%s", conn.ID, req.Alias)
	if err := s.policy.AssignRole(policy.RoleAssignment{
		UserID: claims.UserID,
		RoleID: "bucket_user",
		Scope:  scope,
	}); err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "ASSIGN_ROLE_FAILED",
			"Failed to assign bucket_user role: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(userBucketsConnectResponse{
		ConnectionID: conn.ID,
		BucketID:     req.Alias,
		Alias:        req.Alias,
	})
}

// findOrCreateUserConnection looks up an existing garage-v1 Connection
// whose config.s3_endpoint matches the request, returning it if found.
// Otherwise creates a new user-owned Connection labelled with the user
// + alias for easy admin attribution.
//
// Lookup is case-insensitive on the endpoint to match how operators
// type URLs (https://Garage.example.com vs https://garage.example.com)
// without proliferating near-duplicate Connection rows.
func (s *Server) findOrCreateUserConnection(ctx context.Context, userID string, req userBucketsConnectRequest) (store.Connection, error) {
	wantEndpoint := strings.ToLower(req.S3Endpoint)

	list, err := s.conns.List(ctx)
	if err != nil {
		return store.Connection{}, fmt.Errorf("listing connections: %w", err)
	}
	for _, c := range list {
		if c.Driver != store.DriverGarageV1 {
			continue
		}
		got := strings.ToLower(strings.TrimSpace(c.Config["s3_endpoint"]))
		if got != "" && got == wantEndpoint {
			return c, nil
		}
	}

	// No match — create a new user-scoped Connection. The Owner field
	// gets the userID; later admin UIs can filter org vs user
	// connections by it. Label embeds username + alias for at-a-glance
	// disambiguation in /admin/clusters.
	conn := store.Connection{
		ID:    uuid.NewString(),
		Label: fmt.Sprintf("%s — %s", userID, req.Alias),
		Driver: store.DriverGarageV1,
		Config: map[string]string{
			"s3_endpoint": req.S3Endpoint,
			"region":      req.Region,
		},
		Owner:     userID,
		CreatedAt: time.Now().UTC(),
	}
	created, err := s.conns.Create(ctx, conn)
	if err != nil {
		return store.Connection{}, fmt.Errorf("creating connection: %w", err)
	}
	return created, nil
}
