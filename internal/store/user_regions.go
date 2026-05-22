// Package store: per-user S3 region keychain (ADR-0002, refined v1.2.0d).
//
// A UserRegion binds a basement user to an S3 endpoint (a "region")
// and stores the user-tier S3 credential (access key + AES-GCM-encrypted
// secret key) that signs the user's requests against that endpoint.
//
// Why a NEW type and file rather than reusing BucketGrant: ADR-0002
// supersedes the per-bucket grant model. At the user persona, what
// matters is "I have a key for this endpoint" — bucket visibility is
// the backend's word, not basement's. v1.2.0d refines the model so
// that each ACCESS KEY is the primary user noun: a user may register
// multiple UserRegions against the same endpoint with different aliases
// ("Work S3", "Personal S3") — each card on /files is one of these
// keys. The backend's S3 key bucket-grants govern which buckets each
// key can see. See docs/adr/0002-region-tier-user-model.md.
//
// On-disk file: user_regions.json under {dataDir}. Atomic write via the
// shared saveJSON helper (tmp + fsync + rename). Encryption at rest:
// AES-GCM via crypto.go using the JWT signing secret as the key
// material. The plaintext secret never lives in the in-memory cache
// (only the encrypted bytes) and is never marshalled to JSON. Callers
// wanting the plaintext call store.UserRegions.Decrypt.
//
// Uniqueness (v1.2.0d): the logical key is (UserID, Endpoint, Alias).
// Same user + same endpoint with a DIFFERENT alias is allowed; same
// alias errors with ErrUserRegionDuplicate. Endpoint stays
// canonicalized via NormalizeEndpoint so stylistic variants don't
// sneak past the alias check.
//
// Hot-path lookup: GetByUserEndpoint(userID, endpoint) is the
// signing-layer / sync-resolver lookup. Per v1.2.0d, when multiple
// UserRegions share the same (user, endpoint) it returns the FIRST
// match by index (i.e. by insertion order — oldest first), which is
// fine for the sync resolver because all keys at one endpoint bridge
// to the same admin Connection.
package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// UserRegion is one user's S3 credential for one endpoint ("region").
//
// v1.2.0d: (UserID, Endpoint, Alias) is the logical unique key — same
// user can add the same endpoint multiple times under different
// aliases. Endpoint is stored pre-canonicalized via NormalizeEndpoint,
// so stylistic URL variants (default port, trailing slash, host case)
// still collide on the alias check.
type UserRegion struct {
	ID           string    `json:"id"`
	UserID       string    `json:"userId"`
	Alias        string    `json:"alias"`
	Endpoint     string    `json:"endpoint"` // canonical, see NormalizeEndpoint
	Region       string    `json:"region"`   // S3 region label, default "us-east-1"
	AccessKeyID  string    `json:"accessKeyId"`
	SecretKeyEnc []byte    `json:"secretKeyEnc"` // AES-GCM(nonce||ct||tag), per crypto.go
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	LastUsedAt   time.Time `json:"lastUsedAt,omitempty"`
}

// UserRegions is the CRUD surface for region keychain entries. Mirrors
// the shape of BucketGrants for consistency across the store package.
//
// Create takes the SECRET on the UserRegion value in plaintext; the
// implementation encrypts immediately and never holds plaintext beyond
// the call. Same for Update (which doubles as the rotation path —
// a non-empty SecretKeyEnc... actually, see Update doc below for the
// patch semantics).
//
// Decrypt explicitly unwraps a region's SecretKeyEnc; it's the only
// path to the plaintext secret, so audit greps are easy.
//
// TouchLastUsed bumps LastUsedAt with a per-row 1-minute debounce so a
// burst of signed requests doesn't hammer disk; this is the only
// mutator that may be a no-op on the persistence layer.
type UserRegions interface {
	Create(ctx context.Context, r UserRegion) (UserRegion, error)
	Get(ctx context.Context, id string) (UserRegion, error)
	GetByUserEndpoint(ctx context.Context, userID, endpoint string) (UserRegion, error)
	Update(ctx context.Context, id string, patch UserRegion) (UserRegion, error)
	Delete(ctx context.Context, id string) error
	ListForUser(ctx context.Context, userID string) ([]UserRegion, error)
	TouchLastUsed(ctx context.Context, id string) error
	Decrypt(r UserRegion) (string, error)
}

// ErrUserRegionNotFound is returned by Get / GetByUserEndpoint / Update
// / Delete / TouchLastUsed when the targeted region does not exist.
var ErrUserRegionNotFound = errors.New("user region not found")

// ErrUserRegionDuplicate is returned by Create when an existing region
// already covers the same (userID, canonicalized endpoint, alias)
// triple. v1.2.0d: same endpoint with a DIFFERENT alias is allowed.
var ErrUserRegionDuplicate = errors.New("duplicate user+endpoint+alias")

// touchDebounce is the minimum time between persisted LastUsedAt bumps
// for a single row. A burst of N signed requests within the window
// performs at most one disk write.
const touchDebounce = time.Minute

// NormalizeEndpoint canonicalizes an S3 endpoint URL so equivalent
// inputs ("https://S3.PQ.IO:443/", "https://s3.pq.io") fold to the
// same key for uniqueness checks and lookups.
//
// Rules:
//   - scheme is preserved (http vs https are different endpoints)
//   - scheme + host are lower-cased
//   - default port for scheme (80 for http, 443 for https) is stripped
//   - any path/query/fragment is stripped (S3 endpoint URLs are
//     scheme://host[:port] by convention)
//   - trailing slash is stripped
//
// Returns an error if the URL is unparseable, missing scheme, or
// missing host.
func NormalizeEndpoint(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", errors.New("endpoint is required")
	}

	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("parsing endpoint: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme == "" {
		return "", errors.New("endpoint must include scheme (e.g. https://)")
	}
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}

	host := strings.ToLower(u.Host)
	if host == "" {
		return "", errors.New("endpoint must include host")
	}

	// Strip default port for the scheme.
	if scheme == "https" && strings.HasSuffix(host, ":443") {
		host = strings.TrimSuffix(host, ":443")
	} else if scheme == "http" && strings.HasSuffix(host, ":80") {
		host = strings.TrimSuffix(host, ":80")
	}

	return scheme + "://" + host, nil
}

// userRegionStore implements UserRegions on top of a JSON file.
type userRegionStore struct {
	path      string
	jwtSecret []byte

	mu    sync.RWMutex
	cache []UserRegion

	// lastTouchPersist tracks the wall-clock time at which each row's
	// LastUsedAt was last persisted to disk. Used by TouchLastUsed to
	// debounce hot-path writes. Map is guarded by mu.
	lastTouchPersist map[string]time.Time
}

// OpenUserRegions opens or creates the region-keychain store at
// dataDir, encrypting secrets with a key derived from jwtSecret.
// jwtSecret must be non-empty.
func OpenUserRegions(dataDir string, jwtSecret []byte) (UserRegions, error) {
	if len(jwtSecret) == 0 {
		return nil, errors.New("OpenUserRegions: empty jwtSecret")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}

	s := &userRegionStore{
		path:             filepath.Join(dataDir, "user_regions.json"),
		jwtSecret:        append([]byte(nil), jwtSecret...), // defensive copy
		cache:            make([]UserRegion, 0),
		lastTouchPersist: make(map[string]time.Time),
	}

	if err := s.load(); err != nil {
		return nil, fmt.Errorf("loading user regions: %w", err)
	}

	return s, nil
}

func (s *userRegionStore) load() error {
	rows, err := loadJSON[[]UserRegion](s.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if rows == nil {
		s.cache = make([]UserRegion, 0)
	} else {
		s.cache = rows
	}
	return nil
}

func (s *userRegionStore) save() error {
	return saveJSON(s.path, s.cache)
}

// validateCreateInput trims whitespace, enforces required fields,
// normalizes the endpoint, and applies the default region label.
// Returns the cleaned UserRegion (without ID/timestamps/SecretKeyEnc).
func validateUserRegionCreate(in UserRegion, plaintextSecret string) (UserRegion, error) {
	in.UserID = strings.TrimSpace(in.UserID)
	in.Alias = strings.TrimSpace(in.Alias)
	in.Region = strings.TrimSpace(in.Region)
	in.AccessKeyID = strings.TrimSpace(in.AccessKeyID)

	if in.UserID == "" {
		return in, errors.New("userId is required")
	}
	if in.AccessKeyID == "" {
		return in, errors.New("accessKeyId is required")
	}
	if plaintextSecret == "" {
		return in, errors.New("secretKey is required")
	}

	canon, err := NormalizeEndpoint(in.Endpoint)
	if err != nil {
		return in, err
	}
	in.Endpoint = canon

	if in.Region == "" {
		in.Region = "us-east-1"
	}
	return in, nil
}

// Create inserts a new UserRegion. The caller passes plaintext in
// r.SecretKeyEnc as raw bytes (i.e. []byte("secret")) — the store
// encrypts immediately and overwrites the field with ciphertext. The
// passed value should be treated as moved-from.
func (s *userRegionStore) Create(_ context.Context, r UserRegion) (UserRegion, error) {
	plaintext := string(r.SecretKeyEnc)
	r.SecretKeyEnc = nil

	clean, err := validateUserRegionCreate(r, plaintext)
	if err != nil {
		return UserRegion{}, err
	}

	enc, err := encryptSecret([]byte(plaintext), s.jwtSecret)
	if err != nil {
		return UserRegion{}, fmt.Errorf("encrypting secret: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// v1.2.0d: enforce uniqueness on (userID, canonicalEndpoint, alias).
	// Same user + same endpoint with a DIFFERENT alias is allowed —
	// each access key is the primary user noun, and an operator may
	// legitimately want "Work S3" + "Personal S3" against the same
	// service. Same alias still duplicates so we don't silently shadow
	// an existing keychain entry.
	for _, existing := range s.cache {
		if existing.UserID == clean.UserID &&
			existing.Endpoint == clean.Endpoint &&
			existing.Alias == clean.Alias {
			return UserRegion{}, ErrUserRegionDuplicate
		}
	}

	now := time.Now().UTC()
	out := UserRegion{
		ID:           uuid.NewString(),
		UserID:       clean.UserID,
		Alias:        clean.Alias,
		Endpoint:     clean.Endpoint,
		Region:       clean.Region,
		AccessKeyID:  clean.AccessKeyID,
		SecretKeyEnc: enc,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	s.cache = append(s.cache, out)

	if err := s.save(); err != nil {
		// Roll back the cache append so an in-memory failure doesn't
		// stay observable to subsequent reads.
		s.cache = s.cache[:len(s.cache)-1]
		return UserRegion{}, fmt.Errorf("persisting user region: %w", err)
	}
	return out, nil
}

func (s *userRegionStore) Get(_ context.Context, id string) (UserRegion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.cache {
		if r.ID == id {
			return r, nil
		}
	}
	return UserRegion{}, ErrUserRegionNotFound
}

// GetByUserEndpoint is the hot-path lookup. The supplied endpoint is
// normalized before comparison so callers can pass either the raw form
// they have or the canonical form already on file.
//
// Precedence (v1.2.0d): when a user has multiple UserRegions at the
// same canonical endpoint (different aliases), the FIRST match by
// insertion order wins. That's sufficient for the sync resolver —
// every key against one endpoint bridges to the same admin Connection,
// so any match resolves identically. Callers needing a specific row by
// alias should call ListForUser + filter, or call Get by ID.
func (s *userRegionStore) GetByUserEndpoint(_ context.Context, userID, endpoint string) (UserRegion, error) {
	canon, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return UserRegion{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.cache {
		if r.UserID == userID && r.Endpoint == canon {
			return r, nil
		}
	}
	return UserRegion{}, ErrUserRegionNotFound
}

// Update applies a partial patch keyed by ID. Non-empty Alias, Region,
// AccessKeyID replace the existing fields. Non-empty SecretKeyEnc
// (interpreted as plaintext bytes, matching the Create convention)
// rotates the encrypted blob with a fresh nonce. (UserID, Endpoint)
// are immutable on update — to move a region, Delete + Create.
func (s *userRegionStore) Update(_ context.Context, id string, patch UserRegion) (UserRegion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.cache {
		if s.cache[i].ID != id {
			continue
		}
		row := &s.cache[i]

		// Encrypt outside the cache mutation so a crypto failure leaves
		// the existing record untouched.
		var newEnc []byte
		if len(patch.SecretKeyEnc) > 0 {
			var err error
			newEnc, err = encryptSecret(patch.SecretKeyEnc, s.jwtSecret)
			if err != nil {
				return UserRegion{}, fmt.Errorf("encrypting rotated secret: %w", err)
			}
		}

		if alias := strings.TrimSpace(patch.Alias); alias != "" {
			row.Alias = alias
		}
		if region := strings.TrimSpace(patch.Region); region != "" {
			row.Region = region
		}
		if ak := strings.TrimSpace(patch.AccessKeyID); ak != "" {
			row.AccessKeyID = ak
		}
		if newEnc != nil {
			row.SecretKeyEnc = newEnc
		}
		row.UpdatedAt = time.Now().UTC()

		if err := s.save(); err != nil {
			return UserRegion{}, fmt.Errorf("persisting user region update: %w", err)
		}
		return *row, nil
	}
	return UserRegion{}, ErrUserRegionNotFound
}

func (s *userRegionStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.cache {
		if s.cache[i].ID == id {
			s.cache = append(s.cache[:i], s.cache[i+1:]...)
			delete(s.lastTouchPersist, id)
			if err := s.save(); err != nil {
				return fmt.Errorf("persisting user region delete: %w", err)
			}
			return nil
		}
	}
	return ErrUserRegionNotFound
}

func (s *userRegionStore) ListForUser(_ context.Context, userID string) ([]UserRegion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]UserRegion, 0)
	for _, r := range s.cache {
		if r.UserID == userID {
			out = append(out, r)
		}
	}
	return out, nil
}

// TouchLastUsed bumps the LastUsedAt timestamp for the given row. To
// avoid disk thrash from a high-frequency signing path, persistence is
// debounced to at most one write per row per touchDebounce window;
// in-memory LastUsedAt is updated on every call so reads see a fresh
// value even when the disk write is skipped.
func (s *userRegionStore) TouchLastUsed(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.cache {
		if s.cache[i].ID != id {
			continue
		}
		now := time.Now().UTC()
		s.cache[i].LastUsedAt = now

		last, seen := s.lastTouchPersist[id]
		if seen && now.Sub(last) < touchDebounce {
			// Within the debounce window — skip disk write.
			return nil
		}

		if err := s.save(); err != nil {
			return fmt.Errorf("persisting last-used bump: %w", err)
		}
		s.lastTouchPersist[id] = now
		return nil
	}
	return ErrUserRegionNotFound
}

// Decrypt unwraps the secret key for one region. This is the ONLY path
// from ciphertext back to plaintext — keep it that way so audit greps
// are easy.
func (s *userRegionStore) Decrypt(r UserRegion) (string, error) {
	return decryptSecret(r.SecretKeyEnc, s.jwtSecret)
}
