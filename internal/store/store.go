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
	sharesMu sync.RWMutex

	usersPath   string
	sharesPath  string
	auditDir    string
	orgCapsPath string

	usersCache     []User
	sharesCache    []Share
	orgCaps        *OrgCapabilitiesStore
	bucketGrants   BucketGrants
	userRegions    UserRegions
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
		sharesPath: filepath.Join(dataDir, "shares.json"),
		orgCapsPath: filepath.Join(dataDir, "org_capabilities.json"),
		auditDir: filepath.Join(dataDir, "audit"),
	}

	if err := s.loadAll(); err != nil {
		return nil, fmt.Errorf("loading existing data: %w", err)
	}

	// Initialize OrgCapabilities sub-store. AUTH.RBAC (v0.5.7) added
	// the type + accessor but the freshman forgot to wire it here —
	// /admin/system handler then nil-deref'd, returning 500. Caught
	// in v0.8.0d.12 post-deploy senior testing.
	orgCaps, err := OpenOrgCapabilities(dataDir)
	if err != nil {
		return nil, fmt.Errorf("opening org capabilities: %w", err)
	}
	s.orgCaps = orgCaps

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

// OrgCapabilities returns the org capabilities store.
func (s *Store) OrgCapabilities() *OrgCapabilitiesStore {
	return s.orgCaps
}

// WireBucketGrants opens the per-user per-bucket S3 credential store
// (ADR-0001, v0.9.0c) and attaches it to this Store. Kept separate
// from Open() so the long-existing Open(dataDir, retention) signature
// stays source-compatible with the many test callers in internal/api/
// that don't need credential grants. main.go calls this once at boot
// with cfg.JWT.Secret.
func (s *Store) WireBucketGrants(jwtSecret []byte) error {
	bg, err := OpenBucketGrants(s.dataDir, jwtSecret)
	if err != nil {
		return fmt.Errorf("opening bucket grants: %w", err)
	}
	s.bucketGrants = bg
	return nil
}

// CredGrants returns the credential-grant store (per-user per-bucket
// S3 keys, ADR-0001). Returns nil if WireBucketGrants has not been
// called — callers must nil-check.
//
// Historically named CredGrants() because the now-retired (v1.0.0b)
// legacy Store.BucketGrants(userID string) []string accessor owned the
// BucketGrants() method name. The name is kept for source stability.
func (s *Store) CredGrants() BucketGrants {
	return s.bucketGrants
}

// WireUserRegions opens the per-user S3 region keychain (ADR-0002,
// v1.1.0a) and attaches it to this Store. Kept separate from Open()
// for the same source-compatibility reason as WireBucketGrants. main.go
// calls this once at boot with cfg.JWT.Secret, right after
// WireBucketGrants.
func (s *Store) WireUserRegions(jwtSecret []byte) error {
	ur, err := OpenUserRegions(s.dataDir, jwtSecret)
	if err != nil {
		return fmt.Errorf("opening user regions: %w", err)
	}
	s.userRegions = ur
	return nil
}

// UserRegions returns the region-keychain store (per-user encrypted
// S3 credentials, ADR-0002). Returns nil if WireUserRegions has not
// been called — callers must nil-check.
func (s *Store) UserRegions() UserRegions {
	return s.userRegions
}

// MigrateLegacyUsers sets uiAdmin=true for existing admin users.
// This is a one-time migration on first boot after upgrade.
func (s *Store) MigrateLegacyUsers() error {
	s.usersMu.Lock()
	defer s.usersMu.Unlock()

	for i := range s.usersCache {
		if s.usersCache[i].Role == "admin" && !s.usersCache[i].UIAdmin {
			s.usersCache[i].UIAdmin = true
		}
	}

	return saveJSON(s.usersPath, s.usersCache)
}
