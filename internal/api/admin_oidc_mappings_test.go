// Package api: tests for /admin/oidc-group-mappings GET + PUT (v1.3.0a).
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/store"
)

// newOIDCMappingTestEnv builds a Server with a real store + enforcer
// and (optionally) the host_admin assignment needed for the
// host:manage_policies gate to pass.
func newOIDCMappingTestEnv(t *testing.T, grant bool) (*Server, *store.Store, policy.Enforcer, func()) {
	t.Helper()

	tmp, err := os.MkdirTemp("", "v130a-oidc-map-")
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

	srv := New(cfg, st, &testMockConnectionStore{}, nil, nil)
	srv.SetPolicy(enf)

	if grant {
		if err := enf.AssignRole(policy.RoleAssignment{
			UserID: "admin", RoleID: "host_admin", Scope: "host:*",
		}); err != nil {
			cleanup()
			t.Fatalf("AssignRole: %v", err)
		}
	}

	return srv, st, enf, cleanup
}

func TestOIDCGroupMappings_GET_EmptyByDefault(t *testing.T) {
	srv, _, _, cleanup := newOIDCMappingTestEnv(t, true)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/oidc-group-mappings", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", rr.Code, rr.Body.String())
	}

	var resp oidcGroupMappingsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Mappings == nil {
		t.Error("Mappings is nil, want empty slice")
	}
	if len(resp.Mappings) != 0 {
		t.Errorf("Mappings=%v, want empty", resp.Mappings)
	}
}

func TestOIDCGroupMappings_PUT_RoundTrip(t *testing.T) {
	srv, st, _, cleanup := newOIDCMappingTestEnv(t, true)
	defer cleanup()

	body, _ := json.Marshal(updateOIDCGroupMappingsRequest{
		Mappings: []store.OIDCGroupMapping{
			{Claim: "groups", ClaimValue: "platform-admins", RoleID: "host_admin", Scope: "host:*"},
			{Claim: "groups", ClaimValue: "engineers", RoleID: "cluster_admin", Scope: "cluster:*"},
		},
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/oidc-group-mappings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", rr.Code, rr.Body.String())
	}

	// Round-trip GET sees both mappings.
	got := st.OIDCGroupMappings().Get()
	if len(got.Mappings) != 2 {
		t.Fatalf("Mappings len=%d, want 2", len(got.Mappings))
	}
	if got.UpdatedAt.IsZero() {
		t.Error("UpdatedAt zero, want set by PUT")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/admin/oidc-group-mappings", nil)
	req2.AddCookie(adminCookie())
	rr2 := httptest.NewRecorder()
	srv.router.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("GET status=%d", rr2.Code)
	}
	var resp oidcGroupMappingsResponse
	if err := json.NewDecoder(rr2.Body).Decode(&resp); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if len(resp.Mappings) != 2 {
		t.Errorf("GET Mappings len=%d, want 2", len(resp.Mappings))
	}
}

func TestOIDCGroupMappings_PUT_RejectsMissingFields(t *testing.T) {
	srv, _, _, cleanup := newOIDCMappingTestEnv(t, true)
	defer cleanup()

	body, _ := json.Marshal(updateOIDCGroupMappingsRequest{
		Mappings: []store.OIDCGroupMapping{
			{Claim: "groups", ClaimValue: "admins", RoleID: "", Scope: "host:*"},
		},
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/oidc-group-mappings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "INVALID_MAPPING") {
		t.Errorf("body missing INVALID_MAPPING: %s", rr.Body.String())
	}
}

func TestOIDCGroupMappings_GET_ForbiddenWithoutCapability(t *testing.T) {
	srv, _, _, cleanup := newOIDCMappingTestEnv(t, false)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/oidc-group-mappings", nil)
	req.AddCookie(adminCookie())
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403 body=%s", rr.Code, rr.Body.String())
	}
}

// TestOIDCCallback_AppliesGroupMappingsOnLogin is the end-to-end sync
// test the cycle spec calls out: a configured mapping plus a matching
// claim auto-creates the role assignment on OIDC login.
func TestOIDCCallback_AppliesGroupMappingsOnLogin(t *testing.T) {
	tmp := t.TempDir()
	cfg := newTestConfig()
	cfg.DataDir = tmp
	cfg.SessionTTL = 24 * time.Hour

	st, err := store.Open(tmp, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	enf, err := policy.Open(filepath.Join(tmp, "policy"))
	if err != nil {
		t.Fatalf("policy.Open: %v", err)
	}

	// Seed a mapping that grants host_admin to anyone whose `groups`
	// claim includes "platform-admins".
	if err := st.OIDCGroupMappings().Replace([]store.OIDCGroupMapping{
		{Claim: "groups", ClaimValue: "platform-admins", RoleID: "host_admin", Scope: "host:*"},
	}); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	fake := &fakeOIDC{
		issuer:       "https://idp.example.com",
		autoProvFlag: true,
		verifyAllClaimsFn: func(ctx context.Context, raw, expectedNonce string) (*auth.OIDCClaims, map[string]interface{}, error) {
			return &auth.OIDCClaims{
					Subject:  "subj-alice",
					Email:    "alice@example.com",
					Name:     "Alice",
					Provider: "https://idp.example.com",
				}, map[string]interface{}{
					"groups": []interface{}{"platform-admins", "engineers"},
				}, nil
		},
	}

	srv := New(cfg, st, &testMockConnectionStore{}, nil, nil)
	srv.SetPolicy(enf)
	srv.SetOIDC(fake)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?state=stateA&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: auth.OIDCStateCookieName, Value: "stateA.nonceB"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("callback status=%d body=%s", rr.Code, rr.Body.String())
	}

	// The user should now exist + have host_admin@host:* via OIDC.
	users := st.Users()
	if len(users) != 1 {
		t.Fatalf("users=%d, want 1", len(users))
	}
	got := enf.AssignmentsFor(users[0].ID)
	if len(got) != 1 {
		t.Fatalf("AssignmentsFor=%+v, want exactly host_admin@host:*", got)
	}
	if got[0].RoleID != "host_admin" || got[0].Source != "oidc" {
		t.Errorf("got=%+v, want host_admin source=oidc", got[0])
	}
}

func TestOIDCCallback_RevokesStaleAutoAssignmentsOnReLogin(t *testing.T) {
	tmp := t.TempDir()
	cfg := newTestConfig()
	cfg.DataDir = tmp
	cfg.SessionTTL = 24 * time.Hour

	st, err := store.Open(tmp, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	enf, err := policy.Open(filepath.Join(tmp, "policy"))
	if err != nil {
		t.Fatalf("policy.Open: %v", err)
	}

	// Mapping: groups=admins -> host_admin
	if err := st.OIDCGroupMappings().Replace([]store.OIDCGroupMapping{
		{Claim: "groups", ClaimValue: "admins", RoleID: "host_admin", Scope: "host:*"},
	}); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	// Pre-create the user + seed an OIDC assignment as if a previous
	// login had applied the mapping.
	existing := store.User{
		ID:       "alice-id",
		Username: "alice@example.com",
		Role:     "user",
		Provider: "https://idp.example.com",
		Subject:  "subj-alice",
	}
	if err := st.CreateUser(existing); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, _, err := enf.SyncOIDCAssignments(existing.ID, []policy.RoleAssignment{
		{RoleID: "host_admin", Scope: "host:*"},
	}); err != nil {
		t.Fatalf("seed sync: %v", err)
	}

	// New login — user no longer in "admins" group.
	fake := &fakeOIDC{
		issuer:       "https://idp.example.com",
		autoProvFlag: true,
		verifyAllClaimsFn: func(ctx context.Context, raw, expectedNonce string) (*auth.OIDCClaims, map[string]interface{}, error) {
			return &auth.OIDCClaims{
					Subject:  "subj-alice",
					Email:    "alice@example.com",
					Provider: "https://idp.example.com",
				}, map[string]interface{}{
					"groups": []interface{}{"engineers"},
				}, nil
		},
	}
	srv := New(cfg, st, &testMockConnectionStore{}, nil, nil)
	srv.SetPolicy(enf)
	srv.SetOIDC(fake)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?state=s&code=c", nil)
	req.AddCookie(&http.Cookie{Name: auth.OIDCStateCookieName, Value: "s.n"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	got := enf.AssignmentsFor(existing.ID)
	if len(got) != 0 {
		t.Errorf("AssignmentsFor=%+v, want empty (stale OIDC role revoked)", got)
	}
}

func TestOIDCCallback_ManualAssignmentSurvives(t *testing.T) {
	tmp := t.TempDir()
	cfg := newTestConfig()
	cfg.DataDir = tmp
	cfg.SessionTTL = 24 * time.Hour

	st, err := store.Open(tmp, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	enf, err := policy.Open(filepath.Join(tmp, "policy"))
	if err != nil {
		t.Fatalf("policy.Open: %v", err)
	}

	// No OIDC mappings configured.
	existing := store.User{
		ID: "bob-id", Username: "bob@example.com", Role: "user",
		Provider: "https://idp.example.com", Subject: "subj-bob",
	}
	if err := st.CreateUser(existing); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// Operator manually assigned cluster_admin.
	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: existing.ID, RoleID: "cluster_admin", Scope: "cluster:*",
	}); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}

	fake := &fakeOIDC{
		issuer:       "https://idp.example.com",
		autoProvFlag: true,
		verifyAllClaimsFn: func(ctx context.Context, raw, expectedNonce string) (*auth.OIDCClaims, map[string]interface{}, error) {
			return &auth.OIDCClaims{Subject: "subj-bob", Email: "bob@example.com", Provider: "https://idp.example.com"},
				map[string]interface{}{"groups": []interface{}{"admins"}}, nil
		},
	}
	srv := New(cfg, st, &testMockConnectionStore{}, nil, nil)
	srv.SetPolicy(enf)
	srv.SetOIDC(fake)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?state=s&code=c", nil)
	req.AddCookie(&http.Cookie{Name: auth.OIDCStateCookieName, Value: "s.n"})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	got := enf.AssignmentsFor(existing.ID)
	if len(got) != 1 || got[0].RoleID != "cluster_admin" || got[0].Source == "oidc" {
		t.Errorf("AssignmentsFor=%+v, want manual cluster_admin untouched", got)
	}
}

