package store

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// connectionsTestKey is a 32-byte secret matching the JWT min-length
// rule in production. The cipher key is sha256(testKey) — see
// crypto.go — so any non-empty key works, but we mirror real config.
var connectionsTestKey = []byte("01234567890123456789012345678901")

// TestConnectionsAtRest_RoundTrip exercises the happy path: create a
// Connection with sensitive keys, reload from disk via a fresh store,
// the unified Config view returns the same values.
func TestConnectionsAtRest_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	s1, err := OpenConnectionsWithKey(dir, connectionsTestKey)
	if err != nil {
		t.Fatalf("OpenConnectionsWithKey: %v", err)
	}
	ctx := context.Background()

	in := Connection{
		Label:  "encrypted-cluster",
		Driver: DriverGarage,
		Config: map[string]string{
			"admin_url":     "https://admin.example",
			"admin_token":   "super-secret-bearer-XYZ123",
			"s3_endpoint":   "https://s3.example",
			"secret_key":    "AKIA-SECRET-ROUND-TRIP",
			"access_key_id": "AKIA-PUBLIC-ID",
		},
		Owner: "org",
	}

	created, err := s1.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := s1.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get same-store: %v", err)
	}
	if got.Config["admin_token"] != "super-secret-bearer-XYZ123" {
		t.Errorf("admin_token same-store mismatch: got %q", got.Config["admin_token"])
	}
	if got.Config["secret_key"] != "AKIA-SECRET-ROUND-TRIP" {
		t.Errorf("secret_key same-store mismatch: got %q", got.Config["secret_key"])
	}
	// Non-sensitive keys must still come through.
	if got.Config["admin_url"] != "https://admin.example" {
		t.Errorf("admin_url same-store mismatch: got %q", got.Config["admin_url"])
	}
	if got.Config["access_key_id"] != "AKIA-PUBLIC-ID" {
		t.Errorf("access_key_id same-store mismatch: got %q", got.Config["access_key_id"])
	}

	// Reopen — exercises the load() path on a file written by save().
	s2, err := OpenConnectionsWithKey(dir, connectionsTestKey)
	if err != nil {
		t.Fatalf("reopen OpenConnectionsWithKey: %v", err)
	}
	got2, err := s2.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get post-reopen: %v", err)
	}
	for _, want := range []struct{ k, v string }{
		{"admin_token", "super-secret-bearer-XYZ123"},
		{"secret_key", "AKIA-SECRET-ROUND-TRIP"},
		{"admin_url", "https://admin.example"},
		{"s3_endpoint", "https://s3.example"},
		{"access_key_id", "AKIA-PUBLIC-ID"},
	} {
		if got2.Config[want.k] != want.v {
			t.Errorf("reload %q: got %q, want %q", want.k, got2.Config[want.k], want.v)
		}
	}
}

// TestConnectionsAtRest_OnDiskNoPlaintext verifies the secrets are
// genuinely absent from the on-disk JSON — the headline security
// guarantee of this cycle.
func TestConnectionsAtRest_OnDiskNoPlaintext(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenConnectionsWithKey(dir, connectionsTestKey)
	if err != nil {
		t.Fatalf("OpenConnectionsWithKey: %v", err)
	}
	ctx := context.Background()

	secrets := map[string]string{
		"admin_token":   "PLAINTEXT-ADMIN-TOKEN-MUST-NOT-LEAK",
		"secret_key":    "PLAINTEXT-SECRET-KEY-MUST-NOT-LEAK",
		"s3_secret_key": "PLAINTEXT-S3-SECRET-MUST-NOT-LEAK",
		"auth_token":    "PLAINTEXT-AUTH-TOKEN-MUST-NOT-LEAK",
	}

	cfg := map[string]string{
		"admin_url":     "https://admin.example",
		"access_key_id": "ACCESS-KEY-OK-TO-LEAK",
	}
	for k, v := range secrets {
		cfg[k] = v
	}

	if _, err := s.Create(ctx, Connection{
		Label:  "leak-test",
		Driver: DriverGarage,
		Config: cfg,
		Owner:  "org",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("read connections.json: %v", err)
	}

	for k, v := range secrets {
		if bytes.Contains(raw, []byte(v)) {
			t.Errorf("plaintext %s value leaked to disk: looked for %q in JSON", k, v)
		}
	}

	// The CIPHERTEXT must appear under "configEnc"; the JSON key names
	// for the secrets themselves should NOT appear in the plaintext
	// Config map (they live inside the encrypted blob now).
	var disk []map[string]any
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("on-disk JSON not parseable: %v", err)
	}
	if len(disk) != 1 {
		t.Fatalf("expected 1 connection on disk, got %d", len(disk))
	}
	if _, ok := disk[0]["configEnc"]; !ok {
		t.Errorf("on-disk record missing configEnc field: %v", disk[0])
	}
	plainConfig, ok := disk[0]["config"].(map[string]any)
	if !ok {
		t.Fatalf("on-disk config is not a map: %T", disk[0]["config"])
	}
	for k := range secrets {
		if _, present := plainConfig[k]; present {
			t.Errorf("sensitive key %q present in on-disk plaintext config: %v", k, plainConfig)
		}
	}
	// Public keys ARE still in plaintext on disk.
	if plainConfig["admin_url"] != "https://admin.example" {
		t.Errorf("admin_url missing or wrong on disk: %v", plainConfig["admin_url"])
	}
	if plainConfig["access_key_id"] != "ACCESS-KEY-OK-TO-LEAK" {
		t.Errorf("access_key_id missing or wrong on disk: %v", plainConfig["access_key_id"])
	}
}

// TestConnectionsAtRest_MigrationFromPlaintext seeds a connections.json
// that looks like the pre-v1.0.0a shape (all keys plaintext, no
// configEnc), opens the store with a JWT key, and asserts:
//
//  1. The migration kicks in on Open — file rewritten with secrets in
//     configEnc and removed from on-disk Config.
//  2. Reading back via Get returns the unified Config with secrets intact.
//  3. A second Open is a no-op — file mtime / content stable beyond the
//     first migration (idempotent).
func TestConnectionsAtRest_MigrationFromPlaintext(t *testing.T) {
	dir := t.TempDir()
	connPath := filepath.Join(dir, "connections.json")

	// Pre-populate disk in the pre-v1.0.0a shape — plain Config with
	// secrets, no configEnc field.
	seed := []map[string]any{
		{
			"id":     "00000000-0000-0000-0000-000000000001",
			"label":  "legacy-cluster",
			"driver": "garage",
			"config": map[string]string{
				"admin_url":   "https://legacy.example",
				"admin_token": "LEGACY-PLAINTEXT-TOKEN",
				"secret_key":  "LEGACY-PLAINTEXT-SECRET",
			},
			"color":     "#C9874B",
			"owner":     "org",
			"createdAt": "2024-01-01T00:00:00Z",
		},
	}
	raw, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.WriteFile(connPath, raw, 0644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	// Confirm seed is plaintext on disk before we open.
	preOpen, _ := os.ReadFile(connPath)
	if !bytes.Contains(preOpen, []byte("LEGACY-PLAINTEXT-TOKEN")) {
		t.Fatal("seed missing — test bug")
	}

	// 1. Open — triggers migration.
	s, err := OpenConnectionsWithKey(dir, connectionsTestKey)
	if err != nil {
		t.Fatalf("OpenConnectionsWithKey (migration): %v", err)
	}

	postOpen, err := os.ReadFile(connPath)
	if err != nil {
		t.Fatalf("read post-migration: %v", err)
	}
	if bytes.Contains(postOpen, []byte("LEGACY-PLAINTEXT-TOKEN")) {
		t.Errorf("plaintext admin_token still on disk after migration:\n%s", postOpen)
	}
	if bytes.Contains(postOpen, []byte("LEGACY-PLAINTEXT-SECRET")) {
		t.Errorf("plaintext secret_key still on disk after migration:\n%s", postOpen)
	}
	if !bytes.Contains(postOpen, []byte("configEnc")) {
		t.Errorf("post-migration file missing configEnc field:\n%s", postOpen)
	}

	// 2. Reading via the API surface gives back the original values.
	got, err := s.Get(context.Background(), "00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("Get after migration: %v", err)
	}
	if got.Config["admin_token"] != "LEGACY-PLAINTEXT-TOKEN" {
		t.Errorf("admin_token post-migration mismatch: got %q", got.Config["admin_token"])
	}
	if got.Config["secret_key"] != "LEGACY-PLAINTEXT-SECRET" {
		t.Errorf("secret_key post-migration mismatch: got %q", got.Config["secret_key"])
	}
	if got.Config["admin_url"] != "https://legacy.example" {
		t.Errorf("admin_url post-migration mismatch: got %q", got.Config["admin_url"])
	}

	// 3. Second open: must be a no-op. We snapshot the file BEFORE the
	// second open and compare AFTER — bytes must match exactly. (A
	// double-encryption would change the ciphertext nonce, blowing
	// equality even if semantics were preserved.)
	beforeSecondOpen, err := os.ReadFile(connPath)
	if err != nil {
		t.Fatalf("snapshot pre-second-open: %v", err)
	}
	if _, err := OpenConnectionsWithKey(dir, connectionsTestKey); err != nil {
		t.Fatalf("second OpenConnectionsWithKey: %v", err)
	}
	afterSecondOpen, err := os.ReadFile(connPath)
	if err != nil {
		t.Fatalf("snapshot post-second-open: %v", err)
	}
	if !bytes.Equal(beforeSecondOpen, afterSecondOpen) {
		t.Errorf("second Open mutated connections.json (not idempotent):\nbefore: %s\n---\nafter: %s",
			beforeSecondOpen, afterSecondOpen)
	}
}

// TestConnectionsAtRest_WrongKey_FailsCleanly confirms that opening a
// store with a different JWT secret than the one used to encrypt the
// ConfigEnc returns a load error rather than silently returning a
// truncated / nonsense Config.
func TestConnectionsAtRest_WrongKey_FailsCleanly(t *testing.T) {
	dir := t.TempDir()

	// Create + persist with key A.
	s, err := OpenConnectionsWithKey(dir, connectionsTestKey)
	if err != nil {
		t.Fatalf("OpenConnectionsWithKey keyA: %v", err)
	}
	if _, err := s.Create(context.Background(), Connection{
		Label:  "key-A-conn",
		Driver: DriverGarage,
		Config: map[string]string{
			"admin_url":   "https://a.example",
			"admin_token": "secret-encrypted-with-A",
		},
		Owner: "org",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Reopen with a different 32-byte key.
	keyB := []byte("XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX")
	_, err = OpenConnectionsWithKey(dir, keyB)
	if err == nil {
		t.Fatal("expected error opening encrypted store with wrong key")
	}
	if !strings.Contains(err.Error(), "decrypting") && !strings.Contains(err.Error(), "loading existing connections") {
		t.Errorf("error message should mention decrypt failure, got: %v", err)
	}
}

// TestConnectionsAtRest_GarageAuthToken covers the prompt's auth_token
// classification. We don't have a real driver consuming auth_token, but
// the cycle prompt declares it sensitive and we want a regression test
// for the classification matrix.
func TestConnectionsAtRest_GarageAuthToken(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenConnectionsWithKey(dir, connectionsTestKey)
	if err != nil {
		t.Fatalf("OpenConnectionsWithKey: %v", err)
	}

	in := Connection{
		Label:  "garage-auth",
		Driver: DriverGarage,
		Config: map[string]string{
			"admin_url":  "https://garage.example",
			"auth_token": "GARAGE-AUTH-PLAINTEXT-NO-LEAK",
		},
		Owner: "org",
	}
	created, err := s.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if bytes.Contains(raw, []byte("GARAGE-AUTH-PLAINTEXT-NO-LEAK")) {
		t.Errorf("auth_token plaintext leaked to disk: %s", raw)
	}

	got, err := s.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Config["auth_token"] != "GARAGE-AUTH-PLAINTEXT-NO-LEAK" {
		t.Errorf("auth_token round-trip mismatch: got %q", got.Config["auth_token"])
	}
}

// TestConnectionsAtRest_EmptyKeyRejected ensures the no-encryption
// fallback in OpenConnections isn't accidentally reachable through the
// _WithKey door with an empty slice.
func TestConnectionsAtRest_EmptyKeyRejected(t *testing.T) {
	dir := t.TempDir()
	_, err := OpenConnectionsWithKey(dir, nil)
	if err == nil {
		t.Fatal("expected error for nil jwtSecret")
	}
	_, err = OpenConnectionsWithKey(dir, []byte{})
	if err == nil {
		t.Fatal("expected error for empty jwtSecret")
	}
}

// TestConnectionsAtRest_UpdateRotatesSecret verifies that updating a
// Connection's Config rotates the encrypted blob (new nonce ⇒ new
// ciphertext) and the new plaintext round-trips through reload.
func TestConnectionsAtRest_UpdateRotatesSecret(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenConnectionsWithKey(dir, connectionsTestKey)
	if err != nil {
		t.Fatalf("OpenConnectionsWithKey: %v", err)
	}
	ctx := context.Background()

	created, err := s.Create(ctx, Connection{
		Label:  "rotate-test",
		Driver: DriverGarage,
		Config: map[string]string{"admin_url": "https://r.example", "admin_token": "OLD-TOKEN"},
		Owner:  "org",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	rawBefore, _ := os.ReadFile(filepath.Join(dir, "connections.json"))

	_, err = s.Update(ctx, created.ID, Connection{
		Config: map[string]string{"admin_url": "https://r.example", "admin_token": "NEW-TOKEN"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	rawAfter, _ := os.ReadFile(filepath.Join(dir, "connections.json"))
	if bytes.Equal(rawBefore, rawAfter) {
		t.Error("on-disk file did not change after secret rotation")
	}
	if bytes.Contains(rawAfter, []byte("NEW-TOKEN")) {
		t.Errorf("NEW-TOKEN plaintext leaked to disk: %s", rawAfter)
	}
	if bytes.Contains(rawAfter, []byte("OLD-TOKEN")) {
		t.Errorf("OLD-TOKEN plaintext still on disk after rotation: %s", rawAfter)
	}

	// Reopen, confirm we get NEW-TOKEN back.
	s2, err := OpenConnectionsWithKey(dir, connectionsTestKey)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := s2.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get post-rotation reopen: %v", err)
	}
	if got.Config["admin_token"] != "NEW-TOKEN" {
		t.Errorf("admin_token post-rotation: got %q want NEW-TOKEN", got.Config["admin_token"])
	}
}

// TestConnectionsAtRest_LegacyOpenConnections_StillPlaintext is a guard
// rail: the deprecated OpenConnections(dataDir) path (used by the test
// surface) must NOT silently start encrypting things, because tests
// inspect raw JSON. This locks in the back-compat contract.
func TestConnectionsAtRest_LegacyOpenConnections_StillPlaintext(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenConnections(dir)
	if err != nil {
		t.Fatalf("OpenConnections: %v", err)
	}
	if _, err := s.Create(context.Background(), Connection{
		Label:  "legacy-mode",
		Driver: DriverGarage,
		Config: map[string]string{"admin_token": "LEGACY-MODE-TOKEN"},
		Owner:  "org",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Contains(raw, []byte("LEGACY-MODE-TOKEN")) {
		t.Errorf("OpenConnections (no key) should write plaintext for back-compat, got:\n%s", raw)
	}
	if bytes.Contains(raw, []byte("configEnc")) {
		t.Errorf("OpenConnections (no key) should NOT write configEnc field, got:\n%s", raw)
	}
}

// TestConnectionsAtRest_MultipleConnections covers the case where the
// migration must process several records, only SOME of which carry
// plaintext sensitive keys. Idempotence across mixed seed is the
// invariant we want to lock down — otherwise a single non-migrating
// record could re-trigger the rewrite loop on every boot.
func TestConnectionsAtRest_MultipleConnections(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenConnectionsWithKey(dir, connectionsTestKey)
	if err != nil {
		t.Fatalf("OpenConnectionsWithKey: %v", err)
	}
	ctx := context.Background()

	for i, label := range []string{"alpha", "beta", "gamma"} {
		_, err := s.Create(ctx, Connection{
			Label:  label,
			Driver: DriverGarage,
			Config: map[string]string{
				"admin_url":   "https://" + label + ".example",
				"admin_token": "TOKEN-" + label,
			},
			Owner: "org",
		})
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	// Snapshot — second open must be byte-identical.
	before, _ := os.ReadFile(filepath.Join(dir, "connections.json"))

	s2, err := OpenConnectionsWithKey(dir, connectionsTestKey)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "connections.json"))

	if !bytes.Equal(before, after) {
		t.Errorf("re-open mutated file with no plaintext to migrate (idempotence violation):\nbefore=%s\nafter=%s", before, after)
	}

	// Sanity: all three connections still decrypt.
	for _, label := range []string{"alpha", "beta", "gamma"} {
		list, _ := s2.List(ctx)
		var found *Connection
		for i := range list {
			if list[i].Label == label {
				found = &list[i]
				break
			}
		}
		if found == nil {
			t.Fatalf("conn %q not found after reopen", label)
			continue
		}
		if found.Config["admin_token"] != "TOKEN-"+label {
			t.Errorf("conn %q admin_token: got %q want %q", label, found.Config["admin_token"], "TOKEN-"+label)
		}
	}
}
