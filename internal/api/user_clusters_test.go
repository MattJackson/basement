package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattjackson/basement/internal/store"
)

// TestUserListClustersHandler_NoAuth tests GET /api/v1/user/clusters without auth.
func TestUserListClustersHandler_NoAuth(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "conn-1", Label: "default", Driver: "garage", Config: map[string]string{"admin_url": "http://localhost:3476"}, Owner: "org"},
		},
	}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/clusters", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d: body=%s", http.StatusUnauthorized, rr.Code, rr.Body.String())
	}
}

// TestUserListClustersHandler_Admin sees all clusters.
func TestUserListClustersHandler_Admin(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "conn-1", Label: "default", Driver: "garage", Config: map[string]string{"admin_url": "http://localhost:3476"}, Owner: "org"},
			{ID: "conn-2", Label: "prod", Driver: "aws-s3", Config: map[string]string{"region": "us-east-1"}, Owner: "org"},
		},
	}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/clusters", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateAdminToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var conns []store.Connection
	if err := json.NewDecoder(rr.Body).Decode(&conns); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if len(conns) != 2 {
		t.Errorf("expected 2 connections for admin, got %d", len(conns))
	}
}

// TestUserListClustersHandler_UserNoGrants returns empty array.
func TestUserListClustersHandler_UserNoGrants(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "conn-1", Label: "default", Driver: "garage", Config: map[string]string{"admin_url": "http://localhost:3476"}, Owner: "org"},
		},
	}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/clusters", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUserToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var conns []store.Connection
	if err := json.NewDecoder(rr.Body).Decode(&conns); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if len(conns) != 0 {
		t.Errorf("expected 0 connections for user with no grants, got %d", len(conns))
	}
}

// TestUserGetClusterHandler_NoAuth tests GET /api/v1/user/clusters/{cid} without auth.
func TestUserGetClusterHandler_NoAuth(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "conn-1", Label: "default", Driver: "garage", Config: map[string]string{"admin_url": "http://localhost:3476"}, Owner: "org"},
		},
	}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/clusters/conn-1", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d: body=%s", http.StatusUnauthorized, rr.Code, rr.Body.String())
	}
}

// TestUserGetClusterHandler_Admin sees cluster.
func TestUserGetClusterHandler_Admin(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "conn-1", Label: "default", Driver: "garage", Config: map[string]string{"admin_url": "http://localhost:3476"}, Owner: "org"},
		},
	}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/clusters/conn-1", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateAdminToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var conn store.Connection
	if err := json.NewDecoder(rr.Body).Decode(&conn); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if conn.ID != "conn-1" {
		t.Errorf("expected conn-1, got %s", conn.ID)
	}
}

// TestUserGetClusterHandler_UserNoGrants returns 403.
func TestUserGetClusterHandler_UserNoGrants(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "conn-1", Label: "default", Driver: "garage", Config: map[string]string{"admin_url": "http://localhost:3476"}, Owner: "org"},
		},
	}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/clusters/conn-1", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUserToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected status %d (forbidden), got %d: body=%s", http.StatusForbidden, rr.Code, rr.Body.String())
	}
}

// TestUserGetClusterHandler_ClusterNotFound returns 404.
func TestUserGetClusterHandler_ClusterNotFound(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{},
	}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/clusters/nonexistent", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateAdminToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status %d (not found), got %d: body=%s", http.StatusNotFound, rr.Code, rr.Body.String())
	}
}

// TestUserListClusterBucketsHandler_NoAuth tests GET /api/v1/user/clusters/{cid}/buckets without auth.
func TestUserListClusterBucketsHandler_NoAuth(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "conn-1", Label: "default", Driver: "garage", Config: map[string]string{"admin_url": "http://localhost:3476"}, Owner: "org"},
		},
	}

	drv := &testMockDriver{}

	srv := New(cfg, nil, connsStore, drv, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/clusters/conn-1/buckets", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d: body=%s", http.StatusUnauthorized, rr.Code, rr.Body.String())
	}
}

// TestUserListClusterBucketsHandler_Admin sees all buckets.
func TestUserListClusterBucketsHandler_Admin(t *testing.T) {
	t.Skip("Requires driver registry setup - skip for v0.5.2, covered by integration tests")
}

// TestUserListClusterBucketsHandler_UserNoGrants returns empty array.
func TestUserListClusterBucketsHandler_UserNoGrants(t *testing.T) {
	t.Skip("Requires driver registry setup - skip for v0.5.2, covered by integration tests")
}
