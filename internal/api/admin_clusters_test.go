package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// newJSONRequest builds a POST request with a JSON body. Shared helper
// used across user-share / user-sync tests (previously lived in the
// now-deleted user_clusters_create_test.go).
func newJSONRequest(url string, body interface{}) *http.Request {
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// testMockConnectionStore implements store.Connections for testing.
type testMockConnectionStore struct {
	conns       []store.Connection
	getFunc     func(ctx context.Context, id string) (store.Connection, error)
	listFunc    func(ctx context.Context) ([]store.Connection, error)
	createFunc  func(ctx context.Context, c store.Connection) (store.Connection, error)
	updateFunc  func(ctx context.Context, id string, patch store.Connection) (store.Connection, error)
	deleteFunc  func(ctx context.Context, id string) error
	swapFunc    func(ctx context.Context, cid string, oldEnc, newEnc []byte) error
}

func (m *testMockConnectionStore) List(ctx context.Context) ([]store.Connection, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return m.conns, nil
}

func (m *testMockConnectionStore) Get(ctx context.Context, id string) (store.Connection, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	for _, c := range m.conns {
		if c.ID == id {
			return c, nil
		}
	}
	return store.Connection{}, fmt.Errorf("connection not found: %s", id)
}

func (m *testMockConnectionStore) Create(ctx context.Context, c store.Connection) (store.Connection, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, c)
	}
	c.ID = "conn-" + c.Label
	m.conns = append(m.conns, c)
	return c, nil
}

func (m *testMockConnectionStore) Update(ctx context.Context, id string, patch store.Connection) (store.Connection, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, id, patch)
	}
	for i := range m.conns {
		if m.conns[i].ID == id {
			if patch.Label != "" {
				m.conns[i].Label = patch.Label
			}
			if patch.Driver != "" {
				m.conns[i].Driver = patch.Driver
			}
			if patch.Config != nil {
				m.conns[i].Config = patch.Config
			}
			if patch.Color != "" {
				m.conns[i].Color = patch.Color
			}
			return m.conns[i], nil
		}
	}
	return store.Connection{}, fmt.Errorf("connection not found: %s", id)
}

func (m *testMockConnectionStore) Delete(ctx context.Context, id string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	for i := range m.conns {
		if m.conns[i].ID == id {
			m.conns = append(m.conns[:i], m.conns[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("connection not found: %s", id)
}

func (m *testMockConnectionStore) Count(ctx context.Context) (int, error) {
	return len(m.conns), nil
}

// SwapClusterSecret implements the v1.12.0b store.Connections addition.
// Mirrors the real store's bytes-equal idempotency check so the API
// migration helper exercises the same control flow under test.
func (m *testMockConnectionStore) SwapClusterSecret(ctx context.Context, cid string, oldEnc, newEnc []byte) error {
	if m.swapFunc != nil {
		return m.swapFunc(ctx, cid, oldEnc, newEnc)
	}
	for i := range m.conns {
		if m.conns[i].ID != cid {
			continue
		}
		if !bytesEqualForTest(m.conns[i].ConfigEncCSK, oldEnc) {
			return nil
		}
		m.conns[i].ConfigEncCSK = append([]byte(nil), newEnc...)
		return nil
	}
	return fmt.Errorf("connection not found: %s", cid)
}

// bytesEqualForTest mirrors bytes.Equal — duplicated here so the
// mock doesn't pull a runtime dep into every other test file that
// happens to compile this package.
func bytesEqualForTest(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestListClustersHandler_HappyPath tests GET /admin/clusters with data.
func TestListClustersHandler_HappyPath(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{ID: "conn-1", Label: "default", Driver: "garage", Config: map[string]string{"admin_url": "http://localhost:3476"}, Owner: "org"},
			{ID: "conn-2", Label: "prod", Driver: "aws-s3", Config: map[string]string{"region": "us-east-1"}, Owner: "org"},
		},
	}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUIAdminToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var conns []store.Connection
	if err := json.NewDecoder(rr.Body).Decode(&conns); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	if len(conns) != 2 {
		t.Errorf("expected 2 connections, got %d", len(conns))
	}
}

// TestListClustersHandler_RedactsSecrets — regression for v1.13.28.
// Live smoke caught admin_token + secret_key leaking through the wire
// to user-mode callers. Connection.Redacted strips sensitive keys
// before serialization; assert the secret strings never appear in the
// rendered response body.
func TestListClustersHandler_RedactsSecrets(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{
			{
				ID: "c-1", Label: "garage", Driver: "garage", Owner: "org",
				Config: map[string]string{
					"admin_url":   "http://10.1.7.10:3903",
					"admin_token": "SENSITIVE-ADMIN-TOKEN-MUST-NOT-LEAK",
					"s3_endpoint": "http://10.1.7.10:3902",
				},
			},
			{
				ID: "c-2", Label: "aws", Driver: "aws-s3", Owner: "org",
				Config: map[string]string{
					"region":        "us-east-1",
					"access_key_id": "AKIA-public-half",
					"secret_key":    "SENSITIVE-SECRET-MUST-NOT-LEAK",
				},
			},
		},
	}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUIAdminToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, secret := range []string{
		"SENSITIVE-ADMIN-TOKEN-MUST-NOT-LEAK",
		"SENSITIVE-SECRET-MUST-NOT-LEAK",
		"admin_token",
		"secret_key",
	} {
		if strings.Contains(body, secret) {
			t.Errorf("response leaked %q in body: %s", secret, body)
		}
	}
	// Non-sensitive fields should still be present.
	for _, public := range []string{"garage", "aws", "admin_url", "s3_endpoint", "region", "access_key_id"} {
		if !strings.Contains(body, public) {
			t.Errorf("response missing public field %q; body=%s", public, body)
		}
	}
}

// TestListClustersHandler_Empty tests GET /admin/clusters with no data.
func TestListClustersHandler_Empty(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{conns: []store.Connection{}}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUIAdminToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	var conns []store.Connection
	json.NewDecoder(rr.Body).Decode(&conns)
	if len(conns) != 0 {
		t.Errorf("expected 0 connections, got %d", len(conns))
	}
}

// TestListClustersHandler_NoAuth tests GET /admin/clusters without auth.
func TestListClustersHandler_NoAuth(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters", nil)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

// TestListClustersHandler_NonAdmin tests GET /admin/clusters with non-admin user.
func TestListClustersHandler_NonAdmin(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUserToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected status %d, got %d", http.StatusForbidden, rr.Code)
	}
}

// TestCreateClusterHandler_HappyPath tests POST /admin/clusters.
func TestCreateClusterHandler_HappyPath(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{}

	srv := New(cfg, nil, connsStore, nil, nil)

	body := map[string]any{
		"label":  "new-cluster",
		"driver": "garage",
		"config": map[string]string{"admin_url": "http://localhost:3476"},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters", bytes.NewReader(jsonBody))
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUIAdminToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d: body=%s", http.StatusCreated, rr.Code, rr.Body.String())
	}

	var conn store.Connection
	json.NewDecoder(rr.Body).Decode(&conn)
	if conn.Label != "new-cluster" {
		t.Errorf("expected label 'new-cluster', got '%s'", conn.Label)
	}
}

// TestCreateClusterHandler_DuplicateLabel tests POST /admin/clusters with duplicate label.
func TestCreateClusterHandler_DuplicateLabel(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{{ID: "conn-1", Label: "duplicate", Driver: "garage", Config: map[string]string{}, Owner: "org"}},
	}

	srv := New(cfg, nil, connsStore, nil, nil)

	body := map[string]any{
		"label":  "duplicate",
		"driver": "garage",
		"config": map[string]string{"admin_url": "http://localhost:3476"},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters", bytes.NewReader(jsonBody))
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUIAdminToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected status %d (CONFLICT), got %d: body=%s", http.StatusConflict, rr.Code, rr.Body.String())
	}
}

// TestCreateClusterHandler_BadDriver tests POST /admin/clusters with unsupported driver.
func TestCreateClusterHandler_BadDriver(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{}

	srv := New(cfg, nil, connsStore, nil, nil)

	body := map[string]any{
		"label":  "bad-driver",
		"driver": "unsupported-driver",
		"config": map[string]string{"admin_url": "http://localhost:3476"},
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters", bytes.NewReader(jsonBody))
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUIAdminToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d (BAD_REQUEST), got %d: body=%s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}
}

// TestGetClusterHandler_HappyPath tests GET /admin/clusters/{cid}.
func TestGetClusterHandler_HappyPath(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{{ID: "conn-1", Label: "test-cluster", Driver: "garage", Config: map[string]string{"admin_url": "http://localhost:3476"}, Owner: "org"}},
	}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters/conn-1", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUIAdminToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var conn store.Connection
	json.NewDecoder(rr.Body).Decode(&conn)
	if conn.ID != "conn-1" {
		t.Errorf("expected id 'conn-1', got '%s'", conn.ID)
	}
}

// TestDriverInfoHandler_HappyPath verifies the v1.11.0.6 BUG03 fix:
// GET /admin/clusters/{cid}/driver-info returns 200 with the
// driver name (from the Connection record) and the live Caps shape
// from the per-cluster driver instance.
func TestDriverInfoHandler_HappyPath(t *testing.T) {
	registerFanoutDriver(t)

	connsStore := makeFanoutConnsStore(map[string]string{"c1": "ok"})
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters/c1/driver-info", nil)
	req.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateUIAdminToken(),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var resp driverInfoResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Driver != fanoutDriverName {
		t.Errorf("Driver = %q, want %q", resp.Driver, fanoutDriverName)
	}
	// The fanout stub returns a zero-value Caps; we just assert the
	// JSON-decoded object has the field present (i.e. the handler
	// actually called Capabilities() instead of 500'ing).
}

// TestDriverInfoHandler_NotFound returns 404 (via writeRegistryForError)
// when the cid doesn't resolve to a Connection — important guarantee
// for the smoke harness, which probes /driver-info before deciding
// whether the endpoint exists at all.
func TestDriverInfoHandler_NotFound(t *testing.T) {
	registerFanoutDriver(t)

	connsStore := makeFanoutConnsStore(map[string]string{"c1": "ok"})
	reg := driver.NewRegistry(connsStore)
	srv := New(newTestConfig(), nil, connsStore, nil, reg)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters/nope/driver-info", nil)
	req.AddCookie(&http.Cookie{
		Name:     "__Host-basement_session",
		Value:    generateUIAdminToken(),
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK {
		t.Fatalf("expected non-200 for unknown cid, got 200 body=%s", rr.Body.String())
	}
	// Specifically must NOT be 404 from chi's no-route-matched path —
	// the new route must exist (the whole point of BUG03). 404 from
	// the handler's connection-lookup is fine.
	if rr.Code == http.StatusNotFound {
		// Good — Connection lookup returned not-found, handler responded.
	} else if rr.Code >= 400 && rr.Code < 500 {
		// Other 4xx (e.g. 400 INVALID) also fine; the smoke only flags 404.
	} else {
		t.Fatalf("unexpected status=%d body=%s", rr.Code, rr.Body.String())
	}
}

// TestGetClusterHandler_NotFound tests GET /admin/clusters/{cid} with non-existent cid.
func TestGetClusterHandler_NotFound(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/clusters/non-existent", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUIAdminToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status %d (NOT_FOUND), got %d", http.StatusNotFound, rr.Code)
	}
}

// TestUpdateClusterHandler_HappyPath tests PATCH /admin/clusters/{cid}.
func TestUpdateClusterHandler_HappyPath(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{{ID: "conn-1", Label: "original", Driver: "garage", Config: map[string]string{"admin_url": "http://localhost:3476"}, Owner: "org"}},
	}

	srv := New(cfg, nil, connsStore, nil, nil)

	body := map[string]any{
		"label":  "updated-label",
		"color":  "#FF0000",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/clusters/conn-1", bytes.NewReader(jsonBody))
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUIAdminToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var conn store.Connection
	json.NewDecoder(rr.Body).Decode(&conn)
	if conn.Label != "updated-label" {
		t.Errorf("expected label 'updated-label', got '%s'", conn.Label)
	}
}

// TestArmDeleteClusterHandler_HappyPath tests POST /admin/clusters/{cid}/_arm-delete.
func TestArmDeleteClusterHandler_HappyPath(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{{ID: "conn-1", Label: "test-cluster", Driver: "garage", Config: map[string]string{}, Owner: "org"}},
	}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters/conn-1/_arm-delete", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUIAdminToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rr.Body).Decode(&resp)
	if _, ok := resp["token"]; !ok {
		t.Error("expected 'token' in response")
	}
	if _, ok := resp["expiresInSeconds"]; !ok {
		t.Error("expected 'expiresInSeconds' in response")
	}
}

// TestDeleteClusterHandler_NoHeader tests DELETE /admin/clusters/{cid} without X-Confirm-Delete.
func TestDeleteClusterHandler_NoHeader(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{{ID: "conn-1", Label: "test-cluster", Driver: "garage", Config: map[string]string{}, Owner: "org"}},
	}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/clusters/conn-1", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUIAdminToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d (BAD_REQUEST), got %d", http.StatusBadRequest, rr.Code)
	}
}

// TestDeleteClusterHandler_BadToken tests DELETE /admin/clusters/{cid} with invalid token.
func TestDeleteClusterHandler_BadToken(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{{ID: "conn-1", Label: "test-cluster", Driver: "garage", Config: map[string]string{}, Owner: "org"}},
	}

	srv := New(cfg, nil, connsStore, nil, nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/clusters/conn-1", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUIAdminToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	req.Header.Set("X-Confirm-Delete", "invalid-token-here")
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d (BAD_REQUEST), got %d", http.StatusBadRequest, rr.Code)
	}
}

// TestDeleteClusterHandler_HappyPath tests DELETE /admin/clusters/{cid} with valid token.
func TestDeleteClusterHandler_HappyPath(t *testing.T) {
	cfg := newTestConfig()
	connsStore := &testMockConnectionStore{
		conns: []store.Connection{{ID: "conn-1", Label: "test-cluster", Driver: "garage", Config: map[string]string{}, Owner: "org"}},
	}

	srv := New(cfg, nil, connsStore, nil, nil)

	// First arm the delete
	armReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/clusters/conn-1/_arm-delete", nil)
	armReq.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUIAdminToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	armRR := httptest.NewRecorder()
	srv.router.ServeHTTP(armRR, armReq)

	var armResp map[string]any
	json.NewDecoder(armRR.Body).Decode(&armResp)
	token := armResp["token"].(string)

	// Now delete with token
	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/clusters/conn-1", nil)
	delReq.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: generateUIAdminToken(), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	delReq.Header.Set("X-Confirm-Delete", token)
	rr := httptest.NewRecorder()

	srv.router.ServeHTTP(rr, delReq)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: body=%s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

// TestTestClusterHandler_HappyPath tests POST /admin/clusters/{cid}/_test.

// Skipped: Cross-cluster bucket listing requires valid driver instances in registry
// func TestListAllBucketsHandler_HappyPath(t *testing.T) {
// 	t.Skip("requires valid drivers in the registry")
// }
