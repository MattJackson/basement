// Package store: per-user skin preference storage (v2.0.0-beta.28).
//
// This store provides a simple key-value mapping from UserID to skinName,
// allowing authenticated users to override the org-wide active skin with
// their personal choice when userOverridableSkin is enabled.
//
// On-disk file: user_skins.json under {dataDir}. Atomic write via the
// shared saveJSON helper (tmp + fsync + rename). No encryption needed -
// skin names are non-sensitive metadata.
//
// Uniqueness: one entry per UserID. If a user deletes their preference,
// Get returns ok=false and callers fall back to org ActiveSkin.
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// UserSkins is the interface for per-user skin preference storage.
type UserSkins interface {
	Get(userID string) (skinName string, ok bool)
	Set(userID string, skinName string) error
	Delete(userID string) error
}

// userSkinsStore implements per-user skin name persistence on top of a JSON file.
type userSkinsStore struct {
	path  string
	mu    sync.RWMutex
	cache map[string]string // userID -> skinName
}

// OpenUserSkins opens or creates the user-skin store at dataDir.
func OpenUserSkins(dataDir string) (UserSkins, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}

	s := &userSkinsStore{
		path:  filepath.Join(dataDir, "user_skins.json"),
		cache: make(map[string]string),
	}

	if err := s.load(); err != nil {
		return nil, fmt.Errorf("loading user skins: %w", err)
	}

	return s, nil
}

func (s *userSkinsStore) load() error {
	rows, err := loadJSON[map[string]string](s.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if rows == nil {
		s.cache = make(map[string]string)
	} else {
		s.cache = rows
	}
	return nil
}

func (s *userSkinsStore) save() error {
	return saveJSON(s.path, s.cache)
}


// Get retrieves the skin name for a given user. Returns ok=false if no
// preference is set, indicating callers should fall back to org ActiveSkin.
func (s *userSkinsStore) Get(userID string) (skinName string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	skinName, ok = s.cache[userID]
	return skinName, ok
}

// Set persists a user's skin preference. Empty skinName is allowed and
// treated as "delete this user's preference" - callers should use Delete
// explicitly for clarity, but this provides a convenient no-op path.
func (s *userSkinsStore) Set(userID string, skinName string) error {
	if userID == "" {
		return errors.New("userID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if skinName == "" {
		delete(s.cache, userID)
	} else {
		s.cache[userID] = skinName
	}

	if err := s.save(); err != nil {
		return fmt.Errorf("persisting user skin: %w", err)
	}
	return nil
}

// Delete removes a user's skin preference explicitly. Idempotent - no-op
// if the user has no preference set.
func (s *userSkinsStore) Delete(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.cache, userID)
	return s.save()
}
