// Package clustersecret implements per-cluster envelope encryption
// (ADR-0007). Each cluster has a 256-bit Cluster Secret Key (CSK); the
// CSK is wrapped under one Argon2id-derived key per cluster admin and
// stored persistently. Plaintext CSK lives ONLY in process memory
// inside ClusterSecretManager.csks between unlock and lock or restart.
//
// Threat model and rationale: see docs/adr/0007-per-cluster-envelope-encryption.md.
//
// Storage layout (per cluster):
//
//	[]WrappedCSK   // one entry per admin user ID, persisted via ClusterSecretStore
//
// Each WrappedCSK carries the Argon2id salt + params plus the AES-GCM
// ciphertext of the CSK under the password-derived wrapping key. The
// CSK plaintext NEVER touches the WrappedCSK; only the wrapping key
	// (transiently held during unlock) can recover it.
	//
	// API summary:
	//
	//	Unlock(cid, password)     decode CSK into memory using one admin's password
	//	Lock(cid)                 zero CSK from memory
	//	IsUnlocked(cid)           cheap predicate for the request path
	//	Encrypt/Decrypt(cid, …)   AES-GCM under the in-memory CSK
	//	AddAdmin/RemoveAdmin      manage the set of wrappedCSK records
	//
	// Anything that reaches "I need a cluster's stored secret" calls
	// Decrypt(cid, ciphertext) and converts ErrLocked into HTTP 423 LOCKED
	// at the API edge so the FE can surface the unlock modal.
package clustersecret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/argon2"
)

// CSK and key sizes; AES-256 wants 32 bytes.
const (
	cskSize        = 32
	saltSize       = 16
	nonceSize      = 12
	wrappingKeyLen = 32
)

// Argon2id parameters. OWASP 2026 baseline; memory dominates the
// adversary's per-guess cost, threads = 4 keeps single-unlock latency
// under a second on a modern CPU.
const (
	Argon2Time    uint32 = 3
	Argon2Memory  uint32 = 64 * 1024 // 64 MiB
	Argon2Threads uint8  = 4
)

// Sentinel errors. Callers (especially the API layer) compare with
// errors.Is to decide HTTP mapping.
var (
	// ErrLocked is returned by Encrypt/Decrypt when the cluster's CSK
	// is not in memory. The API edge maps this to 423 LOCKED.
	ErrLocked = errors.New("clustersecret: cluster locked")

	// ErrInvalidPassword is returned by Unlock when AES-GCM auth-tag
	// verification fails on every wrappedCSK record we try. Indicates
	// either a wrong password or tampered storage; the caller cannot
	// distinguish (deliberately, to avoid an oracle).
	ErrInvalidPassword = errors.New("clustersecret: invalid password")

	// ErrNoWrappedCSK is returned by Unlock when no wrappedCSK record
	// exists for this cluster at all. Caller flips this into a
	// "cluster has no CSK admins yet" UI prompt.
	ErrNoWrappedCSK = errors.New("clustersecret: no wrapped CSK")

	// ErrUnknownAdmin is returned by Unlock when wrappedCSK records
	// exist but none belong to the supplied admin user ID. Maps to a
	// 401 INVALID_PASSWORD at the API edge — distinguishing "wrong
	// password" from "wrong admin" would leak the admin list.
	ErrUnknownAdmin = errors.New("clustersecret: unknown admin")

	// ErrAdminAlreadyExists is returned by AddAdmin when a wrappedCSK
	// already exists for that admin. Operators rotate by Remove then
	// Add — deliberate non-idempotency to keep the audit trail clear.
	ErrAdminAlreadyExists = errors.New("clustersecret: admin already exists")
)

// WrappedCSK is one (clusterID, adminUserID) record persisted to disk.
// All fields are required; JSON marshalling is the disk shape.
//
// Wrapped is AES-256-GCM(CSK, wrappingKey) with Nonce prepended:
// `Nonce(12) || ciphertext(32) || tag(16)`. wrappingKey is derived from
// (password, Salt, KDFParams) via Argon2id; never stored.
type WrappedCSK struct {
	ClusterID   string     `json:"clusterId"`
	AdminUserID string     `json:"adminUserId"`
	Wrapped     []byte     `json:"wrapped"` // nonce||ct||tag
	Salt        []byte     `json:"salt"`
	KDFParams   KDFParams  `json:"kdfParams"`
}

// KDFParams captures the Argon2id cost parameters used at wrap time so
// future re-unlocks reproduce the same derivation. Stored per
// WrappedCSK so we can raise costs in the future without invalidating
// existing wrapped records.
type KDFParams struct {
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
	KeyLen  uint32 `json:"keyLen"`
}

// DefaultKDFParams returns the cycle-locked Argon2id parameters; new
// wraps always use these. Old wraps keep whatever values were stamped
// at write time.
func DefaultKDFParams() KDFParams {
	return KDFParams{
		Time:    Argon2Time,
		Memory:  Argon2Memory,
		Threads: Argon2Threads,
		KeyLen:  wrappingKeyLen,
	}
}

// ClusterSecretStore is the persistence surface. Implementations are
// expected to be safe for concurrent reads + writes; the typical impl
// is internal/store backed by a JSON file under {dataDir}/cluster_secrets.json.
//
// Multi-admin records are stored as a flat list keyed implicitly by
// (ClusterID, AdminUserID). Get returns every record for a cluster;
// Put upserts on (cid, adminUserID); Delete removes a single record.
type ClusterSecretStore interface {
	GetWrappedCSKs(clusterID string) ([]WrappedCSK, error)
	PutWrappedCSK(rec WrappedCSK) error
	DeleteWrappedCSK(clusterID, adminUserID string) error
}

// ClusterSecretManager is the in-memory CSK cache + API. Construct
// with New(store); pass the same instance everywhere the running
// process touches per-cluster secrets.
//
// Concurrency: safe to call any method from any goroutine. The cache
// is guarded by an RWMutex so the request hot path (Decrypt) stays
// read-locked while only the rare Unlock/Lock/AddAdmin paths take the
// write lock.
type ClusterSecretManager struct {
	mu    sync.RWMutex
	csks  map[string][]byte // clusterID → plaintext CSK (in memory only)
	store ClusterSecretStore
}

// New constructs a manager wired to the supplied store. Nil store is
// allowed for tests but every store-touching method will return an
// error in that case; production wires the JSON-file store from
// internal/store.
func New(store ClusterSecretStore) *ClusterSecretManager {
	return &ClusterSecretManager{
		csks:  make(map[string][]byte),
		store: store,
	}
}

// Unlock decodes the CSK for clusterID using the supplied admin's
// password. On success, the CSK is cached in memory until Lock or
// process restart; subsequent IsUnlocked / Encrypt / Decrypt for this
// cluster succeed.
//
// The cluster's wrappedCSK list is walked and each record is tried in
// turn — typical multi-admin setups will succeed on the matching
// admin's record. We don't take the admin user ID as an argument
// because the FE doesn't necessarily know which admin record belongs
// to the logged-in user; trying-them-all is cheap (each Argon2id is
// ~100ms and there are typically 1-3 admins) and avoids leaking the
// admin set to the caller.
//
// Returns ErrInvalidPassword if no record decrypts successfully,
// ErrNoWrappedCSK if the cluster has no recorded admin at all.
func (m *ClusterSecretManager) Unlock(clusterID, password string) error {
	if m.store == nil {
		return errors.New("clustersecret: no store wired")
	}
	if clusterID == "" {
		return errors.New("clustersecret: clusterID required")
	}
	if password == "" {
		return ErrInvalidPassword
	}

	recs, err := m.store.GetWrappedCSKs(clusterID)
	if err != nil {
		return fmt.Errorf("clustersecret: load wrapped CSKs: %w", err)
	}
	if len(recs) == 0 {
		return ErrNoWrappedCSK
	}

	for _, rec := range recs {
		csk, ok := tryUnwrap(rec, password)
		if !ok {
			continue
		}
		m.mu.Lock()
		m.csks[clusterID] = csk
		m.mu.Unlock()
		return nil
	}
	return ErrInvalidPassword
}

// UnlockAs is like Unlock but only tries the record belonging to the
// supplied admin user ID. Useful for the API edge that does know the
// caller's identity; avoids spending Argon2id cost on records that
// can't match.
//
// Returns ErrUnknownAdmin if no record for that admin user exists,
// ErrInvalidPassword if the record exists but the password fails.
func (m *ClusterSecretManager) UnlockAs(clusterID, adminUserID, password string) error {
	if m.store == nil {
		return errors.New("clustersecret: no store wired")
	}
	if clusterID == "" || adminUserID == "" {
		return errors.New("clustersecret: clusterID and adminUserID required")
	}
	if password == "" {
		return ErrInvalidPassword
	}

	recs, err := m.store.GetWrappedCSKs(clusterID)
	if err != nil {
		return fmt.Errorf("clustersecret: load wrapped CSKs: %w", err)
	}
	if len(recs) == 0 {
		return ErrNoWrappedCSK
	}

	for _, rec := range recs {
		if rec.AdminUserID != adminUserID {
			continue
		}
		csk, ok := tryUnwrap(rec, password)
		if !ok {
			return ErrInvalidPassword
		}
		m.mu.Lock()
		m.csks[clusterID] = csk
		m.mu.Unlock()
		return nil
	}
	return ErrUnknownAdmin
}

// Lock zeroes the in-memory CSK for clusterID. Subsequent Encrypt /
// Decrypt return ErrLocked until the next Unlock. Idempotent — locking
// an already-locked cluster is a no-op.
//
// The zeroing pass overwrites the CSK bytes before deleting the map
// entry so a Go GC pass that left the bytes in old memory hasn't kept
// a recoverable copy. Defence-in-depth only; the Go runtime can still
// hold copies in moved slices and we accept that residual exposure.
func (m *ClusterSecretManager) Lock(clusterID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if csk, ok := m.csks[clusterID]; ok {
		for i := range csk {
			csk[i] = 0
		}
		delete(m.csks, clusterID)
	}
}

// LockAll zeros every cached CSK. Called on graceful shutdown so a
// crash dump after Stop can't leak credentials.
func (m *ClusterSecretManager) LockAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for cid, csk := range m.csks {
		for i := range csk {
			csk[i] = 0
		}
		delete(m.csks, cid)
	}
}

// IsUnlocked reports whether the CSK for clusterID is currently in
// memory. Used by the API edge to short-circuit unlock-modal
// rendering and by background jobs to skip locked clusters.
func (m *ClusterSecretManager) IsUnlocked(clusterID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.csks[clusterID]
	return ok
}

// HasAdmins reports whether any wrappedCSK record exists for the
// cluster. Used by the API edge to distinguish "cluster has never had
// CSK enabled" (offer setup flow) from "cluster has CSK but is locked"
// (offer unlock modal).
func (m *ClusterSecretManager) HasAdmins(clusterID string) (bool, error) {
	if m.store == nil {
		return false, errors.New("clustersecret: no store wired")
	}
	recs, err := m.store.GetWrappedCSKs(clusterID)
	if err != nil {
		return false, err
	}
	return len(recs) > 0, nil
}

// ListAdmins returns the admin user IDs that hold a wrappedCSK for
// this cluster. The order is the store's natural order. CSK contents
// are NEVER returned — only the metadata needed to render the
// /admin/clusters/{cid}/admins page.
func (m *ClusterSecretManager) ListAdmins(clusterID string) ([]string, error) {
	if m.store == nil {
		return nil, errors.New("clustersecret: no store wired")
	}
	recs, err := m.store.GetWrappedCSKs(clusterID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.AdminUserID)
	}
	return out, nil
}

// Encrypt AES-256-GCM encrypts plaintext under the cached CSK for
// clusterID. Returns ErrLocked if the cluster's CSK isn't in memory.
//
// Wire format: nonce(12) || ciphertext(N) || tag(16). Matches
// internal/store/crypto.go so existing decryption call sites can
// switch over with minimal churn.
func (m *ClusterSecretManager) Encrypt(clusterID string, plaintext []byte) ([]byte, error) {
	m.mu.RLock()
	csk, ok := m.csks[clusterID]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrLocked
	}
	return aesGCMSeal(csk, plaintext)
}

// Decrypt reverses Encrypt. Returns ErrLocked if the cluster's CSK
// isn't in memory; returns a non-sentinel error on tampered /
// truncated ciphertext (callers should treat as a 500, not a 423 —
// the user can't unlock their way out of corrupted storage).
func (m *ClusterSecretManager) Decrypt(clusterID string, ciphertext []byte) ([]byte, error) {
	m.mu.RLock()
	csk, ok := m.csks[clusterID]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrLocked
	}
	return aesGCMOpen(csk, ciphertext)
}

// AddAdmin wraps the in-memory CSK under a new admin's
// password-derived key and persists the resulting WrappedCSK. Requires
// the cluster to be already unlocked (i.e. another admin must currently
// be authenticated). Returns ErrLocked if not.
//
// For the very-first admin (no existing CSK), use BootstrapFirstAdmin
// instead — it generates a fresh CSK as well.
func (m *ClusterSecretManager) AddAdmin(clusterID, adminUserID, password string) error {
	if m.store == nil {
		return errors.New("clustersecret: no store wired")
	}
	if clusterID == "" || adminUserID == "" {
		return errors.New("clustersecret: clusterID and adminUserID required")
	}
	if password == "" {
		return errors.New("clustersecret: password required")
	}

	m.mu.RLock()
	csk, ok := m.csks[clusterID]
	m.mu.RUnlock()
	if !ok {
		return ErrLocked
	}

	// Reject duplicate-admin add so the audit log captures
	// rotate-by-remove-then-add explicitly rather than a silent overwrite.
	recs, err := m.store.GetWrappedCSKs(clusterID)
	if err != nil {
		return fmt.Errorf("clustersecret: load wrapped CSKs: %w", err)
	}
	for _, r := range recs {
		if r.AdminUserID == adminUserID {
			return ErrAdminAlreadyExists
		}
	}

	rec, err := wrap(csk, clusterID, adminUserID, password)
	if err != nil {
		return err
	}
	if err := m.store.PutWrappedCSK(rec); err != nil {
		return fmt.Errorf("clustersecret: persist wrapped CSK: %w", err)
	}
	return nil
}

// BootstrapFirstAdmin generates a brand-new CSK for the cluster, wraps
// it under the supplied admin's password, persists, and caches the CSK
// in memory (so the caller can immediately encrypt secrets).
//
// Requires the cluster to have zero existing wrappedCSK records — use
// AddAdmin for subsequent admins. Idempotency: a partial failure that
// successfully writes the WrappedCSK but never reaches the in-memory
// cache leaves the cluster in a "first admin bootstrapped, must
// Unlock with that same password" state, which is a clean recovery.
func (m *ClusterSecretManager) BootstrapFirstAdmin(clusterID, adminUserID, password string) error {
	if m.store == nil {
		return errors.New("clustersecret: no store wired")
	}
	if clusterID == "" || adminUserID == "" {
		return errors.New("clustersecret: clusterID and adminUserID required")
	}
	if password == "" {
		return errors.New("clustersecret: password required")
	}

	recs, err := m.store.GetWrappedCSKs(clusterID)
	if err != nil {
		return fmt.Errorf("clustersecret: load wrapped CSKs: %w", err)
	}
	if len(recs) > 0 {
		return ErrAdminAlreadyExists
	}

	csk := make([]byte, cskSize)
	if _, err := io.ReadFull(rand.Reader, csk); err != nil {
		return fmt.Errorf("clustersecret: generate CSK: %w", err)
	}

	rec, err := wrap(csk, clusterID, adminUserID, password)
	if err != nil {
		// Best-effort zero before returning.
		for i := range csk {
			csk[i] = 0
		}
		return err
	}
	if err := m.store.PutWrappedCSK(rec); err != nil {
		for i := range csk {
			csk[i] = 0
		}
		return fmt.Errorf("clustersecret: persist wrapped CSK: %w", err)
	}

	m.mu.Lock()
	m.csks[clusterID] = csk
	m.mu.Unlock()
	return nil
}

// RemoveAdmin deletes one admin's wrappedCSK record. Other admins are
// unaffected — they still hold their own wraps and can unlock to the
// same CSK. The cluster's stored secrets are NOT re-encrypted (the
// CSK is unchanged); only the removed admin's path to it is severed.
//
// Removing the LAST admin while the cluster is unlocked is allowed —
// the in-memory CSK still works for the rest of the process lifetime,
// but a restart leaves the cluster with no path back. Callers
// (admin handler) should warn the operator before going through with
// last-admin removal.
func (m *ClusterSecretManager) RemoveAdmin(clusterID, adminUserID string) error {
	if m.store == nil {
		return errors.New("clustersecret: no store wired")
	}
	if clusterID == "" || adminUserID == "" {
		return errors.New("clustersecret: clusterID and adminUserID required")
	}
	return m.store.DeleteWrappedCSK(clusterID, adminUserID)
}

// MigrateFromJWT and MigrateFromJWTMap were removed in v2.0.0-beta.2.
// Legacy JWT-encrypted credentials are no longer supported; clusters with
// ConfigEnc but no ConfigEncCSK are dropped on first boot per [[v2_clean_break]].

// ─── internals ──────────────────────────────────────────────────────

// wrap derives the wrapping key from the password and the freshly
// allocated salt + default KDF params, AES-GCM-seals the CSK, and
// returns the resulting WrappedCSK record ready to persist.
func wrap(csk []byte, clusterID, adminUserID, password string) (WrappedCSK, error) {
	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return WrappedCSK{}, fmt.Errorf("clustersecret: salt: %w", err)
	}
	params := DefaultKDFParams()
	wk := argon2.IDKey([]byte(password), salt, params.Time, params.Memory, params.Threads, params.KeyLen)
	defer func() {
		// Wipe wrapping key as soon as the seal is done; it's
		// recoverable from password + salt + params, never needs to
		// persist beyond this function.
		for i := range wk {
			wk[i] = 0
		}
	}()
	sealed, err := aesGCMSealRaw(wk, csk)
	if err != nil {
		return WrappedCSK{}, fmt.Errorf("clustersecret: seal CSK: %w", err)
	}
	return WrappedCSK{
		ClusterID:   clusterID,
		AdminUserID: adminUserID,
		Wrapped:     sealed,
		Salt:        salt,
		KDFParams:   params,
	}, nil
}

// tryUnwrap attempts to recover the CSK from one WrappedCSK record
// using the supplied password. Returns (csk, true) on success,
// (nil, false) on AEAD tag mismatch or any other unwrap error. Never
// surfaces detail — the caller can't distinguish "wrong password"
// from "tampered ciphertext" (deliberate, to avoid an oracle).
func tryUnwrap(rec WrappedCSK, password string) ([]byte, bool) {
	if len(rec.Wrapped) < nonceSize+16 || len(rec.Salt) == 0 {
		return nil, false
	}
	params := rec.KDFParams
	if params.KeyLen == 0 {
		// Default KDFParams.KeyLen so very old records (which never
		// existed in production but might in tests) don't blow up.
		params.KeyLen = wrappingKeyLen
	}
	wk := argon2.IDKey([]byte(password), rec.Salt, params.Time, params.Memory, params.Threads, params.KeyLen)
	defer func() {
		for i := range wk {
			wk[i] = 0
		}
	}()
	csk, err := aesGCMOpenRaw(wk, rec.Wrapped)
	if err != nil {
		return nil, false
	}
	if len(csk) != cskSize {
		// Wrong length means we're decoding the wrong blob (storage
		// drift, corruption); refuse.
		for i := range csk {
			csk[i] = 0
		}
		return nil, false
	}
	return csk, true
}

// aesGCMSeal seals plaintext under the cached CSK. Caller MUST hold
// the read lock OR own the csk via a freshly-decoded local copy.
func aesGCMSeal(csk, plaintext []byte) ([]byte, error) {
	if len(csk) != cskSize {
		return nil, fmt.Errorf("clustersecret: invalid CSK size %d (want %d)", len(csk), cskSize)
	}
	return aesGCMSealRaw(csk, plaintext)
}

// aesGCMSealRaw seals plaintext under the supplied 32-byte key. Wire
// format: nonce(12) || ciphertext || tag(16).
func aesGCMSealRaw(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// aesGCMOpen opens ciphertext that was sealed by aesGCMSeal.
func aesGCMOpen(csk, ciphertext []byte) ([]byte, error) {
	if len(csk) != cskSize {
		return nil, fmt.Errorf("clustersecret: invalid CSK size %d (want %d)", len(csk), cskSize)
	}
	return aesGCMOpenRaw(csk, ciphertext)
}

func aesGCMOpenRaw(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("clustersecret: ciphertext too short")
	}
	nonce := ciphertext[:gcm.NonceSize()]
	ct := ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

// jwtAESGCMOpen reverses internal/store/crypto.go's encryptSecret —
// the wire format is identical, the key derivation is
// sha256(jwtSecret) which is what store/crypto.go uses.
//
// Lives here (instead of importing internal/store) to avoid a package
// import cycle: store needs to know about clustersecret to call this,
// but clustersecret can't import store. Duplicating ~30 lines of
// well-tested AES-GCM glue is the lesser evil.
func jwtAESGCMOpen(jwtSecret, ciphertext []byte) ([]byte, error) {
	if len(jwtSecret) == 0 {
		return nil, errors.New("clustersecret: empty jwtSecret")
	}
	// sha256 of the secret; matches store.deriveKey shape.
	derived := sha256Sum(jwtSecret)
	return aesGCMOpenRaw(derived[:], ciphertext)
}

// sha256Sum is the same derivation as crypto/sha256.Sum256 but kept
// inline so the imports stay tidy.
func sha256Sum(b []byte) [32]byte {
	return sha256.Sum256(b)
}

// EqualBytes is a constant-time equality check; exported so tests can
// assert "wrappedCSK changed" without flake-prone slice comparison.
// Not used by the manager itself; provided as a convenience for the
// API layer and tests.
func EqualBytes(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}
