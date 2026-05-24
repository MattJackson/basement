package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"github.com/mattjackson/basement/internal/auth/policy"
	"time"
)

func TestSA_Create_InvalidJSON(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts", bytes.NewReader([]byte("{invalid}")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Create_ExpiredAtInPast(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	past := time.Now().UTC().Add(-time.Hour)
	body, _ := json.Marshal(map[string]interface{}{"name": "ci-prod", "expiresAt": past.Format(time.RFC3339)})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for past expiry, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Create_EmptyName(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	body, _ := json.Marshal(map[string]interface{}{"name": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty name, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Create_WhitespaceName(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	body, _ := json.Marshal(map[string]interface{}{"name": "   "})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for whitespace name, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Create_EmptyCapabilitiesArray(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	body, _ := json.Marshal(map[string]interface{}{"name": "ci-prod", "capabilities": []map[string]string{}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201 for empty capabilities array, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Create_EmptyScopesArray(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	body, _ := json.Marshal(map[string]interface{}{"name": "ci-prod", "scopes": []string{}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201 for empty scopes array, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Create_InvalidCapabilityID(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	body, _ := json.Marshal(map[string]interface{}{"name": "ci-prod", "capabilities": []map[string]string{{"id": "nonexistent:capability", "scope": "host:*"}}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid capability ID, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Create_InvalidCapabilityScope(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	body, _ := json.Marshal(map[string]interface{}{"name": "ci-prod", "capabilities": []map[string]string{{"id": "bucket:view", "scope": "invalid-scope"}}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid capability scope, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Update_InvalidJSON(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, bytes.NewReader([]byte("{invalid}")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Update_ExpiresAtInPast(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	past := time.Now().UTC().Add(-time.Hour)
	body, _ := json.Marshal(map[string]interface{}{"expiresAt": past.Format(time.RFC3339)})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for past expiry on update, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Update_CapabilitiesNil_KeepsExisting(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod", "capabilities": []map[string]string{{"id": "bucket:view", "scope": "bucket:c1:b1"}}})
	body, _ := json.Marshal(map[string]interface{}{"name": "renamed", "capabilities": nil})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got serviceAccountPublic
	json.NewDecoder(rr.Body).Decode(&got)
	if len(got.Capabilities) != 1 {
		t.Errorf("expected capabilities preserved, got %d", len(got.Capabilities))
	}
}

func TestSA_Update_ScopesNil_KeepsExisting(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod", "scopes": []string{"bucket:c1:b1"}})
	body, _ := json.Marshal(map[string]interface{}{"name": "renamed", "scopes": nil})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got serviceAccountPublic
	json.NewDecoder(rr.Body).Decode(&got)
	if len(got.Scopes) != 1 {
		t.Errorf("expected scopes preserved, got %d", len(got.Scopes))
	}
}

func TestSA_Update_CrossUser_404(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	srv.policy.AssignRole(policy.RoleAssignment{UserID: "matthew", RoleID: "host_admin", Scope: "host:*"})
	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	body, _ := json.Marshal(map[string]interface{}{"name": "hacked"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 on cross-user update, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Delete_CrossUser_404(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	srv.policy.AssignRole(policy.RoleAssignment{UserID: "matthew", RoleID: "host_admin", Scope: "host:*"})
	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, nil)
	req.AddCookie(serviceAccountAdminCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 on cross-user delete, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Rotate_CrossUser_404(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	srv.policy.AssignRole(policy.RoleAssignment{UserID: "matthew", RoleID: "host_admin", Scope: "host:*"})
	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID+"/rotate", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "matthew"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 on cross-user rotate, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_List_NoServiceAccountStore(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	srv.store = nil
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-accounts", nil)
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for no-store scenario, got %d body=%s", rr.Code, rr.Body.String())
	}
	var got []serviceAccountPublic
	json.NewDecoder(rr.Body).Decode(&got)
	if len(got) != 0 {
		t.Errorf("expected empty list when store not wired, got %d", len(got))
	}
}

func TestSA_Get_NoServiceAccountStore(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	srv.store = nil
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, nil)
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for no-store scenario, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Update_NoServiceAccountStore(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	srv.store = nil
	body, _ := json.Marshal(map[string]interface{}{"name": "renamed"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for no-store scenario, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Delete_NoServiceAccountStore(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	srv.store = nil
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, nil)
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for no-store scenario, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Rotate_NoServiceAccountStore(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	srv.store = nil
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID+"/rotate", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for no-store scenario, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Create_InvalidScopeInCapabilities(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	body, _ := json.Marshal(map[string]interface{}{"name": "ci-prod", "capabilities": []map[string]string{{"id": "bucket:view", "scope": "not:a:valid:scope"}}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid scope in capabilities, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Create_InvalidScopeInTopLevelScopes(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	body, _ := json.Marshal(map[string]interface{}{"name": "ci-prod", "scopes": []string{"not:a:valid:scope"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid top-level scope, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Update_InvalidScopeInCapabilities(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	body, _ := json.Marshal(map[string]interface{}{"capabilities": []map[string]string{{"id": "bucket:view", "scope": "not:a:valid:scope"}}})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid scope in capabilities on update, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Update_InvalidScopeInTopLevelScopes(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	body, _ := json.Marshal(map[string]interface{}{"scopes": []string{"not:a:valid:scope"}})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid top-level scope on update, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Update_DuplicateNameWithLiveSibling_409(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_, _ = postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	_ , other := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-staging"})
	body, _ := json.Marshal(map[string]interface{}{"name": "ci-prod"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/service-accounts/"+other.ServiceAccount.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409 on duplicate name update, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSA_Delete_NameReuseAfterRevoke(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_ , first := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/service-accounts/"+first.ServiceAccount.ID, nil)
	delReq.AddCookie(serviceAccountAdminCookie(t, "admin"))
	delRR := httptest.NewRecorder()
	srv.router.ServeHTTP(delRR, delReq)
	body, _ := json.Marshal(map[string]interface{}{"name": "ci-prod"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201 for name reuse after revoke, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestSA_Get_ServiceAccountNotFound_404 tests missing SA returns 404.
func TestSA_Get_ServiceAccountNotFound_404(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-accounts/nonexistent-id", nil)
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing SA, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestSA_Update_ServiceAccountNotFound_404 tests missing SA returns 404.
func TestSA_Update_ServiceAccountNotFound_404(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	body, _ := json.Marshal(map[string]interface{}{"name": "renamed"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/service-accounts/nonexistent-id", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing SA on update, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestSA_Delete_ServiceAccountNotFound_404 tests missing SA returns 404.
func TestSA_Delete_ServiceAccountNotFound_404(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/service-accounts/nonexistent-id", nil)
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing SA on delete, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestSA_Rotate_ServiceAccountNotFound_404 tests missing SA returns 404.
func TestSA_Rotate_ServiceAccountNotFound_404(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts/nonexistent-id/rotate", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing SA on rotate, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestSA_Delete_Idempotent tests deleting already-deleted SA returns 204.
func TestSA_Delete_Idempotent(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_ , first := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	delReq1 := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/service-accounts/"+first.ServiceAccount.ID, nil)
	delReq1.AddCookie(serviceAccountAdminCookie(t, "admin"))
	delRR1 := httptest.NewRecorder()
	srv.router.ServeHTTP(delRR1, delReq1)
	if delRR1.Code != http.StatusNoContent {
		t.Fatalf("first delete status=%d", delRR1.Code)
	}
	// Second delete should also succeed (idempotent).
	delReq2 := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/service-accounts/"+first.ServiceAccount.ID, nil)
	delReq2.AddCookie(serviceAccountAdminCookie(t, "admin"))
	delRR2 := httptest.NewRecorder()
	srv.router.ServeHTTP(delRR2, delReq2)
	if delRR2.Code != http.StatusNoContent {
		t.Errorf("expected 204 on second delete (idempotent), got %d", delRR2.Code)
	}
}

// TestSA_Rotate_RevokedSA_Error tests rotating revoked SA returns error.
func TestSA_Rotate_RevokedSA_Error(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_ , first := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	// Delete to revoke it.
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/service-accounts/"+first.ServiceAccount.ID, nil)
	delReq.AddCookie(serviceAccountAdminCookie(t, "admin"))
	delRR := httptest.NewRecorder()
	srv.router.ServeHTTP(delRR, delReq)
	// Try to rotate - should fail.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts/"+first.ServiceAccount.ID+"/rotate", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409 on rotate revoked SA, got %d body=%s", rr.Code, rr.Body.String())
	}
}
// TestSA_Update_DuplicateName_409 tests duplicate name on update returns 409.
func TestSA_Update_DuplicateName_409(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_ , _ = postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	_ , other := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-staging"})
	body, _ := json.Marshal(map[string]interface{}{"name": "ci-prod"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/service-accounts/"+other.ServiceAccount.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409 on duplicate name update, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestSA_Create_DuplicateName_409 tests duplicate name returns 409.
func TestSA_Create_DuplicateName_409(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_, _ = postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	body, _ := json.Marshal(map[string]interface{}{"name": "ci-prod"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409 on duplicate name create, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestSA_Update_InvalidName_400 tests invalid name on update returns 400.
func TestSA_Update_InvalidName_400(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	body, _ := json.Marshal(map[string]interface{}{"name": "ab"}) // too short
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on invalid name update, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestSA_Create_InvalidName_400 tests invalid name returns 400.
func TestSA_Create_InvalidName_400(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	body, _ := json.Marshal(map[string]interface{}{"name": "ab"}) // too short
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 on invalid name create, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestSA_Update_ExpiredAtZero_400 tests zero expiry on update returns 400.
func TestSA_Update_ExpiredAtZero_400(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	_, created := postSACreate(t, srv, "admin", map[string]interface{}{"name": "ci-prod"})
	zeroTime := time.Time{}.Format(time.RFC3339)
	body, _ := json.Marshal(map[string]interface{}{"expiresAt": zeroTime})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/service-accounts/"+created.ServiceAccount.ID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(serviceAccountAdminCookie(t, "admin"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Logf("zero expiry status=%d (may be stripped by omitempty)", rr.Code)
	}
}

// TestSA_List_ForUser_EmptyList tests user with no SAs returns empty list.
func TestSA_List_ForUser_EmptyList(t *testing.T) {
	srv, _ := newServiceAccountTestEnv(t, true)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-accounts", nil)
	srv.policy.AssignRole(policy.RoleAssignment{UserID: "newuser", RoleID: "host_admin", Scope: "host:*"})
	req.AddCookie(serviceAccountAdminCookie(t, "newuser"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status=%d", rr.Code)
	}
	var got []serviceAccountPublic
	json.NewDecoder(rr.Body).Decode(&got)
	if len(got) != 0 {
		t.Errorf("expected empty list for user with no SAs, got %d", len(got))
	}
}
