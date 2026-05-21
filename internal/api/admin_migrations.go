// Package api: migration helpers — ADR-0001 cycle v0.9.0h.
//
// The v0.9.0d cycle stripped the access_key_id / secret_key fields from
// the Edit Cluster form but DID NOT touch existing Connection records on
// disk. v0.9.0f then added runtime gates that force user-tier S3 ops
// through a BucketGrant lookup, bypassing those legacy in-config creds
// entirely. The result: matthew's basement.pq.io has at least one
// Connection (the "classe" cluster) with creds still in config that no
// longer flow anywhere — he can't browse objects without first being
// granted access via the new BucketGrant pathway.
//
// This file ships the operator-side cleanup path:
//
//   GET    /api/v1/admin/migrations/orphan_creds
//          → list Connections whose config still has access_key_id set
//   POST   /api/v1/admin/migrations/orphan_creds/{cid}/grant
//          → for a specific cluster + selected buckets, mint a
//            BucketGrant + bucket_user role assignment for the named
//            user, then strip the creds from the Connection's config.
//
// Both endpoints are gated on host:manage_policies — only the operator
// who controls the basement matrix should also be allowed to migrate
// orphaned creds. The grant endpoint is destructive (writes to disk +
// edits the Connection), so it explicitly rolls back its in-memory
// BucketGrants on partial failure before the cred-strip step lands.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/store"
)

// orphanCred is the per-Connection wire shape returned by GET
// /admin/migrations/orphan_creds. accessKeyId is returned in full so
// the operator can verify which key is being migrated (vs e.g. the
// admin-token), but the secret is never returned — only a presence
// boolean. discoveredBuckets best-effort lists the bucket aliases the
// cluster reports, so the UI can pre-check the right rows.
type orphanCred struct {
	ConnectionID      string   `json:"connectionId"`
	Label             string   `json:"label"`
	Driver            string   `json:"driver"`
	AccessKeyID       string   `json:"accessKeyId"`
	HasSecretKey      bool     `json:"hasSecretKey"`
	DiscoveredBuckets []string `json:"discoveredBuckets"`
}

// orphanCredsResponse is the GET shape. Always non-nil orphans slice
// so the UI's banner logic can render "0 orphans" as "all clean".
type orphanCredsResponse struct {
	Orphans []orphanCred `json:"orphans"`
}

// migrateOrphanCredsRequest is the POST body. userId names the existing
// basement user the migrated creds will be granted to; bucketAliases
// is the operator-selected subset of discoveredBuckets to mint grants
// against. Empty bucketAliases is a 400 — without aliases there are no
// grants to mint and the cred-strip step would silently destroy the
// only credential reachable from disk.
type migrateOrphanCredsRequest struct {
	UserID         string   `json:"userId"`
	BucketAliases  []string `json:"bucketAliases"`
}

// migrateOrphanCredsResponse mirrors the prompt's contract:
// grantsCreated counts the per-bucket BucketGrant rows created and
// connectionUpdated reports whether the cred-strip step persisted.
type migrateOrphanCredsResponse struct {
	GrantsCreated     int  `json:"grantsCreated"`
	ConnectionUpdated bool `json:"connectionUpdated"`
}

// listOrphanCredsHandler implements GET /api/v1/admin/migrations/orphan_creds.
// Walks every Connection and surfaces those whose config still carries
// access_key_id (the legacy field). For Garage / Garage-v1 connections
// the handler also asks the driver registry to list buckets — best
// effort, errors are swallowed so an unreachable cluster still appears
// in the list (just without a discovered-bucket hint).
func (s *Server) listOrphanCredsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	if _, ok := s.requireCapability(w, r, "host:manage_policies", "host:*"); !ok {
		return
	}

	conns, err := s.conns.List(r.Context())
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR",
			"Failed to list connections: "+err.Error())
		return
	}

	orphans := make([]orphanCred, 0)
	for _, c := range conns {
		ak := strings.TrimSpace(c.Config["access_key_id"])
		if ak == "" {
			continue
		}

		entry := orphanCred{
			ConnectionID:      c.ID,
			Label:             c.Label,
			Driver:            c.Driver,
			AccessKeyID:       ak,
			HasSecretKey:      strings.TrimSpace(c.Config["secret_key"]) != "",
			DiscoveredBuckets: []string{},
		}

		// Best-effort bucket discovery via the driver. Only attempt for
		// Garage drivers — other backends may need the legacy creds
		// for ListBuckets (e.g. aws-s3), and we don't want a failed
		// admin-call to look like a missing orphan. Errors swallowed.
		if s.reg != nil && (c.Driver == store.DriverGarage || c.Driver == store.DriverGarageV1) {
			entry.DiscoveredBuckets = bestEffortDiscoverBuckets(r.Context(), s, c.ID)
		}

		orphans = append(orphans, entry)
	}

	writeJSON(w, http.StatusOK, orphanCredsResponse{Orphans: orphans})
}

// bestEffortDiscoverBuckets asks the driver registry to list buckets on
// the given connection and returns the aliases (preferred) or IDs as a
// fallback. Errors are swallowed deliberately — discovery is a UX
// nicety on the migration page, not a correctness guarantee.
func bestEffortDiscoverBuckets(ctx context.Context, s *Server, cid string) []string {
	out := []string{}
	drv, err := s.reg.For(ctx, cid)
	if err != nil {
		return out
	}
	buckets, err := drv.ListBuckets(ctx)
	if err != nil {
		return out
	}
	for _, b := range buckets {
		if len(b.Aliases) > 0 {
			out = append(out, b.Aliases[0])
			continue
		}
		// No alias — surface the raw bucket id so the operator at least
		// sees a row to opt into.
		if b.ID != "" {
			out = append(out, b.ID)
		}
	}
	return out
}

// migrateOrphanCredsHandler implements
// POST /api/v1/admin/migrations/orphan_creds/{cid}/grant. Behaviour
// follows the cycle prompt step-by-step:
//
//  1. Verify the Connection still has orphan creds (else 400).
//  2. Mint a BucketGrant per requested alias + a bucket_user assignment.
//     Track the grant IDs created so step 4 can roll back on failure.
//  3. Strip access_key_id + secret_key from the Connection's config and
//     persist via s.conns.Update.
//  4. On step-3 failure, roll back the BucketGrants from step 2 so disk
//     state stays internally consistent. Assignments are left in place:
//     they're idempotent + harmless on retry and the policy enforcer
//     correctly returns no caps if the Grant is missing.
//  5. Invalidate the registry cache so the next driver build for this
//     connection drops the stripped creds.
func (s *Server) migrateOrphanCredsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	if _, ok := s.requireCapability(w, r, "host:manage_policies", "host:*"); !ok {
		return
	}

	cid := strings.TrimSpace(chi.URLParam(r, "cid"))
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "Connection id is required")
		return
	}

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

	var req migrateOrphanCredsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST", "userId is required")
		return
	}
	cleanAliases := make([]string, 0, len(req.BucketAliases))
	for _, a := range req.BucketAliases {
		a = strings.TrimSpace(a)
		if a != "" {
			cleanAliases = append(cleanAliases, a)
		}
	}
	if len(cleanAliases) == 0 {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			"At least one bucket alias is required")
		return
	}

	conn, err := s.conns.Get(r.Context(), cid)
	if err != nil {
		writeRegistryForError(w, err)
		return
	}

	ak := strings.TrimSpace(conn.Config["access_key_id"])
	sk := strings.TrimSpace(conn.Config["secret_key"])
	if ak == "" || sk == "" {
		writeErrorSimple(w, http.StatusBadRequest, "NO_ORPHAN_CREDS",
			"Connection config does not carry orphaned access_key_id + secret_key.")
		return
	}

	// Step 2: mint grants. Track IDs so we can roll back the slice if
	// the cred-strip in step 3 fails. We mint grants serially rather
	// than transactionally — the underlying file store has no batch
	// API and a partial failure HERE simply leaves earlier grants in
	// place which the operator can rerun without harm (Create returns
	// ErrBucketGrantDuplicate on the same triple).
	createdGrants := make([]string, 0, len(cleanAliases))
	for _, alias := range cleanAliases {
		g, err := s.store.CredGrants().Create(r.Context(), store.BucketGrantInput{
			UserID:       req.UserID,
			ConnectionID: cid,
			BucketID:     alias,
			AccessKeyID:  ak,
			SecretKey:    sk,
		})
		if err != nil {
			if errors.Is(err, store.ErrBucketGrantDuplicate) {
				// Already exists — accept it and continue. The operator
				// likely retried; we don't want to double-create.
				continue
			}
			// Real failure — roll back what we created so far.
			rollbackGrants(r.Context(), s, createdGrants)
			writeErrorSimple(w, http.StatusInternalServerError, "GRANT_CREATE_FAILED",
				fmt.Sprintf("Failed to create grant for alias %q: %s", alias, err.Error()))
			return
		}
		createdGrants = append(createdGrants, g.ID)

		scope := scopeBucket(cid, alias)
		if err := s.policy.AssignRole(policy.RoleAssignment{
			UserID: req.UserID,
			RoleID: "bucket_user",
			Scope:  scope,
		}); err != nil {
			rollbackGrants(r.Context(), s, createdGrants)
			writeErrorSimple(w, http.StatusInternalServerError, "ASSIGN_ROLE_FAILED",
				fmt.Sprintf("Failed to assign bucket_user role at %q: %s", scope, err.Error()))
			return
		}
	}

	// Step 3: strip the orphan creds from the Connection's config. The
	// connections.Update path replaces the entire Config map if patch.Config
	// is non-nil, so we copy the live config minus the two keys and feed
	// THAT in. Other keys (admin_url, admin_token, s3_endpoint, region)
	// stay intact.
	newCfg := make(map[string]string, len(conn.Config))
	for k, v := range conn.Config {
		if k == "access_key_id" || k == "secret_key" {
			continue
		}
		newCfg[k] = v
	}
	patch := store.Connection{Config: newCfg}
	if _, err := s.conns.Update(r.Context(), cid, patch); err != nil {
		// Roll back grants — disk state otherwise diverges: grants
		// would credit the user with creds that disk still says are
		// in the Connection (confusing audit), and a retry would
		// re-attempt grant creation hitting the duplicate path.
		rollbackGrants(r.Context(), s, createdGrants)
		writeErrorSimple(w, http.StatusInternalServerError, "CONNECTION_UPDATE_FAILED",
			"Failed to strip orphan creds from Connection: "+err.Error())
		return
	}

	// Step 5: invalidate the cached driver so the next user-grant lookup
	// builds fresh against the post-strip config.
	if s.reg != nil {
		s.reg.Invalidate(cid)
	}

	writeJSON(w, http.StatusOK, migrateOrphanCredsResponse{
		GrantsCreated:     len(createdGrants),
		ConnectionUpdated: true,
	})
}

// rollbackGrants best-effort deletes the listed grant IDs. Errors are
// swallowed because the caller is already returning a 500 — surfacing
// the rollback error would mask the original cause and operator can
// inspect bucket_grants.json directly if state looks wrong.
func rollbackGrants(ctx context.Context, s *Server, grantIDs []string) {
	if s.store == nil || s.store.CredGrants() == nil {
		return
	}
	for _, id := range grantIDs {
		_ = s.store.CredGrants().Delete(ctx, id)
	}
}
