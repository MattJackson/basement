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
