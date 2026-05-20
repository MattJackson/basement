package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mattjackson/basement/internal/store"
)

// TestUserListKeysHandler_NoAuth tests GET /api/v1/user/keys without auth.
func TestUserListKeysHandler_NoAuth(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "conn-1", Label: "default", Driver: "garage", Config: map[string]string{"admin_url": "http://localhost:3476"}, Owner: "org"},
		},
	}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/user/keys", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d: body=%s", http.StatusUnauthorized, rr.Code, rr.Body.String())
	}
}

// TestUserListKeysHandler_Admin sees all keys.
func TestUserListKeysHandler_Admin(t *testing.T) {
	t.Skip("Requires driver registry setup - skip for v0.5.2, covered by integration tests")
}

// TestUserListKeysHandler_UserNoGrants returns empty array.
func TestUserListKeysHandler_UserNoGrants(t *testing.T) {
	t.Skip("Requires driver registry setup - skip for v0.5.2, covered by integration tests")
}
