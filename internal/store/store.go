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
	dataDir   string
	retention time.Duration

	usersMu  sync.RWMutex
	sharesMu sync.RWMutex

	usersPath   string
	sharesPath  string
	auditDir    string
	orgCapsPath string

	usersCache  []User
	sharesCache []Share
	orgCaps     *OrgCapabilitiesStore
	userRegions UserRegions
}

// Open opens or creates the store at dataDir with the given retention period.
func Open(dataDir string, retention time.Duration) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}

	s := &Store{
		dataDir:   dataDir,
		retention: retention,

		usersPath:   filepath.Join(dataDir, "users.json"),
		sharesPath:  filepath.Join(dataDir, "shares.json"),
		orgCapsPath: filepath.Join(dataDir, "org_capabilities.json"),
		auditDir:    filepath.Join(dataDir, "audit"),
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

// WireUserRegions opens the per-user S3 region keychain (ADR-0002,
// v1.1.0a) and attaches it to this Store. Kept separate from Open()
// for source-compatibility with the many api.New(...) test callers
// that don't need the keychain wired up. main.go calls this once at
// boot with cfg.JWT.Secret.
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

// ArchiveLegacyBucketGrants renames {dataDir}/bucket_grants.json to
// bucket_grants.json.migrated-v1.1.0e if the legacy file still exists.
// Idempotent: no-op if the file is already archived or never existed.
//
// The per-user per-bucket BucketGrants table was the v0.9.0c–v1.1.0d
// credential store; ADR-0002 replaced it with the per-user region
// keychain (user_regions.json). v1.1.0d's MigrateBucketGrantsToUserRegions
// (now retired) populated the new file; this archive step retires the
// old one without deleting the bytes — operators rolling back can
// rename the .migrated suffix off and reintroduce the legacy code path
// from git history.
//
// Returns (true, nil) when a rename happened on this call, (false, nil)
// when the file was absent or already archived, and (false, err) on a
// filesystem failure.
func (s *Store) ArchiveLegacyBucketGrants() (bool, error) {
	src := filepath.Join(s.dataDir, "bucket_grants.json")
	dst := filepath.Join(s.dataDir, "bucket_grants.json.migrated-v1.1.0e")

	// Idempotency: if the source is gone, there's nothing to archive.
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("statting bucket_grants.json: %w", err)
	}

	// Idempotency: if the destination already exists, treat the source
	// as a leftover and leave both in place — operator intervention
	// territory. We refuse to overwrite to preserve the original
	// archive (the FIRST migration is the one that matched v1.1.0d's
	// migration; later boots that find bucket_grants.json reappear
	// probably indicate a partial restore from backup).
	if _, err := os.Stat(dst); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("statting archive target: %w", err)
	}

	if err := os.Rename(src, dst); err != nil {
		return false, fmt.Errorf("renaming bucket_grants.json: %w", err)
	}
	return true, nil
}
