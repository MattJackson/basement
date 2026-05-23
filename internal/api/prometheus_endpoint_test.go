package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/metrics"
	"github.com/mattjackson/basement/internal/store"
)

// TestMetricsEndpoint_WithoutCollector returns 503 so misconfig
// surfaces clearly.
func TestMetricsEndpoint_WithoutCollector(t *testing.T) {
	cfg := &config.Config{Listen: ":8080"}
	st, _ := store.Open(t.TempDir(), 90*24*time.Hour)
	srv := New(cfg, st, nil, &mockDriver{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 without collector, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "METRICS_NOT_WIRED") {
		t.Errorf("expected METRICS_NOT_WIRED in body, got %s", rr.Body.String())
	}
}

// TestMetricsEndpoint_WithCollector exposes the basement_build_info
// family and at least the build_info sample after one request through
// the middleware.
func TestMetricsEndpoint_WithCollector(t *testing.T) {
	cfg := &config.Config{Listen: ":8080"}
	st, _ := store.Open(t.TempDir(), 90*24*time.Hour)
	srv := New(cfg, st, nil, &mockDriver{}, nil)

	c := metrics.NewCollector()
	c.SetBuildInfo("v1.11.0f-test", "abc123")
	srv.SetPromCollector(c, "")

	// One health request so the HTTP middleware records a sample.
	srv.router.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("/metrics returned %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		`basement_build_info{commit="abc123",version="v1.11.0f-test"} 1`,
		`basement_http_requests_total`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in /metrics body, got:\n%s", want, body)
		}
	}
}

// TestMetricsEndpoint_TokenGated verifies the bearer-token gate works
// end-to-end via the API router.
func TestMetricsEndpoint_TokenGated(t *testing.T) {
	cfg := &config.Config{Listen: ":8080"}
	st, _ := store.Open(t.TempDir(), 90*24*time.Hour)
	srv := New(cfg, st, nil, &mockDriver{}, nil)
	srv.SetPromCollector(metrics.NewCollector(), "scrape-token")

	// No header -> 401.
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("no-token: status=%d, want 401", rr.Code)
	}

	// Correct header -> 200.
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer scrape-token")
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("right-token: status=%d, want 200", rr.Code)
	}
}

// TestAuditCollector_PluggedIntoServer_IncrementsCounters wires the
// audit collector into a Server and verifies that hitting the login
// endpoint with bad creds bumps auth_attempts_total{result="failure"}.
func TestAuditCollector_PluggedIntoServer_IncrementsCounters(t *testing.T) {
	cfg := &config.Config{
		Listen: ":8080",
		Admin:  config.AdminConfig{User: "admin", PasswordHash: "$2a$04$AbCdEfGhIjKlMnOpQrStUu"},
		JWT:    config.JWTConfig{Secret: []byte("01234567890123456789012345678901")},
	}
	st, _ := store.Open(t.TempDir(), 90*24*time.Hour)
	srv := New(cfg, st, nil, &mockDriver{}, nil)
	c := metrics.NewCollector()
	srv.SetPromCollector(c, "")
	srv.SetAuditLogger(metrics.NewAuditCollector(audit.NewNoop(), c))

	// Post wrong creds — expect 401.
	body := strings.NewReader(`{"username":"admin","password":"wrong"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("login: status=%d, want 401; body=%s", rr.Code, rr.Body.String())
	}

	// Now scrape /metrics.
	scrapeRR := httptest.NewRecorder()
	srv.router.ServeHTTP(scrapeRR,
		httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if scrapeRR.Code != http.StatusOK {
		t.Fatalf("/metrics: status=%d, body=%s", scrapeRR.Code, scrapeRR.Body.String())
	}
	scrapeBody := scrapeRR.Body.String()

	if !strings.Contains(scrapeBody, `basement_auth_attempts_total{result="failure"} 1`) {
		t.Errorf("expected auth_attempts_total failure=1 after bad login, got:\n%s", scrapeBody)
	}
}
