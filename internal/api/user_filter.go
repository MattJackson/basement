package api

import (
	"context"
	"strings"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/store"
)

// userVisibleConnections returns connection IDs that the current user can
// see in the user persona — either they're admin (sees everything), they
// own the connection, or they hold a UserRegion whose canonical endpoint
// matches one of the connection's s3_endpoint config values.
//
// Per ADR-0002 (v1.1.0e): visibility is sourced from the user's region
// keychain. The retired BucketGrants table previously held the same
// information at finer per-bucket grain — region-tier collapses that to
// a single (user, endpoint) entry per region.
//
// Returns nil only on a connections-store failure (treated as 500 by
// callers). An empty slice means "user sees no connections" and is the
// expected no-grant case for non-admin callers.
func (s *Server) userVisibleConnections(ctx context.Context) []string {
	claims, ok := auth.FromContext(ctx)
	if !ok {
		return nil
	}

	if s.conns == nil {
		return nil
	}

	conns, err := s.conns.List(ctx)
	if err != nil {
		return nil
	}

	// Admin users see all connections regardless of their personal
	// keychain — preserves the host-admin "browse anything" path.
	if claims.Role == "admin" {
		ids := make([]string, len(conns))
		for i, c := range conns {
			ids[i] = c.ID
		}
		return ids
	}

	// Non-admin users see connections backed by an endpoint that matches
	// one of their UserRegions, or that they own outright. Both checks
	// share the same connections.List so we walk it once.
	regions := s.regionsStore()
	endpoints := map[string]bool{}
	if regions != nil {
		regs, err := regions.ListForUser(ctx, claims.UserID)
		if err == nil {
			for _, r := range regs {
				if r.Endpoint != "" {
					endpoints[r.Endpoint] = true
				}
			}
		}
	}

	result := make([]string, 0)
	for _, c := range conns {
		if c.Owner != "" && c.Owner == claims.UserID {
			result = append(result, c.ID)
			continue
		}
		if len(endpoints) == 0 {
			continue
		}
		canon := canonicalizeConnEndpoint(c)
		if canon == "" {
			continue
		}
		if endpoints[canon] {
			result = append(result, c.ID)
		}
	}
	return result
}

// userOwnsConnection reports whether the current user is the owner field
// on the connection record. Kept as a separate predicate so callers can
// short-circuit visibility checks without re-reading the regions list.
func (s *Server) userOwnsConnection(ctx context.Context, connID string) bool {
	claims, ok := auth.FromContext(ctx)
	if !ok {
		return false
	}
	if s.conns == nil {
		return false
	}
	conn, err := s.conns.Get(ctx, connID)
	if err != nil {
		return false
	}
	return conn.Owner != "" && conn.Owner == claims.UserID
}

// canonicalizeConnEndpoint returns the connection's s3 endpoint in the
// same canonical form UserRegions stores. Mirrors the per-driver config
// key convention used in user_regions.go's bridge (Garage variants store
// "s3_endpoint"; aws-s3 + minio store "endpoint"). Returns empty string
// when the raw value is absent or unparseable.
func canonicalizeConnEndpoint(c store.Connection) string {
	raw := strings.TrimSpace(c.Config["s3_endpoint"])
	if raw == "" {
		raw = strings.TrimSpace(c.Config["endpoint"])
	}
	if raw == "" {
		return ""
	}
	canon, err := store.NormalizeEndpoint(raw)
	if err != nil {
		return ""
	}
	return canon
}
