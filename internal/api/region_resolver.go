// Package api: region-to-connection resolver bridging the post-ADR-0002
// region-tier user model with the still-cluster-tier sync + share
// engines (v1.1.0g).
//
// The UI picker reads from /user/regions (ADR-0002 v1.1.0e) but the sync
// engine + share record expect a Connection.ID at the cluster layer.
// resolveRegionToConnection translates a UserRegion's canonical endpoint
// to the FIRST admin Connection registered at the same endpoint, so the
// FE can keep posting `regionId` in `srcConnectionId` / `connectionId`
// fields without back-end stack failures.
//
// Bridge semantics mirror the v1.1.0d Garage admin bridge in
// user_regions.go: canonical endpoint match via store.NormalizeEndpoint,
// per-driver config-key convention (`s3_endpoint` for Garage variants,
// `endpoint` for aws-s3 + minio).
//
// Errors are typed (sentinels) so handlers can map cleanly to specific
// HTTP responses — ErrNoAdminBridge gets 400 NO_ADMIN_BRIDGE with the
// endpoint surfaced; ErrRegionNotFound stays a 404 so we don't leak
// the existence of other users' regions.
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/mattjackson/basement/internal/store"
)

// ErrNoAdminBridge is returned by resolveRegionToConnection when the
// region exists and belongs to the caller but there is no admin
// Connection registered at the same canonical endpoint. The caller
// surfaces this to the user with the endpoint included so a cluster
// admin can register it at /admin/clusters/new.
var ErrNoAdminBridge = errors.New("no admin connection registered at this region's endpoint")

// ErrRegionNotFound is returned by resolveRegionToConnection when the
// regionID does not exist OR exists but belongs to a different user.
// Handlers map this to 404 (not 403) so the existence of other users'
// regions never leaks.
var ErrRegionNotFound = errors.New("region not found")

// resolveRegionToConnection returns the admin Connection.ID for a
// UserRegion's endpoint. Lookup is by canonical endpoint match
// (store.NormalizeEndpoint) against each admin Connection's S3
// endpoint configuration key.
//
// Used by sync + share handlers post-ADR-0002: the FE passes regionId
// where the engine expects connectionId. This resolver bridges.
//
// Returns ErrNoAdminBridge if no admin Connection exists at the same
// canonical endpoint as the region. The caller surfaces a clear error
// to the user: "this region has no admin bridge — ask cluster admin
// to register this endpoint at /admin/clusters/new".
//
// Returns ErrRegionNotFound when the region doesn't exist or isn't
// owned by the caller (handlers must 404 either case to avoid an
// existence leak).
//
// When multiple admin Connections share the same canonical endpoint
// (rare — two clusters registered against the same S3 service) the
// FIRST match wins and a slog warning is logged so an operator can
// see the ambiguity. Picking the first match is deterministic across
// boots because connections.List preserves on-disk order; a future
// cycle can promote this to operator-pickable config.
func (s *Server) resolveRegionToConnection(ctx context.Context, userID, regionID string) (string, error) {
	regions := s.regionsStore()
	if regions == nil {
		return "", errors.New("region keychain store is not wired")
	}

	region, err := regions.Get(ctx, regionID)
	if err != nil || region.UserID != userID {
		// Treat both not-found and owned-by-other as not-found — the
		// caller's 404 mapping prevents leaking the existence of other
		// users' regions.
		return "", ErrRegionNotFound
	}

	if s.conns == nil {
		return "", ErrNoAdminBridge
	}

	conns, err := s.conns.List(ctx)
	if err != nil {
		return "", err
	}

	target := region.Endpoint // already canonical per store.NormalizeEndpoint
	var firstMatch string
	matchCount := 0
	for _, c := range conns {
		raw := connectionS3Endpoint(c)
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
		if firstMatch == "" {
			firstMatch = c.ID
		}
		matchCount++
	}

	if firstMatch == "" {
		return "", ErrNoAdminBridge
	}

	if matchCount > 1 {
		s.logger.Warn("region resolver: multiple admin Connections at same endpoint — picking first",
			"endpoint", target, "regionId", regionID, "userId", userID,
			"connectionId", firstMatch, "matches", matchCount)
	}

	return firstMatch, nil
}

// regionEndpointForID is a small helper for sync/share handlers that
// need to include the bridge-failed region's endpoint in the
// NO_ADMIN_BRIDGE error response. Returns an empty string if the
// region can't be loaded (the handler is already on the error path —
// any further lookup failure shouldn't mask the original error).
func (s *Server) regionEndpointForID(ctx context.Context, userID, regionID string) string {
	regions := s.regionsStore()
	if regions == nil {
		return ""
	}
	r, err := regions.Get(ctx, regionID)
	if err != nil || r.UserID != userID {
		return ""
	}
	return r.Endpoint
}

// maybeResolveRegionField inspects a single connection-id field on a
// sync/share request body. If the value is a UserRegion ID owned by
// the caller, it returns the resolved Connection.ID for that region's
// endpoint; otherwise it returns the value unchanged (treating it as a
// real Connection.ID for back-compat).
//
// The bool return distinguishes "handled — continue" (true) from
// "error response written — stop" (false). On NO_ADMIN_BRIDGE the
// handler writes a 400 with the offending endpoint + the field name
// so the FE can render a targeted error pointing the user at
// /admin/clusters/new.
//
// Why we check ownership before treating it as a region: an attacker
// who guesses another user's region UUID must NOT be able to discover
// the corresponding admin Connection.ID via an error path. Unowned
// IDs fall through to the legacy connection lookup, which itself
// 404s for nonexistent connections.
func (s *Server) maybeResolveRegionField(w http.ResponseWriter, r *http.Request, userID, raw, fieldName string) (string, bool) {
	if raw == "" {
		// Pass through — downstream validation handles missing fields
		// with its own 4xx response.
		return raw, true
	}

	regions := s.regionsStore()
	if regions == nil {
		// No keychain wired — can't possibly be a region ID. Treat
		// as a Connection.ID for back-compat.
		return raw, true
	}

	region, err := regions.Get(r.Context(), raw)
	if err != nil || region.UserID != userID {
		// Either not a region at all, or a region we don't own. Fall
		// through to the legacy Connection.ID code path; if it's
		// neither, the downstream Get() returns CLUSTER_NOT_FOUND.
		return raw, true
	}

	// It IS a region owned by the caller — resolve to a Connection.ID.
	connID, err := s.resolveRegionToConnection(r.Context(), userID, raw)
	if err == nil {
		return connID, true
	}

	if errors.Is(err, ErrNoAdminBridge) {
		writeError(w, http.StatusBadRequest, "NO_ADMIN_BRIDGE",
			"This region has no admin bridge. Ask cluster admin to register this endpoint at /admin/clusters/new.",
			map[string]interface{}{
				"endpoint": region.Endpoint,
				"field":    fieldName,
			})
		return "", false
	}

	if errors.Is(err, ErrRegionNotFound) {
		// We just looked it up successfully above — a race with delete
		// is the only way to get here. Surface as 404 to keep the
		// error mapping consistent with /user/regions/{id}.
		writeErrorSimple(w, http.StatusNotFound, "REGION_NOT_FOUND", "Region not found")
		return "", false
	}

	writeErrorSimple(w, http.StatusInternalServerError, "REGION_RESOLVE_FAILED", err.Error())
	return "", false
}
