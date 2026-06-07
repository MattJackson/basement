package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/sync"
)

// ErrNotFound is a mock error for testing.
var syncErrNotFound = fmt.Errorf("sync job not found")

// mockSyncStore implements sync.Store for testing.
type mockSyncStore struct {
	listErr       error
	saveErr       error
	deleteErr     error
	syncJob       *sync.SyncJob
	jobs          []*sync.SyncJob
	isNotExistErr bool
}

func (m *mockSyncStore) Save(job *sync.SyncJob) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.jobs = append(m.jobs, job)
	return nil
}

func (m *mockSyncStore) List(userID string) ([]*sync.SyncJob, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var filtered []*sync.SyncJob
	for _, j := range m.jobs {
		if j.OwnerUserID == userID {
			filtered = append(filtered, j)
		}
	}
	return filtered, nil
}

func (m *mockSyncStore) Load(id string) (*sync.SyncJob, error) {
	if m.syncJob != nil && m.syncJob.ID == id {
		return m.syncJob, nil
	}
	return nil, syncErrNotFound
}

func (m *mockSyncStore) Delete(id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	for i, j := range m.jobs {
		if j.ID == id && j.OwnerUserID == "test-user" {
			m.jobs = append(m.jobs[:i], m.jobs[i+1:]...)
			return nil
		}
	}
	return syncErrNotFound
}

// newMockSyncJob creates a minimal sync job for testing.
func newMockSyncJob(id, userID string) *sync.SyncJob {
	return &sync.SyncJob{ID: id, OwnerUserID: userID, State: "queued"}
}

func TestUserListSyncsHandler_NilStore(t *testing.T) {
	s := &Server{syncStore: nil}
	req := httptest.NewRequest("GET", "/api/v1/user/syncs", nil)
	w := httptest.NewRecorder()

	s.userListSyncsHandler(w, req)

	if w.Code != 503 {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestUserListSyncsHandler_ENOENT(t *testing.T) {
	mockStore := &mockSyncStore{}
	s := &Server{syncStore: mockStore, cfg: &config.Config{JWT: config.JWTConfig{Secret: testSecret}}}

	secret := testSecret
	token, err := auth.IssueTokenWithActiveRole(secret, "test-user", "admin", true, "user", 0, 24*time.Hour, nil)
	if err != nil {
		t.Fatalf("failed to generate test token: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/user/syncs", nil)
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: token, Path: "/", Secure: true, HttpOnly: true})
	w := httptest.NewRecorder()

	authMw := s.authMiddleware()
	handler := authMw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.userListSyncsHandler(w, r)
	}))
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("expected status 200, got %d. Body: %s", w.Code, w.Body.String())
	}
}

// withSyncAuth wraps a handler with the auth middleware and a valid
// session cookie for "test-user", so handlers that read
// auth.FromContext before their nil-store guard can be exercised.
func withSyncAuth(t *testing.T, s *Server, h http.HandlerFunc, method, url string) *httptest.ResponseRecorder {
	t.Helper()
	if s.cfg == nil {
		s.cfg = &config.Config{JWT: config.JWTConfig{Secret: testSecret}}
	}
	token, err := auth.IssueTokenWithActiveRole(testSecret, "test-user", "admin", true, "user", 0, 24*time.Hour, nil)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	req := httptest.NewRequest(method, url, nil)
	// Inject the chi {id} param the per-record handlers read so the
	// empty-id 400 doesn't pre-empt the nil-store 503 we're testing.
	req = withChiParam(req, "id", "j1")
	req.AddCookie(&http.Cookie{Name: "__Host-basement_session", Value: token, Path: "/", Secure: true, HttpOnly: true})
	w := httptest.NewRecorder()
	s.authMiddleware()(h).ServeHTTP(w, req)
	return w
}

// TestUserSyncHandlers_NilStore_503: every per-record sync handler
// (create/get/delete/pause/resume) returns 503 — not a nil panic —
// when the sync store isn't wired (r11). Previously only the list
// handler guarded this.
func TestUserSyncHandlers_NilStore_503(t *testing.T) {
	s := &Server{syncStore: nil, cfg: &config.Config{JWT: config.JWTConfig{Secret: testSecret}}}

	cases := []struct {
		name   string
		h      http.HandlerFunc
		method string
		url    string
	}{
		{"create", s.userCreateSyncHandler, http.MethodPost, "/api/v1/user/syncs"},
		{"get", s.userGetSyncHandler, http.MethodGet, "/api/v1/user/syncs/j1"},
		{"delete", s.userDeleteSyncHandler, http.MethodDelete, "/api/v1/user/syncs/j1"},
		{"pause", s.userPauseSyncHandler, http.MethodPost, "/api/v1/user/syncs/j1/pause"},
		{"resume", s.userResumeSyncHandler, http.MethodPost, "/api/v1/user/syncs/j1/resume"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := withSyncAuth(t, s, c.h, c.method, c.url)
			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("%s: status=%d body=%s; want 503", c.name, w.Code, w.Body.String())
			}
		})
	}
}
