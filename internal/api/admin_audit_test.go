package api

import (
	"encoding/json"
	"fmt"
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
		// SeedEnvAdmin grants matthew the three assignments needed for
		// every admin gate: host_admin @ host:*, host_admin @ *,
		// cluster_admin @ cluster:*. The superuser "*" scope is what
		// the wiring end-to-end test needs so cluster:create @
		// cluster:* passes. ADR-0002 (v1.1.0f) dropped the legacy
		// bucket_user @ bucket:* seed; the superuser row still covers
		// any bucket-scoped check.
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

// TestAuditHandler_Pagination_v1_4_0a seeds 75 events and walks two
// pages of 50 each, asserting Total, Offset, Limit echo back and the
// second page picks up where the first left off.
func TestAuditHandler_Pagination_v1_4_0a(t *testing.T) {
	srv, logger, cleanup := newAuditTestEnv(t, true)
	defer cleanup()
	defer logger.Close()

	// Seed 75 events in order. Each carries a sequence number in
	// Detail so we can assert the second page's first row is the 26th
	// newest (i.e. seed #50, zero-indexed from the latest at #74).
	for i := 0; i < 75; i++ {
		logger.Log(audit.Event{
			Actor:    "matthew",
			Action:   "test:event",
			Resource: "r",
			Result:   audit.ResultSuccess,
			Detail:   fmt.Sprintf("seq=%03d", i),
		})
	}

	// Page 1: default limit 50, offset 0.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("page 1 status=%d body=%s", rr.Code, rr.Body.String())
	}
	var page1 auditResponse
	if err := json.NewDecoder(rr.Body).Decode(&page1); err != nil {
		t.Fatalf("decode page 1: %v", err)
	}
	if len(page1.Events) != 50 {
		t.Errorf("page 1 len(Events)=%d, want 50", len(page1.Events))
	}
	if page1.Total != 75 {
		t.Errorf("page 1 Total=%d, want 75", page1.Total)
	}
	if page1.Offset != 0 || page1.Limit != 50 {
		t.Errorf("page 1 offset/limit=%d/%d, want 0/50", page1.Offset, page1.Limit)
	}
	if !page1.Truncated {
		t.Errorf("page 1 Truncated=false, want true (75>50)")
	}

	// Page 2: offset=50.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/audit?offset=50", nil)
	req.AddCookie(adminCookie())
	rr = httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("page 2 status=%d body=%s", rr.Code, rr.Body.String())
	}
	var page2 auditResponse
	if err := json.NewDecoder(rr.Body).Decode(&page2); err != nil {
		t.Fatalf("decode page 2: %v", err)
	}
	// Remaining 25 rows.
	if len(page2.Events) != 25 {
		t.Errorf("page 2 len(Events)=%d, want 25", len(page2.Events))
	}
	if page2.Total != 75 {
		t.Errorf("page 2 Total=%d, want 75", page2.Total)
	}
	if page2.Offset != 50 {
		t.Errorf("page 2 Offset=%d, want 50", page2.Offset)
	}
	if page2.Truncated {
		t.Errorf("page 2 Truncated=true, want false (offset+len >= total)")
	}

	// Sanity: page 1's last event differs from page 2's first.
	last1 := page1.Events[len(page1.Events)-1].Detail
	first2 := page2.Events[0].Detail
	if last1 == first2 {
		t.Errorf("page 1 last (%q) == page 2 first (%q); pagination is dropping or duplicating rows",
			last1, first2)
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

