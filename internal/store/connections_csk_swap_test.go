// Package store: v1.12.0b tests for SwapClusterSecret — the
// per-record swap-and-save plumbing that backs the lazy
// JWT-encrypted → CSK-encrypted ConfigEnc migration on first
// cluster unlock (ADR-0007).
//
// The tests lock down three invariants the migration helper in
// internal/api relies on:
//
//  1. Atomicity — the swap goes through the same tmp+fsync+rename
//     pipeline as the rest of the store, so a partial write can't
//     leave disk in a half-migrated state.
//  2. Idempotency — when the on-disk ConfigEncCSK doesn't match the
//     expected old value (because another goroutine raced and won),
//     the swap is a no-op rather than a clobber.
//  3. Legacy ConfigEnc preservation — the swap never mutates the
//     JWT-encrypted bridge field; the existing load() path must
//     keep working until a future cycle teaches it to prefer the
//     CSK-encrypted parallel.

package store

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSwapClusterSecret_HappyPath: a brand-new CSK blob lands on disk
// and round-trips through a fresh Open.
func TestSwapClusterSecret_HappyPath(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenConnectionsWithKey(dir, connectionsTestKey)
	if err != nil {
		t.Fatalf("OpenConnectionsWithKey: %v", err)
	}
	ctx := context.Background()

	created, err := s.Create(ctx, Connection{
		Label:  "swap-target",
		Driver: DriverGarage,
		Config: map[string]string{"admin_url": "https://swap.example", "admin_token": "JWT-WRAPPED-TOKEN"},
		Owner:  "org",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// First migration: no existing CSK blob.
	newBlob := []byte("CSK-ENCRYPTED-BYTES-V1")
	if err := s.SwapClusterSecret(ctx, created.ID, nil, newBlob); err != nil {
		t.Fatalf("SwapClusterSecret first: %v", err)
	}

	// Reopen to confirm the field landed on disk and round-trips.
	s2, err := OpenConnectionsWithKey(dir, connectionsTestKey)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := s2.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get post-swap reopen: %v", err)
	}
	if !bytes.Equal(got.ConfigEncCSK, newBlob) {
		t.Errorf("ConfigEncCSK round-trip mismatch: got %q want %q", got.ConfigEncCSK, newBlob)
	}

	// Sanity: the on-disk JSON carries configEncCSK.
	raw, err := os.ReadFile(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("read connections.json: %v", err)
	}
	if !bytes.Contains(raw, []byte(`"configEncCSK"`)) {
		t.Errorf("on-disk JSON missing configEncCSK field:\n%s", raw)
	}
}

// TestSwapClusterSecret_IdempotentRace: the second of two racing
// migrations is a no-op (old-value mismatch) and returns nil.
func TestSwapClusterSecret_IdempotentRace(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenConnectionsWithKey(dir, connectionsTestKey)
	if err != nil {
		t.Fatalf("OpenConnectionsWithKey: %v", err)
	}
	ctx := context.Background()

	created, err := s.Create(ctx, Connection{
		Label:  "race",
		Driver: DriverGarage,
		Config: map[string]string{"admin_url": "https://r.example", "admin_token": "T"},
		Owner:  "org",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	winnerBlob := []byte("WINNER-CSK-BYTES")
	loserBlob := []byte("LOSER-CSK-BYTES")

	// "Winner" runs first with the expected starting value (empty).
	if err := s.SwapClusterSecret(ctx, created.ID, nil, winnerBlob); err != nil {
		t.Fatalf("Swap (winner): %v", err)
	}

	// "Loser" comes in with the same stale expectation (empty old
	// value) — its swap should no-op without error AND without
	// clobbering the winner's blob.
	if err := s.SwapClusterSecret(ctx, created.ID, nil, loserBlob); err != nil {
		t.Fatalf("Swap (loser stale): %v", err)
	}
	got, _ := s.Get(ctx, created.ID)
	if !bytes.Equal(got.ConfigEncCSK, winnerBlob) {
		t.Errorf("loser clobbered the winner: got %q want %q", got.ConfigEncCSK, winnerBlob)
	}

	// A re-swap with the CURRENT value as oldEnc is a legit follow-up
	// (e.g. a CSK rotation in a future cycle) and must succeed.
	rotatedBlob := []byte("CSK-ROTATED-BYTES")
	if err := s.SwapClusterSecret(ctx, created.ID, winnerBlob, rotatedBlob); err != nil {
		t.Fatalf("Swap (rotation): %v", err)
	}
	got, _ = s.Get(ctx, created.ID)
	if !bytes.Equal(got.ConfigEncCSK, rotatedBlob) {
		t.Errorf("rotation did not land: got %q want %q", got.ConfigEncCSK, rotatedBlob)
	}
}

// TestSwapClusterSecret_LegacyConfigEncPreserved: the swap never
// touches the JWT-encrypted ConfigEnc field. This is the headline
// safety invariant of the v1.12.0b cycle — the legacy bridge stays
// recoverable until a future cycle decides to retire it.
func TestSwapClusterSecret_LegacyConfigEncPreserved(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenConnectionsWithKey(dir, connectionsTestKey)
	if err != nil {
		t.Fatalf("OpenConnectionsWithKey: %v", err)
	}
	ctx := context.Background()

	created, err := s.Create(ctx, Connection{
		Label:  "legacy-preserved",
		Driver: DriverGarage,
		Config: map[string]string{
			"admin_url":   "https://legacy.example",
			"admin_token": "JWT-SECRET-MUST-SURVIVE",
		},
		Owner: "org",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Confirm ConfigEnc exists pre-swap (saveLocked back-fills the
	// cache to match disk so Get exposes the JWT blob to the
	// migration helper).
	before, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get before: %v", err)
	}
	if len(before.ConfigEnc) == 0 {
		t.Fatal("test setup: expected non-empty legacy ConfigEnc after Create")
	}

	if err := s.SwapClusterSecret(ctx, created.ID, nil, []byte("CSK-BLOB")); err != nil {
		t.Fatalf("SwapClusterSecret: %v", err)
	}

	// Post-swap the legacy ConfigEnc bytes are NOT byte-identical
	// (saveLocked re-seals with a fresh nonce on every save) — what
	// matters is the JWT-decryptable bridge still decrypts to the
	// original plaintext. That's the v1.12.0b safety promise.
	after, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after: %v", err)
	}
	if len(after.ConfigEnc) == 0 {
		t.Errorf("legacy ConfigEnc dropped by swap (bridge burned)")
	}
	if after.Config["admin_token"] != "JWT-SECRET-MUST-SURVIVE" {
		t.Errorf("legacy admin_token lost: got %q", after.Config["admin_token"])
	}

	// Reload and re-confirm — the bridge must work across process
	// boundaries, not just same-store reads.
	s2, err := OpenConnectionsWithKey(dir, connectionsTestKey)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	reloaded, err := s2.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if reloaded.Config["admin_token"] != "JWT-SECRET-MUST-SURVIVE" {
		t.Errorf("legacy admin_token lost on reopen: got %q", reloaded.Config["admin_token"])
	}
	if !bytes.Equal(reloaded.ConfigEncCSK, []byte("CSK-BLOB")) {
		t.Errorf("ConfigEncCSK lost on reopen: got %q", reloaded.ConfigEncCSK)
	}
}

// TestSwapClusterSecret_NotFound: a swap against a missing cid is an
// explicit error so callers don't silently lose a migration write
// against a typo'd cluster ID.
func TestSwapClusterSecret_NotFound(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenConnectionsWithKey(dir, connectionsTestKey)
	if err != nil {
		t.Fatalf("OpenConnectionsWithKey: %v", err)
	}
	err = s.SwapClusterSecret(context.Background(), "no-such-cid", nil, []byte("X"))
	if err == nil {
		t.Fatal("expected error for missing connection")
	}
}

// TestSwapClusterSecret_OnDiskShape: the on-disk JSON carries the
// CSK blob under the documented "configEncCSK" key, separate from
// "configEnc" (the legacy JWT field). This locks the schema so a
// follow-up cycle adding CSK-decrypt-on-load reads the right field.
func TestSwapClusterSecret_OnDiskShape(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenConnectionsWithKey(dir, connectionsTestKey)
	if err != nil {
		t.Fatalf("OpenConnectionsWithKey: %v", err)
	}
	ctx := context.Background()

	created, err := s.Create(ctx, Connection{
		Label:  "shape",
		Driver: DriverGarage,
		Config: map[string]string{"admin_url": "https://s.example", "admin_token": "T"},
		Owner:  "org",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.SwapClusterSecret(ctx, created.ID, nil, []byte("CSK-SHAPE-CHECK")); err != nil {
		t.Fatalf("SwapClusterSecret: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "connections.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var disk []map[string]any
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("on-disk JSON not parseable: %v", err)
	}
	if len(disk) != 1 {
		t.Fatalf("expected 1 record, got %d", len(disk))
	}
	if _, present := disk[0]["configEncCSK"]; !present {
		t.Errorf("expected configEncCSK field in on-disk JSON, got: %v", disk[0])
	}
	if _, present := disk[0]["configEnc"]; !present {
		t.Errorf("legacy configEnc field disappeared from on-disk JSON, got: %v", disk[0])
	}
}
