// Package store implements JSON-based persistent storage.
package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// BucketGrantsToUserRegionsMigration is the report returned by
// MigrateBucketGrantsToUserRegions so the caller can log a
// human-readable summary of what happened. Counts are post-migration:
// Created is the number of new UserRegion rows minted on this run,
// SkippedDuplicate is the number of (userId, endpoint) pairs that
// already existed in user_regions.json and were therefore no-ops.
// Failed lists per-row errors so an operator can investigate without
// blocking the rest of the migration.
type BucketGrantsToUserRegionsMigration struct {
	Scanned          int
	Created          int
	SkippedDuplicate int
	Failed           []BucketGrantMigrationFailure
}

// BucketGrantMigrationFailure records a single grant that couldn't be
// migrated. The error is preserved so the operator can correlate with
// the offending row in bucket_grants.json.
type BucketGrantMigrationFailure struct {
	GrantID  string
	UserID   string
	Endpoint string
	Err      error
}

// MigrateBucketGrantsToUserRegions scans the BucketGrants store for
// legacy per-bucket grants, dedupes by (userId, connectionId-endpoint),
// and mints an equivalent UserRegion per unique (userId, endpoint)
// pair. Picks the latest BucketGrant by UpdatedAt as the canonical
// source for the secret — in practice grants for the same (user,
// endpoint) carry the same secret because operators issue one S3 key
// per user per backend and reuse it across bucket grants.
//
// Idempotent: if a UserRegion for (userId, endpoint) already exists,
// the grant is counted as SkippedDuplicate and not re-created. Safe
// to call on every boot.
//
// Does NOT delete bucket_grants.json — that's deferred to v1.1.0e
// once the user-tier endpoints stop reading from it.
//
// Requires WireBucketGrants AND WireUserRegions to have run; returns
// an error if either is unwired. The Connection store is consulted
// to translate each grant's ConnectionID into the cluster's s3_endpoint
// (legacy BucketGrants carried ConnectionID, not endpoint).
func (s *Store) MigrateBucketGrantsToUserRegions(ctx context.Context, conns Connections) (BucketGrantsToUserRegionsMigration, error) {
	report := BucketGrantsToUserRegionsMigration{}

	if s.bucketGrants == nil {
		return report, fmt.Errorf("MigrateBucketGrantsToUserRegions: bucket-grants store not wired")
	}
	if s.userRegions == nil {
		return report, fmt.Errorf("MigrateBucketGrantsToUserRegions: user-regions store not wired")
	}
	if conns == nil {
		return report, fmt.Errorf("MigrateBucketGrantsToUserRegions: connections store is nil")
	}

	// Snapshot legacy grants. Iterating ListForUser per user would
	// double-scan; instead we read the file directly via the
	// BucketGrants interface using a per-connection probe — but the
	// store doesn't expose "list all", only "list for user/bucket".
	// Walk via the connections list: for each Connection, list the
	// grants attached to any of its buckets... that's wrong too
	// (ListForBucket is per-bucket).
	//
	// Path of least resistance: read bucket_grants.json directly via
	// the same load helper the store uses. Safe because we hold no
	// lock yet — at boot time the migration runs single-threaded
	// before the HTTP server starts.
	grantsPath := filepath.Join(s.dataDir, "bucket_grants.json")
	grants, err := loadJSON[[]BucketGrant](grantsPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No legacy file yet (fresh install) — nothing to do.
			return report, nil
		}
		return report, fmt.Errorf("loading bucket_grants.json: %w", err)
	}
	report.Scanned = len(grants)
	if len(grants) == 0 {
		return report, nil
	}

	// Build an endpoint lookup keyed by connection ID. We only need
	// the endpoint and driver; failed lookups skip the grant rather
	// than abort the whole migration.
	allConns, err := conns.List(ctx)
	if err != nil {
		return report, fmt.Errorf("listing connections for migration: %w", err)
	}
	endpointByConnID := make(map[string]string, len(allConns))
	for _, c := range allConns {
		ep := ""
		if v := strings.TrimSpace(c.Config["s3_endpoint"]); v != "" {
			ep = v
		} else if v := strings.TrimSpace(c.Config["endpoint"]); v != "" {
			ep = v
		}
		if ep == "" {
			continue
		}
		canon, err := NormalizeEndpoint(ep)
		if err != nil {
			continue
		}
		endpointByConnID[c.ID] = canon
	}

	// Group grants by (userID, canonical endpoint). Keep the newest
	// by UpdatedAt as the canonical row — its secret wins. Per the
	// cycle prompt all grants for the same (user, endpoint) should
	// share the same secret anyway; the newest-wins rule only matters
	// for the degenerate case where they differ.
	type key struct{ user, endpoint string }
	candidates := make(map[key]BucketGrant)
	for _, g := range grants {
		endpoint, ok := endpointByConnID[g.ConnectionID]
		if !ok {
			report.Failed = append(report.Failed, BucketGrantMigrationFailure{
				GrantID:  g.ID,
				UserID:   g.UserID,
				Endpoint: "",
				Err:      fmt.Errorf("no connection found for connectionId=%s", g.ConnectionID),
			})
			continue
		}
		k := key{user: g.UserID, endpoint: endpoint}
		existing, seen := candidates[k]
		if !seen || g.UpdatedAt.After(existing.UpdatedAt) {
			candidates[k] = g
		}
	}

	// Mint a UserRegion per unique (user, endpoint).
	for k, g := range candidates {
		// Idempotency: if the region already exists, skip — don't
		// rotate the secret on the user's behalf.
		if _, err := s.userRegions.GetByUserEndpoint(ctx, k.user, k.endpoint); err == nil {
			report.SkippedDuplicate++
			continue
		} else if !errors.Is(err, ErrUserRegionNotFound) {
			report.Failed = append(report.Failed, BucketGrantMigrationFailure{
				GrantID:  g.ID,
				UserID:   k.user,
				Endpoint: k.endpoint,
				Err:      fmt.Errorf("checking duplicate: %w", err),
			})
			continue
		}

		// Decrypt the legacy secret with the bucket-grants key, then
		// hand the plaintext to UserRegions.Create which re-encrypts
		// with the same JWT-derived AES key. Both stores share the
		// JWT secret so the resulting ciphertext is interchangeable
		// in entropy terms — separate nonces, same key.
		plain, err := s.bucketGrants.Decrypt(g)
		if err != nil {
			report.Failed = append(report.Failed, BucketGrantMigrationFailure{
				GrantID:  g.ID,
				UserID:   k.user,
				Endpoint: k.endpoint,
				Err:      fmt.Errorf("decrypting legacy secret: %w", err),
			})
			continue
		}

		_, err = s.userRegions.Create(ctx, UserRegion{
			UserID:       k.user,
			Alias:        "migrated",
			Endpoint:     k.endpoint,
			Region:       "", // Create defaults to us-east-1
			AccessKeyID:  g.AccessKeyID,
			SecretKeyEnc: []byte(plain), // Create encrypts immediately
		})
		if err != nil {
			// Race: another caller (re-run migration?) inserted the
			// same row between the GetByUserEndpoint probe and Create.
			// Count it as a skipped duplicate and carry on.
			if errors.Is(err, ErrUserRegionDuplicate) {
				report.SkippedDuplicate++
				continue
			}
			report.Failed = append(report.Failed, BucketGrantMigrationFailure{
				GrantID:  g.ID,
				UserID:   k.user,
				Endpoint: k.endpoint,
				Err:      fmt.Errorf("creating user region: %w", err),
			})
			continue
		}
		report.Created++
	}

	return report, nil
}
