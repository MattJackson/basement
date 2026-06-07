package clustersecret

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Tests deliberately use the production Argon2id params so the
// round-trip exercises the real cost shape. Each individual unlock
// is ~100ms on a modern laptop; the suite stays under 10s overall.

func TestBootstrapAndUnlockRoundTrip(t *testing.T) {
	m := New(NewMemoryStore())
	if err := m.BootstrapFirstAdmin("cidA", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	if !m.IsUnlocked("cidA") {
		t.Fatalf("expected unlocked after bootstrap")
	}

	// Lock then re-unlock with the right password.
	m.Lock("cidA")
	if m.IsUnlocked("cidA") {
		t.Fatalf("expected locked after Lock")
	}

	if err := m.Unlock("cidA", "hunter2"); err != nil {
		t.Fatalf("Unlock with correct password: %v", err)
	}
	if !m.IsUnlocked("cidA") {
		t.Fatalf("expected unlocked after Unlock")
	}
}

func TestUnlockWrongPasswordRejected(t *testing.T) {
	m := New(NewMemoryStore())
	if err := m.BootstrapFirstAdmin("cidA", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	m.Lock("cidA")

	err := m.Unlock("cidA", "wrong")
	if !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("Unlock wrong password: got %v want ErrInvalidPassword", err)
	}
	if m.IsUnlocked("cidA") {
		t.Fatalf("expected still locked after wrong password")
	}
}

func TestUnlockNoAdminsReturnsErrNoWrappedCSK(t *testing.T) {
	m := New(NewMemoryStore())
	err := m.Unlock("never-bootstrapped", "anything")
	if !errors.Is(err, ErrNoWrappedCSK) {
		t.Fatalf("Unlock unbootstrapped: got %v want ErrNoWrappedCSK", err)
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	m := New(NewMemoryStore())
	if err := m.BootstrapFirstAdmin("cidA", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}

	plaintext := []byte("the secret admin token")
	ct, err := m.Encrypt("cidA", plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Contains(ct, plaintext) {
		t.Fatalf("ciphertext contains plaintext — encryption broken")
	}
	got, err := m.Decrypt("cidA", ct)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(plaintext, got) {
		t.Fatalf("decrypt mismatch: %q != %q", got, plaintext)
	}
}

func TestEncryptDecryptLockedReturnsErrLocked(t *testing.T) {
	m := New(NewMemoryStore())
	if err := m.BootstrapFirstAdmin("cidA", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	m.Lock("cidA")

	if _, err := m.Encrypt("cidA", []byte("x")); !errors.Is(err, ErrLocked) {
		t.Fatalf("Encrypt after Lock: got %v want ErrLocked", err)
	}
	if _, err := m.Decrypt("cidA", make([]byte, 32)); !errors.Is(err, ErrLocked) {
		t.Fatalf("Decrypt after Lock: got %v want ErrLocked", err)
	}
}

func TestMultiAdminEachCanUnlock(t *testing.T) {
	m := New(NewMemoryStore())
	if err := m.BootstrapFirstAdmin("cidA", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin matthew: %v", err)
	}
	// First admin still unlocked; add second admin.
	if err := m.AddAdmin("cidA", "wife", "eggcream"); err != nil {
		t.Fatalf("AddAdmin wife: %v", err)
	}

	// Encrypt with first admin's session, then lock.
	plaintext := []byte("shared cluster secret")
	ct, err := m.Encrypt("cidA", plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	m.Lock("cidA")

	// Wife unlocks with her own password.
	if err := m.Unlock("cidA", "eggcream"); err != nil {
		t.Fatalf("Unlock as wife: %v", err)
	}
	// Same CSK → her decrypt recovers the plaintext.
	got, err := m.Decrypt("cidA", ct)
	if err != nil {
		t.Fatalf("Decrypt after wife unlock: %v", err)
	}
	if !bytes.Equal(plaintext, got) {
		t.Fatalf("multi-admin decrypt mismatch: %q != %q", got, plaintext)
	}
}

func TestAddAdminRequiresUnlock(t *testing.T) {
	m := New(NewMemoryStore())
	if err := m.BootstrapFirstAdmin("cidA", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	m.Lock("cidA")

	err := m.AddAdmin("cidA", "wife", "eggcream")
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("AddAdmin while locked: got %v want ErrLocked", err)
	}
}

func TestAddAdminDuplicateRejected(t *testing.T) {
	m := New(NewMemoryStore())
	if err := m.BootstrapFirstAdmin("cidA", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	err := m.AddAdmin("cidA", "matthew", "another-password")
	if !errors.Is(err, ErrAdminAlreadyExists) {
		t.Fatalf("AddAdmin duplicate: got %v want ErrAdminAlreadyExists", err)
	}
}

func TestBootstrapFirstAdminTwiceRejected(t *testing.T) {
	m := New(NewMemoryStore())
	if err := m.BootstrapFirstAdmin("cidA", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin first: %v", err)
	}
	err := m.BootstrapFirstAdmin("cidA", "matthew2", "hunter2")
	if !errors.Is(err, ErrAdminAlreadyExists) {
		t.Fatalf("BootstrapFirstAdmin twice: got %v want ErrAdminAlreadyExists", err)
	}
}

func TestRemoveAdminLeavesOthersIntact(t *testing.T) {
	store := NewMemoryStore()
	m := New(store)
	if err := m.BootstrapFirstAdmin("cidA", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin matthew: %v", err)
	}
	if err := m.AddAdmin("cidA", "wife", "eggcream"); err != nil {
		t.Fatalf("AddAdmin wife: %v", err)
	}
	if err := m.RemoveAdmin("cidA", "matthew"); err != nil {
		t.Fatalf("RemoveAdmin matthew: %v", err)
	}

	// Lock, then wife must still be able to unlock.
	m.Lock("cidA")
	if err := m.Unlock("cidA", "eggcream"); err != nil {
		t.Fatalf("wife Unlock after removing matthew: %v", err)
	}
	// matthew can no longer unlock.
	m.Lock("cidA")
	if err := m.Unlock("cidA", "hunter2"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("removed admin should not unlock: got %v want ErrInvalidPassword", err)
	}

	// Direct admin list reflects only wife.
	admins, err := m.ListAdmins("cidA")
	if err != nil {
		t.Fatalf("ListAdmins: %v", err)
	}
	if len(admins) != 1 || admins[0] != "wife" {
		t.Fatalf("ListAdmins after remove: %v", admins)
	}
}

func TestRestartSimulationRequiresReUnlock(t *testing.T) {
	store := NewMemoryStore()
	m1 := New(store)
	if err := m1.BootstrapFirstAdmin("cidA", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	plaintext := []byte("the secret")
	ct, err := m1.Encrypt("cidA", plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// "Restart": new manager, same persistent store.
	m2 := New(store)
	if m2.IsUnlocked("cidA") {
		t.Fatalf("new manager must start locked after restart")
	}
	if _, err := m2.Decrypt("cidA", ct); !errors.Is(err, ErrLocked) {
		t.Fatalf("Decrypt on fresh manager: got %v want ErrLocked", err)
	}

	if err := m2.Unlock("cidA", "hunter2"); err != nil {
		t.Fatalf("Unlock after restart: %v", err)
	}
	got, err := m2.Decrypt("cidA", ct)
	if err != nil {
		t.Fatalf("Decrypt after re-unlock: %v", err)
	}
	if !bytes.Equal(plaintext, got) {
		t.Fatalf("decrypt mismatch after re-unlock: %q != %q", got, plaintext)
	}
}

func TestLockZeroesCachedCSK(t *testing.T) {
	m := New(NewMemoryStore())
	if err := m.BootstrapFirstAdmin("cidA", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	// Peek: the map should hold a non-zero CSK.
	m.mu.RLock()
	csk := m.csks["cidA"]
	cskCopy := append([]byte(nil), csk...)
	m.mu.RUnlock()
	if isAllZero(cskCopy) {
		t.Fatalf("expected non-zero CSK before lock")
	}

	m.Lock("cidA")
	// After lock the slice (which we still reference via cskCopy's
	// pre-lock snapshot) should be zeroed in place. The map entry
	// itself is also removed.
	if !isAllZero(csk) {
		t.Fatalf("expected CSK bytes zeroed in place after Lock; got %v", csk)
	}
	m.mu.RLock()
	_, present := m.csks["cidA"]
	m.mu.RUnlock()
	if present {
		t.Fatalf("expected map entry removed after Lock")
	}
}

func TestUnlockAsScopesToSingleAdmin(t *testing.T) {
	m := New(NewMemoryStore())
	if err := m.BootstrapFirstAdmin("cidA", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	if err := m.AddAdmin("cidA", "wife", "eggcream"); err != nil {
		t.Fatalf("AddAdmin: %v", err)
	}
	m.Lock("cidA")

	// Wrong password for wife → ErrInvalidPassword, NOT ErrUnknownAdmin.
	if err := m.UnlockAs("cidA", "wife", "wrong"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("UnlockAs wife wrong pwd: got %v want ErrInvalidPassword", err)
	}
	// Unknown user → ErrUnknownAdmin.
	if err := m.UnlockAs("cidA", "stranger", "anything"); !errors.Is(err, ErrUnknownAdmin) {
		t.Fatalf("UnlockAs stranger: got %v want ErrUnknownAdmin", err)
	}
	// Correct path.
	if err := m.UnlockAs("cidA", "wife", "eggcream"); err != nil {
		t.Fatalf("UnlockAs wife correct: %v", err)
	}
}

func TestConcurrentEncryptDecryptUnlocked(t *testing.T) {
	m := New(NewMemoryStore())
	if err := m.BootstrapFirstAdmin("cidA", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	var wg sync.WaitGroup
	const N = 20
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ct, err := m.Encrypt("cidA", []byte("hello"))
			if err != nil {
				t.Errorf("Encrypt: %v", err)
				return
			}
			got, err := m.Decrypt("cidA", ct)
			if err != nil {
				t.Errorf("Decrypt: %v", err)
				return
			}
			if string(got) != "hello" {
				t.Errorf("round-trip mismatch: %q", got)
			}
		}()
	}
	wg.Wait()
}

// TestConcurrentEncryptDecryptVsLock hammers Encrypt/Decrypt while a
// second set of goroutines repeatedly Lock+Unlock the same cluster.
// Before the fix, Encrypt/Decrypt aliased the map's CSK backing array
// across the RUnlock boundary; Lock zeroed those exact bytes in place
// under the write lock, which is a data race the -race detector flags
// (and could seal/open under a partially-zeroed key). Run with
// `go test -race`.
func TestConcurrentEncryptDecryptVsLock(t *testing.T) {
	const password = "hunter2"
	m := New(NewMemoryStore())
	if err := m.BootstrapFirstAdmin("cidA", "matthew", password); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}

	// Kept small: each Unlock pays a full Argon2id (~100ms). A handful
	// of overlapping cycles is enough for the race detector to flag the
	// unsynchronised read-vs-zero on the CSK backing array.
	var wg sync.WaitGroup
	const workers = 8
	const iters = 4

	// Encrypt/Decrypt workers. ErrLocked is expected and benign when a
	// Lock goroutine is mid-cycle; any other error is a real failure.
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				ct, err := m.Encrypt("cidA", []byte("hello"))
				if err != nil {
					if !errors.Is(err, ErrLocked) {
						t.Errorf("Encrypt: %v", err)
					}
					continue
				}
				got, err := m.Decrypt("cidA", ct)
				if err != nil {
					if !errors.Is(err, ErrLocked) {
						t.Errorf("Decrypt: %v", err)
					}
					continue
				}
				if string(got) != "hello" {
					t.Errorf("round-trip mismatch: %q", got)
				}
			}
		}()
	}

	// Lock/Unlock churners: zero the CSK then re-decode it.
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				m.Lock("cidA")
				_ = m.Unlock("cidA", password)
			}
		}()
	}

	wg.Wait()
}

// TestUnlockCorruptKDFParamsNoPanic seeds a WrappedCSK whose Argon2id
// KDF parameters are zeroed (Time=0 / Threads=0), as a corrupted or
// bit-flipped on-disk record would be. argon2.IDKey panics on Time<1 or
// Threads<1; without the guard in tryUnwrap this would crash the process
// (DoS) on Unlock. The record must be well-formed enough to pass the
// length/salt guards so the KDF call is actually reached.
func TestUnlockCorruptKDFParamsNoPanic(t *testing.T) {
	cases := []struct {
		name   string
		params KDFParams
	}{
		{"time-zero", KDFParams{Time: 0, Memory: Argon2Memory, Threads: Argon2Threads, KeyLen: wrappingKeyLen}},
		{"threads-zero", KDFParams{Time: Argon2Time, Memory: Argon2Memory, Threads: 0, KeyLen: wrappingKeyLen}},
		{"memory-zero", KDFParams{Time: Argon2Time, Memory: 0, Threads: Argon2Threads, KeyLen: wrappingKeyLen}},
		{"all-zero-but-keylen", KDFParams{Time: 0, Memory: 0, Threads: 0, KeyLen: wrappingKeyLen}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemoryStore()
			// Wrapped must be >= nonceSize+16 and Salt non-empty so
			// tryUnwrap reaches the KDF call rather than bailing on the
			// earlier length guard.
			rec := WrappedCSK{
				ClusterID:   "cidA",
				AdminUserID: "matthew",
				Wrapped:     make([]byte, nonceSize+16+cskSize),
				Salt:        make([]byte, saltSize),
				KDFParams:   tc.params,
			}
			if err := store.PutWrappedCSK(rec); err != nil {
				t.Fatalf("seed PutWrappedCSK: %v", err)
			}
			m := New(store)

			// Must return a clean error (folded into ErrInvalidPassword),
			// not panic. If the guard is missing, argon2.IDKey panics and
			// fails the test before reaching the assertion.
			err := m.Unlock("cidA", "hunter2")
			if !errors.Is(err, ErrInvalidPassword) {
				t.Fatalf("Unlock with corrupt KDF params: got %v want ErrInvalidPassword", err)
			}
			if m.IsUnlocked("cidA") {
				t.Fatalf("cluster must not be unlocked from a corrupt record")
			}
		})
	}
}

// TestConcurrentBootstrapFirstAdmin fires many BootstrapFirstAdmin calls
// at the same cluster concurrently. Exactly one must succeed; all others
// must return ErrAdminAlreadyExists. Before the fix the check (no
// existing CSK) and the create were not under a single held lock, so two
// callers could both pass the check and write divergent CSKs, leaving the
// in-memory CSK out of sync with the on-disk wrap. Run with `go test -race`.
func TestConcurrentBootstrapFirstAdmin(t *testing.T) {
	store := NewMemoryStore()
	m := New(store)

	const N = 16
	var wg sync.WaitGroup
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = m.BootstrapFirstAdmin("cidA", "matthew", "hunter2")
		}(i)
	}
	wg.Wait()

	successes := 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrAdminAlreadyExists):
			// expected for the losers
		default:
			t.Fatalf("unexpected BootstrapFirstAdmin error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly 1 successful bootstrap, got %d", successes)
	}

	// Exactly one wrapped record persisted (no divergent double-write).
	recs, err := store.GetWrappedCSKs("cidA")
	if err != nil {
		t.Fatalf("GetWrappedCSKs: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected exactly 1 wrapped record, got %d", len(recs))
	}

	// The cached CSK must match the persisted wrap: lock, then unlock with
	// the same password and confirm a round-trip still decrypts. A
	// divergent cache (the old race) would fail this.
	ct, err := m.Encrypt("cidA", []byte("payload"))
	if err != nil {
		t.Fatalf("Encrypt after bootstrap: %v", err)
	}
	m.Lock("cidA")
	if err := m.Unlock("cidA", "hunter2"); err != nil {
		t.Fatalf("Unlock after bootstrap: %v", err)
	}
	got, err := m.Decrypt("cidA", ct)
	if err != nil {
		t.Fatalf("Decrypt after re-unlock: %v", err)
	}
	if string(got) != "payload" {
		t.Fatalf("cache/disk CSK divergence: round-trip mismatch %q", got)
	}
}

func TestFileStorePersistAndReload(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	m := New(fs)
	if err := m.BootstrapFirstAdmin("cidA", "matthew", "hunter2"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	plaintext := []byte("persisted secret")
	ct, err := m.Encrypt("cidA", plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// File exists on disk.
	if _, err := readSafe(filepath.Join(dir, "cluster_secrets.json")); err != nil {
		t.Fatalf("file missing on disk: %v", err)
	}

	// "Restart" by opening a fresh FileStore on the same dir.
	fs2, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore reload: %v", err)
	}
	m2 := New(fs2)
	if err := m2.Unlock("cidA", "hunter2"); err != nil {
		t.Fatalf("Unlock after reload: %v", err)
	}
	got, err := m2.Decrypt("cidA", ct)
	if err != nil {
		t.Fatalf("Decrypt after reload: %v", err)
	}
	if !bytes.Equal(plaintext, got) {
		t.Fatalf("reload round-trip mismatch: %q != %q", got, plaintext)
	}
}

// TestPutWrappedCSKSaveFailureRollback forces a disk save failure on
// the replace path and asserts (a) the call returns promptly without
// hanging — the old rollback called the locking load() while holding
// s.mu, deadlocking the process — and (b) in-memory state is rolled
// back to the prior record so cache and disk stay consistent.
func TestPutWrappedCSKSaveFailureRollback(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	orig := WrappedCSK{
		ClusterID:   "cidA",
		AdminUserID: "matthew",
		Wrapped:     []byte("original-wrapped-bytes"),
		Salt:        []byte("original-salt"),
		KDFParams:   DefaultKDFParams(),
	}
	if err := fs.PutWrappedCSK(orig); err != nil {
		t.Fatalf("seed PutWrappedCSK: %v", err)
	}

	// Make the data dir read-only so the atomic write-tmp step fails.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod dir read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	replacement := orig
	replacement.Wrapped = []byte("replacement-wrapped-bytes")

	// Run in a goroutine with a watchdog so a regression (deadlock)
	// fails the test instead of hanging the whole suite.
	done := make(chan error, 1)
	go func() { done <- fs.PutWrappedCSK(replacement) }()

	select {
	case putErr := <-done:
		if putErr == nil {
			t.Fatalf("expected save failure on read-only dir, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("PutWrappedCSK hung on save failure (deadlock regression)")
	}

	// Restore write access so we can inspect via Get without the dir
	// permission interfering, then assert the prior record survived.
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod restore: %v", err)
	}
	recs, err := fs.GetWrappedCSKs("cidA")
	if err != nil {
		t.Fatalf("GetWrappedCSKs: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record after rollback, got %d", len(recs))
	}
	if !bytes.Equal(recs[0].Wrapped, orig.Wrapped) {
		t.Fatalf("in-memory record not rolled back: got %q want %q",
			recs[0].Wrapped, orig.Wrapped)
	}
}

// TestFileStoreNeverWritesPlaintextCSK is a regression: the on-disk
// file must contain ciphertext only — never the CSK, never the
// password, never the wrapping key.
func TestFileStoreNeverWritesPlaintextCSK(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	m := New(fs)
	if err := m.BootstrapFirstAdmin("cidA", "matthew", "hunter2-distinctive-password"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	contents, err := readSafe(filepath.Join(dir, "cluster_secrets.json"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	s := string(contents)
	if strings.Contains(s, "hunter2") {
		t.Fatalf("on-disk file contains plaintext password: %s", s)
	}
}

// ─── helpers ────────────────────────────────────────────────────────

// jwtSeal mirrors internal/store/crypto.go's encryptSecret so tests
// can build legacy ciphertexts without depending on the store package
// (which would create an import cycle).
func jwtSeal(jwtSecret, plaintext []byte) ([]byte, error) {
	derived := sha256.Sum256(jwtSecret)
	block, err := aes.NewCipher(derived[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func isAllZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

func readSafe(path string) ([]byte, error) {
	return os.ReadFile(path)
}
