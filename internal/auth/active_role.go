package auth

import (
	"net/http"
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

			// If cluster-specific gating is required, verify the cluster
			// matches for ANY kind (fail-closed). Live callers pass
			// requiredCluster=="" so today's behaviour is unchanged; this
			// closes a latent bypass for any future caller that gates a
			// non-cluster-admin kind on a specific cluster.
			if requiredCluster != "" {
				if ar.Cluster != requiredCluster {
					http.Error(w, `{"error":{"code":"FORBIDDEN","message":"Active role not permitted for this route"}}`, http.StatusForbidden)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// NOTE (ADR-0009 Phase C, v2.0.0-beta.39): the cluster active-role
// middleware that used to gate per-cluster routes —
// ActiveRoleClusterMiddleware(cid) and ActiveRoleClusterMiddlewareFromPath()
// — were DELETED here. The latter carried the "UI Admin is super-admin —
// passes any cluster route" branch that was the root cause of the recurring
// cluster-contents leak class (beta.6, beta.30, beta.36). Cluster routes now
// gate on RequireCapability(cluster.*) in internal/api/server.go, where UI
// Admin holds wiring caps but NOT contents caps. Do not reintroduce a
// role-kind super-admin shortcut for cluster routes.

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
