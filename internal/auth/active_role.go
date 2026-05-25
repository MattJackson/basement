package auth

import (
	"net/http"
	"strings"
)

// ActiveRoleMiddleware creates middleware that gates access to routes based on active role.
// Returns 403 FORBIDDEN if the user's active role doesn't match required kinds or cluster.
func ActiveRoleMiddleware(requiredKinds []string, requiredCluster string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := FromContext(r.Context())
			if !ok || claims == nil || claims.ActiveRole == nil {
				http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"No active role in session"}}`, http.StatusUnauthorized)
				return
			}

			ar := claims.ActiveRole

			// Check if active role kind is in the required list
			kindAllowed := false
			for _, kind := range requiredKinds {
				if ar.Kind == kind {
					kindAllowed = true
					break
				}
			}
			if !kindAllowed {
				http.Error(w, `{"error":{"code":"FORBIDDEN","message":"Active role not permitted for this route"}}`, http.StatusForbidden)
				return
			}

			// If cluster-specific gating is required, verify the cluster matches
			if requiredCluster != "" && ar.Kind == "cluster-admin" {
				if ar.Cluster != requiredCluster {
					http.Error(w, `{"error":{"code":"FORBIDDEN","message":"Active role not permitted for this route"}}`, http.StatusForbidden)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ActiveRoleClusterMiddleware is a convenience wrapper for cluster-admin routes.
// Requires activeRole.kind == "cluster-admin" AND activeRole.cluster == cid.
func ActiveRoleClusterMiddleware(cid string) func(http.Handler) http.Handler {
	return ActiveRoleMiddleware([]string{"cluster-admin"}, cid)
}

// ActiveRoleUIAdminMiddleware is a convenience wrapper for UI admin routes.
// Requires activeRole.kind == "ui-admin".
func ActiveRoleUIAdminMiddleware() func(http.Handler) http.Handler {
	return ActiveRoleMiddleware([]string{"ui-admin"}, "")
}

// ActiveRoleAnyAdminMiddleware allows cluster-admin OR ui-admin. Used as a
// coarse "you're not in user mode" gate on the /admin/* surface — defense
// in depth for routes that were missing per-route active-role gating.
//
// v1.13.28: introduced after a live smoke caught /api/v1/admin/clusters
// returning the cluster list (with admin_token leaked) to user-mode
// callers because the per-route gating was applied to the wrong chi
// group inside server.go.
func ActiveRoleAnyAdminMiddleware() func(http.Handler) http.Handler {
	return ActiveRoleMiddleware([]string{"cluster-admin", "ui-admin"}, "")
}

// RequireActiveRole checks if the user has an active role that matches at least one of the required kinds.
// Returns true if allowed, false otherwise. Used inline in handlers.
func RequireActiveRole(r *http.Request, requiredKinds []string) bool {
	claims, ok := FromContext(r.Context())
	if !ok || claims == nil || claims.ActiveRole == nil {
		return false
	}

	ar := claims.ActiveRole
	for _, kind := range requiredKinds {
		if ar.Kind == kind {
			return true
		}
	}
	return false
}

// RequireActiveRoleCluster checks if the user has an active role that is cluster-admin for the given cid.
func RequireActiveRoleCluster(r *http.Request, cid string) bool {
	claims, ok := FromContext(r.Context())
	if !ok || claims == nil || claims.ActiveRole == nil {
		return false
	}

	ar := claims.ActiveRole
	if ar.Kind != "cluster-admin" {
		return false
	}
	return ar.Cluster == cid
}

// ActiveRoleFromRequest extracts the active role from the JWT in the request.
func ActiveRoleFromRequest(r *http.Request) (*ActiveRole, bool) {
	claims, ok := FromContext(r.Context())
	if !ok || claims == nil || claims.ActiveRole == nil {
		return nil, false
	}
	return claims.ActiveRole, true
}

// IsUIAdminActiveRole checks if the user's active role is ui-admin.
func IsUIAdminActiveRole(r *http.Request) bool {
	return RequireActiveRole(r, []string{"ui-admin"})
}

// IsClusterAdminForClusterActiveRole checks if the user's active role is cluster-admin for the given cid.
func IsClusterAdminForClusterActiveRole(r *http.Request, cid string) bool {
	claims, ok := FromContext(r.Context())
	if !ok || claims == nil || claims.ActiveRole == nil {
		return false
	}
	ar := claims.ActiveRole
	return ar.Kind == "cluster-admin" && ar.Cluster == cid
}

// GetActiveClusterID returns the cluster ID from the active role if it's a cluster-admin.
func GetActiveClusterIDActiveRole(r *http.Request) string {
	claims, ok := FromContext(r.Context())
	if !ok || claims == nil || claims.ActiveRole == nil {
		return ""
	}
	ar := claims.ActiveRole
	if ar.Kind != "cluster-admin" {
		return ""
	}
	return ar.Cluster
}

// ExtractClusterFromPath extracts the cluster ID from a URL path like /admin/clusters/{cid}/...
func ExtractClusterFromPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "clusters" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
