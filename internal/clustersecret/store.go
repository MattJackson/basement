// Package clustersecret: persistent store implementation.
//
// Lives in the same package as ClusterSecretManager so the JSON file
// layout stays in lockstep with the in-memory representation. Production
// wires NewFileStore(dataDir); the manager treats it as the
// ClusterSecretStore interface so tests can substitute MemoryStore.
//
// On-disk shape: a single JSON file under {dataDir}/cluster_secrets.json
// containing a flat list of WrappedCSK records. Atomic writes via
// write-tmp-then-rename so a crash mid-save leaves the previous version
// intact.

package clustersecret

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileStore persists WrappedCSK records as a single JSON file.
// Goroutine-safe; all reads + writes serialise on mu.
type FileStore struct {
	path string

	mu      sync.Mutex
	records []WrappedCSK
	loaded  bool
}

// NewFileStore opens or creates the cluster_secrets.json file under
// dataDir. Missing file is treated as an empty record list — first
// PutWrappedCSK creates it.
func NewFileStore(dataDir string) (*FileStore, error) {
	if dataDir == "" {
		return nil, errors.New("clustersecret: empty dataDir")
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("clustersecret: mkdir %s: %w", dataDir, err)
	}
	s := &FileStore{
		path: filepath.Join(dataDir, "cluster_secrets.json"),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// load reads the file into the in-memory cache. Missing file is fine.
func (s *FileStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.records = nil
			s.loaded = true
			return nil
		}
		return fmt.Errorf("clustersecret: read %s: %w", s.path, err)
	}
	if len(data) == 0 {
		s.records = nil
		s.loaded = true
		return nil
	}
	var recs []WrappedCSK
	if err := json.Unmarshal(data, &recs); err != nil {
		return fmt.Errorf("clustersecret: unmarshal %s: %w", s.path, err)
	}
	s.records = recs
	s.loaded = true
	return nil
}

// saveLocked writes the in-memory list back to disk atomically.
// Caller must hold s.mu.
func (s *FileStore) saveLocked() error {
	data, err := json.MarshalIndent(s.records, "", "  ")
	if err != nil {
		return fmt.Errorf("clustersecret: marshal: %w", err)
	}
	data = append(data, '\n')

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("clustersecret: write tmp: %w", err)
	}
	// fsync before rename so the bytes are durable on disk.
	f, err := os.OpenFile(tmp, os.O_RDONLY, 0600)
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("clustersecret: open tmp for fsync: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("clustersecret: fsync tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("clustersecret: close tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("clustersecret: rename tmp to final: %w", err)
	}
	return nil
}

// GetWrappedCSKs returns every record for clusterID. Empty slice when
// no record exists.
func (s *FileStore) GetWrappedCSKs(clusterID string) ([]WrappedCSK, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]WrappedCSK, 0)
	for _, r := range s.records {
		if r.ClusterID == clusterID {
			out = append(out, r)
		}
	}
	return out, nil
}

// PutWrappedCSK inserts or replaces the record for
// (rec.ClusterID, rec.AdminUserID). Persists immediately.
func (s *FileStore) PutWrappedCSK(rec WrappedCSK) error {
	if rec.ClusterID == "" || rec.AdminUserID == "" {
		return errors.New("clustersecret: PutWrappedCSK requires ClusterID + AdminUserID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	replaced := false
	for i, r := range s.records {
		if r.ClusterID == rec.ClusterID && r.AdminUserID == rec.AdminUserID {
			s.records[i] = rec
			replaced = true
			break
		}
	}
	if !replaced {
		s.records = append(s.records, rec)
	}
	if err := s.saveLocked(); err != nil {
		// Roll back the in-memory mutation so the cache stays in sync
		// with disk on save failure.
		if replaced {
			// Best effort: reload from disk. If load fails the next
			// boot picks up the on-disk state regardless.
			s.loaded = false
			s.records = nil
			_ = s.load() // best-effort
		} else {
			s.records = s.records[:len(s.records)-1]
		}
		return err
	}
	return nil
}

// DeleteWrappedCSK removes the record for (clusterID, adminUserID).
// No-op when the record is absent (idempotent — operators may delete
// an admin twice during cleanup scripts).
func (s *FileStore) DeleteWrappedCSK(clusterID, adminUserID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.records {
		if r.ClusterID == clusterID && r.AdminUserID == adminUserID {
			removed := s.records[i]
			s.records = append(s.records[:i], s.records[i+1:]...)
			if err := s.saveLocked(); err != nil {
				// Restore on save failure.
				rest := append([]WrappedCSK{removed}, s.records[i:]...)
				s.records = append(s.records[:i], rest...)
				return err
			}
			return nil
		}
	}
	return nil
}

// MemoryStore is an in-memory ClusterSecretStore used by tests. Safe
// for concurrent access.
type MemoryStore struct {
	mu      sync.Mutex
	records []WrappedCSK
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{} }

// GetWrappedCSKs implements ClusterSecretStore.
func (m *MemoryStore) GetWrappedCSKs(clusterID string) ([]WrappedCSK, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]WrappedCSK, 0)
	for _, r := range m.records {
		if r.ClusterID == clusterID {
			out = append(out, r)
		}
	}
	return out, nil
}

// PutWrappedCSK implements ClusterSecretStore.
func (m *MemoryStore) PutWrappedCSK(rec WrappedCSK) error {
	if rec.ClusterID == "" || rec.AdminUserID == "" {
		return errors.New("clustersecret: MemoryStore Put requires ClusterID + AdminUserID")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.records {
		if r.ClusterID == rec.ClusterID && r.AdminUserID == rec.AdminUserID {
			m.records[i] = rec
			return nil
		}
	}
	m.records = append(m.records, rec)
	return nil
}

// DeleteWrappedCSK implements ClusterSecretStore.
func (m *MemoryStore) DeleteWrappedCSK(clusterID, adminUserID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.records {
		if r.ClusterID == clusterID && r.AdminUserID == adminUserID {
			m.records = append(m.records[:i], m.records[i+1:]...)
			return nil
		}
	}
	return nil
}
