package api

import (
	"context"
	"strings"

	"github.com/mattjackson/basement/internal/auth"
)

// userVisibleConnections returns connection IDs that the current user has any grant on,
// or owns. For admin users (role="admin"), all connections are visible.
// This is the filter used by GET /api/v1/user/clusters.
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

	// For non-admin users, check grants.
	if s.store == nil {
		return []string{}
	}
	userGrants := s.store.Grants(claims.UserID)
	if userGrants == nil || len(userGrants) == 0 {
		return []string{}
	}

	// Collect unique connection IDs from grants.
	connIDs := make(map[string]bool)
	for _, g := range userGrants {
		connID := ""
		bucketID := ""

		// Parse grant path: format is "connectionId/bucketId" or just "connectionId" for cluster-level.
		parts := strings.Split(g.Bucket, "/")
		if len(parts) >= 1 && parts[0] != "" {
			connID = parts[0]
		}
		if len(parts) >= 2 && parts[1] != "" {
			bucketID = parts[1]
		}

		if connID != "" {
			connIDs[connID] = true
		}
		if bucketID != "" {
			// Bucket grant implies connection visibility.
			connIDs[connID] = true
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

	// For non-admin users, check grants for this specific connection.
	if s.store == nil {
		return []string{}
	}
	userGrants := s.store.Grants(claims.UserID)
	if userGrants == nil || len(userGrants) == 0 {
		return []string{}
	}

	// Collect bucket IDs from grants matching this connection.
	bucketIDs := make(map[string]bool)
	for _, g := range userGrants {
		parts := strings.Split(g.Bucket, "/")
		if len(parts) >= 2 && parts[0] == connID && parts[1] != "" {
			bucketIDs[parts[1]] = true
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
