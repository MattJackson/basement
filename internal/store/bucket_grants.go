// Package store: per-user per-bucket S3 credential grants (ADR-0001).
//
// A BucketGrant binds a basement user to a (cluster, bucket) pair and
// stores the user-tier S3 credential (access key + encrypted secret key)
// the runtime mints an S3 client from.
//
// Why a NEW type and file rather than reusing the legacy `Grant` in
// grants.go: the legacy Grant is a *policy* artefact (which buckets a
// user may see, with what permissions). Per ADR-0001 the credentials
// and the policy are now SEPARATE concerns — assignments live under
// internal/auth/policy/, credentials live here. The legacy Grant is on
// the slow road to deprecation but is still used by user_filter.go and
// the v0.8.x share-creation flow; freshman cycle v0.9.0c does not
// touch it (the cycle prompt explicitly forbids changes outside
// internal/store/ and the single main.go call site).
//
// On-disk file: bucket_grants.json under {dataDir}. Atomic write via
// the shared saveJSON helper (tmp + fsync + rename).
//
// Encryption at rest: AES-GCM via crypto.go using the JWT signing
// secret as the key material. The plaintext secret never lives in the
// in-memory cache (only the encrypted bytes) and is never marshalled
// to JSON. Callers wanting the plaintext call store.DecryptBucketGrant.
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

	"github.com/google/uuid"
)

// BucketGrant is one user's S3 credential for one bucket on one cluster.
//
// (UserID, ConnectionID, BucketID) is the logical unique key — there is
// exactly one BucketGrant per user per bucket. ID is a UUID kept as the
// primary key so URLs and grant-rotation flows stay stable across
// secret rotations.
type BucketGrant struct {
	ID           string    `json:"id"`
	UserID       string    `json:"userId"`
	ConnectionID string    `json:"connectionId"`
	BucketID     string    `json:"bucketId"`
	AccessKeyID  string    `json:"accessKeyId"`
	SecretKeyEnc []byte    `json:"secretKeyEnc"` // AES-GCM(nonce||ct||tag), per crypto.go
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// BucketGrants is the CRUD surface for credential grants. Mirrors the
// shape of the Connections interface to keep the store package
// consistent across collection types.
//
// Create takes the SECRET on the BucketGrant value in plaintext (via the
// SecretKey accessor below — see BucketGrantInput); the implementation
// encrypts immediately and never holds plaintext beyond the call. Same
// for Update.
//
// Decrypt explicitly unwraps a grant's SecretKeyEnc; it's the only path
// to the plaintext secret, so it's easy to grep for at audit time.
type BucketGrants interface {
	Create(ctx context.Context, in BucketGrantInput) (BucketGrant, error)
	Get(ctx context.Context, id string) (BucketGrant, error)
	GetByUserBucket(ctx context.Context, userID, connectionID, bucketID string) (BucketGrant, error)
	Update(ctx context.Context, id string, patch BucketGrantInput) (BucketGrant, error)
	Delete(ctx context.Context, id string) error
	ListForUser(ctx context.Context, userID string) ([]BucketGrant, error)
	ListForBucket(ctx context.Context, connectionID, bucketID string) ([]BucketGrant, error)
	Decrypt(g BucketGrant) (string, error)
}

// BucketGrantInput is the write-side shape: plaintext SecretKey lives
// here, encryption happens inside the store. Empty string fields on
// Update are treated as "leave alone" (partial update semantics
// matching connections.Update); explicit rotation supplies a new value.
type BucketGrantInput struct {
	UserID       string
	ConnectionID string
	BucketID     string
	AccessKeyID  string
	SecretKey    string // plaintext, encrypted on the way in
}

// ErrBucketGrantNotFound is returned by Get / GetByUserBucket / Update /
// Delete when the targeted grant does not exist. Callers can errors.Is
// to translate to a 404 at the API layer in later cycles.
var ErrBucketGrantNotFound = errors.New("bucket grant not found")

// ErrBucketGrantDuplicate is returned by Create when an existing grant
// already covers the same (userID, connectionID, bucketID) triple. The
// caller should Update or Delete-then-Create instead.
var ErrBucketGrantDuplicate = errors.New("bucket grant already exists for user/cluster/bucket")

// bucketGrantStore implements BucketGrants on top of a JSON file.
type bucketGrantStore struct {
	path      string
	jwtSecret []byte

	mu    sync.RWMutex
	cache []BucketGrant
}

// OpenBucketGrants opens or creates the credential-grants store at
// dataDir, encrypting secrets with a key derived from jwtSecret.
// jwtSecret must be non-empty; per cfg validation it's at least 32
// bytes in real deployments.
func OpenBucketGrants(dataDir string, jwtSecret []byte) (BucketGrants, error) {
	if len(jwtSecret) == 0 {
		return nil, errors.New("OpenBucketGrants: empty jwtSecret")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}

	s := &bucketGrantStore{
		path:      filepath.Join(dataDir, "bucket_grants.json"),
		jwtSecret: append([]byte(nil), jwtSecret...), // defensive copy
		cache:     make([]BucketGrant, 0),
	}

	if err := s.load(); err != nil {
		return nil, fmt.Errorf("loading bucket grants: %w", err)
	}

	return s, nil
}

func (s *bucketGrantStore) load() error {
	grants, err := loadJSON[[]BucketGrant](s.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if grants == nil {
		s.cache = make([]BucketGrant, 0)
	} else {
		s.cache = grants
	}
	return nil
}

func (s *bucketGrantStore) save() error {
	return saveJSON(s.path, s.cache)
}

// validateInput trims whitespace, enforces required fields, and returns
// the cleaned values. Doesn't touch the plaintext secret beyond
// length-zero check — encryption happens at the call site.
func validateBucketGrantInput(in BucketGrantInput, requireSecret bool) (BucketGrantInput, error) {
	in.UserID = strings.TrimSpace(in.UserID)
	in.ConnectionID = strings.TrimSpace(in.ConnectionID)
	in.BucketID = strings.TrimSpace(in.BucketID)
	in.AccessKeyID = strings.TrimSpace(in.AccessKeyID)

	if in.UserID == "" {
		return in, errors.New("userId is required")
	}
	if in.ConnectionID == "" {
		return in, errors.New("connectionId is required")
	}
	if in.BucketID == "" {
		return in, errors.New("bucketId is required")
	}
	if in.AccessKeyID == "" {
		return in, errors.New("accessKeyId is required")
	}
	if requireSecret && in.SecretKey == "" {
		return in, errors.New("secretKey is required")
	}
	return in, nil
}

func (s *bucketGrantStore) Create(_ context.Context, in BucketGrantInput) (BucketGrant, error) {
	in, err := validateBucketGrantInput(in, true)
	if err != nil {
		return BucketGrant{}, err
	}

	enc, err := encryptSecret([]byte(in.SecretKey), s.jwtSecret)
	if err != nil {
		return BucketGrant{}, fmt.Errorf("encrypting secret: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Enforce uniqueness on (userID, connectionID, bucketID).
	for _, g := range s.cache {
		if g.UserID == in.UserID && g.ConnectionID == in.ConnectionID && g.BucketID == in.BucketID {
			return BucketGrant{}, ErrBucketGrantDuplicate
		}
	}

	now := time.Now().UTC()
	g := BucketGrant{
		ID:           uuid.NewString(),
		UserID:       in.UserID,
		ConnectionID: in.ConnectionID,
		BucketID:     in.BucketID,
		AccessKeyID:  in.AccessKeyID,
		SecretKeyEnc: enc,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	s.cache = append(s.cache, g)

	if err := s.save(); err != nil {
		// Roll back the cache append so an in-memory failure doesn't
		// stay observable to subsequent reads.
		s.cache = s.cache[:len(s.cache)-1]
		return BucketGrant{}, fmt.Errorf("persisting bucket grant: %w", err)
	}
	return g, nil
}

func (s *bucketGrantStore) Get(_ context.Context, id string) (BucketGrant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, g := range s.cache {
		if g.ID == id {
			return g, nil
		}
	}
	return BucketGrant{}, ErrBucketGrantNotFound
}

func (s *bucketGrantStore) GetByUserBucket(_ context.Context, userID, connectionID, bucketID string) (BucketGrant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, g := range s.cache {
		if g.UserID == userID && g.ConnectionID == connectionID && g.BucketID == bucketID {
			return g, nil
		}
	}
	return BucketGrant{}, ErrBucketGrantNotFound
}

// Update applies a partial patch. Non-empty AccessKeyID replaces the
// existing one; non-empty SecretKey rotates the encrypted blob (a fresh
// nonce is used, see crypto.go). The triple (UserID, ConnectionID,
// BucketID) is immutable on update — to move a grant, Delete + Create.
func (s *bucketGrantStore) Update(_ context.Context, id string, patch BucketGrantInput) (BucketGrant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.cache {
		if s.cache[i].ID != id {
			continue
		}
		g := &s.cache[i]

		// Encrypt outside the cache mutation so a crypto failure leaves
		// the existing record untouched.
		var newEnc []byte
		if patch.SecretKey != "" {
			var err error
			newEnc, err = encryptSecret([]byte(patch.SecretKey), s.jwtSecret)
			if err != nil {
				return BucketGrant{}, fmt.Errorf("encrypting rotated secret: %w", err)
			}
		}

		if patch.AccessKeyID != "" {
			g.AccessKeyID = strings.TrimSpace(patch.AccessKeyID)
		}
		if newEnc != nil {
			g.SecretKeyEnc = newEnc
		}
		g.UpdatedAt = time.Now().UTC()

		if err := s.save(); err != nil {
			return BucketGrant{}, fmt.Errorf("persisting bucket grant update: %w", err)
		}
		return *g, nil
	}
	return BucketGrant{}, ErrBucketGrantNotFound
}

func (s *bucketGrantStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.cache {
		if s.cache[i].ID == id {
			s.cache = append(s.cache[:i], s.cache[i+1:]...)
			if err := s.save(); err != nil {
				return fmt.Errorf("persisting bucket grant delete: %w", err)
			}
			return nil
		}
	}
	return ErrBucketGrantNotFound
}

func (s *bucketGrantStore) ListForUser(_ context.Context, userID string) ([]BucketGrant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]BucketGrant, 0)
	for _, g := range s.cache {
		if g.UserID == userID {
			out = append(out, g)
		}
	}
	return out, nil
}

func (s *bucketGrantStore) ListForBucket(_ context.Context, connectionID, bucketID string) ([]BucketGrant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]BucketGrant, 0)
	for _, g := range s.cache {
		if g.ConnectionID == connectionID && g.BucketID == bucketID {
			out = append(out, g)
		}
	}
	return out, nil
}

// Decrypt unwraps the secret key for one grant. This is the ONLY path
// from ciphertext back to plaintext — keep it that way so audit greps
// are easy.
func (s *bucketGrantStore) Decrypt(g BucketGrant) (string, error) {
	return decryptSecret(g.SecretKeyEnc, s.jwtSecret)
}
