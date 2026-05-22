package clilib

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadConfigMissing verifies a fresh install (no config file)
// returns an empty Config rather than an error — letting callers
// add their first profile without a special-case bootstrap path.
func TestLoadConfigMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BASEMENT_CONFIG", filepath.Join(dir, "nope.yaml"))

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig on missing file: unexpected error %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig returned nil config")
	}
	if len(cfg.Profiles) != 0 {
		t.Errorf("expected empty Profiles map, got %d entries", len(cfg.Profiles))
	}
}

// TestSaveLoadRoundtrip writes a profile then re-reads it from disk
// — covers the YAML marshalling shape AND the 0600 file mode.
func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	t.Setenv("BASEMENT_CONFIG", path)

	cfg := &Config{Profiles: map[string]Profile{
		"default": {
			Endpoint:    "https://basement.example.com",
			AccessKeyID: "BMNT000000000001",
			SecretKey:   "deadbeef",
		},
	}}
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// File mode must be 0600 — credentials at rest.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat saved file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("config file mode = %o, want 0600", mode)
	}

	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig roundtrip: %v", err)
	}
	p, ok := got.Profiles["default"]
	if !ok {
		t.Fatal("default profile missing after roundtrip")
	}
	if p.Endpoint != "https://basement.example.com" {
		t.Errorf("endpoint = %q, want https://basement.example.com", p.Endpoint)
	}
	if p.AccessKeyID != "BMNT000000000001" {
		t.Errorf("akid = %q", p.AccessKeyID)
	}
	if p.SecretKey != "deadbeef" {
		t.Errorf("secret = %q", p.SecretKey)
	}
}

// TestProfileNamePrecedence locks the three-tier resolution:
// explicit > $BASEMENT_PROFILE > "default".
func TestProfileNamePrecedence(t *testing.T) {
	t.Setenv("BASEMENT_PROFILE", "from-env")

	if got := ProfileName("explicit"); got != "explicit" {
		t.Errorf("explicit should win: got %q", got)
	}
	if got := ProfileName(""); got != "from-env" {
		t.Errorf("env should win when explicit is empty: got %q", got)
	}

	t.Setenv("BASEMENT_PROFILE", "")
	if got := ProfileName(""); got != "default" {
		t.Errorf("default fallback: got %q", got)
	}
}

// TestResolveProfileSecretOverride locks the CI-friendly bypass:
// $BASEMENT_SECRET_KEY overrides whatever's on disk so CI can
// rotate secrets without re-writing config files in pipelines.
func TestResolveProfileSecretOverride(t *testing.T) {
	cfg := &Config{Profiles: map[string]Profile{
		"ci": {
			Endpoint:    "https://example.test",
			AccessKeyID: "BMNT0000000000ab",
			SecretKey:   "stale-from-disk",
		},
	}}

	t.Setenv("BASEMENT_SECRET_KEY", "fresh-from-env")
	p, err := ResolveProfile(cfg, "ci")
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if p.SecretKey != "fresh-from-env" {
		t.Errorf("env did not override on-disk secret: got %q", p.SecretKey)
	}

	// Endpoint must NOT be overridable from env — audit needs a
	// stable deployment pin for the SA.
	if p.Endpoint != "https://example.test" {
		t.Errorf("endpoint should not be overridable: got %q", p.Endpoint)
	}
}

// TestResolveProfileMissing returns a helpful error listing the
// available profile names so the operator can spot typos.
func TestResolveProfileMissing(t *testing.T) {
	cfg := &Config{Profiles: map[string]Profile{
		"prod":    {},
		"staging": {},
	}}
	_, err := ResolveProfile(cfg, "stagign") // typo
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
	// Error must mention the actual profiles that exist.
	msg := err.Error()
	if !contains(msg, "prod") || !contains(msg, "staging") {
		t.Errorf("error doesn't list available profiles: %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
