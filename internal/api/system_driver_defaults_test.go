package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// TestDriverDefaultsHandler exercises GET /api/v1/system/driver-defaults
// end-to-end through the chi router. The endpoint is public — no auth
// cookie needed — so we just hit it and inspect the response shape.
func TestDriverDefaultsHandler(t *testing.T) {
	cfg := &config.Config{Listen: ":8080"}
	st, _ := store.Open(t.TempDir()+"/store", 90*24*time.Hour)
	drv := &mockDriver{}

	srv := New(cfg, st, nil, drv, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/driver-defaults", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type=%q, want application/json", ct)
	}

	var out []driver.EndpointDefaults
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	// One entry per registered driver — driver/registry init() calls
	// pull in garage-v1/garage/aws-s3/minio (whichever are imported by
	// the test binary). At minimum the response must be non-empty and
	// every entry must carry the required fields.
	if len(out) == 0 {
		t.Fatal("expected at least one driver default entry")
	}

	for _, d := range out {
		if d.Driver == "" {
			t.Error("entry has empty Driver")
		}
		if d.DisplayName == "" {
			t.Errorf("entry %q has empty DisplayName", d.Driver)
		}
	}
}

// TestDriverDefaultsHandlerNoAuth confirms the endpoint stays public —
// no JWT cookie, no auth middleware. Mirrors TestHealthHandler shape.
func TestDriverDefaultsHandlerNoAuth(t *testing.T) {
	cfg := &config.Config{Listen: ":8080"}
	st, _ := store.Open(t.TempDir()+"/store", 90*24*time.Hour)
	drv := &mockDriver{}

	srv := New(cfg, st, nil, drv, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/driver-defaults", nil)
	// Deliberately no Cookie / Authorization header.
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("public endpoint should return 200 without auth, got %d", rr.Code)
	}
}
