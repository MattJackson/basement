package store

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestOpenCreatesDataDir(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "nonexistent", "data")

	s, err := Open(dataDir, 24*time.Hour)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		t.Fatal("data dir was not created")
	}

	if s == nil {
		t.Fatal("Store is nil")
	}
}

func TestUserRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := Open(tmpDir, 24*time.Hour)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	u := User{
		ID:           "test-user-id-123",
		Username:     "alice",
		PasswordHash: "hashed-password",
		Role:         "admin",
		Created:      time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC),
	}

	if err := s.CreateUser(u); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	users := s.Users()
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}

	reloaded, err := s.UserByUsername("alice")
	if err != nil {
		t.Fatalf("UserByUsername failed: %v", err)
	}

	if reloaded.Username != "alice" {
		t.Errorf("expected username 'alice', got '%s'", reloaded.Username)
	}

	if reloaded.Role != "admin" {
		t.Errorf("expected role 'admin', got '%s'", reloaded.Role)
	}

	reloadedStore, err := Open(tmpDir, 24*time.Hour)
	if err != nil {
		t.Fatalf("Open (reload) failed: %v", err)
	}

	fromReload, err := reloadedStore.UserByUsername("alice")
	if err != nil {
		t.Fatalf("UserByUsername after reload failed: %v", err)
	}

	if fromReload.Username != "alice" {
		t.Errorf("expected username 'alice' after reload, got '%s'", fromReload.Username)
	}
}

// TestUserByID covers UserByID hit (lookup by the canonical UUID
// primary key) and miss. This is the lookup the elevation flow uses
// because the session JWT carries user.ID — not username — as its
// subject (notably for OIDC-provisioned users whose ID is a UUID).
func TestUserByID(t *testing.T) {
	s, err := Open(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	u := User{
		ID:       "11111111-2222-3333-4444-555555555555",
		Username: "alice",
		Role:     "user",
		Provider: "https://idp.example.com",
		Subject:  "subj-alice",
	}
	if err := s.CreateUser(u); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Hit: lookup by the UUID returns the user.
	got, err := s.UserByID("11111111-2222-3333-4444-555555555555")
	if err != nil {
		t.Fatalf("UserByID hit failed: %v", err)
	}
	if got.Username != "alice" {
		t.Errorf("UserByID returned username %q, want alice", got.Username)
	}

	// Miss: an unknown ID (and notably the username, which must NOT
	// match an ID lookup) returns an error.
	if _, err := s.UserByID("no-such-id"); err == nil {
		t.Error("UserByID(unknown) returned nil error, want not-found")
	}
	if _, err := s.UserByID("alice"); err == nil {
		t.Error("UserByID(username) matched; ID lookup must not match on username")
	}
}

func TestAtomicSaveCorruptionRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	usersPath := filepath.Join(tmpDir, "users.json")

	u1 := User{ID: "user-1", Username: "first", PasswordHash: "h1", Role: "user"}
	if err := saveJSON(usersPath, []User{u1}); err != nil {
		t.Fatalf("saveJSON failed: %v", err)
	}

	u2 := User{ID: "user-2", Username: "second", PasswordHash: "h2", Role: "user"}
	if err := saveJSON(usersPath, []User{u1, u2}); err != nil {
		t.Fatalf("saveJSON failed: %v", err)
	}

	tmpPath := usersPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		t.Fatalf("create tmp failed: %v", err)
	}
	if _, err := f.WriteString("{\"corrupted"); err != nil {
		t.Fatalf("write corrupted data failed: %v", err)
	}
	_ = f.Close()

	reloaded, err := loadJSON[[]User](usersPath)
	if err != nil {
		t.Fatalf("loadJSON should succeed with valid file, got: %v", err)
	}

	if len(reloaded) != 2 {
		t.Errorf("expected 2 users after atomic save, got %d", len(reloaded))
	}

	_ = os.Remove(tmpPath)
}

func TestConcurrentWrites(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := Open(tmpDir, 24*time.Hour)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	var wg sync.WaitGroup
	numGoroutines := 10
	usersPerGoroutine := 5

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < usersPerGoroutine; j++ {
				u := User{
					ID:           uuid.New().String(),
					Username:     "user-" + string(rune('a'+base)) + "-" + string(rune('0'+j)),
					PasswordHash: "hash",
					Role:         "user",
				}
				if err := s.CreateUser(u); err != nil {
					t.Errorf("concurrent CreateUser failed: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	users := s.Users()
	expected := numGoroutines * usersPerGoroutine
	if len(users) != expected {
		t.Errorf("expected %d users after concurrent writes, got %d", expected, len(users))
	}

	reloaded, err := Open(tmpDir, 24*time.Hour)
	if err != nil {
		t.Fatalf("Open (reload after concurrency) failed: %v", err)
	}

	finalUsers := reloaded.Users()
	if len(finalUsers) != expected {
		t.Errorf("expected %d users after reload, got %d", expected, len(finalUsers))
	}
}

// TestMatchGrantLongestPrefix removed in v1.0.0b: the legacy Grant +
// MatchGrant tested here was retired in favour of BucketGrants (per-user
// per-bucket S3 credentials, ADR-0001) plus the policy enforcer. Prefix
// matching has no equivalent in the new model — visibility comes from
// BucketGrants and permission from policy.Can.

func TestAuditRotation(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := Open(tmpDir, 24*time.Hour)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	entry1 := AuditEntry{
		Timestamp: time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC),
		UserID:    "user-1",
		Action:    "bucket.create",
		Resource:  "bucket:photos",
	}

	entry2 := AuditEntry{
		Timestamp: time.Date(2026, 5, 19, 14, 30, 0, 0, time.UTC),
		UserID:    "user-2",
		Action:    "share.create",
		Resource:  "share:abc123",
	}

	if err := s.AppendAudit(entry1); err != nil {
		t.Fatalf("AppendAudit entry1 failed: %v", err)
	}

	if err := s.AppendAudit(entry2); err != nil {
		t.Fatalf("AppendAudit entry2 failed: %v", err)
	}

	dir := filepath.Join(tmpDir, "audit")
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir audit dir failed: %v", err)
	}

	if len(files) != 2 {
		t.Errorf("expected 2 audit files (one per day), got %d", len(files))
		for _, f := range files {
			t.Logf("file: %s", f.Name())
		}
	}

	foundDates := make(map[string]bool)
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".jsonl") {
			dateStr := strings.TrimSuffix(f.Name(), ".jsonl")
			foundDates[dateStr] = true
		}
	}

	if !foundDates["2026-05-18"] {
		t.Error("missing 2026-05-18.jsonl")
	}
	if !foundDates["2026-05-19"] {
		t.Error("missing 2026-05-19.jsonl")
	}
}

// TestAuditRetention anchors on the REAL wall clock (CleanupAudit reads
// time.Now() internally and we don't want to change its signature). Audit
// files are keyed purely off their date-stamped filename, so we drop empty
// .jsonl files at known day-offsets from today and assert exactly which the
// 48h-retention cleanup deletes vs keeps. This guards the off-by-one-day
// cutoff fix: the file dated exactly 2 days ago (the boundary day) must be
// KEPT, because its newest entry is still inside the 48h window.
func TestAuditRetention(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := Open(tmpDir, 48*time.Hour) // retention = 2 days
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	dir := filepath.Join(tmpDir, "audit")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("mkdir audit: %v", err)
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	mk := func(offsetDays int) string {
		return today.AddDate(0, 0, offsetDays).Format("2006-01-02")
	}

	// Create files from 4 days ago through tomorrow.
	for i := -4; i <= 1; i++ {
		name := mk(i) + ".jsonl"
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	if err := s.CleanupAudit(); err != nil {
		t.Fatalf("CleanupAudit failed: %v", err)
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir audit dir failed: %v", err)
	}
	foundDates := make(map[string]bool)
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".jsonl") {
			foundDates[strings.TrimSuffix(f.Name(), ".jsonl")] = true
		}
	}

	// cutoff = today - 2 days (truncated). Strict Before deletes files
	// strictly older than that day: 3 and 4 days ago. The boundary day
	// (exactly 2 days ago) and everything newer is kept.
	for _, off := range []int{-4, -3} {
		if foundDates[mk(off)] {
			t.Errorf("file %s.jsonl (%d days old) should have been deleted but exists", mk(off), -off)
		}
	}
	for _, off := range []int{-2, -1, 0, 1} {
		if !foundDates[mk(off)] {
			t.Errorf("file %s.jsonl (%d days old) is within retention and should still exist", mk(off), -off)
		}
	}
}

func TestUserDelete(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := Open(tmpDir, 24*time.Hour)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	u := User{ID: "user-to-delete", Username: "delete-me", PasswordHash: "h", Role: "user"}
	if err := s.CreateUser(u); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	if len(s.Users()) != 1 {
		t.Fatal("expected 1 user before delete")
	}

	if err := s.DeleteUser("user-to-delete"); err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}

	if len(s.Users()) != 0 {
		t.Error("expected 0 users after delete")
	}
}

func TestShareRevoke(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := Open(tmpDir, 24*time.Hour)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	expires := time.Now().Add(24 * time.Hour)
	sh := Share{
		Token:       "share-token-123",
		OwnerUserID: "user-1",
		BucketID:    "photos",
		Key:         "img.jpg",
		ExpiresAt:   &expires,
	}

	if err := s.CreateShare(sh); err != nil {
		t.Fatalf("CreateShare failed: %v", err)
	}

	got, err := s.Share("share-token-123")
	if err != nil {
		t.Fatalf("Share failed: %v", err)
	}

	if got.Revoked {
		t.Error("share should not be revoked initially")
	}

	if err := s.RevokeShare("share-token-123"); err != nil {
		t.Fatalf("RevokeShare failed: %v", err)
	}

	// After revoke, Share() returns an error (intentional — revoked shares
	// are not retrievable through the same path that serves live ones).
	_, err = s.Share("share-token-123")
	if err == nil {
		t.Error("Share() should error after RevokeShare; got nil")
	}
}

func TestSharesByUser(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := Open(tmpDir, 24*time.Hour)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	user1 := "user-1"
	user2 := "user-2"

	shares := []Share{
		{Token: "s1", OwnerUserID: user1, BucketID: "bucket1"},
		{Token: "s2", OwnerUserID: user1, BucketID: "bucket2"},
		{Token: "s3", OwnerUserID: user2, BucketID: "bucket3"},
	}

	for _, sh := range shares {
		if err := s.CreateShare(sh); err != nil {
			t.Fatalf("CreateShare failed: %v", err)
		}
	}

	user1Shares := s.SharesByUser(user1)
	if len(user1Shares) != 2 {
		t.Errorf("expected 2 shares for user1, got %d", len(user1Shares))
	}

	user2Shares := s.SharesByUser(user2)
	if len(user2Shares) != 1 {
		t.Errorf("expected 1 share for user2, got %d", len(user2Shares))
	}
}

// TestGrantUpdate removed in v1.0.0b together with the legacy Grant
// type. BucketGrants has its own Update test in bucket_grants_test.go.
