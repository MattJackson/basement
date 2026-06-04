package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// withClaims injects *Claims into a request context the same way
// the auth Middleware does, so RequireCapability can read them.
func withClaims(r *http.Request, c *Claims) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), claimsKey, c))
}

// withCID stuffs a chi URL param "cid" into the request so the
// cluster-scoped branch of RequireCapability can read it without
// mounting a full router.
func withCID(r *http.Request, cid string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("cid", cid)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// TestRequireCapability covers the ADR-0009 Phase C middleware contract.
func TestRequireCapability(t *testing.T) {
	uiAdmin := &Claims{ActiveRole: &ActiveRole{Kind: "ui-admin"}}
	clusterAdminA := &Claims{ActiveRole: &ActiveRole{Kind: "cluster-admin", Cluster: "cluster-a"}}
	userRole := &Claims{ActiveRole: &ActiveRole{Kind: "user"}}

	cases := []struct {
		name       string
		capability string
		claims     *Claims // nil → no claims in context
		cid        string
		want       int
	}{
		{"ui-admin wiring update no cid", CapClusterWiringUpdate, uiAdmin, "", http.StatusOK},
		{"ui-admin cluster buckets create denied", CapClusterBucketsCreate, uiAdmin, "cluster-a", http.StatusForbidden},
		{"ui-admin reads any cluster wiring", CapClusterWiringRead, uiAdmin, "cluster-a", http.StatusOK},
		{"cluster-admin buckets create matching cid", CapClusterBucketsCreate, clusterAdminA, "cluster-a", http.StatusOK},
		{"cluster-admin buckets create mismatched cid", CapClusterBucketsCreate, clusterAdminA, "cluster-b", http.StatusForbidden},
		{"cluster-admin reads own wiring", CapClusterWiringRead, clusterAdminA, "cluster-a", http.StatusOK},
		{"cluster-admin reads other wiring denied", CapClusterWiringRead, clusterAdminA, "cluster-b", http.StatusForbidden},
		{"user any cluster cap denied", CapClusterBucketsCreate, userRole, "cluster-a", http.StatusForbidden},
		{"nil claims unauthorized", CapClusterWiringUpdate, nil, "", http.StatusUnauthorized},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.cid != "" {
				req = withCID(req, tc.cid)
			}
			if tc.claims != nil {
				req = withClaims(req, tc.claims)
			}

			rr := httptest.NewRecorder()
			RequireCapability(tc.capability)(next).ServeHTTP(rr, req)

			if rr.Code != tc.want {
				t.Fatalf("status = %d, want %d (body=%s)", rr.Code, tc.want, rr.Body.String())
			}
			if (rr.Code == http.StatusOK) != nextCalled {
				t.Fatalf("nextCalled = %v but status = %d", nextCalled, rr.Code)
			}
		})
	}
}
