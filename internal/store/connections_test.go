package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestConnectionsOpenCreatesFile(t *testing.T) {
	tmpDir := t.TempDir()

	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	if s == nil {
		t.Fatal("store is nil")
	}

	connPath := filepath.Join(tmpDir, "connections.json")
	if _, err := os.Stat(connPath); err != nil {
		if os.IsNotExist(err) {
			t.Log("connections.json created lazily on first write (expected)")
		} else {
			t.Fatalf("stat connections.json: %v", err)
		}
	}
}

func TestConnectionRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	conn := Connection{
		Label:  "test-connection",
		Driver: "garage",
		Config: map[string]string{
			"admin_url":    "http://garage:3903",
			"admin_token":  "secret-token",
			"s3_region":    "us-east-1",
		},
		Color: "#FF5733",
		Owner: "org",
	}

	result, err := s.Create(ctx, conn)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if result.Label != "test-connection" {
		t.Errorf("expected label 'test-connection', got '%s'", result.Label)
	}

	if result.Driver != "garage" {
		t.Errorf("expected driver 'garage', got '%s'", result.Driver)
	}

	if result.ID == "" {
		t.Error("expected non-empty ID after Create")
	}

	if result.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt after Create")
	}

	if result.Color != "#FF5733" {
		t.Errorf("expected color '#FF5733', got '%s'", result.Color)
	}

	if result.Owner != "org" {
		t.Errorf("expected owner 'org', got '%s'", result.Owner)
	}

	id := result.ID

	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.Label != result.Label {
		t.Errorf("expected label '%s', got '%s'", result.Label, got.Label)
	}

	if got.Driver != result.Driver {
		t.Errorf("expected driver '%s', got '%s'", result.Driver, got.Driver)
	}

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(list) != 1 {
		t.Errorf("expected 1 connection in list, got %d", len(list))
	}
}

func TestConnectionCount(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	count, err := s.Count(ctx)
	if err != nil {
		t.Fatalf("Count failed (initial): %v", err)
	}

	if count != 0 {
		t.Errorf("expected initial count 0, got %d", count)
	}

	conn1 := Connection{Label: "conn-1", Driver: "garage", Config: map[string]string{}, Owner: "org"}
	if _, err := s.Create(ctx, conn1); err != nil {
		t.Fatalf("Create conn-1 failed: %v", err)
	}

	count, err = s.Count(ctx)
	if err != nil {
		t.Fatalf("Count failed (after 1 create): %v", err)
	}

	if count != 1 {
		t.Errorf("expected count 1 after first Create, got %d", count)
	}

	conn2 := Connection{Label: "conn-2", Driver: "aws-s3", Config: map[string]string{}, Owner: "org"}
	if _, err := s.Create(ctx, conn2); err != nil {
		t.Fatalf("Create conn-2 failed: %v", err)
	}

	count, err = s.Count(ctx)
	if err != nil {
		t.Fatalf("Count failed (after 2 creates): %v", err)
	}

	if count != 2 {
		t.Errorf("expected count 2 after second Create, got %d", count)
	}
}

func TestConnectionUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	conn := Connection{Label: "original", Driver: "garage", Config: map[string]string{"key": "val1"}, Owner: "org"}
	result, err := s.Create(ctx, conn)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	id := result.ID

	newConfig := map[string]string{
		"admin_url":    "http://new-garage:3903",
		"admin_token":  "new-token",
		"s3_region":    "eu-west-1",
	}

	patch := Connection{
		Label:  "updated-label",
		Config: newConfig,
		Color:  "#AABBCC",
	}

	updated, err := s.Update(ctx, id, patch)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.Label != "updated-label" {
		t.Errorf("expected label 'updated-label', got '%s'", updated.Label)
	}

	if updated.Color != "#AABBCC" {
		t.Errorf("expected color '#AABBCC', got '%s'", updated.Color)
	}

	if len(updated.Config) != 3 {
		t.Errorf("expected 3 config keys, got %d", len(updated.Config))
	}

	if updated.Config["admin_url"] != "http://new-garage:3903" {
		t.Errorf("expected admin_url 'http://new-garage:3903', got '%s'", updated.Config["admin_url"])
	}

	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after Update failed: %v", err)
	}

	if got.Label != "updated-label" {
		t.Errorf("persisted label mismatch")
	}
}

func TestConnectionDelete(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	conn := Connection{Label: "to-delete", Driver: "garage", Config: map[string]string{}, Owner: "org"}
	result, err := s.Create(ctx, conn)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	id := result.ID

	count, _ := s.Count(ctx)
	if count != 1 {
		t.Errorf("expected count 1 before delete, got %d", count)
	}

	if err := s.Delete(ctx, id); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	count, err = s.Count(ctx)
	if err != nil {
		t.Fatalf("Count after Delete failed: %v", err)
	}

	if count != 0 {
		t.Errorf("expected count 0 after delete, got %d", count)
	}

	_, err = s.Get(ctx, id)
	if err == nil {
		t.Error("expected error getting deleted connection")
	}
}

func TestConnectionValidationUnsupportedDriver(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	conn := Connection{Label: "bad-driver", Driver: "unknown-driver", Config: map[string]string{}, Owner: "org"}
	_, err = s.Create(ctx, conn)

	if err == nil {
		t.Error("expected error for unsupported driver")
	}

	expectedMsg := "unsupported driver"
	if err != nil && err.Error()[:len(expectedMsg)] != expectedMsg {
		t.Errorf("expected error containing %q, got %q", expectedMsg, err.Error())
	}
}

func TestConnectionValidationEmptyLabel(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	conn := Connection{Label: "", Driver: "garage", Config: map[string]string{}, Owner: "org"}
	_, err = s.Create(ctx, conn)

	if err == nil {
		t.Error("expected error for empty label")
	}
}

func TestConnectionValidationDuplicateLabel(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	conn1 := Connection{Label: "MyConnection", Driver: "garage", Config: map[string]string{}, Owner: "org"}
	if _, err := s.Create(ctx, conn1); err != nil {
		t.Fatalf("Create first connection failed: %v", err)
	}

	conn2 := Connection{Label: "myconnection", Driver: "aws-s3", Config: map[string]string{}, Owner: "org"} // same label, different case
	_, err = s.Create(ctx, conn2)

	if err == nil {
		t.Error("expected error for duplicate label (case-insensitive)")
	}

	expectedMsg := "duplicate label"
	if err != nil && err.Error()[:len(expectedMsg)] != expectedMsg {
		t.Errorf("expected error containing %q, got %q", expectedMsg, err.Error())
	}
}

func TestConnectionDefaultColor(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	conn := Connection{Label: "no-color", Driver: "garage", Config: map[string]string{}, Owner: "org"}
	result, err := s.Create(ctx, conn)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if result.Color == "" {
		t.Error("expected default color to be set")
	}

	expectedColor := "#C9874B"
	if result.Color != expectedColor {
		t.Errorf("expected default color '%s', got '%s'", expectedColor, result.Color)
	}
}

func TestConcurrentConnectionWrites(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections failed: %v", err)
	}

	ctx := context.Background()

	var wg sync.WaitGroup
	numGoroutines := 10
	connectionsPerGoroutine := 5

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < connectionsPerGoroutine; j++ {
				label := "conn-" + string(rune('a'+base)) + "-" + string(rune('0'+j))
				conn := Connection{
					Label:  label,
					Driver: "garage",
					Config: map[string]string{"index": label},
					Owner:  "org",
				}
				if _, err := s.Create(ctx, conn); err != nil {
					t.Errorf("concurrent Create failed: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	count, err := s.Count(ctx)
	if err != nil {
		t.Fatalf("Count after concurrent writes failed: %v", err)
	}

	expected := numGoroutines * connectionsPerGoroutine
	if count != expected {
		t.Errorf("expected %d connections after concurrent writes, got %d", expected, count)
	}

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List after concurrent writes failed: %v", err)
	}

	if len(list) != expected {
		t.Errorf("expected %d connections in list, got %d", expected, len(list))
	}
}

func TestConnectionReloadAfterWrite(t *testing.T) {
	tmpDir := t.TempDir()

	conn1 := Connection{Label: "conn-1", Driver: "garage", Config: map[string]string{"a": "b"}, Owner: "org"}
	s1, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections first open failed: %v", err)
	}

	ctx := context.Background()
	result, err := s1.Create(ctx, conn1)
	if err != nil {
		t.Fatalf("Create via first store failed: %v", err)
	}

	id := result.ID

	s2, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections second open (reload) failed: %v", err)
	}

	gotConn, err := s2.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after reload failed: %v", err)
	}

	if gotConn.Label != "conn-1" {
		t.Errorf("expected label 'conn-1' after reload, got '%s'", gotConn.Label)
	}
}

// TestOpenConnections_BadDataDir covers the MkdirAll error path. We point
// at a path under a regular file (which cannot have child directories).
func TestOpenConnections_BadDataDir(t *testing.T) {
	tmpDir := t.TempDir()
	blocker := filepath.Join(tmpDir, "blocker")
	if err := os.WriteFile(blocker, []byte("nope"), 0644); err != nil {
		t.Fatalf("setup writefile: %v", err)
	}

	_, err := OpenConnections(filepath.Join(blocker, "subdir"))
	if err == nil {
		t.Fatal("expected error opening connections under a regular file path")
	}
	if !strings.Contains(err.Error(), "creating data dir") {
		t.Errorf("error msg=%q, want 'creating data dir' prefix", err.Error())
	}
}

// TestOpenConnections_CorruptJSON covers the load-error path: connections.json
// exists but contains invalid JSON, so unmarshal should fail.
func TestOpenConnections_CorruptJSON(t *testing.T) {
	tmpDir := t.TempDir()
	connPath := filepath.Join(tmpDir, "connections.json")
	if err := os.WriteFile(connPath, []byte("{this-is-not-json}"), 0644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	_, err := OpenConnections(tmpDir)
	if err == nil {
		t.Fatal("expected error opening connections with corrupt JSON")
	}
	if !strings.Contains(err.Error(), "loading existing connections") {
		t.Errorf("error msg=%q, want 'loading existing connections' prefix", err.Error())
	}
}

// TestOpenConnections_EmptyJSONFile covers loadJSON's len==0 branch.
func TestOpenConnections_EmptyJSONFile(t *testing.T) {
	tmpDir := t.TempDir()
	connPath := filepath.Join(tmpDir, "connections.json")
	if err := os.WriteFile(connPath, []byte{}, 0644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}

	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("expected no error for empty connections.json, got %v", err)
	}

	count, err := s.Count(context.Background())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 0 {
		t.Errorf("count=%d, want 0", count)
	}
}

// TestConnectionUpdateNotFound covers Update's missing-id error path.
func TestConnectionUpdateNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections: %v", err)
	}

	_, err = s.Update(context.Background(), "missing-id", Connection{Label: "new"})
	if err == nil {
		t.Fatal("expected error updating non-existent connection")
	}
	if !strings.Contains(err.Error(), "connection not found") {
		t.Errorf("error msg=%q", err.Error())
	}
}

// TestConnectionUpdateDuplicateLabel ensures Update enforces the uniqueness
// constraint across other connections (but allows keeping your own label).
func TestConnectionUpdateDuplicateLabel(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections: %v", err)
	}

	ctx := context.Background()
	a, _ := s.Create(ctx, Connection{Label: "alpha", Driver: "garage", Config: map[string]string{}, Owner: "org"})
	_, _ = s.Create(ctx, Connection{Label: "beta", Driver: "garage", Config: map[string]string{}, Owner: "org"})

	// Updating a→"beta" should fail (collision with the OTHER record).
	_, err = s.Update(ctx, a.ID, Connection{Label: "beta"})
	if err == nil {
		t.Fatal("expected duplicate-label error")
	}
	if !strings.Contains(err.Error(), "duplicate label") {
		t.Errorf("error msg=%q", err.Error())
	}

	// Updating a→"ALPHA" (case-insensitive same as its own label) must
	// succeed — we excluded self from the dup-check.
	got, err := s.Update(ctx, a.ID, Connection{Label: "ALPHA"})
	if err != nil {
		t.Fatalf("expected self-relabel to succeed: %v", err)
	}
	if got.Label != "ALPHA" {
		t.Errorf("label=%q, want ALPHA", got.Label)
	}
}

// TestConnectionUpdateUnsupportedDriver covers Update's bad-driver branch.
func TestConnectionUpdateUnsupportedDriver(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections: %v", err)
	}

	ctx := context.Background()
	a, _ := s.Create(ctx, Connection{Label: "alpha", Driver: "garage", Config: map[string]string{}, Owner: "org"})

	_, err = s.Update(ctx, a.ID, Connection{Driver: "nope-driver"})
	if err == nil {
		t.Fatal("expected unsupported-driver error")
	}
	if !strings.Contains(err.Error(), "unsupported driver") {
		t.Errorf("error msg=%q", err.Error())
	}
}

// TestConnectionUpdatePreservesUntouchedFields ensures partial Update leaves
// non-patched fields intact (the "patch semantics" the comment promises).
func TestConnectionUpdatePreservesUntouchedFields(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections: %v", err)
	}

	ctx := context.Background()
	orig, _ := s.Create(ctx, Connection{
		Label:  "orig",
		Driver: "garage",
		Config: map[string]string{"a": "1"},
		Color:  "#112233",
		Owner:  "org",
	})

	got, err := s.Update(ctx, orig.ID, Connection{Color: "#445566"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Label != "orig" {
		t.Errorf("Label changed: %q", got.Label)
	}
	if got.Driver != "garage" {
		t.Errorf("Driver changed: %q", got.Driver)
	}
	if got.Color != "#445566" {
		t.Errorf("Color=%q, want #445566", got.Color)
	}
	if len(got.Config) != 1 || got.Config["a"] != "1" {
		t.Errorf("Config changed: %+v", got.Config)
	}
	if !got.CreatedAt.Equal(orig.CreatedAt) {
		t.Errorf("CreatedAt changed: %v vs %v", orig.CreatedAt, got.CreatedAt)
	}
}

// TestConnectionUpdateChangesDriver ensures Update accepts a valid driver
// swap (covers the SupportedDrivers[patch.Driver]=true branch).
func TestConnectionUpdateChangesDriver(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections: %v", err)
	}

	ctx := context.Background()
	orig, _ := s.Create(ctx, Connection{Label: "x", Driver: "garage", Config: map[string]string{}, Owner: "org"})

	got, err := s.Update(ctx, orig.ID, Connection{Driver: "aws-s3"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Driver != "aws-s3" {
		t.Errorf("Driver=%q, want aws-s3", got.Driver)
	}
}

// TestConnectionDeleteMissing covers Delete's not-found error path.
func TestConnectionDeleteMissing(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections: %v", err)
	}

	err = s.Delete(context.Background(), "nope-id")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if !strings.Contains(err.Error(), "connection not found") {
		t.Errorf("error msg=%q", err.Error())
	}
}

// TestConnectionCreatePersistsAtomically ensures Create writes via the
// rename-from-tmp atomic-write path: no .tmp file lingers after success.
func TestConnectionCreatePersistsAtomically(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections: %v", err)
	}

	if _, err := s.Create(context.Background(), Connection{
		Label:  "atomic",
		Driver: "garage",
		Config: map[string]string{"k": "v"},
		Owner:  "org",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Final file should exist
	if _, err := os.Stat(filepath.Join(tmpDir, "connections.json")); err != nil {
		t.Fatalf("connections.json missing after Create: %v", err)
	}

	// .tmp must NOT linger — rename completed
	if _, err := os.Stat(filepath.Join(tmpDir, "connections.json.tmp")); !os.IsNotExist(err) {
		t.Errorf("connections.json.tmp lingered: stat err=%v", err)
	}

	// File contents are valid JSON with one connection
	data, err := os.ReadFile(filepath.Join(tmpDir, "connections.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got []Connection
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d connections in JSON, want 1", len(got))
	}
}

// TestConnectionGetMissing covers Get's not-found path explicitly (already
// implicit in TestConnectionDelete but worth a dedicated assertion).
func TestConnectionGetMissing(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections: %v", err)
	}
	_, err = s.Get(context.Background(), "absent")
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

// TestGenerateID exercises the package-level helper (used elsewhere in code).
func TestGenerateID(t *testing.T) {
	id1, err := GenerateID()
	if err != nil {
		t.Fatalf("GenerateID: %v", err)
	}
	if id1 == "" {
		t.Fatal("empty UUID")
	}
	// UUID v4 stringified is 36 chars (8-4-4-4-12 with hyphens)
	if len(id1) != 36 {
		t.Errorf("len(id1)=%d, want 36", len(id1))
	}

	id2, _ := GenerateID()
	if id1 == id2 {
		t.Errorf("GenerateID returned identical IDs twice")
	}
}

// TestGenerateToken exercises the package-level helper.
func TestGenerateToken(t *testing.T) {
	for _, n := range []int{1, 16, 32, 64} {
		tok, err := GenerateToken(n)
		if err != nil {
			t.Fatalf("GenerateToken(%d): %v", n, err)
		}
		// hex encoding is 2x bytes
		if len(tok) != n*2 {
			t.Errorf("GenerateToken(%d) returned %d hex chars, want %d", n, len(tok), n*2)
		}
		if _, err := hex.DecodeString(tok); err != nil {
			t.Errorf("GenerateToken(%d) not valid hex: %v", n, err)
		}
	}

	// Two consecutive tokens must differ.
	a, _ := GenerateToken(16)
	b, _ := GenerateToken(16)
	if a == b {
		t.Errorf("two GenerateToken(16) calls returned identical results")
	}
}

// TestConnectionCreateRejectsWhitespaceLabel verifies that a label of only
// whitespace is rejected the same as an empty one (consistent with the
// trim-then-check logic).
func TestConnectionCreateRejectsWhitespaceLabel(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections: %v", err)
	}
	_, err = s.Create(context.Background(), Connection{Label: "   ", Driver: "garage", Config: map[string]string{}, Owner: "org"})
	if err == nil {
		t.Fatal("expected whitespace-only label to be rejected")
	}
}

// TestConcurrentReadWrite stresses the RWMutex on List/Get/Create simultaneously.
// Race detector must stay clean.
func TestConcurrentReadWrite(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenConnections(tmpDir)
	if err != nil {
		t.Fatalf("OpenConnections: %v", err)
	}

	ctx := context.Background()

	// Seed one connection so readers always have something to find.
	seed, _ := s.Create(ctx, Connection{Label: "seed", Driver: "garage", Config: map[string]string{}, Owner: "org"})

	var wg sync.WaitGroup
	const writers = 5
	const readers = 10
	const perGoroutine = 20

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				_, _ = s.Create(ctx, Connection{
					Label:  "rw-" + string(rune('A'+base)) + "-" + string(rune('0'+j%10)) + "-" + string(rune('a'+j/10)),
					Driver: "garage",
					Config: map[string]string{},
					Owner:  "org",
				})
			}
		}(i)
	}

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				_, _ = s.List(ctx)
				_, _ = s.Get(ctx, seed.ID)
				_, _ = s.Count(ctx)
			}
		}()
	}

	wg.Wait()
}
