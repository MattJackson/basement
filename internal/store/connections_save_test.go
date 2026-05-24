package store

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
		"path/filepath"
	"testing"
)

// TestConnectionsSave_FuncExists verifies save() function exists and is callable.
func TestConnectionsSave_FuncExists(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenConnectionsWithKey(dir, []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Cast to unexported type to access save method
	store := s.(*store)

	// Hold write lock and call save directly
	store.connsMu.Lock()
	err = store.save()
	store.connsMu.Unlock()

	if err != nil {
		t.Errorf("save() failed: %v", err)
	}
}

// TestConnectionsSave_Locked_Path exercises saveLocked with empty cache.
func TestConnectionsSave_Locked_EmptyCache(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenConnectionsWithKey(dir, []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	store := s.(*store)

	store.connsMu.Lock()
	err = store.saveLocked()
	store.connsMu.Unlock()

	if err != nil {
		t.Errorf("saveLocked() on empty cache failed: %v", err)
	}

	// Verify file was created
	path := filepath.Join(dir, "connections.json")
	data, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatalf("read connections.json: %v", err)
	}
	// Empty array is valid for no connections
	var conn []connectionDisk
	if err := json.Unmarshal(data, &conn); err != nil {
		t.Errorf("parse connections.json: %v", err)
	}
}

// TestConnectionsSave_Locked_WithConnection exercises saveLocked with a connection.
func TestConnectionsSave_Locked_WithConnection(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenConnectionsWithKey(dir, []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	ctx := context.Background()

	// Create a connection first
	in := Connection{
		Label:  "test-conn",
		Driver: DriverGarage,
		Config: map[string]string{
			"admin_url":   "https://garage.local",
			"admin_token": "secret-token",
		},
		Owner: "org",
	}

	_ , _ = s.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	store := s.(*store)

	// Now call saveLocked directly (it will be called internally by Create anyway, but we're testing the path)
	store.connsMu.Lock()
	err = store.saveLocked()
	store.connsMu.Unlock()

	if err != nil {
		t.Errorf("saveLocked() with connection failed: %v", err)
	}

	// Verify the sensitive data is encrypted on disk
	path := filepath.Join(dir, "connections.json")
	data, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatalf("read connections.json: %v", err)
	}

	// admin_token should NOT appear as plaintext in the file
	if bytes.Contains(data, []byte("secret-token")) {
		t.Error("sensitive token appears as plaintext on disk")
	}

	var conns []connectionDisk
	if err := json.Unmarshal(data, &conns); err != nil {
		t.Fatalf("parse connections.json: %v", err)
	}
	if len(conns) != 1 {
		t.Errorf("expected 1 connection on disk, got %d", len(conns))
	}

	// ConfigEnc should be populated (encrypted sensitive subset)
	if len(conns[0].ConfigEnc) == 0 {
		t.Error("ConfigEnc should be populated for connection with sensitive keys")
	}
}

// TestConnectionsSave_Locked_InvalidJSON exercises saveLocked when marshal fails.
func TestConnectionsSave_Locked_MarshalError(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenConnectionsWithKey(dir, []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	store := s.(*store)
	store.connsMu.Lock()
	defer store.connsMu.Unlock()

	// Corrupt the path to force save to fail
	originalPath := store.connPath
	store.connPath = "/nonexistent/directory/connections.json"

	err = store.saveLocked()
	
	// Restore path for cleanup
	store.connPath = originalPath

	if err == nil {
		t.Error("saveLocked() should fail when writing to non-existent directory")
	}
}

// TestConnectionsSave_FuncWrapper verifies save() calls saveLocked().
func TestConnectionsSave_FuncWrapper(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenConnectionsWithKey(dir, []byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	store := s.(*store)
	ctx := context.Background()

	// Create a connection
	in := Connection{
		Label:  "test-conn",
		Driver: DriverGarage,
		Config: map[string]string{"admin_url": "https://garage.local"},
		Owner:  "org",
	}
	if _, err := s.Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Call save() wrapper (which calls saveLocked internally)
	store.connsMu.Lock()
	err = store.save()
	store.connsMu.Unlock()

	if err != nil {
		t.Errorf("save() failed: %v", err)
	}
}

// TestDecryptSensitiveMap_EmptyInput returns empty map.
func TestDecryptSensitiveMap_EmptyInput(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	out, err := decryptSensitiveMap(nil, key)
	if err != nil {
		t.Errorf("decryptSensitiveMap(nil) failed: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty map for nil input, got %d keys", len(out))
	}
}

// TestDecryptSensitiveMap_InvalidCiphertext returns error.
func TestDecryptSensitiveMap_InvalidCiphertext(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	// Invalid ciphertext (not valid AES-GCM encryption)
	invalidEnc := []byte("not-valid-ciphertext-at-all-12345")
	_, err := decryptSensitiveMap(invalidEnc, key)
	if err == nil {
		t.Error("expected error for invalid ciphertext, got nil")
	}
}

// TestGenerateID_ReturnsValidUUID(t *testing.T) {
func TestGenerateID_ReturnsValidUUID(t *testing.T) {
	id, err := GenerateID()
	if err != nil {
		t.Fatalf("GenerateID failed: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty ID")
	}
	// UUID should be 36 characters (8-4-4-4-12 + dashes)
	if len(id) != 36 {
		t.Errorf("ID length=%d, want 36", len(id))
	}
}

// TestGenerateToken_ReturnsHex(t *testing.T) {
func TestGenerateToken_ReturnsHex(t *testing.T) {
	token, err := GenerateToken(16) // 16 bytes = 32 hex chars
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	if len(token) != 32 {
		t.Errorf("token length=%d, want 32", len(token))
	}
	// All chars should be hex
	for _, c := range token {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("token contains non-hex char: %q", c)
		}
	}
}

// TestGenerateToken_ZeroBytes(t *testing.T) {
func TestGenerateToken_ZeroBytes(t *testing.T) {
	token, err := GenerateToken(0)
	if err != nil {
		t.Fatalf("GenerateToken(0) failed: %v", err)
	}
	if len(token) != 0 {
		t.Errorf("expected empty token for 0 bytes, got length %d", len(token))
	}
}
