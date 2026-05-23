package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/store"
)

// TestOnboardingStateHandler_NeedsOnboardingOnFreshDeploy covers the
// v1.11.0a happy path: a freshly-opened store has no clusters and no
// users beyond the env-seeded admin, so needsOnboarding=true.
// completed=false because the in-memory default constructed by
// OpenOrgCapabilities (path doesn't exist on disk yet) keeps the
// onboarding flag at its zero value — only an explicit dismiss flips
// it.
func TestOnboardingStateHandler_NeedsOnboardingOnFreshDeploy(t *testing.T) {
	cfg := newTestConfig()
	st, err := store.Open(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	connsStore := &testMockConnectionStore{conns: []store.Connection{}}

	srv := New(cfg, st, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/onboarding/state", nil)
	req.AddCookie(&http.Cookie{
		Name: "__Host-basement_session", Value: generateAdminToken(),
		Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp OnboardingState
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.NeedsOnboarding {
		t.Errorf("NeedsOnboarding: want true on fresh deploy, got false")
	}
	if resp.Completed {
		t.Errorf("Completed: want false on fresh deploy, got true")
	}
}

// TestOnboardingStateHandler_NoNeedAfterClusterAdded covers the
// "operator added a cluster outside the wizard" path. Once a cluster
// exists, needsOnboarding flips to false even if the wizard was never
// formally dismissed — the FE redirect only fires when both 0 clusters
// AND 0 users hold, so this guards against bouncing an in-progress
// operator into a wizard they don't need.
func TestOnboardingStateHandler_NoNeedAfterClusterAdded(t *testing.T) {
	cfg := newTestConfig()
	st, err := store.Open(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	connsStore := &testMockConnectionStore{conns: []store.Connection{
		{ID: "conn-1", Label: "prod", Driver: "garage", Config: map[string]string{"admin_url": "http://x"}, Owner: "org"},
	}}

	srv := New(cfg, st, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/onboarding/state", nil)
	req.AddCookie(&http.Cookie{
		Name: "__Host-basement_session", Value: generateAdminToken(),
		Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp OnboardingState
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.NeedsOnboarding {
		t.Errorf("NeedsOnboarding: want false when a cluster exists, got true")
	}
}

// TestOnboardingDismissHandler_FlipsCompleted covers the dismiss
// latch: POST /admin/onboarding/dismiss promotes completed=true so a
// subsequent GET reports the new state. Idempotent — calling it twice
// stays at completed=true rather than erroring.
func TestOnboardingDismissHandler_FlipsCompleted(t *testing.T) {
	cfg := newTestConfig()
	st, err := store.Open(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	connsStore := &testMockConnectionStore{conns: []store.Connection{}}

	srv := New(cfg, st, connsStore, nil, nil)

	// First confirm fresh state shows completed=false.
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/onboarding/state", nil)
		req.AddCookie(&http.Cookie{
			Name: "__Host-basement_session", Value: generateAdminToken(),
			Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
		})
		rr := httptest.NewRecorder()
		srv.router.ServeHTTP(rr, req)
		var s OnboardingState
		_ = json.NewDecoder(rr.Body).Decode(&s)
		if s.Completed {
			t.Fatalf("precondition: completed should be false before dismiss")
		}
	}

	// Dismiss.
	{
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/onboarding/dismiss", nil)
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{
			Name: "__Host-basement_session", Value: generateAdminToken(),
			Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
		})
		rr := httptest.NewRecorder()
		srv.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("dismiss: want 200, got %d body=%s", rr.Code, rr.Body.String())
		}
	}

	// Re-read state; completed should now be true.
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/onboarding/state", nil)
		req.AddCookie(&http.Cookie{
			Name: "__Host-basement_session", Value: generateAdminToken(),
			Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
		})
		rr := httptest.NewRecorder()
		srv.router.ServeHTTP(rr, req)
		var s OnboardingState
		_ = json.NewDecoder(rr.Body).Decode(&s)
		if !s.Completed {
			t.Errorf("completed: want true after dismiss, got false")
		}
	}

	// Second dismiss is a no-op (idempotent latch).
	{
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/onboarding/dismiss", nil)
		req.AddCookie(&http.Cookie{
			Name: "__Host-basement_session", Value: generateAdminToken(),
			Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
		})
		rr := httptest.NewRecorder()
		srv.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("second dismiss: want 200, got %d", rr.Code)
		}
	}
}

// TestOnboardingStateHandler_NoAuth — no cookie → 401.
func TestOnboardingStateHandler_NoAuth(t *testing.T) {
	cfg := newTestConfig()
	st, err := store.Open(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	srv := New(cfg, st, &testMockConnectionStore{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/onboarding/state", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status: want 401, got %d", rr.Code)
	}
}

// TestOnboardingStateHandler_NonAdminForbidden — UIAdmin gate enforces
// 403 for a non-UI-admin user.
func TestOnboardingStateHandler_NonAdminForbidden(t *testing.T) {
	cfg := newTestConfig()
	st, err := store.Open(t.TempDir(), 90*24*time.Hour)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	srv := New(cfg, st, &testMockConnectionStore{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/onboarding/state", nil)
	req.AddCookie(&http.Cookie{
		Name: "__Host-basement_session", Value: generateUserToken(),
		Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("status: want 403, got %d body=%s", rr.Code, rr.Body.String())
	}
}
