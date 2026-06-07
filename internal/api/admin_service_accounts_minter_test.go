// Package api: tests for the service-account privilege-escalation
// bound (audit r10). An SA may only carry capabilities its MINTER
// currently holds at the requested scope — otherwise any holder of the
// host:manage_users mint gate could stamp a durable token with
// capabilities they don't possess (and that outlives any later
// downgrade of their own role).
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mattjackson/basement/internal/auth/policy"
)

// putSAUpdate issues a PUT /admin/service-accounts/{id} as userID and
// returns the recorder.
func putSAUpdate(t *testing.T, srv *Server, userID, id string, body map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/service-accounts/"+id, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, userID))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	return rr
}

// grantLimitedMinter assigns userID a custom role that grants ONLY
// host:manage_users @ host:* (enough to reach the SA mint handler) and
// nothing else. The caller can then assert which grants the minter can
// and cannot stamp onto an SA.
func grantLimitedMinter(t *testing.T, srv *Server, userID string) {
	t.Helper()
	enf := srv.policy
	if err := enf.UpsertRole(policy.Role{
		ID:           "users_only",
		Label:        "Users Only",
		Capabilities: []string{"host:manage_users"},
	}); err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: userID, RoleID: "users_only", Scope: "host:*",
	}); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
}

// TestSA_Create_RejectsCapMinterLacks: a minter holding only
// host:manage_users cannot mint an SA carrying a capability (here
// policy:edit_matrix) they do not themselves hold — 403
// INSUFFICIENT_PRIVILEGE.
func TestSA_Create_RejectsCapMinterLacks(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, false)
	grantLimitedMinter(t, srv, "limited")

	rr, _ := postSACreate(t, srv, "limited", map[string]interface{}{
		"name": "escalate",
		"capabilities": []map[string]string{
			{"id": "policy:edit_matrix", "scope": "host:*"},
		},
		"scopes": []string{"host:*"},
	})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s; want 403", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); !strings.Contains(got, "INSUFFICIENT_PRIVILEGE") {
		t.Errorf("body=%s; want INSUFFICIENT_PRIVILEGE", got)
	}
}

// TestSA_Create_RejectsCapAtScopeMinterLacks: the minter holds the
// capability somewhere, but not at the requested scope. host:* does
// NOT cover a bucket scope, so granting bucket:view @ bucket:c1:b1 is
// refused even though the minter has host-wide manage_users.
func TestSA_Create_RejectsCapAtScopeMinterLacks(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, false)
	grantLimitedMinter(t, srv, "limited")

	rr, _ := postSACreate(t, srv, "limited", map[string]interface{}{
		"name": "scope-escalate",
		"capabilities": []map[string]string{
			{"id": "bucket:view", "scope": "bucket:c1:b1"},
		},
		"scopes": []string{"bucket:c1:b1"},
	})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s; want 403", rr.Code, rr.Body.String())
	}
}

// TestSA_Create_RejectsWildcardCapID: a wildcard capability expression
// (host:*, *:*) is not a valid SA capability ID — SA caps must be
// registered leaves (policy.Validate rejects wildcards). This closes
// the wildcard-grant escalation vector at the validation layer before
// the minter-authority bound even runs, so the bound's Expand handling
// is purely defense-in-depth. Asserted here so a future loosening of
// validation can't silently reopen it.
func TestSA_Create_RejectsWildcardCapID(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, false)
	grantLimitedMinter(t, srv, "limited")

	rr, _ := postSACreate(t, srv, "limited", map[string]interface{}{
		"name": "wild",
		"capabilities": []map[string]string{
			{"id": "host:*", "scope": "host:*"},
		},
		"scopes": []string{"host:*"},
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s; want 400 (wildcard cap id invalid)", rr.Code, rr.Body.String())
	}
}

// TestSA_Create_AllowsCapMinterHolds: the SAME limited minter CAN mint
// an SA carrying a capability it does hold (host:manage_users @ host:*).
func TestSA_Create_AllowsCapMinterHolds(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, false)
	grantLimitedMinter(t, srv, "limited")

	rr, resp := postSACreate(t, srv, "limited", map[string]interface{}{
		"name": "within-bounds",
		"capabilities": []map[string]string{
			{"id": "host:manage_users", "scope": "host:*"},
		},
		"scopes": []string{"host:*"},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s; want 201", rr.Code, rr.Body.String())
	}
	if resp.Secret == "" {
		t.Error("expected plaintext secret on successful mint")
	}
}

// TestSA_Create_SuperuserMinterCanGrantAnything: a full host_admin
// (granted at host:* AND the "*" superuser scope, as SeedEnvAdmin does)
// can mint an SA carrying any registered capability at any scope — the
// bound only clamps minters who lack the grant.
func TestSA_Create_SuperuserMinterCanGrantAnything(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true) // grants admin host_admin@host:* and @*

	rr, resp := postSACreate(t, srv, "admin", map[string]interface{}{
		"name": "broad",
		"capabilities": []map[string]string{
			{"id": "policy:edit_matrix", "scope": "host:*"},
			{"id": "cluster:delete", "scope": "cluster:c1"},
		},
		"scopes": []string{"host:*", "cluster:c1"},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s; want 201", rr.Code, rr.Body.String())
	}
	if resp.Secret == "" {
		t.Error("expected plaintext secret on successful mint")
	}
}

// TestSA_Update_RejectsCapMinterLacks: the same bound applies on
// update. A limited minter creates a within-bounds SA, then tries to
// PUT a capability it doesn't hold — 403.
func TestSA_Update_RejectsCapMinterLacks(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, false)
	grantLimitedMinter(t, srv, "limited")

	_, created := postSACreate(t, srv, "limited", map[string]interface{}{
		"name": "upd",
		"capabilities": []map[string]string{
			{"id": "host:manage_users", "scope": "host:*"},
		},
		"scopes": []string{"host:*"},
	})
	if created.ServiceAccount.ID == "" {
		t.Fatalf("setup mint failed")
	}

	rr := putSAUpdate(t, srv, "limited", created.ServiceAccount.ID, map[string]interface{}{
		"capabilities": []map[string]string{
			{"id": "policy:edit_matrix", "scope": "host:*"},
		},
		"scopes": []string{"host:*"},
	})
	if rr.Code != http.StatusForbidden {
		t.Fatalf("update status=%d body=%s; want 403", rr.Code, rr.Body.String())
	}
}
