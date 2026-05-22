package api

import (
	"context"

	"github.com/mattjackson/basement/internal/auth"
)

// userVisibleConnections returns connection IDs that the current user
// is allowed to see in the user-persona context (POST /user/syncs,
// POST /user/shares). For admin-role callers, all connections are
// visible; for everyone else only connections they OWN (created via
// the legacy BYO flow; new region keychain entries are NOT
// connections).
//
// Per ADR-0002 (v1.1.0e) the per-bucket BucketGrant visibility model
// is retired — basement no longer maintains a "which buckets does
// user X see" cache. Bucket visibility is the backend's word, queried
// at request time via the user's UserRegion key. The non-admin
// callsites that still pivot off connection-id (sync engine src/dst
// pair, share-link target) therefore fall back to ownership only.
//
// Returns nil only on a store-level error so callers can surface a
// 500. An empty slice for "no visible connections" is normal.
func (s *Server) userVisibleConnections(ctx context.Context) []string {
	claims, ok := auth.FromContext(ctx)
	if !ok {
		return nil
	}

	conns, err := s.conns.List(ctx)
	if err != nil {
		return nil
	}

	if claims.Role == "admin" {
		ids := make([]string, len(conns))
		for i, c := range conns {
			ids[i] = c.ID
		}
		return ids
	}

	result := make([]string, 0)
	for _, c := range conns {
		if c.Owner == claims.UserID {
			result = append(result, c.ID)
		}
	}
	return result
}

// userOwnsConnection checks if the current user owns a specific
// connection. Used by user_syncs.go / user_shares.go to authorise
// connection-scoped writes.
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

	return conn.Owner == claims.UserID
}
