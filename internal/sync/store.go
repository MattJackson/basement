package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

// Store handles persistence of sync jobs to JSON files.
type Store interface {
	Load(id string) (*SyncJob, error)
	Save(job *SyncJob) error
	List(userID string) ([]*SyncJob, error)
	Delete(id string) error
}

type fileStore struct {
	dir  string
	mu   sync.RWMutex
}

// NewFileStore creates a new file-based store.
func NewFileStore(dataDir string) Store {
	syncDir := filepath.Join(dataDir, "syncs")
	if err := os.MkdirAll(syncDir, 0755); err != nil {
		panic(fmt.Errorf("creating sync directory: %w", err))
	}
	return &fileStore{dir: syncDir}
}

// GenerateID creates a new unique job ID.
func GenerateID() string {
	return uuid.New().String()
}

// Load loads a sync job from disk.
func (s *fileStore) Load(id string) (*SyncJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.dir, id, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading job state: %w", err)
	}

	var job SyncJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("parsing job state: %w", err)
	}

	return &job, nil
}

// Save persists a sync job to disk atomically.
func (s *fileStore) Save(job *SyncJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	jobDir := filepath.Join(s.dir, job.ID)
	if err := os.MkdirAll(jobDir, 0755); err != nil {
		return fmt.Errorf("creating job directory: %w", err)
	}

	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling job state: %w", err)
	}

	tmpPath := filepath.Join(jobDir, "state.json.tmp")
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}

	if err := os.Rename(tmpPath, filepath.Join(jobDir, "state.json")); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming state file: %w", err)
	}

	return nil
}

// List returns all sync jobs for a user.
func (s *fileStore) List(userID string) ([]*SyncJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("reading sync directory: %w", err)
	}

	var jobs []*SyncJob
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		job, err := s.Load(entry.Name())
		if err != nil {
			continue
		}

		if job.OwnerUserID == userID {
			jobs = append(jobs, job)
		}
	}

	return jobs, nil
}

// Delete removes a sync job from disk.
func (s *fileStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	jobDir := filepath.Join(s.dir, id)
	if err := os.RemoveAll(jobDir); err != nil {
		return fmt.Errorf("removing job directory: %w", err)
	}

	return nil
}
