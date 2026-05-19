// Package store implements JSON-based persistent storage.
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store holds all data with per-file mutexes for concurrency safety.
type Store struct {
	dataDir  string
	retention time.Duration

	usersMu sync.RWMutex
	grantsMu sync.RWMutex
	sharesMu sync.RWMutex

	usersPath   string
	grantsPath  string
	sharesPath  string
	auditDir    string

	usersCache     []User
	grantsCache    map[string][]Grant // userID -> grants
	sharesCache    []Share
}

// Open opens or creates the store at dataDir with the given retention period.
func Open(dataDir string, retention time.Duration) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}

	s := &Store{
		dataDir:   dataDir,
		retention: retention,

		usersPath: filepath.Join(dataDir, "users.json"),
		grantsPath: filepath.Join(dataDir, "grants.json"),
		sharesPath: filepath.Join(dataDir, "shares.json"),
		auditDir: filepath.Join(dataDir, "audit"),
	}

	if err := s.loadAll(); err != nil {
		return nil, fmt.Errorf("loading existing data: %w", err)
	}

	return s, nil
}

// loadAll loads all cached data from disk.
func (s *Store) loadAll() error {
	var errs []error

	if users, err := loadJSON[[]User](s.usersPath); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("loading users: %w", err))
	} else if err == nil {
		s.usersMu.Lock()
		s.usersCache = users
		s.usersMu.Unlock()
	}

	if grants, err := loadJSON[map[string][]Grant](s.grantsPath); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("loading grants: %w", err))
	} else if err == nil {
		s.grantsMu.Lock()
		s.grantsCache = grants
		s.grantsMu.Unlock()
	}

	if shares, err := loadJSON[[]Share](s.sharesPath); err != nil && !os.IsNotExist(err) {
		errs = append(errs, fmt.Errorf("loading shares: %w", err))
	} else if err == nil {
		s.sharesMu.Lock()
		s.sharesCache = shares
		s.sharesMu.Unlock()
	}

	if len(errs) > 0 {
		return fmt.Errorf("multiple errors loading data: %v", errs)
	}

	return nil
}
