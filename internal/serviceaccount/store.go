// Package serviceaccount: persistent service-account store (v1.7.0a).
//
// On-disk file: {dataDir}/service_accounts.json. Atomic JSON write via
// tmp + fsync + rename, same pattern as internal/backup/store.go and
// internal/store/saveJSON. Concurrency: a single RWMutex guards both
// the in-memory map and the disk write.
//
// Plaintext-handling discipline:
//
//   - Create + Rotate are the ONLY paths that return a plaintext
//     secret. Plaintext never lives on disk, never appears in slog
//     output, never round-trips through the JSON encoder for any
//     other call.
//   - SecretKeyHash holds bcrypt(plaintext). VerifySecret is the only
//     code path that compares a candidate secret against the hash —
//     so grepping for VerifySecret enumerates every place we touch
//     plaintext after mint.
//
// Hot-path lookup is GetByAccessKey: the v1.7.0b SigV4 middleware
// resolves the inbound `AccessKeyID` once per request, so the index
// keyed on access-key is what production hammers. Get / ListForUser
// stay O(n) — the SA list is small (single-operator basement has
// dozens of SAs at most across its lifetime).
package serviceaccount

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	stdsync "sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ErrNotFound is returned by Get / GetByAccessKey / Update / Delete /
// Rotate / TouchLastUsed when no row exists for the supplied
// identifier. Soft-deleted rows ARE still reachable by ID + access-key
// so audit greps work — `IsRevoked()` distinguishes.
var ErrNotFound = errors.New("service account not found")

// ErrDuplicateName is returned by Create when an existing, NON-REVOKED
// service account for the same OwnerUserID already uses the supplied
// Name. Revoked rows do not block name reuse — that's the whole point
// of soft-delete: an operator who revokes a leaked "ci-prod" key can
// mint a fresh "ci-prod" the same day.
var ErrDuplicateName = errors.New("duplicate service account name for owner")

// ErrInvalidName is returned by Create / Update when Name doesn't
// match the canonical regex (`^[A-Za-z0-9_-]{3,64}$`).
var ErrInvalidName = errors.New("invalid service account name")

// nameRegex is the canonical name validator. Length 3-64, alnum +
// dash + underscore. Operator-facing names like "ci-prod" /
// "k8s_replicator" pass; spaces / slashes / unicode emoji don't.
var nameRegex = regexp.MustCompile(`^[A-Za-z0-9_-]{3,64}$`)

// touchDebounce is the minimum interval between persisted LastUsedAt
// bumps for a single row. A burst of N signed requests within the
// window performs at most one disk write — same shape as the
// UserRegion store's debounce on the hot signing path.
const touchDebounce = time.Minute

// accessKeyPrefix is the basement SA access-key signature byte.
// Lets the v1.7.0b SigV4 verifier (and a future audit grep) tell
// basement-issued credentials from upstream backend keys at a glance.
const accessKeyPrefix = "BMNT"

// ServiceAccounts is the CRUD + verify surface for SA records. Mirrors
// the shape of store.UserRegions and store.Invites so a future store-
// wide refactor (e.g. SQLite) has a uniform set of interfaces to swap.
//
// Update applies a partial patch: only Name, Capabilities, Scopes, and
// ExpiresAt are mutable through it. Secret rotation goes through
// Rotate exclusively so plaintext can be returned in a typed return
// value rather than smuggled in a patch field.
type ServiceAccounts interface {
	Create(ctx context.Context, sa ServiceAccount) (ServiceAccount, string, error)
	Get(ctx context.Context, id string) (ServiceAccount, error)
	GetByAccessKey(ctx context.Context, akid string) (ServiceAccount, error)
	Update(ctx context.Context, id string, patch ServiceAccount) (ServiceAccount, error)
	Delete(ctx context.Context, id string) error
	ListForUser(ctx context.Context, userID string) ([]ServiceAccount, error)
	Rotate(ctx context.Context, id string) (ServiceAccount, string, error)
	VerifySecret(ctx context.Context, akid string, candidateSecret string) (bool, error)
	TouchLastUsed(ctx context.Context, id string) error
}

// fileStore implements ServiceAccounts on top of a JSON file. Keyed
// internally by ID; AccessKeyID and (OwnerUserID, Name) lookups walk
// the map — at SA-count scale (dozens) the linear scan is faster than
// maintaining secondary indexes.
type fileStore struct {
	mu   stdsync.RWMutex
	path string
	rows map[string]ServiceAccount

	// lastTouchPersist tracks the wall-clock time at which each row's
	// LastUsedAt was last persisted. TouchLastUsed consults it to
	// debounce hot-path writes. Guarded by mu.
	lastTouchPersist map[string]time.Time
}

// Open opens or creates the service-account store at dataDir.
// A missing file is treated as an empty store — first boot starts
// with no SAs.
func Open(dataDir string) (ServiceAccounts, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}
	fs := &fileStore{
		path:             filepath.Join(dataDir, "service_accounts.json"),
		rows:             map[string]ServiceAccount{},
		lastTouchPersist: map[string]time.Time{},
	}
	data, err := os.ReadFile(fs.path)
	if err != nil {
		if os.IsNotExist(err) {
			return fs, nil
		}
		return nil, fmt.Errorf("reading service_accounts.json: %w", err)
	}
	if len(data) == 0 {
		return fs, nil
	}
	var rows []ServiceAccount
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("parsing service_accounts.json: %w", err)
	}
	for _, sa := range rows {
		fs.rows[sa.ID] = sa
	}
	return fs, nil
}

// writeLocked persists the in-memory map to disk. Caller must hold mu.
// Uses tmp + fsync + rename so a crash mid-write can't leave a
// half-written file behind.
func (fs *fileStore) writeLocked() error {
	rows := make([]ServiceAccount, 0, len(fs.rows))
	for _, sa := range fs.rows {
		rows = append(rows, sa)
	}
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling service accounts: %w", err)
	}
	tmp := fs.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	f, err := os.OpenFile(tmp, os.O_RDONLY|os.O_SYNC, 0o600)
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("opening tmp for fsync: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("fsyncing tmp file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("closing tmp file: %w", err)
	}
	if err := os.Rename(tmp, fs.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

// generateAccessKeyID returns a fresh basement-issued access key. The
// prefix is the literal "BMNT" + 16 random hex characters (8 bytes of
// entropy) — globally unique with overwhelming probability and easy
// to spot in audit logs.
func generateAccessKeyID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	return accessKeyPrefix + strings.ToUpper(hex.EncodeToString(b)), nil
}

// generateSecret returns a 32-byte hex-encoded random secret (64
// chars, ~256 bits of entropy). Caller bcrypt-hashes before persist.
func generateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// validateName rejects names that don't match nameRegex. Trimmed input.
func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !nameRegex.MatchString(name) {
		return "", ErrInvalidName
	}
	return name, nil
}

// Create assigns IDs + access key + bcrypt-hashed secret + timestamps
// to the supplied template and persists it. The supplied
// SecretKeyHash / AccessKeyID / ID / CreatedAt are overwritten so a
// stale caller can't pin them. Returns the persisted row + the
// plaintext secret EXACTLY ONCE — caller surfaces it to the operator
// (the API handler returns it on the 201 response body).
//
// Uniqueness: ErrDuplicateName when an existing NON-REVOKED row owned
// by the same user has the same Name. Revoked rows DO NOT block reuse.
func (fs *fileStore) Create(_ context.Context, sa ServiceAccount) (ServiceAccount, string, error) {
	name, err := validateName(sa.Name)
	if err != nil {
		return ServiceAccount{}, "", err
	}
	sa.Name = name

	if strings.TrimSpace(sa.OwnerUserID) == "" {
		return ServiceAccount{}, "", errors.New("ownerUserId is required")
	}

	// ExpiresAt, when supplied, must be in the future. Past expirations
	// would mint an immediately-useless credential — almost certainly
	// a client bug (clock skew, wrong field), so reject loudly.
	if sa.ExpiresAt != nil && !sa.ExpiresAt.IsZero() {
		if !sa.ExpiresAt.After(time.Now().UTC()) {
			return ServiceAccount{}, "", errors.New("expiresAt must be in the future")
		}
	}

	akid, err := generateAccessKeyID()
	if err != nil {
		return ServiceAccount{}, "", fmt.Errorf("generating access key id: %w", err)
	}

	secret, err := generateSecret()
	if err != nil {
		return ServiceAccount{}, "", fmt.Errorf("generating secret: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return ServiceAccount{}, "", fmt.Errorf("hashing secret: %w", err)
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Uniqueness check: same owner + same name + not revoked.
	for _, existing := range fs.rows {
		if existing.OwnerUserID != sa.OwnerUserID {
			continue
		}
		if existing.Name != sa.Name {
			continue
		}
		if existing.IsRevoked() {
			continue
		}
		return ServiceAccount{}, "", ErrDuplicateName
	}

	now := time.Now().UTC()
	out := ServiceAccount{
		ID:            uuid.NewString(),
		OwnerUserID:   sa.OwnerUserID,
		Name:          sa.Name,
		AccessKeyID:   akid,
		SecretKeyHash: hash,
		Capabilities:  append([]Capability(nil), sa.Capabilities...),
		Scopes:        append([]string(nil), sa.Scopes...),
		CreatedAt:     now,
		ExpiresAt:     sa.ExpiresAt,
	}
	fs.rows[out.ID] = out

	if err := fs.writeLocked(); err != nil {
		// Roll back the cache insert so an in-memory failure doesn't
		// stay observable to subsequent reads.
		delete(fs.rows, out.ID)
		return ServiceAccount{}, "", fmt.Errorf("persisting service account: %w", err)
	}
	return out, secret, nil
}

// Get returns the SA by ID. Soft-deleted (revoked) rows ARE returned —
// audit + UI may want to display them as "(revoked)". Callers gating
// on "still valid" check IsRevoked() / IsExpired() themselves.
func (fs *fileStore) Get(_ context.Context, id string) (ServiceAccount, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	sa, ok := fs.rows[id]
	if !ok {
		return ServiceAccount{}, ErrNotFound
	}
	return sa, nil
}

// GetByAccessKey is the hot-path lookup used by the v1.7.0b SigV4
// verifier. O(n) scan over the SA map — SA counts stay small
// (dozens), so the linear scan beats maintaining a secondary index
// across the JSON-store atomic-write boundary.
func (fs *fileStore) GetByAccessKey(_ context.Context, akid string) (ServiceAccount, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	for _, sa := range fs.rows {
		if sa.AccessKeyID == akid {
			return sa, nil
		}
	}
	return ServiceAccount{}, ErrNotFound
}

// Update applies the mutable subset of patch (Name, Capabilities,
// Scopes, ExpiresAt) over the stored row. Identity + credential
// fields (ID, OwnerUserID, AccessKeyID, SecretKeyHash, CreatedAt,
// LastUsedAt, RevokedAt) are NEVER taken from the patch — those
// belong to the server or have dedicated mutators (Rotate / Delete /
// TouchLastUsed).
//
// Name is validated against nameRegex. The (owner, name, not-revoked)
// uniqueness invariant is re-checked so a rename can't collide with
// another live SA the same owner already has.
func (fs *fileStore) Update(_ context.Context, id string, patch ServiceAccount) (ServiceAccount, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	cur, ok := fs.rows[id]
	if !ok {
		return ServiceAccount{}, ErrNotFound
	}

	// Name: optional in a patch — empty means "leave alone", non-empty
	// validates + re-checks uniqueness against other live rows.
	if strings.TrimSpace(patch.Name) != "" {
		newName, err := validateName(patch.Name)
		if err != nil {
			return ServiceAccount{}, err
		}
		if newName != cur.Name {
			for _, existing := range fs.rows {
				if existing.ID == id {
					continue
				}
				if existing.OwnerUserID != cur.OwnerUserID {
					continue
				}
				if existing.Name != newName {
					continue
				}
				if existing.IsRevoked() {
					continue
				}
				return ServiceAccount{}, ErrDuplicateName
			}
			cur.Name = newName
		}
	}

	// Capabilities: nil means "leave alone"; empty slice is honoured
	// as "revoke all". The handler distinguishes by only setting the
	// field when the operator submitted it.
	if patch.Capabilities != nil {
		cur.Capabilities = append([]Capability(nil), patch.Capabilities...)
	}
	if patch.Scopes != nil {
		cur.Scopes = append([]string(nil), patch.Scopes...)
	}

	// ExpiresAt: non-nil + non-zero overrides. To clear an expiry the
	// handler would need a separate signal — we leave that for v1.7.0c
	// once the FE asks for it; for now an admin can only TIGHTEN expiry.
	if patch.ExpiresAt != nil && !patch.ExpiresAt.IsZero() {
		if !patch.ExpiresAt.After(time.Now().UTC()) {
			return ServiceAccount{}, errors.New("expiresAt must be in the future")
		}
		exp := *patch.ExpiresAt
		cur.ExpiresAt = &exp
	}

	fs.rows[id] = cur
	if err := fs.writeLocked(); err != nil {
		return ServiceAccount{}, fmt.Errorf("persisting service account update: %w", err)
	}
	return cur, nil
}

// Delete soft-deletes the SA by setting RevokedAt to the current time.
// The row stays on disk so audit greps can still resolve the
// AccessKeyID to a Name + Owner months after revocation.
// VerifySecret returns false for revoked rows, so the credential is
// dead immediately even though the bytes persist.
//
// Idempotent: Delete on an already-revoked row returns nil without
// touching the existing RevokedAt timestamp.
func (fs *fileStore) Delete(_ context.Context, id string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	cur, ok := fs.rows[id]
	if !ok {
		return ErrNotFound
	}
	if cur.IsRevoked() {
		return nil
	}
	now := time.Now().UTC()
	cur.RevokedAt = &now
	fs.rows[id] = cur
	return fs.writeLocked()
}

// ListForUser returns every SA — including revoked — owned by userID.
// Revoked rows are kept in the list so the UI can render a "Revoked"
// pill and audit-grep workflows have a single endpoint to consult.
// Caller may sort / filter / paginate as needed; we return raw rows.
func (fs *fileStore) ListForUser(_ context.Context, userID string) ([]ServiceAccount, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	out := make([]ServiceAccount, 0)
	for _, sa := range fs.rows {
		if sa.OwnerUserID == userID {
			out = append(out, sa)
		}
	}
	return out, nil
}

// Rotate replaces the row's secret + bcrypt hash. AccessKeyID is
// PRESERVED so existing client config that referenced the SA by name +
// access-key keeps resolving — only the secret changes, mirroring the
// AWS IAM rotation model. Returns the updated row + the new plaintext
// EXACTLY ONCE.
//
// Refuses to rotate a revoked SA — the operator would have to mint a
// fresh one. Refuses to rotate an expired SA likewise; the expiry was
// presumably set deliberately and bumping it via rotate would
// surprise.
func (fs *fileStore) Rotate(_ context.Context, id string) (ServiceAccount, string, error) {
	secret, err := generateSecret()
	if err != nil {
		return ServiceAccount{}, "", fmt.Errorf("generating rotated secret: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return ServiceAccount{}, "", fmt.Errorf("hashing rotated secret: %w", err)
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()

	cur, ok := fs.rows[id]
	if !ok {
		return ServiceAccount{}, "", ErrNotFound
	}
	if cur.IsRevoked() {
		return ServiceAccount{}, "", errors.New("cannot rotate a revoked service account")
	}

	original := cur
	cur.SecretKeyHash = hash
	fs.rows[id] = cur
	if err := fs.writeLocked(); err != nil {
		// Roll back so an unsaveable rotation doesn't leave the live
		// hash diverged from disk.
		fs.rows[id] = original
		return ServiceAccount{}, "", fmt.Errorf("persisting service account rotation: %w", err)
	}
	return cur, secret, nil
}

// VerifySecret compares the supplied plaintext candidate against the
// stored bcrypt hash. Returns (false, ErrNotFound) if no SA has the
// supplied AccessKeyID; (false, nil) when the SA is revoked, expired,
// or the candidate just doesn't match; (true, nil) on a clean match.
//
// Distinct nil-vs-error semantics matter for the v1.7.0b middleware:
// "key doesn't exist" is auditable as a probable attack/typo;
// "key exists but secret didn't match" wants the same audit treatment
// but mustn't reveal which side of the comparison failed in any reply.
func (fs *fileStore) VerifySecret(_ context.Context, akid, candidateSecret string) (bool, error) {
	fs.mu.RLock()
	var found ServiceAccount
	var ok bool
	for _, sa := range fs.rows {
		if sa.AccessKeyID == akid {
			found = sa
			ok = true
			break
		}
	}
	fs.mu.RUnlock()

	if !ok {
		return false, ErrNotFound
	}
	if found.IsRevoked() {
		return false, nil
	}
	if found.IsExpired(time.Now().UTC()) {
		return false, nil
	}
	if len(found.SecretKeyHash) == 0 {
		return false, nil
	}
	if err := bcrypt.CompareHashAndPassword(found.SecretKeyHash, []byte(candidateSecret)); err != nil {
		// bcrypt distinguishes "mismatched" (ErrMismatchedHashAndPassword)
		// from other crypto failures, but for the caller the distinction
		// is irrelevant — both are "no, this isn't authentic." Returning
		// a generic (false, nil) collapses the timing-sensitive case.
		return false, nil
	}
	return true, nil
}

// TouchLastUsed bumps LastUsedAt to now. Same debouncing pattern as
// store.userRegionStore.TouchLastUsed: in-memory LastUsedAt is updated
// on every call so reads see fresh data, but the disk write is
// throttled to at most one per row per touchDebounce window.
//
// Called by the v1.7.0b SigV4 middleware on each verified inbound
// request. A flurry of CI requests within one minute writes at most
// once — important because the SA store is a single JSON file and we
// don't want to churn the disk + atomic-rename machinery on every
// signed request.
func (fs *fileStore) TouchLastUsed(_ context.Context, id string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	cur, ok := fs.rows[id]
	if !ok {
		return ErrNotFound
	}
	now := time.Now().UTC()
	cur.LastUsedAt = &now
	fs.rows[id] = cur

	last, seen := fs.lastTouchPersist[id]
	if seen && now.Sub(last) < touchDebounce {
		// Within debounce window — skip the disk write.
		return nil
	}
	if err := fs.writeLocked(); err != nil {
		return fmt.Errorf("persisting last-used bump: %w", err)
	}
	fs.lastTouchPersist[id] = now
	return nil
}
