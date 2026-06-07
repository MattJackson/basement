package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// okHandler is the next handler in the chain; if it runs the request was
// allowed through the middleware.
func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// TestActiveRoleMiddleware_ClusterMismatchDeniedAnyKind locks in the
// fail-closed cluster gate: when a non-empty requiredCluster is configured,
// the cluster check fires for ANY allowed kind — not just cluster-admin.
// A ui-admin whose ar.Cluster != requiredCluster is DENIED (403), even
// though "ui-admin" is in requiredKinds.
func TestActiveRoleMiddleware_ClusterMismatchDeniedAnyKind(t *testing.T) {
	mw := ActiveRoleMiddleware([]string{"ui-admin"}, "cluster-a")

	// ui-admin carries no Cluster (empty), so it cannot match cluster-a.
	claims := &Claims{ActiveRole: &ActiveRole{Kind: "ui-admin"}}
	req := withClaims(httptest.NewRequest(http.MethodGet, "/admin/clusters/cluster-a", nil), claims)
	rec := httptest.NewRecorder()

	mw(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("ui-admin with mismatched cluster must be 403, got %d", rec.Code)
	}
}

// TestActiveRoleMiddleware_ClusterMatchAllowed confirms the gate still lets
// a matching cluster through (cluster-admin@cluster-a on a cluster-a route).
func TestActiveRoleMiddleware_ClusterMatchAllowed(t *testing.T) {
	mw := ActiveRoleMiddleware([]string{"cluster-admin"}, "cluster-a")

	claims := &Claims{ActiveRole: &ActiveRole{Kind: "cluster-admin", Cluster: "cluster-a"}}
	req := withClaims(httptest.NewRequest(http.MethodGet, "/admin/clusters/cluster-a", nil), claims)
	rec := httptest.NewRecorder()

	mw(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("matching cluster must pass, got %d", rec.Code)
	}
}

// TestActiveRoleMiddleware_NoClusterRequired confirms today's live behaviour
// is unchanged: with requiredCluster=="" the cluster check is skipped and
// the kind gate alone decides.
func TestActiveRoleMiddleware_NoClusterRequired(t *testing.T) {
	mw := ActiveRoleMiddleware([]string{"ui-admin"}, "")

	claims := &Claims{ActiveRole: &ActiveRole{Kind: "ui-admin"}}
	req := withClaims(httptest.NewRequest(http.MethodGet, "/admin/users", nil), claims)
	rec := httptest.NewRecorder()

	mw(okHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ui-admin with no cluster requirement must pass, got %d", rec.Code)
	}
}
