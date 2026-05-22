package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/store"
)

// newAuditTestEnv builds a Server with a real audit logger attached,
// a real policy enforcer, and an env-admin assignment for the
// adminCookie() caller. Returns the server, the live logger (so the
// caller can pre-seed events), and a cleanup func.
func newAuditTestEnv(t *testing.T, grantHostAdmin bool) (*Server, *audit.FileLogger, func()) {
	t.Helper()

	tmp, err := os.MkdirTemp("", "v100c-audit-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }

	cfg := newTestConfig()
	cfg.DataDir = tmp

	st, err := store.Open(tmp, 90*24*time.Hour)
	if err != nil {
		cleanup()
		t.Fatalf("store.Open: %v", err)
	}

	enf, err := policy.Open(filepath.Join(tmp, "policy"))
	if err != nil {
		cleanup()
		t.Fatalf("policy.Open: %v", err)
	}

	conns := &testMockConnectionStore{conns: nil}
	srv := New(cfg, st, conns, nil, nil)
	srv.SetPolicy(enf)

	auditLogger := audit.NewFileLogger(tmp)
	srv.SetAuditLogger(auditLogger)

	if grantHostAdmin {
		// SeedEnvAdmin grants the four assignments matthew has in
		// production: host_admin @ host:*, host_admin @ *,
		// cluster_admin @ cluster:*, bucket_user @ bucket:*. The
		// superuser "*" scope is what the wiring end-to-end test
		// needs so cluster:create @ cluster:* passes.
		if err := enf.SeedEnvAdmin("admin"); err != nil {
			cleanup()
			t.Fatalf("SeedEnvAdmin: %v", err)
		}
	}

	return srv, auditLogger, cleanup
}

// TestAuditHandler_HappyPath seeds three events and asserts they
// come back through the HTTP endpoint, newest-first.
func TestAuditHandler_HappyPath(t *testing.T) {
	srv, logger, cleanup := newAuditTestEnv(t, true)
	defer cleanup()
	defer logger.Close()

	// Seed three events.
	logger.Log(audit.Event{Actor: "matthew", Action: "cluster:create", Resource: "cluster:abc", Result: audit.ResultSuccess})
	logger.Log(audit.Event{Actor: "matthew", Action: "bucket:create", Resource: "bucket:abc:foo", Result: audit.ResultSuccess})
	logger.Log(audit.Event{Actor: "matthew", Action: "bucket:delete", Resource: "bucket:abc:foo", Result: audit.ResultFailure, Detail: "still has objects"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit?limit=10", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var resp auditResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Events) != 3 {
		t.Fatalf("len(Events)=%d, want 3", len(resp.Events))
	}
	if resp.Events[0].Action != "bucket:delete" {
		t.Errorf("newest event action=%q, want bucket:delete", resp.Events[0].Action)
	}
	if resp.Total != 3 {
		t.Errorf("Total=%d, want 3", resp.Total)
	}
}

// TestAuditHandler_FilterByActor confirms the actor query param
// reaches the audit logger.
func TestAuditHandler_FilterByActor(t *testing.T) {
	srv, logger, cleanup := newAuditTestEnv(t, true)
	defer cleanup()
	defer logger.Close()

	logger.Log(audit.Event{Actor: "matthew", Action: "test", Resource: "r", Result: audit.ResultSuccess})
	logger.Log(audit.Event{Actor: "alice", Action: "test", Resource: "r", Result: audit.ResultSuccess})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit?actor=alice", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp auditResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Events) != 1 {
		t.Fatalf("len(Events)=%d, want 1", len(resp.Events))
	}
	if resp.Events[0].Actor != "alice" {
		t.Errorf("Actor=%q, want alice", resp.Events[0].Actor)
	}
}

// TestAuditHandler_CapabilityGate ensures a caller without
// host:manage_policies is rejected.
func TestAuditHandler_CapabilityGate(t *testing.T) {
	srv, logger, cleanup := newAuditTestEnv(t, false /* no host_admin */)
	defer cleanup()
	defer logger.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status=%d, want 403; body=%s", rr.Code, rr.Body.String())
	}
}

// TestAuditHandler_InvalidTime asserts the from/to parser surfaces
// a clear error code.
func TestAuditHandler_InvalidTime(t *testing.T) {
	srv, logger, cleanup := newAuditTestEnv(t, true)
	defer cleanup()
	defer logger.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit?from=not-a-date", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", rr.Code)
	}
}

// TestAuditHandler_WiringEndToEnd creates an actual cluster via the
// API handler and confirms the event surfaces on /admin/audit.
// This is the end-to-end glue test that catches a missing
// auditSuccess() call in any of the mutating handlers.
func TestAuditHandler_WiringEndToEnd(t *testing.T) {
	srv, logger, cleanup := newAuditTestEnv(t, true)
	defer cleanup()
	defer logger.Close()

	// POST /admin/clusters to create a stub-driver cluster. The
	// connection store is the in-memory testMockConnectionStore so
	// Create succeeds without touching a real backend.
	body := `{"label":"test-cluster","driver":"garage","config":{"admin_url":"http://x","admin_token":"y"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create cluster: status=%d body=%s", rr.Code, rr.Body.String())
	}

	// Read it back via /admin/audit.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit?action=cluster", nil)
	req.AddCookie(adminCookie())
	rr = httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("query audit: status=%d body=%s", rr.Code, rr.Body.String())
	}

	var resp auditResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, e := range resp.Events {
		if e.Action == "cluster:create" && e.Result == audit.ResultSuccess {
			found = true
			if e.Actor != "admin" {
				t.Errorf("event Actor=%q, want admin", e.Actor)
			}
			break
		}
	}
	if !found {
		t.Errorf("no cluster:create success event found; events: %+v", resp.Events)
	}
}

