package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrateLegacyUsers(t *testing.T) {
	s, err := Open(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// A v1-era admin predating the UIAdmin field, a regular user, and an
	// admin already flagged UIAdmin.
	mustCreate(t, s, User{ID: "u-admin-legacy", Username: "legacyadmin", Role: "admin", UIAdmin: false})
	mustCreate(t, s, User{ID: "u-regular", Username: "regular", Role: "user", UIAdmin: false})
	mustCreate(t, s, User{ID: "u-admin-ok", Username: "okadmin", Role: "admin", UIAdmin: true})

	if err := s.MigrateLegacyUsers(); err != nil {
		t.Fatalf("MigrateLegacyUsers: %v", err)
	}

	assertUIAdmin(t, s, "u-admin-legacy", true) // flipped
	assertUIAdmin(t, s, "u-regular", false)     // untouched (not admin)
	assertUIAdmin(t, s, "u-admin-ok", true)     // untouched (already true)

	// Idempotent + persisted: reopening from disk shows the migrated flag.
	if err := s.MigrateLegacyUsers(); err != nil {
		t.Fatalf("MigrateLegacyUsers (2nd): %v", err)
	}
	reopened, err := Open(s.dataDir, 24*time.Hour)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	assertUIAdmin(t, reopened, "u-admin-legacy", true)
}

func TestMigrateBucketUserAssignments(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, 24*time.Hour)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	path := filepath.Join(dir, "policies.json")

	writePolicies(t, path, []map[string]string{
		{"userId": "alice", "roleId": "bucket_user", "scope": "bucket:b1:*"},
		{"userId": "bob", "roleId": "host_admin", "scope": "host:*"},
		{"userId": "carol", "roleId": "bucket_user", "scope": "bucket:b2:*"},
		{"userId": "dave", "roleId": "cluster_admin", "scope": "cluster:c1"},
	})

	n, err := s.MigrateBucketUserAssignments(path)
	if err != nil {
		t.Fatalf("MigrateBucketUserAssignments: %v", err)
	}
	if n != 2 {
		t.Fatalf("dropped = %d, want 2", n)
	}

	got := readAssignmentRoleIDs(t, path)
	for _, rid := range got {
		if rid == "bucket_user" {
			t.Fatalf("bucket_user assignment survived: %v", got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("remaining assignments = %d (%v), want 2 (host_admin, cluster_admin)", len(got), got)
	}

	// Idempotent: a second run drops nothing.
	n2, err := s.MigrateBucketUserAssignments(path)
	if err != nil {
		t.Fatalf("MigrateBucketUserAssignments (2nd): %v", err)
	}
	if n2 != 0 {
		t.Fatalf("2nd run dropped = %d, want 0", n2)
	}

	// Missing file is not an error (seed creates it later).
	nMissing, err := s.MigrateBucketUserAssignments(filepath.Join(dir, "nope.json"))
	if err != nil || nMissing != 0 {
		t.Fatalf("missing file: n=%d err=%v, want 0,nil", nMissing, err)
	}
}

// TestMigrateBucketUserAssignments_NoOpDoesNotRewrite: when there are no
// bucket_user assignments to drop, the migration must NOT rewrite
// policies.json (no reformat, no fsync) — a clean store shouldn't be
// touched on every boot.
func TestMigrateBucketUserAssignments_NoOpDoesNotRewrite(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, 24*time.Hour)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	path := filepath.Join(dir, "policies.json")

	// No bucket_user rows — nothing to drop.
	writePolicies(t, path, []map[string]string{
		{"userId": "bob", "roleId": "host_admin", "scope": "host:*"},
	})

	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}
	// Capture the exact bytes too — mtime resolution can be coarse.
	beforeBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	n, err := s.MigrateBucketUserAssignments(path)
	if err != nil || n != 0 {
		t.Fatalf("MigrateBucketUserAssignments: n=%d err=%v, want 0,nil", n, err)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("policies.json was rewritten on a no-op migration (mtime changed %v -> %v)",
			before.ModTime(), after.ModTime())
	}
	afterBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(afterBytes) != string(beforeBytes) {
		t.Fatalf("policies.json content changed on a no-op migration")
	}
}

// TestMigrateLegacyUsers_NoOpWhenNothingChanged: when no admin needs the
// UIAdmin flag flipped, the migration returns nil without rewriting users.
func TestMigrateLegacyUsers_NoOpWhenNothingChanged(t *testing.T) {
	s, err := Open(t.TempDir(), 24*time.Hour)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// All admins already UIAdmin; a regular user that should stay untouched.
	mustCreate(t, s, User{ID: "u-admin-ok", Username: "okadmin", Role: "admin", UIAdmin: true})
	mustCreate(t, s, User{ID: "u-regular", Username: "regular", Role: "user", UIAdmin: false})

	before, err := os.Stat(s.usersPath)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}

	if err := s.MigrateLegacyUsers(); err != nil {
		t.Fatalf("MigrateLegacyUsers: %v", err)
	}

	after, err := os.Stat(s.usersPath)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("users.json was rewritten on a no-op migration (mtime changed %v -> %v)",
			before.ModTime(), after.ModTime())
	}
}

// --- helpers ---

func mustCreate(t *testing.T, s *Store, u User) {
	t.Helper()
	if u.Created.IsZero() {
		u.Created = time.Unix(0, 0).UTC()
	}
	if err := s.CreateUser(u); err != nil {
		t.Fatalf("CreateUser(%s): %v", u.Username, err)
	}
}

func assertUIAdmin(t *testing.T, s *Store, id string, want bool) {
	t.Helper()
	u, err := s.UserByID(id)
	if err != nil {
		t.Fatalf("UserByID(%s): %v", id, err)
	}
	if u.UIAdmin != want {
		t.Fatalf("user %s UIAdmin = %v, want %v", id, u.UIAdmin, want)
	}
}

func writePolicies(t *testing.T, path string, assignments []map[string]string) {
	t.Helper()
	pf := map[string]any{"roles": []any{}, "assignments": assignments}
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		t.Fatalf("marshal policies: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write policies: %v", err)
	}
}

func readAssignmentRoleIDs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read policies: %v", err)
	}
	var pf struct {
		Assignments []struct {
			RoleID string `json:"roleId"`
		} `json:"assignments"`
	}
	if err := json.Unmarshal(data, &pf); err != nil {
		t.Fatalf("unmarshal policies: %v", err)
	}
	out := make([]string, 0, len(pf.Assignments))
	for _, a := range pf.Assignments {
		out = append(out, a.RoleID)
	}
	return out
}
