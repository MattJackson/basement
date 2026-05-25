// v1.13.29.1: rewrite of freshman's regression file.
//
// The freshman's original file used t.Logf instead of t.Errorf for
// every assertion, so every scenario "passed" regardless of the
// observed status. It also asserted expected=403 on scenarios that
// should return 200 (super-admin pass) and hid that mismatch behind
// the logf-only style. This rewrite asserts properly and covers only
// what can be reliably tested at the middleware level.
//
// What this file covers (the gating contract introduced in v1.13.29):
//   - user-mode active role → 403 on any /admin/* route
//   - cluster-admin@X → 403 on /admin/clusters/Y/* (scope mismatch)
//   - cluster-admin@X → 200/pass on /admin/clusters/X/*
//   - ui-admin → pass on any /admin/* route (super-admin)
//   - missing JWT activeRole → 401 (defense in depth)
//
// We use the listClusterAdminsHandler route (/admin/clusters/{cid}/admins)
// as the canary because it has minimal driver dependencies — the gating
// is checked before any backend interaction, so "200" here means the
// gate passed (the body is whatever the admin store returns, typically
// empty []).
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/store"
)

// activeRoleGatingScenario covers one row of the contract table.
type activeRoleGatingScenario struct {
	name         string
	method       string
	path         string
	activeRole   *auth.ActiveRole // nil → no activeRole claim → 401 expected
	wantStatuses []int            // 2xx OR the specific 4xx — accepts any in the set
}

// TestActiveRoleGating_Regression covers the 8-scenario contract for the
// active-role middleware introduced in v1.13.29.
//
// Per-cluster routes (clusterG) use /admin/clusters/{cid}/admins as the
// canary — it's gated by ActiveRoleClusterMiddlewareFromPath and the
// listClusterAdminsHandler returns a quick 200 with [] when no admins
// are registered, so a 200 here unambiguously means the middleware let
// the request through.
//
// Cross-cluster routes (crossG) use /admin/clusters (list) — gated by
// ActiveRoleUIAdminMiddleware. listClustersHandler returns 200 with the
// connection list, so 200 means the gate passed.
func TestActiveRoleGating_Regression(t *testing.T) {
	t.Parallel()

	scenarios := []activeRoleGatingScenario{
		{
			name:         "no_active_role_blocked_at_any_admin_admin_route",
			method:       http.MethodGet,
			path:         "/api/v1/admin/clusters/test-cluster-1/admins",
			activeRole:   nil,
			wantStatuses: []int{http.StatusUnauthorized},
		},
		{
			name:         "user_mode_blocked_on_per_cluster",
			method:       http.MethodGet,
			path:         "/api/v1/admin/clusters/test-cluster-1/admins",
			activeRole:   &auth.ActiveRole{Kind: "user"},
			wantStatuses: []int{http.StatusForbidden},
		},
		{
			name:         "user_mode_blocked_on_cross_cluster_buckets",
			method:       http.MethodGet,
			path:         "/api/v1/admin/buckets",
			activeRole:   &auth.ActiveRole{Kind: "user"},
			wantStatuses: []int{http.StatusForbidden},
		},
		{
			name:         "user_mode_blocked_on_cross_cluster_clusters_list",
			method:       http.MethodGet,
			path:         "/api/v1/admin/clusters",
			activeRole:   &auth.ActiveRole{Kind: "user"},
			wantStatuses: []int{http.StatusForbidden},
		},
		{
			name:         "cluster_admin_scope_mismatch_blocked",
			method:       http.MethodGet,
			path:         "/api/v1/admin/clusters/test-cluster-1/admins",
			activeRole:   &auth.ActiveRole{Kind: "cluster-admin", Cluster: "test-cluster-2"},
			wantStatuses: []int{http.StatusForbidden},
		},
		{
			name:         "cluster_admin_matching_scope_allowed",
			method:       http.MethodGet,
			path:         "/api/v1/admin/clusters/test-cluster-3/admins",
			activeRole:   &auth.ActiveRole{Kind: "cluster-admin", Cluster: "test-cluster-3"},
			wantStatuses: []int{http.StatusOK},
		},
		{
			name:         "ui_admin_super_admin_passes_per_cluster",
			method:       http.MethodGet,
			path:         "/api/v1/admin/clusters/test-cluster-4/admins",
			activeRole:   &auth.ActiveRole{Kind: "ui-admin"},
			wantStatuses: []int{http.StatusOK},
		},
		{
			name:         "cluster_admin_blocked_on_cross_cluster_clusters_list",
			method:       http.MethodGet,
			path:         "/api/v1/admin/clusters",
			activeRole:   &auth.ActiveRole{Kind: "cluster-admin", Cluster: "test-cluster-5"},
			wantStatuses: []int{http.StatusForbidden},
		},
		{
			name:         "ui_admin_allows_cross_cluster_clusters_list",
			method:       http.MethodGet,
			path:         "/api/v1/admin/clusters",
			activeRole:   &auth.ActiveRole{Kind: "ui-admin"},
			wantStatuses: []int{http.StatusOK},
		},
	}

	srv := newActiveRoleGatingTestServer(t)

	for _, sc := range scenarios {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(sc.method, sc.path, nil)
			req.AddCookie(&http.Cookie{
				Name:     "__Host-basement_session",
				Value:    mintActiveRoleTestToken(t, sc.activeRole),
				Path:     "/",
				Secure:   true,
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})

			rr := httptest.NewRecorder()
			srv.router.ServeHTTP(rr, req)

			if !statusIn(rr.Code, sc.wantStatuses) {
				t.Fatalf("%s: status=%d (want one of %v); body=%s",
					sc.name, rr.Code, sc.wantStatuses, rr.Body.String())
			}
		})
	}
}

// mintActiveRoleTestToken builds a JWT for the admin user with an
// explicit activeRole (or nil to test the no-activeRole 401 case).
//
// Always sets role="admin" + uiAdmin=true + mode="admin" because the
// middleware chain before ActiveRoleAnyAdminMiddleware (s.authMiddleware,
// RequireRole("admin")) needs those — the scenarios under test are
// specifically about what the active-role gates do after those pass.
func mintActiveRoleTestToken(t *testing.T, ar *auth.ActiveRole) string {
	t.Helper()
	if ar == nil {
		// IssueTokenWithActiveRole defaults nil to {kind:"user"} — for the
		// pre-v1.13.18-legacy-token scenario we mint the JWT directly so
		// the activeRole field is genuinely absent from claims.
		return mintPreV1318Token(t)
	}
	tok, err := auth.IssueTokenWithActiveRole(
		testSecret, "admin", "admin", true, "admin",
		time.Now().Add(1*time.Hour).Unix(),
		24*time.Hour,
		ar,
	)
	if err != nil {
		t.Fatalf("IssueTokenWithActiveRole: %v", err)
	}
	return tok
}

// mintPreV1318Token forges a JWT with no ActiveRole field — what a
// pre-v1.13.18 session cookie would carry. Used to verify the
// middleware's 401 branch for legacy tokens.
func mintPreV1318Token(t *testing.T) string {
	t.Helper()
	claims := &auth.Claims{
		UserID:  "admin",
		Role:    "admin",
		UIAdmin: true,
		Mode:    "admin",
		// ActiveRole intentionally nil
		RegisteredClaims: &jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Subject:   "admin",
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(testSecret)
	if err != nil {
		t.Fatalf("sign legacy token: %v", err)
	}
	return signed
}

func statusIn(got int, want []int) bool {
	for _, w := range want {
		if got == w {
			return true
		}
	}
	return false
}

// newActiveRoleGatingTestServer spins up a Server with the 5 connection
// records the table-driven scenarios reference. The server runs with no
// driver wired so calls that would otherwise dispatch to a backend just
// return their handler-level default (200 + [] for listClusterAdmins, etc.) —
// that's enough to differentiate "gate let it through" (200) from
// "gate blocked it" (4xx).
func newActiveRoleGatingTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := newTestConfig()
	cfg.DataDir = t.TempDir()

	connsStore := &testMockConnectionStore{conns: connRecsForGatingTest()}
	srv := New(cfg, nil, connsStore, nil, nil)
	return srv
}

func connRecsForGatingTest() []store.Connection {
	return []store.Connection{
		{ID: "test-cluster-1"},
		{ID: "test-cluster-2"},
		{ID: "test-cluster-3"},
		{ID: "test-cluster-4"},
		{ID: "test-cluster-5"},
	}
}
