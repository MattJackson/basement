package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestOrgCapabilities_SaveAtomicRoundTrip verifies Save() persists via the
// atomic tmp+fsync+rename path (saveJSON): after a save the live file is
// well-formed JSON, no .tmp file is left behind, and reopening the store
// reads the persisted values back. Guards the non-atomic os.WriteFile fix.
func TestOrgCapabilities_SaveAtomicRoundTrip(t *testing.T) {
	dir := t.TempDir()

	s, err := OpenOrgCapabilities(dir)
	if err != nil {
		t.Fatalf("OpenOrgCapabilities: %v", err)
	}

	caps := s.Get()
	caps.SignupMode = "open"
	caps.OIDCOnly = true
	caps.AdminSessionTTLSec = 1200
	if err := s.Update(caps); err != nil {
		t.Fatalf("Update: %v", err)
	}

	path := filepath.Join(dir, "org_capabilities.json")

	// No leftover tmp file — the atomic path renames it away.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected no leftover .tmp file, stat err = %v", err)
	}

	// Live file is well-formed JSON, not a torn write.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read org_capabilities.json: %v", err)
	}
	var onDisk OrgCapabilities
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("on-disk file is not valid JSON: %v\n%s", err, string(data))
	}

	// File mode must be 0600 (org config is not world-readable).
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("org_capabilities.json mode = %o, want 600", perm)
	}

	// Reopen and confirm the values round-tripped.
	s2, err := OpenOrgCapabilities(dir)
	if err != nil {
		t.Fatalf("reopen OpenOrgCapabilities: %v", err)
	}
	got := s2.Get()
	if got.SignupMode != "open" {
		t.Errorf("SignupMode = %q, want open", got.SignupMode)
	}
	if !got.OIDCOnly {
		t.Error("OIDCOnly did not persist")
	}
	if got.AdminSessionTTLSec != 1200 {
		t.Errorf("AdminSessionTTLSec = %d, want 1200", got.AdminSessionTTLSec)
	}
}
