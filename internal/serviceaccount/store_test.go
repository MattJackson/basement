// Package serviceaccount: store-layer tests (v1.7.0a).
//
// Covers the contract obligations from the cycle prompt:
//   - Create + GetByAccessKey
//   - Plaintext returned once on create + never present on subsequent
//     reads (the SecretKeyHash is on disk; plaintext only rides the
//     Create / Rotate return values)
//   - VerifySecret: correct → true, wrong → false
//   - VerifySecret on revoked → false
//   - VerifySecret on expired → false
//   - Update doesn't touch secret
//   - Rotate invalidates old creds
//   - TouchLastUsed debounced
//   - Persists across reopen
//
// Plus a handful of edge cases (duplicate-name, name-validation,
// soft-delete idempotency) that the SA admin API leans on for its
// 4xx wire shape.
package serviceaccount

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCreate_GeneratesAccessKeyAndReturnsPlaintextOnce(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	sa, secret, err := store.Create(ctx, ServiceAccount{
		OwnerUserID: "matthew",
		Name:        "ci-prod",
		Capabilities: []Capability{
			{ID: "bucket:view", Scope: "bucket:c1:b1"},
		},
		Scopes: []string{"bucket:c1:b1"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sa.ID == "" {
		t.Error("expected non-empty ID")
	}
	if !strings.HasPrefix(sa.AccessKeyID, "BMNT") {
		t.Errorf("AccessKeyID=%q, want BMNT prefix", sa.AccessKeyID)
	}
	// BMNT + 16 hex chars = 20 chars.
	if len(sa.AccessKeyID) != 20 {
		t.Errorf("AccessKeyID length=%d, want 20", len(sa.AccessKeyID))
	}
	if secret == "" {
		t.Fatal("expected non-empty plaintext secret on create")
	}
	if len(sa.SecretKeyHash) == 0 {
		t.Fatal("expected SecretKeyHash to be populated")
	}
	// bcrypt hashes start with $2 / $2a / $2b — the plaintext must NOT
	// be present in the persisted hash bytes.
	if strings.Contains(string(sa.SecretKeyHash), secret) {
		t.Error("bcrypt hash unexpectedly contains plaintext secret")
	}

	// GetByAccessKey resolves to the same row.
	got, err := store.GetByAccessKey(ctx, sa.AccessKeyID)
	if err != nil {
		t.Fatalf("GetByAccessKey: %v", err)
	}
	if got.ID != sa.ID {
		t.Errorf("GetByAccessKey returned ID=%q, want %q", got.ID, sa.ID)
	}
}

func TestCreate_PlaintextNeverPersistedToDisk(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	_, secret, err := store.Create(ctx, ServiceAccount{
		OwnerUserID: "matthew",
		Name:        "ci-prod",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Read the raw JSON file and assert the plaintext doesn't appear
	// anywhere inside. bcrypt hash bytes are present (they have to be),
	// but the plaintext itself must not.
	raw, err := os.ReadFile(dir + "/service_accounts.json")
	if err != nil {
		t.Fatalf("reading service_accounts.json: %v", err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatalf("plaintext secret unexpectedly present in on-disk JSON")
	}
}

func TestCreate_DuplicateNameSameOwnerErrors(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	_, _, err := store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "ci-prod"})
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, _, err = store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "ci-prod"})
	if err != ErrDuplicateName {
		t.Errorf("second Create err=%v, want ErrDuplicateName", err)
	}
}

func TestCreate_DuplicateNameDifferentOwnersAllowed(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	_, _, err := store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "ci-prod"})
	if err != nil {
		t.Fatalf("Create matthew: %v", err)
	}
	_, _, err = store.Create(ctx, ServiceAccount{OwnerUserID: "alice", Name: "ci-prod"})
	if err != nil {
		t.Errorf("Create alice with same name should be allowed, got %v", err)
	}
}

func TestCreate_DuplicateNameAfterRevokeAllowed(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	first, _, err := store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "ci-prod"})
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if err := store.Delete(ctx, first.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, _, err = store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "ci-prod"})
	if err != nil {
		t.Errorf("Create after revoke should succeed, got %v", err)
	}
}

func TestCreate_NameValidation(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	cases := []struct {
		name string
		want error
	}{
		{"ci", ErrInvalidName}, // too short
		{"", ErrInvalidName},   // empty
		{strings.Repeat("a", 65), ErrInvalidName},
		{"ci prod", ErrInvalidName}, // space
		{"ci/prod", ErrInvalidName}, // slash
		{"ci-prod", nil},
		{"ci_prod", nil},
		{"ABC123", nil},
	}
	for _, c := range cases {
		_, _, err := store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: c.name})
		if err != c.want {
			t.Errorf("Create(name=%q) err=%v, want %v", c.name, err, c.want)
		}
		// Clean up successful inserts so the next iteration doesn't trip
		// the duplicate-name guard.
		if err == nil {
			if list, _ := store.ListForUser(ctx, "matthew"); len(list) > 0 {
				_ = store.Delete(ctx, list[0].ID)
				// Delete soft-deletes — clear the rows by re-Open on a
				// fresh dir so subsequent name reuse isn't blocked. The
				// existing block exists but is revoked → allowed anyway.
			}
		}
	}
}

func TestCreate_ExpiresAtInPastRejected(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	past := time.Now().UTC().Add(-time.Hour)
	_, _, err := store.Create(ctx, ServiceAccount{
		OwnerUserID: "matthew", Name: "ci-prod", ExpiresAt: &past,
	})
	if err == nil {
		t.Error("expected error for past ExpiresAt, got nil")
	}
}

func TestVerifySecret_CorrectMatches_WrongFails(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	sa, secret, err := store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "ci-prod"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ok, err := store.VerifySecret(ctx, sa.AccessKeyID, secret)
	if err != nil {
		t.Fatalf("VerifySecret(correct): %v", err)
	}
	if !ok {
		t.Error("correct secret did not verify")
	}

	ok, err = store.VerifySecret(ctx, sa.AccessKeyID, "nope-wrong-secret")
	if err != nil {
		t.Fatalf("VerifySecret(wrong): %v", err)
	}
	if ok {
		t.Error("wrong secret unexpectedly verified")
	}
}

func TestVerifySecret_UnknownAccessKeyReturnsNotFound(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	ok, err := store.VerifySecret(ctx, "BMNT0000000000000000", "whatever")
	if ok {
		t.Error("expected verification to fail for unknown access key")
	}
	if err != ErrNotFound {
		t.Errorf("err=%v, want ErrNotFound", err)
	}
}

func TestVerifySecret_RevokedReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	sa, secret, _ := store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "ci-prod"})

	if err := store.Delete(ctx, sa.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	ok, err := store.VerifySecret(ctx, sa.AccessKeyID, secret)
	if err != nil {
		t.Fatalf("VerifySecret: %v", err)
	}
	if ok {
		t.Error("revoked SA unexpectedly verified")
	}
}

func TestVerifySecret_ExpiredReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	// Create with a future expiry so Create() accepts it, then mutate
	// the persisted row backwards via Update — but Update also rejects
	// past expiries, so instead we surgery the in-memory row directly
	// through the typed fileStore. Cast back via Open's interface.
	future := time.Now().UTC().Add(time.Hour)
	sa, secret, err := store.Create(ctx, ServiceAccount{
		OwnerUserID: "matthew", Name: "ci-prod", ExpiresAt: &future,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Reach into the store via type-assertion and rewrite ExpiresAt to
	// a past timestamp. This mirrors the situation where time simply
	// advanced past ExpiresAt between mints.
	fs := store.(*fileStore)
	fs.mu.Lock()
	past := time.Now().UTC().Add(-time.Hour)
	row := fs.rows[sa.ID]
	row.ExpiresAt = &past
	fs.rows[sa.ID] = row
	fs.mu.Unlock()

	ok, err := store.VerifySecret(ctx, sa.AccessKeyID, secret)
	if err != nil {
		t.Fatalf("VerifySecret: %v", err)
	}
	if ok {
		t.Error("expired SA unexpectedly verified")
	}
}

func TestUpdate_DoesNotTouchSecret(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	sa, secret, _ := store.Create(ctx, ServiceAccount{
		OwnerUserID:  "matthew",
		Name:         "ci-prod",
		Capabilities: []Capability{{ID: "bucket:view", Scope: "bucket:c1:b1"}},
	})

	updated, err := store.Update(ctx, sa.ID, ServiceAccount{
		Name:         "ci-prod-renamed",
		Capabilities: []Capability{{ID: "objects:get", Scope: "bucket:c1:b1"}},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "ci-prod-renamed" {
		t.Errorf("Name=%q, want renamed", updated.Name)
	}
	if updated.AccessKeyID != sa.AccessKeyID {
		t.Errorf("AccessKeyID changed by Update: was %q, now %q", sa.AccessKeyID, updated.AccessKeyID)
	}
	// Original secret must still verify against the updated row.
	ok, err := store.VerifySecret(ctx, sa.AccessKeyID, secret)
	if err != nil {
		t.Fatalf("VerifySecret: %v", err)
	}
	if !ok {
		t.Error("original secret no longer verifies after Update — Update changed the secret")
	}
}

func TestUpdate_DuplicateNameWithLiveSiblingErrors(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	_, _, _ = store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "ci-prod"})
	other, _, _ := store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "ci-staging"})

	_, err := store.Update(ctx, other.ID, ServiceAccount{Name: "ci-prod"})
	if err != ErrDuplicateName {
		t.Errorf("Update err=%v, want ErrDuplicateName", err)
	}
}

func TestRotate_InvalidatesOldSecret_ReturnsNewOnce(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	sa, oldSecret, _ := store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "ci-prod"})

	rotated, newSecret, err := store.Rotate(ctx, sa.ID)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if newSecret == "" {
		t.Fatal("expected new plaintext secret on rotate")
	}
	if newSecret == oldSecret {
		t.Fatal("Rotate returned the same plaintext as before")
	}
	if rotated.AccessKeyID != sa.AccessKeyID {
		t.Errorf("AccessKeyID changed by Rotate: was %q, now %q", sa.AccessKeyID, rotated.AccessKeyID)
	}

	// Old secret must no longer verify.
	ok, err := store.VerifySecret(ctx, sa.AccessKeyID, oldSecret)
	if err != nil {
		t.Fatalf("VerifySecret(old): %v", err)
	}
	if ok {
		t.Error("old secret unexpectedly still verifies after rotate")
	}

	// New secret must verify.
	ok, err = store.VerifySecret(ctx, sa.AccessKeyID, newSecret)
	if err != nil {
		t.Fatalf("VerifySecret(new): %v", err)
	}
	if !ok {
		t.Error("new secret did not verify after rotate")
	}
}

func TestRotate_RevokedSARefuses(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	sa, _, _ := store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "ci-prod"})
	_ = store.Delete(ctx, sa.ID)

	_, _, err := store.Rotate(ctx, sa.ID)
	if err == nil {
		t.Error("expected error rotating a revoked SA")
	}
}

func TestDelete_SoftDeletes(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	sa, _, _ := store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "ci-prod"})

	if err := store.Delete(ctx, sa.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Row is still resolvable, but RevokedAt is set.
	got, err := store.Get(ctx, sa.ID)
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if !got.IsRevoked() {
		t.Error("expected IsRevoked() = true after Delete")
	}
}

func TestDelete_Idempotent(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	sa, _, _ := store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "ci-prod"})
	if err := store.Delete(ctx, sa.ID); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	if err := store.Delete(ctx, sa.ID); err != nil {
		t.Errorf("second Delete (idempotent) err=%v, want nil", err)
	}
}

func TestTouchLastUsed_DebouncedWithinWindow(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	sa, _, _ := store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "ci-prod"})

	// First touch persists.
	if err := store.TouchLastUsed(ctx, sa.ID); err != nil {
		t.Fatalf("first Touch: %v", err)
	}

	// Capture mtime of the JSON file. A second touch within the
	// debounce window must NOT rewrite the file. We check by reading
	// the file's stat info before + after.
	info1, err := os.Stat(dir + "/service_accounts.json")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	// Sleep a hair so any second-resolution mtime change would surface.
	time.Sleep(10 * time.Millisecond)

	if err := store.TouchLastUsed(ctx, sa.ID); err != nil {
		t.Fatalf("second Touch: %v", err)
	}
	info2, err := os.Stat(dir + "/service_accounts.json")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info2.ModTime().Equal(info1.ModTime()) {
		t.Errorf("second TouchLastUsed within debounce window rewrote the file (mtime changed)")
	}

	// In-memory LastUsedAt should still reflect a fresh time, though.
	got, _ := store.Get(ctx, sa.ID)
	if got.LastUsedAt == nil || got.LastUsedAt.IsZero() {
		t.Error("expected LastUsedAt to be populated after Touch")
	}
}

func TestTouchLastUsed_PersistsAcrossDebounceWindow(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	sa, _, _ := store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "ci-prod"})
	if err := store.TouchLastUsed(ctx, sa.ID); err != nil {
		t.Fatalf("first Touch: %v", err)
	}

	// Wind the per-row lastTouchPersist backwards so the next call
	// falls outside the debounce window without actually sleeping a
	// real minute. This mirrors the production case where two CI
	// pushes more than a minute apart both persist.
	fs := store.(*fileStore)
	fs.mu.Lock()
	fs.lastTouchPersist[sa.ID] = time.Now().UTC().Add(-2 * time.Minute)
	fs.mu.Unlock()

	info1, _ := os.Stat(dir + "/service_accounts.json")
	time.Sleep(10 * time.Millisecond)

	if err := store.TouchLastUsed(ctx, sa.ID); err != nil {
		t.Fatalf("second Touch: %v", err)
	}
	info2, _ := os.Stat(dir + "/service_accounts.json")
	if info2.ModTime().Equal(info1.ModTime()) {
		// Some filesystems have only-1s-precision mtime — fall back to
		// asserting the recorded persist timestamp moved forward.
		fs.mu.Lock()
		last := fs.lastTouchPersist[sa.ID]
		fs.mu.Unlock()
		if last.Before(time.Now().UTC().Add(-time.Second)) {
			t.Errorf("expected TouchLastUsed past the debounce window to persist; lastTouchPersist=%v", last)
		}
	}
}

func TestPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	store1, _ := Open(dir)
	sa, secret, err := store1.Create(ctx, ServiceAccount{
		OwnerUserID: "matthew", Name: "ci-prod",
		Capabilities: []Capability{{ID: "bucket:view", Scope: "bucket:c1:b1"}},
		Scopes:       []string{"bucket:c1:b1"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Reopen — fresh process semantics. Same disk dir.
	store2, err := Open(dir)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}

	got, err := store2.Get(ctx, sa.ID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got.AccessKeyID != sa.AccessKeyID {
		t.Errorf("AccessKeyID lost across reopen: got %q, want %q", got.AccessKeyID, sa.AccessKeyID)
	}
	if got.Name != "ci-prod" {
		t.Errorf("Name lost across reopen: got %q", got.Name)
	}
	if len(got.Capabilities) != 1 {
		t.Errorf("Capabilities count after reopen: got %d, want 1", len(got.Capabilities))
	}

	// Secret must still verify against the reopened store — bcrypt
	// hash on disk is the canonical source.
	ok, err := store2.VerifySecret(ctx, sa.AccessKeyID, secret)
	if err != nil {
		t.Fatalf("VerifySecret after reopen: %v", err)
	}
	if !ok {
		t.Error("secret did not verify after reopen")
	}
}

// TestJSONShape_NoPlaintextField asserts the on-disk JSON has the
// expected field layout — `secretKeyHash` (bytes) but no `secretKey`
// / `plaintext` field. A regression on the wire shape would let
// plaintext escape into operator backups + log mining.
func TestJSONShape_NoPlaintextField(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	_, _, err := store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "ci-prod"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	raw, err := os.ReadFile(dir + "/service_accounts.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var rows []map[string]interface{}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	for _, banned := range []string{"secretKey", "secret", "plaintext", "plain"} {
		if _, ok := rows[0][banned]; ok {
			t.Errorf("on-disk row unexpectedly contains field %q", banned)
		}
	}
	if _, ok := rows[0]["secretKeyHash"]; !ok {
		t.Error("expected secretKeyHash on disk")
	}
}

// TestListForUser_ScopesByOwner asserts ListForUser only returns
// SAs owned by the supplied userID. Foundation for the API handler's
// 404-on-cross-user-access pattern.
func TestListForUser_ScopesByOwner(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	ctx := context.Background()

	_, _, _ = store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "matt-1"})
	_, _, _ = store.Create(ctx, ServiceAccount{OwnerUserID: "matthew", Name: "matt-2"})
	_, _, _ = store.Create(ctx, ServiceAccount{OwnerUserID: "alice", Name: "alice-1"})

	mine, err := store.ListForUser(ctx, "matthew")
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(mine) != 2 {
		t.Errorf("expected 2 SAs for matthew, got %d", len(mine))
	}
	for _, sa := range mine {
		if sa.OwnerUserID != "matthew" {
			t.Errorf("unexpected owner %q in matthew's list", sa.OwnerUserID)
		}
	}
}

// TestCountAll_ReturnsTotalCount verifies CountAll returns correct count.
func TestCountAll_ReturnsTotalCount(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	// Create multiple SAs for different users
	_, _, _ = store.Create(ctx, ServiceAccount{OwnerUserID: "user1", Name: "sa-1"})
	_, _, _ = store.Create(ctx, ServiceAccount{OwnerUserID: "user1", Name: "sa-2"})
	_, _, _ = store.Create(ctx, ServiceAccount{OwnerUserID: "user2", Name: "sa-3"})

	count, err := store.CountAll(ctx)
	if err != nil {
		t.Fatalf("CountAll failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}
}

// TestCountAll_EmptyStore_ReturnsZero verifies CountAll returns 0 for empty store.
func TestCountAll_EmptyStore_ReturnsZero(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	count, err := store.CountAll(ctx)
	if err != nil {
		t.Fatalf("CountAll failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected count 0 for empty store, got %d", count)
	}
}

// TestWriteLocked_ErrorPath exercises writeLocked when file write fails.
func TestWriteLocked_ErrorPath(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	fs := store.(*fileStore)

	// Corrupt the path to force write failure
	originalPath := fs.path
	fs.path = "/nonexistent/directory/service_accounts.json"

	fs.mu.Lock()
	err = fs.writeLocked()
	fs.mu.Unlock()

	// Restore path for cleanup
	fs.path = originalPath

	if err == nil {
		t.Error("writeLocked should fail when directory doesn't exist")
	}
}

// TestWriteLocked_PersistsData verifies writeLocked correctly persists data.
func TestWriteLocked_PersistsData(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	// Create a SA first to populate the cache
	_, _, _ = store.Create(ctx, ServiceAccount{OwnerUserID: "test", Name: "sa-1"})

	fs := store.(*fileStore)

	// Call writeLocked directly
	fs.mu.Lock()
	err = fs.writeLocked()
	fs.mu.Unlock()

	if err != nil {
		t.Errorf("writeLocked failed: %v", err)
	}

	// Verify data was written by reopening
	store2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen failed: %v", err)
	}

	count, err := store2.CountAll(ctx)
	if err != nil {
		t.Fatalf("CountAll after reopen failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected count 1 after reopen, got %d", count)
	}
}
