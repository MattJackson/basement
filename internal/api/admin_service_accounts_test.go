// Package api: admin service-account handler tests (v1.7.0a).
//
// Mirrors the cycle prompt's acceptance list:
//   - Mint 201 with plaintext secret in response body
//   - Subsequent GET never includes plaintext
//   - Rotate returns new secret
//   - Without host:manage_users → 403
//   - Cross-user GET → 404 (ownership)
//   - Duplicate name → 409
//   - Invalid scope → 400
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/store"
)

// newServiceAccountTestEnv builds a Server with a wired SA store, a
// real policy enforcer, and (optionally) the host_admin role assigned
// to userID="admin" so the host:manage_users gate passes.
func newServiceAccountTestEnv(t *testing.T, grant bool) (*Server, *store.Store) {
	t.Helper()
	tmp := t.TempDir()

	cfg := newTestConfig()
	cfg.DataDir = tmp

	st, err := store.Open(tmp, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := st.WireServiceAccounts(); err != nil {
		t.Fatalf("WireServiceAccounts: %v", err)
	}

	enf, err := policy.Open(filepath.Join(tmp, "policy"))
	if err != nil {
		t.Fatalf("policy.Open: %v", err)
	}

	srv := New(cfg, st, &testMockConnectionStore{}, nil, nil)
	srv.SetPolicy(enf)

	if grant {
		if err := enf.AssignRole(policy.RoleAssignment{
			UserID: "admin", RoleID: "host_admin", Scope: "host:*",
		}); err != nil {
			t.Fatalf("AssignRole: %v", err)
		}
	}

	return srv, st
}

// serviceAccountAdminCookie issues an ADMIN-mode JWT for userID
// (defaults to "admin" — the username generateAdminToken uses). The
// host:manage_users gate requires ADMIN mode per MinModeFor.
func serviceAccountAdminCookie(t *testing.T, userID string) *http.Cookie {
	t.Helper()
	modeExpiresAt := time.Now().Add(time.Hour).Unix()
	token, err := auth.IssueTokenWithMode(testSecret, userID, "admin", true,
		"admin", modeExpiresAt, 24*time.Hour)
	if err != nil {
		t.Fatalf("IssueTokenWithMode: %v", err)
	}
	return &http.Cookie{
		Name:     "__Host-basement_session",
		Value:    token,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
}

// postSACreate is a helper that posts a Create request as the supplied
// admin user and returns the parsed serviceAccountWithSecret response.
func postSACreate(t *testing.T, srv *Server, userID string, body map[string]interface{}) (*httptest.ResponseRecorder, serviceAccountWithSecret) {
	t.Helper()
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, userID))

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	var resp serviceAccountWithSecret
	if rr.Code == http.StatusCreated {
		_ = json.NewDecoder(rr.Body).Decode(&resp)
	}
	return rr, resp
}

func TestSA_Create_201_ReturnsPlaintextSecret(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)

	rr, resp := postSACreate(t, srv, "admin", map[string]interface{}{
		"name": "ci-prod",
		"capabilities": []map[string]string{
			{"id": "bucket:view", "scope": "bucket:c1:b1"},
		},
		"scopes": []string{"bucket:c1:b1"},
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if resp.Secret == "" {
		t.Error("expected plaintext secret in response")
	}
	if resp.ServiceAccount.AccessKeyID == "" {
		t.Error("expected AccessKeyID in response")
	}
	if !strings.HasPrefix(resp.ServiceAccount.AccessKeyID, "BMNT") {
		t.Errorf("AccessKeyID=%q, want BMNT prefix", resp.ServiceAccount.AccessKeyID)
	}
	if resp.ServiceAccount.OwnerUserID != "admin" {
		t.Errorf("OwnerUserID=%q, want 'admin'", resp.ServiceAccount.OwnerUserID)
	}
}

func TestSA_Get_NeverIncludesSecret(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)

	_, created := postSACreate(t, srv, "admin", map[string]interface{}{
		"name": "ci-prod",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, nil)
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	// The response body must NOT contain the secret plaintext returned
	// at create time. Also must not contain any banned field name.
	bodyStr := rr.Body.String()
	if strings.Contains(bodyStr, created.Secret) {
		t.Error("GET response unexpectedly contains plaintext secret")
	}
	for _, banned := range []string{`"secret"`, `"secretKey"`, `"plaintext"`, `"secretKeyHash"`} {
		if strings.Contains(bodyStr, banned) {
			t.Errorf("GET response unexpectedly contains banned field %q in body=%s", banned, bodyStr)
		}
	}
}

func TestSA_List_NeverIncludesSecret(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)

	_, created := postSACreate(t, srv, "admin", map[string]interface{}{
		"name": "ci-prod",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-accounts", nil)
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), created.Secret) {
		t.Error("LIST response unexpectedly contains plaintext secret")
	}
}

func TestSA_Rotate_ReturnsNewSecret_KeepsAccessKey(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)

	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID+"/rotate", nil)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("rotate status=%d body=%s", rr.Code, rr.Body.String())
	}
	var rotated serviceAccountWithSecret
	if err := json.NewDecoder(rr.Body).Decode(&rotated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rotated.Secret == "" {
		t.Fatal("expected new plaintext secret on rotate")
	}
	if rotated.Secret == created.Secret {
		t.Error("rotated secret unexpectedly matches original")
	}
	if rotated.ServiceAccount.AccessKeyID != created.ServiceAccount.AccessKeyID {
		t.Errorf("AccessKeyID changed on rotate: was %q, now %q",
			created.ServiceAccount.AccessKeyID, rotated.ServiceAccount.AccessKeyID)
	}
}

func TestSA_Without_HostManageUsers_403(t *testing.T) {
	// grant=false → admin token does NOT have host_admin assigned.
	srv, _ := newServiceAccountTestEnv(t, false)

	data, _ := json.Marshal(map[string]interface{}{"name": "ci-prod"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))

	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_CrossUser_GET_404(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	// Grant host_admin to a second user too so the gate passes for both.
	srv.policy.AssignRole(policy.RoleAssignment{
		UserID: "matthew", RoleID: "host_admin", Scope: "host:*",
	})

	// admin creates an SA.
	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})

	// matthew tries to GET admin's SA — should collapse to 404, not 403.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, nil)
	req.AddCookie(serviceAccountAdminCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 on cross-user GET, got %d body=%s", rr.Code, rr.Body.String())
	}

	// And matthew's list should not surface admin's SA.
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-accounts", nil)
	listReq.AddCookie(serviceAccountAdminCookie(t, "matthew"))
	listRR := httptest.NewRecorder()
	srv.router.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRR.Code, listRR.Body.String())
	}
	var got []serviceAccountPublic
	_ = json.NewDecoder(listRR.Body).Decode(&got)
	if len(got) != 0 {
		t.Errorf("expected empty list for matthew, got %d entries", len(got))
	}
}

func TestSA_DuplicateName_409(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)

	rr1, _ := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	if rr1.Code != http.StatusCreated {
		t.Fatalf("first create status=%d body=%s", rr1.Code, rr1.Body.String())
	}
	rr2, _ := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	if rr2.Code != http.StatusConflict {
		t.Errorf("expected 409 on duplicate name, got %d body=%s", rr2.Code, rr2.Body.String())
	}
}

func TestSA_InvalidScope_400(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)

	rr, _ := postSACreate(t, srv, "admin", map[string]interface{}{
		"name": "ci-prod",
		"capabilities": []map[string]string{
			{"id": "bucket:view", "scope": "not-a-valid-scope"},
		},
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on invalid scope, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_InvalidCapability_400(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)

	rr, _ := postSACreate(t, srv, "admin", map[string]interface{}{
		"name": "ci-prod",
		"capabilities": []map[string]string{
			{"id": "imaginary:cap", "scope": "host:*"},
		},
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on unknown capability, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_InvalidName_400(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)

	rr, _ := postSACreate(t, srv, "admin", map[string]interface{}{
		"name": "no", // too short
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on invalid name, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Delete_SoftDeletes_NextGetShowsRevokedAt(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)

	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})

	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, nil)
	delReq.AddCookie(serviceAccountAdminCookie(t, "admin"))
	delRR := httptest.NewRecorder()
	srv.router.ServeHTTP(delRR, delReq)
	if delRR.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", delRR.Code, delRR.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, nil)
	getReq.AddCookie(serviceAccountAdminCookie(t, "admin"))
	getRR := httptest.NewRecorder()
	srv.router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET after delete status=%d", getRR.Code)
	}
	var got serviceAccountPublic
	_ = json.NewDecoder(getRR.Body).Decode(&got)
	if got.RevokedAt == nil || got.RevokedAt.IsZero() {
		t.Error("expected RevokedAt to be populated after delete")
	}
}

func TestSA_Update_PreservesAccessKey_DoesNotTouchSecret(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)

	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})

	body, _ := json.Marshal(map[string]interface{}{
		"name": "ci-prod-renamed",
		"capabilities": []map[string]string{
			{"id": "objects:get", "scope": "bucket:c1:b1"},
		},
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got serviceAccountPublic
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Name != "ci-prod-renamed" {
		t.Errorf("Name=%q, want renamed", got.Name)
	}
	if got.AccessKeyID != created.ServiceAccount.AccessKeyID {
		t.Errorf("AccessKeyID changed: was %q now %q", created.ServiceAccount.AccessKeyID, got.AccessKeyID)
	}
}
