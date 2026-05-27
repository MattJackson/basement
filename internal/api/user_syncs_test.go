package api

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/mattjackson/basement/internal/sync"
)

// ErrNotFound is a mock error for testing.
var syncErrNotFound = fmt.Errorf("sync job not found")

// mockSyncStore implements sync.Store for testing.
type mockSyncStore struct {
	listErr     error
	saveErr     error
	deleteErr   error
	syncJob     *sync.SyncJob
	jobs        []*sync.SyncJob
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
