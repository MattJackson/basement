package api

import (
	"context"

	"github.com/mattjackson/basement/internal/auth"
)

// userVisibleConnections returns connection IDs that the current user has any grant on,
// or owns. For admin users (role="admin"), all connections are visible.
// This is the filter used by GET /api/v1/user/clusters.
//
// Per ADR-0001 (v1.0.0b): visibility is sourced from BucketGrants
// (s.store.CredGrants()), not the retired legacy `Grant` policy table.
// A user "sees" a cluster iff they hold a BucketGrant on any bucket
// there. Cluster-wide admin scopes live in the policy enforcer now —
// the visibility filter only needs to surface clusters the user has
// SOMETHING to do on, and cred-grant presence is the right signal.
func (s *Server) userVisibleConnections(ctx context.Context) []string {
	claims, ok := auth.FromContext(ctx)
	if !ok {
		return nil
	}

	// Admin users see all connections.
	if claims.Role == "admin" {
		conns, err := s.conns.List(ctx)
		if err != nil {
			return nil
		}
		ids := make([]string, len(conns))
		for i, c := range conns {
			ids[i] = c.ID
		}
		return ids
	}

	// For non-admin users, derive visibility from BucketGrants.
	if s.store == nil || s.store.CredGrants() == nil {
		return []string{}
	}
	grants, err := s.store.CredGrants().ListForUser(ctx, claims.UserID)
	if err != nil || len(grants) == 0 {
		return []string{}
	}

	connIDs := make(map[string]bool)
	for _, g := range grants {
		if g.ConnectionID != "" {
			connIDs[g.ConnectionID] = true
		}
	}

	result := make([]string, 0, len(connIDs))
	for id := range connIDs {
		result = append(result, id)
	}
	return result
}

// userVisibleBuckets returns bucket IDs that the current user has any grant on within a cluster.
// This is the filter used by GET /api/v1/user/clusters/{cid}/buckets.
//
// Per ADR-0001 (v1.0.0b): sourced from BucketGrants; one BucketGrant
// per (user, cluster, bucket) triple, so the filter is a straight
// connectionID match.
func (s *Server) userVisibleBuckets(ctx context.Context, connID string) []string {
	claims, ok := auth.FromContext(ctx)
	if !ok {
		return nil
	}

	// Admin users see all buckets in the cluster.
	if claims.Role == "admin" {
		drv, err := s.reg.For(ctx, connID)
		if err != nil {
			return nil
		}
		buckets, err := drv.ListBuckets(ctx)
		if err != nil {
			return nil
		}
		ids := make([]string, len(buckets))
		for i, b := range buckets {
			ids[i] = b.ID
		}
		return ids
	}

	// For non-admin users, derive visibility from BucketGrants on this cluster.
	if s.store == nil || s.store.CredGrants() == nil {
		return []string{}
	}
	grants, err := s.store.CredGrants().ListForUser(ctx, claims.UserID)
	if err != nil || len(grants) == 0 {
		return []string{}
	}

	bucketIDs := make(map[string]bool)
	for _, g := range grants {
		if g.ConnectionID == connID && g.BucketID != "" {
			bucketIDs[g.BucketID] = true
		}
	}

	result := make([]string, 0, len(bucketIDs))
	for id := range bucketIDs {
		result = append(result, id)
	}
	return result
}

// userOwnsConnection checks if the current user owns a specific connection.
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

	// For v0.2.0, owner is always "org". This will be used when user-owned connections land.
	return conn.Owner == claims.UserID
}
