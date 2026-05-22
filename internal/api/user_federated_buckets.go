// Package api: user-persona federation endpoints (v1.6.0c).
//
// FederatedBuckets layer a "this bucket lives on multiple backends as the
// same logical bucket" concept on top of the per-user region keychain
// (ADR-0002). Each federation owns one primary + N replicas; the
// replication engine (v1.6.0b) continuously mirrors primary → replicas
// and tracks per-replica health.
//
// Authorization: USER tier only. Federations are first-class
// user-property — the operator who set one up is the only one who can
// see / edit / delete it. Ownership is verified via OwnerUserID; a
// mismatch collapses to 404 (never 403) so the API never leaks the
// existence of other users' federations. Same convention as
// user_backups.go and user_regions.go.
//
// Engine hooks:
//   - Create → engine.EnsureLoop so the new federation starts ticking
//     without waiting for an engine restart.
//   - Delete → engine.RemoveLoop to stop the per-federation goroutine.
//   - Update / Resync → engine.TriggerNow to re-evaluate the diff with
//     the new policy / replica list.
//   - Failover → engine.TriggerNow against the new primary's loop.
//
// The engine interface is narrow (federationEngine below) so tests can
// substitute a recording mock and assert "EnsureLoop was called for ID X"
// without spinning up the real per-federation goroutines.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/robfig/cron/v3"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/federation"
)

// federationEngine is the narrow slice of *federation.Engine the API
// handlers need. Defined as an interface so user_federated_buckets_test.go
// can substitute a recording mock without standing up the real
// per-federation goroutines.
type federationEngine interface {
	EnsureLoop(ctx context.Context, fbID string)
	RemoveLoop(fbID string)
	TriggerNow(fbID string) error
}

// federatedBucketTargetRequest is the wire shape for one (region, bucket)
// endpoint inside a create / update body. Health + lag fields are NOT
// accepted from the wire — those are engine-owned.
type federatedBucketTargetRequest struct {
	RegionID string `json:"regionId"`
	Bucket   string `json:"bucket"`
}

// federatedBucketCreateRequest is the body shape for POST + PUT on
// /user/federated-buckets. Policy is optional on create — when omitted
// the server applies federation.DefaultPolicy.
type federatedBucketCreateRequest struct {
	Name     string                          `json:"name"`
	Primary  federatedBucketTargetRequest    `json:"primary"`
	Replicas []federatedBucketTargetRequest  `json:"replicas"`
	Policy   *federation.FederationPolicy    `json:"policy,omitempty"`
}

// federatedBucketFailoverRequest is the body shape for
// POST /user/federated-buckets/{id}/failover.
type federatedBucketFailoverRequest struct {
	NewPrimaryRegionID string `json:"newPrimaryRegionId"`
	NewPrimaryBucket   string `json:"newPrimaryBucket"`
}

// nameRegex matches the v1.6.0c federation name policy:
// alphanumeric + dash + underscore, 3-64 chars. Kept tight so a
// future move into the URL path (e.g. /user/federated-buckets/by-name/{name})
// won't have to URL-escape user input.
var federationNameRegex = regexp.MustCompile(`^[A-Za-z0-9_-]{3,64}$`)

// federationCronParser parses the 5-field cron expressions allowed in
// FederationPolicy.Schedule when SyncMode=scheduled. Same configuration
// as backup.Scheduler (minute + hour + dom + month + dow + descriptors)
// so an operator picks up the same dialect across the product.
var federationCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// maxReplicasPerFederation caps how many replicas can ride on one
// federation. ADR-0005 floats "1-5 in the wizard; more via API"; the
// engine ticks every replica per federation, so we apply a sanity cap
// here so a fat-fingered client can't queue thousands of replicate
// goroutines per tick. Can be raised later without a schema change.
const maxReplicasPerFederation = 10

// validateFederationRequest returns the first user-visible reason a
// create / update body should be rejected, or "" if everything looks
// well-formed. Returns the typed error code so the API can map it to
// the right HTTP status.
func validateFederationRequest(req federatedBucketCreateRequest) (code, msg string) {
	name := strings.TrimSpace(req.Name)
	if !federationNameRegex.MatchString(name) {
		return "INVALID_NAME", "Name must be 3-64 characters of letters, digits, dashes, or underscores"
	}
	if strings.TrimSpace(req.Primary.RegionID) == "" || strings.TrimSpace(req.Primary.Bucket) == "" {
		return "INVALID_PRIMARY", "Primary region and bucket are required"
	}
	if len(req.Replicas) == 0 {
		return "INVALID_REPLICAS", "At least one replica is required"
	}
	if len(req.Replicas) > maxReplicasPerFederation {
		return "TOO_MANY_REPLICAS", fmt.Sprintf("At most %d replicas per federation", maxReplicasPerFederation)
	}
	// Uniqueness check: (regionId, bucket) pairs must not collide across
	// primary + replicas. Otherwise the engine would replicate from a
	// bucket back to itself.
	seen := map[string]bool{}
	primaryKey := strings.TrimSpace(req.Primary.RegionID) + "|" + strings.TrimSpace(req.Primary.Bucket)
	seen[primaryKey] = true
	for i, rep := range req.Replicas {
		if strings.TrimSpace(rep.RegionID) == "" || strings.TrimSpace(rep.Bucket) == "" {
			return "INVALID_REPLICAS", fmt.Sprintf("Replica %d: region and bucket are required", i+1)
		}
		key := strings.TrimSpace(rep.RegionID) + "|" + strings.TrimSpace(rep.Bucket)
		if seen[key] {
			return "DUPLICATE_TARGET", fmt.Sprintf("Replica %d duplicates primary or another replica (region=%s bucket=%s)", i+1, rep.RegionID, rep.Bucket)
		}
		seen[key] = true
	}

	// Policy: optional on create. When supplied, validate enum + bounds.
	// The store records whatever we hand it; DefaultPolicy fills in on
	// the create path when *Policy is nil.
	if req.Policy != nil {
		p := *req.Policy
		switch p.SyncMode {
		case "", federation.SyncModeContinuous, federation.SyncModeScheduled:
			// ok — empty string is back-filled to continuous below.
		default:
			return "INVALID_SYNC_MODE", fmt.Sprintf("syncMode must be %q or %q", federation.SyncModeContinuous, federation.SyncModeScheduled)
		}
		if p.SyncMode == federation.SyncModeScheduled {
			if strings.TrimSpace(p.Schedule) == "" {
				return "INVALID_SCHEDULE", "schedule is required when syncMode=scheduled"
			}
			if _, err := federationCronParser.Parse(strings.TrimSpace(p.Schedule)); err != nil {
				return "INVALID_SCHEDULE", "schedule is not a valid cron expression: " + err.Error()
			}
		}
		if p.LagAlertSec != 0 && (p.LagAlertSec < 30 || p.LagAlertSec > 86400) {
			return "INVALID_LAG_ALERT", "lagAlertSec must be between 30 and 86400 (or 0 to use the default)"
		}
		// WriteQuorum is 1..(1+len(replicas)) when set. Zero means
		// "use the default" (DefaultPolicy returns 1).
		if p.WriteQuorum != 0 {
			maxQuorum := 1 + len(req.Replicas)
			if p.WriteQuorum < 1 || p.WriteQuorum > maxQuorum {
				return "INVALID_WRITE_QUORUM", fmt.Sprintf("writeQuorum must be between 1 and %d", maxQuorum)
			}
		}
	}
	return "", ""
}

// resolveFederationPolicy back-fills empty fields with DefaultPolicy
// values so the store never persists a half-empty policy block. Called
// from the create + update handlers after validation.
func resolveFederationPolicy(req *federation.FederationPolicy) federation.FederationPolicy {
	def := federation.DefaultPolicy()
	if req == nil {
		return def
	}
	out := *req
	if out.SyncMode == "" {
		out.SyncMode = def.SyncMode
	}
	if out.LagAlertSec == 0 {
		out.LagAlertSec = def.LagAlertSec
	}
	if out.WriteQuorum == 0 {
		out.WriteQuorum = def.WriteQuorum
	}
	return out
}

// targetFromRequest copies the wire shape into a federation.ReplicaTarget,
// trimming whitespace defensively. Health + lag fields stay zero — those
// are engine-owned and only the engine writes them via UpdateReplicaHealth.
func targetFromRequest(t federatedBucketTargetRequest) federation.ReplicaTarget {
	return federation.ReplicaTarget{
		RegionID: strings.TrimSpace(t.RegionID),
		Bucket:   strings.TrimSpace(t.Bucket),
	}
}

// federatedBucketResponse decorates the store record with engine-derived
// fields (computedHealth) so the FE doesn't have to recompute. The
// wrapping is shallow — every store field is passed through unchanged
// so a future field addition propagates without an explicit copy.
type federatedBucketResponse struct {
	federation.FederatedBucket
	// ComputedHealth is the overall federation health: "in-sync" if
	// every replica is healthy, otherwise the worst replica's health.
	// Surfaced separately from per-replica Health so the list view can
	// render a one-row summary without iterating the replicas array.
	ComputedHealth string `json:"computedHealth"`
}

// toFederatedBucketResponse builds the wire shape, computing the
// overall federation health from the per-replica records via
// federation.ComputeHealth.
func toFederatedBucketResponse(fb federation.FederatedBucket) federatedBucketResponse {
	now := time.Now().UTC()
	worst := federation.HealthInSync
	for _, rep := range fb.Replicas {
		// The engine writes Health into ReplicaTarget directly; if it's
		// empty (federation freshly created) recompute from the lag
		// fields so the FE still gets a coherent value.
		h := rep.Health
		if h == "" {
			h = federation.ComputeHealth(rep.LastSync, now, fb.Policy.LagAlertSec, 0)
		}
		if healthRank(h) > healthRank(worst) {
			worst = h
		}
	}
	return federatedBucketResponse{
		FederatedBucket: fb,
		ComputedHealth:  worst,
	}
}

// healthRank assigns an ordinal to each health value so the worst can
// be picked with a > comparison. Higher means worse.
func healthRank(h string) int {
	switch h {
	case federation.HealthBroken:
		return 3
	case federation.HealthStale:
		return 2
	case federation.HealthLagging:
		return 1
	}
	return 0
}

// userFederationsStore returns the federation store handle off the
// server, or nil if OpenFederated hasn't been called. Every handler
// nil-checks and emits 503 FEDERATIONS_NOT_WIRED — same shape as
// REGIONS_NOT_WIRED / BACKUPS_NOT_WIRED.
func (s *Server) userFederationsStore() federation.FederatedBuckets {
	return s.federations
}

// requireOwnedFederation looks up a FederatedBucket by id, returning it
// only when the caller's userID matches OwnerUserID. Returns 404 on any
// mismatch (not found OR owned by someone else) so the existence of
// other users' federations never leaks — same convention as
// loadOwnedBackup / requireOwnedRegion.
func (s *Server) requireOwnedFederation(w http.ResponseWriter, r *http.Request) (federation.FederatedBucket, string, bool) {
	store := s.userFederationsStore()
	if store == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "FEDERATIONS_NOT_WIRED",
			"Federation subsystem is not enabled on this server")
		return federation.FederatedBucket{}, "", false
	}
	claims, ok := auth.FromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return federation.FederatedBucket{}, "", false
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_ID", "Federation ID required")
		return federation.FederatedBucket{}, "", false
	}
	fb, err := store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, federation.ErrNotFound) {
			writeErrorSimple(w, http.StatusNotFound, "FEDERATION_NOT_FOUND", "Federation not found")
			return federation.FederatedBucket{}, "", false
		}
		writeErrorSimple(w, http.StatusInternalServerError, "FEDERATION_STORE_ERROR", err.Error())
		return federation.FederatedBucket{}, "", false
	}
	if fb.OwnerUserID != claims.UserID {
		writeErrorSimple(w, http.StatusNotFound, "FEDERATION_NOT_FOUND", "Federation not found")
		return federation.FederatedBucket{}, "", false
	}
	return fb, claims.UserID, true
}

// verifyUserOwnsRegion confirms the calling user owns the named
// UserRegion. Used by create + update + failover to refuse a federation
// that points at someone else's keys. Returns the typed error code so
// the caller can produce the matching HTTP status.
func (s *Server) verifyUserOwnsRegion(ctx context.Context, userID, regionID string) (code, msg string) {
	regions := s.regionsStore()
	if regions == nil {
		return "REGIONS_NOT_WIRED", "Region keychain store is not configured on this deployment"
	}
	region, err := regions.Get(ctx, regionID)
	if err != nil {
		return "INVALID_REGION", fmt.Sprintf("Region %q not found", regionID)
	}
	if region.UserID != userID {
		// 404 message but with INVALID_REGION code — we don't want to
		// leak "this is owned by someone else" any more than the
		// per-record handlers do.
		return "INVALID_REGION", fmt.Sprintf("Region %q not found", regionID)
	}
	return "", ""
}

// resourceFederation builds the canonical audit Resource string.
func resourceFederation(id string) string { return "federation:" + id }

// userCreateFederationHandler handles POST /api/v1/user/federated-buckets.
//
//   - 503 FEDERATIONS_NOT_WIRED when OpenFederated wasn't called
//   - 400 INVALID_* for any validation failure
//   - 404 INVALID_REGION when any (primary or replica) region isn't owned
//     by the caller
//   - 409 DUPLICATE_NAME when the caller already has a federation by
//     that name
//   - 201 with the stored record (decorated with computedHealth) on
//     success. engine.EnsureLoop is fired after persist so the new
//     federation starts replicating immediately.
func (s *Server) userCreateFederationHandler(w http.ResponseWriter, r *http.Request) {
	store := s.userFederationsStore()
	if store == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "FEDERATIONS_NOT_WIRED",
			"Federation subsystem is not enabled on this server")
		return
	}
	claims, ok := auth.FromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}

	var req federatedBucketCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}
	if code, msg := validateFederationRequest(req); code != "" {
		writeErrorSimple(w, http.StatusBadRequest, code, msg)
		return
	}

	// Ownership check: every (primary + each replica) region must be
	// owned by the caller. Catches the "I copied a regionId from a
	// shared chat log" mistake before the engine tries to use someone
	// else's key. Errors surface as 400 INVALID_REGION (not 404) so the
	// wizard renders a targeted field error.
	if code, msg := s.verifyUserOwnsRegion(r.Context(), claims.UserID, strings.TrimSpace(req.Primary.RegionID)); code != "" {
		writeErrorSimple(w, http.StatusBadRequest, code, msg)
		return
	}
	for _, rep := range req.Replicas {
		if code, msg := s.verifyUserOwnsRegion(r.Context(), claims.UserID, strings.TrimSpace(rep.RegionID)); code != "" {
			writeErrorSimple(w, http.StatusBadRequest, code, msg)
			return
		}
	}

	replicas := make([]federation.ReplicaTarget, 0, len(req.Replicas))
	for _, rep := range req.Replicas {
		replicas = append(replicas, targetFromRequest(rep))
	}
	fb := federation.FederatedBucket{
		OwnerUserID: claims.UserID,
		Name:        strings.TrimSpace(req.Name),
		Primary:     targetFromRequest(req.Primary),
		Replicas:    replicas,
		Policy:      resolveFederationPolicy(req.Policy),
	}

	created, err := store.Create(r.Context(), fb)
	if err != nil {
		if errors.Is(err, federation.ErrDuplicateName) {
			s.auditFailure(r, "federation:create", resourceFederation(""), err)
			writeErrorSimple(w, http.StatusConflict, "DUPLICATE_NAME",
				"You already have a federation with this name. Pick a different name.")
			return
		}
		s.auditFailure(r, "federation:create", resourceFederation(""), err)
		writeErrorSimple(w, http.StatusInternalServerError, "FEDERATION_STORE_ERROR",
			"Failed to save federation: "+err.Error())
		return
	}

	// Wake the engine for the freshly-created federation so it starts
	// ticking without waiting for an engine restart. Best-effort — a
	// missing engine is a deploy that opted out of replication (e.g.
	// tests), and the store persisted record still survives a server
	// reboot.
	if s.federationEngine != nil {
		s.federationEngine.EnsureLoop(r.Context(), created.ID)
	}

	s.auditEmit(r, "federation:create", resourceFederation(created.ID),
		"success", fmt.Sprintf("name=%s replicas=%d", created.Name, len(created.Replicas)))

	writeJSON(w, http.StatusCreated, toFederatedBucketResponse(created))
}

// userListFederationsHandler handles GET /api/v1/user/federated-buckets.
// Returns the caller's federations as an array of federatedBucketResponse
// (each decorated with computedHealth).
func (s *Server) userListFederationsHandler(w http.ResponseWriter, r *http.Request) {
	store := s.userFederationsStore()
	if store == nil {
		// Empty list is the safe degraded response — FE renders "no
		// federations" rather than a 5xx, and the operator sees the
		// missing-wiring warning at boot.
		writeJSON(w, http.StatusOK, []federatedBucketResponse{})
		return
	}
	claims, ok := auth.FromContext(r.Context())
	if !ok || claims.UserID == "" {
		writeErrorSimple(w, http.StatusUnauthorized, "UNAUTHORIZED", "No active session")
		return
	}
	rows, err := store.ListForUser(r.Context(), claims.UserID)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "FEDERATION_STORE_ERROR",
			"Failed to list federations: "+err.Error())
		return
	}
	out := make([]federatedBucketResponse, 0, len(rows))
	for _, fb := range rows {
		out = append(out, toFederatedBucketResponse(fb))
	}
	writeJSON(w, http.StatusOK, out)
}

// userGetFederationHandler handles GET /api/v1/user/federated-buckets/{id}.
// 404 on not-yours-or-not-found.
func (s *Server) userGetFederationHandler(w http.ResponseWriter, r *http.Request) {
	fb, _, ok := s.requireOwnedFederation(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, toFederatedBucketResponse(fb))
}

// userUpdateFederationHandler handles PUT /api/v1/user/federated-buckets/{id}.
//
// The body shape is the same as create — primary, replicas, policy. Name
// can be renamed (re-validated for the same alphanumeric/dash/underscore
// rules and for per-user uniqueness; the store doesn't currently surface
// rename-collision so we collapse it into a regular store error). On
// success the engine is poked via TriggerNow so the new policy / replica
// list takes effect on the next tick rather than waiting up to
// tickInterval.
func (s *Server) userUpdateFederationHandler(w http.ResponseWriter, r *http.Request) {
	existing, _, ok := s.requireOwnedFederation(w, r)
	if !ok {
		return
	}

	var req federatedBucketCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}
	if code, msg := validateFederationRequest(req); code != "" {
		writeErrorSimple(w, http.StatusBadRequest, code, msg)
		return
	}
	// Re-verify region ownership — the regions list may have changed
	// since create (the operator could have deleted a key and pointed
	// at one they don't own).
	if code, msg := s.verifyUserOwnsRegion(r.Context(), existing.OwnerUserID, strings.TrimSpace(req.Primary.RegionID)); code != "" {
		writeErrorSimple(w, http.StatusBadRequest, code, msg)
		return
	}
	for _, rep := range req.Replicas {
		if code, msg := s.verifyUserOwnsRegion(r.Context(), existing.OwnerUserID, strings.TrimSpace(rep.RegionID)); code != "" {
			writeErrorSimple(w, http.StatusBadRequest, code, msg)
			return
		}
	}

	replicas := make([]federation.ReplicaTarget, 0, len(req.Replicas))
	for _, rep := range req.Replicas {
		replicas = append(replicas, targetFromRequest(rep))
	}
	patch := federation.FederatedBucket{
		Name:     strings.TrimSpace(req.Name),
		Primary:  targetFromRequest(req.Primary),
		Replicas: replicas,
		Policy:   resolveFederationPolicy(req.Policy),
	}

	store := s.userFederationsStore()
	updated, err := store.Update(r.Context(), existing.ID, patch)
	if err != nil {
		s.auditFailure(r, "federation:update", resourceFederation(existing.ID), err)
		if errors.Is(err, federation.ErrNotFound) {
			writeErrorSimple(w, http.StatusNotFound, "FEDERATION_NOT_FOUND", "Federation not found")
			return
		}
		writeErrorSimple(w, http.StatusInternalServerError, "FEDERATION_STORE_ERROR",
			"Failed to update federation: "+err.Error())
		return
	}

	// Re-evaluate immediately so the new policy / replica list takes
	// effect on the next tick rather than waiting up to tickInterval.
	// TriggerNow is non-blocking and best-effort.
	if s.federationEngine != nil {
		// EnsureLoop covers the edge where Update is the first time the
		// engine sees this federation (e.g. a manual edit on a record
		// the engine didn't pick up at boot).
		s.federationEngine.EnsureLoop(r.Context(), updated.ID)
		_ = s.federationEngine.TriggerNow(updated.ID)
	}

	s.auditEmit(r, "federation:update", resourceFederation(updated.ID),
		"success", fmt.Sprintf("name=%s replicas=%d", updated.Name, len(updated.Replicas)))

	writeJSON(w, http.StatusOK, toFederatedBucketResponse(updated))
}

// userDeleteFederationHandler handles DELETE /api/v1/user/federated-buckets/{id}.
// Removes the federation record + tears down the engine loop. Does NOT
// delete replica data on the backends — operators expect their data to
// remain on B2/Wasabi/Garage after they decommission a federation;
// destructive cleanup belongs in a separate explicit endpoint that
// v1.6.0c intentionally doesn't ship.
func (s *Server) userDeleteFederationHandler(w http.ResponseWriter, r *http.Request) {
	existing, _, ok := s.requireOwnedFederation(w, r)
	if !ok {
		return
	}
	store := s.userFederationsStore()
	if err := store.Delete(r.Context(), existing.ID); err != nil {
		s.auditFailure(r, "federation:delete", resourceFederation(existing.ID), err)
		writeErrorSimple(w, http.StatusInternalServerError, "FEDERATION_STORE_ERROR",
			"Failed to delete federation: "+err.Error())
		return
	}
	if s.federationEngine != nil {
		s.federationEngine.RemoveLoop(existing.ID)
	}
	s.auditEmit(r, "federation:delete", resourceFederation(existing.ID),
		"success", fmt.Sprintf("name=%s", existing.Name))
	w.WriteHeader(http.StatusNoContent)
}

// userFailoverFederationHandler handles
// POST /api/v1/user/federated-buckets/{id}/failover.
//
// Body: {newPrimaryRegionId, newPrimaryBucket}. The new primary must
// currently be one of the federation's replicas; the handler swaps the
// primary and that replica entry in storage, then pokes the engine to
// re-evaluate. The lag/health fields on the demoted-old-primary entry
// start zero so the engine treats it as fresh — first tick will
// recompute health from scratch.
func (s *Server) userFailoverFederationHandler(w http.ResponseWriter, r *http.Request) {
	existing, _, ok := s.requireOwnedFederation(w, r)
	if !ok {
		return
	}
	var req federatedBucketFailoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
		return
	}
	newRegion := strings.TrimSpace(req.NewPrimaryRegionID)
	newBucket := strings.TrimSpace(req.NewPrimaryBucket)
	if newRegion == "" || newBucket == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			"newPrimaryRegionId and newPrimaryBucket are required")
		return
	}

	// Find the replica entry matching the new-primary target. If it
	// isn't a current replica, refuse — failover only promotes existing
	// replicas (you can't fail over to a backend the federation has
	// never replicated to). The 404 message intentionally names the
	// constraint so the operator knows what went wrong.
	replicaIdx := -1
	for i, rep := range existing.Replicas {
		if rep.RegionID == newRegion && rep.Bucket == newBucket {
			replicaIdx = i
			break
		}
	}
	if replicaIdx < 0 {
		writeErrorSimple(w, http.StatusNotFound, "NOT_A_REPLICA",
			"newPrimaryRegionId/newPrimaryBucket is not currently a replica of this federation")
		return
	}

	// Build the patch: swap primary <-> that replica. Reset health/lag
	// on the new-primary entry (it becomes the source of truth — no
	// more lag to track), but preserve health/lag on the demoted entry
	// since the engine will recompute it on the next tick.
	oldPrimary := existing.Primary
	newPrimary := federation.ReplicaTarget{
		RegionID: existing.Replicas[replicaIdx].RegionID,
		Bucket:   existing.Replicas[replicaIdx].Bucket,
	}
	newReplicas := make([]federation.ReplicaTarget, 0, len(existing.Replicas))
	for i, rep := range existing.Replicas {
		if i == replicaIdx {
			// Replace the promoted replica with the demoted primary.
			newReplicas = append(newReplicas, federation.ReplicaTarget{
				RegionID: oldPrimary.RegionID,
				Bucket:   oldPrimary.Bucket,
			})
		} else {
			newReplicas = append(newReplicas, rep)
		}
	}

	patch := federation.FederatedBucket{
		Name:     existing.Name,
		Primary:  newPrimary,
		Replicas: newReplicas,
		Policy:   existing.Policy,
	}
	store := s.userFederationsStore()
	updated, err := store.Update(r.Context(), existing.ID, patch)
	if err != nil {
		s.auditFailure(r, "federation:failover", resourceFederation(existing.ID), err)
		writeErrorSimple(w, http.StatusInternalServerError, "FEDERATION_STORE_ERROR",
			"Failed to update federation: "+err.Error())
		return
	}
	if s.federationEngine != nil {
		s.federationEngine.EnsureLoop(r.Context(), updated.ID)
		_ = s.federationEngine.TriggerNow(updated.ID)
	}

	detail := fmt.Sprintf("old_primary=%s:%s new_primary=%s:%s",
		oldPrimary.RegionID, oldPrimary.Bucket,
		newPrimary.RegionID, newPrimary.Bucket)
	s.auditEmit(r, "federation:failover", resourceFederation(updated.ID), "success", detail)

	writeJSON(w, http.StatusOK, toFederatedBucketResponse(updated))
}

// userResyncFederationHandler handles
// POST /api/v1/user/federated-buckets/{id}/resync.
//
// Pokes the engine to re-evaluate the diff immediately rather than
// waiting for the next 10s tick. Used after a network outage where the
// operator knows there's a backlog and wants to drain it now. The
// engine recomputes the diff from scratch on every tick anyway, so
// this is just "skip to the next tick" rather than a separate
// re-replicate path.
func (s *Server) userResyncFederationHandler(w http.ResponseWriter, r *http.Request) {
	existing, _, ok := s.requireOwnedFederation(w, r)
	if !ok {
		return
	}
	if s.federationEngine != nil {
		// EnsureLoop in case the federation was created before the
		// engine was running (e.g. tests that wire the engine after the
		// federation row).
		s.federationEngine.EnsureLoop(r.Context(), existing.ID)
		if err := s.federationEngine.TriggerNow(existing.ID); err != nil {
			// TriggerNow only errors when no loop exists — log + carry
			// on rather than 500ing, the next tick still works.
			s.logger.Warn("federation:resync TriggerNow failed",
				"federationId", existing.ID, "error", err)
		}
	}
	s.auditSuccess(r, "federation:resync", resourceFederation(existing.ID))
	writeJSON(w, http.StatusOK, map[string]string{
		"id":     existing.ID,
		"status": "queued",
	})
}
