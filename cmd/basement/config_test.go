// Package main — config_test.go covers the YAML profile store. Every
// test isolates itself by pointing $BASEMENT_CONFIG at a temp dir so
// the real ~/.config/basement/config.yaml is never touched.
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func tempConfigPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	t.Setenv("BASEMENT_CONFIG", path)
	t.Setenv("BASEMENT_SECRET_KEY", "")
	t.Setenv("BASEMENT_PROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	return path
}

func TestLoadConfigMissingFile(t *testing.T) {
	tempConfigPath(t)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() on missing file: %v", err)
	}
	if cfg.Profiles == nil {
		t.Fatal("Profiles map should be initialised on a missing-file load")
	}
	if len(cfg.Profiles) != 0 {
		t.Errorf("expected empty profiles on missing-file load, got %d", len(cfg.Profiles))
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := tempConfigPath(t)
	cfg := &Config{Profiles: map[string]Profile{
		"default": {
			Endpoint:        "https://example.test",
			AccessKeyID:     "BMNT0000111122223333",
			SecretKey:       "supersecret",
			CurrentRegionID: "rid-123",
		},
		"work": {
			Endpoint:    "https://work.example.test",
			AccessKeyID: "BMNTaaaabbbbccccdddd",
			SecretKey:   "alsosecret",
		},
	}}
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat %s: %v", path, err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("config file mode = %v, want 0600", mode)
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := loaded.Profiles["default"].Endpoint; got != "https://example.test" {
		t.Errorf("default endpoint = %q, want %q", got, "https://example.test")
	}
	if got := loaded.Profiles["default"].SecretKey; got != "supersecret" {
		t.Errorf("default secret key = %q, want %q", got, "supersecret")
	}
	if got := loaded.Profiles["default"].CurrentRegionID; got != "rid-123" {
		t.Errorf("default current_region_id = %q, want rid-123", got)
	}
	if got := loaded.Profiles["work"].AccessKeyID; got != "BMNTaaaabbbbccccdddd" {
		t.Errorf("work access key id = %q", got)
	}
}

func TestProfileNamePrecedence(t *testing.T) {
	tempConfigPath(t)
	// Flag wins over env wins over "default".
	if got := profileName("from-flag"); got != "from-flag" {
		t.Errorf("flag should win: got %q", got)
	}
	t.Setenv("BASEMENT_PROFILE", "from-env")
	if got := profileName(""); got != "from-env" {
		t.Errorf("env should win when no flag: got %q", got)
	}
	t.Setenv("BASEMENT_PROFILE", "")
	if got := profileName(""); got != "default" {
		t.Errorf("default fallback: got %q", got)
	}
}

func TestResolveProfileSecretEnvOverride(t *testing.T) {
	tempConfigPath(t)
	cfg := &Config{Profiles: map[string]Profile{
		"default": {Endpoint: "https://e.test", AccessKeyID: "K", SecretKey: "from-disk"},
	}}
	t.Setenv("BASEMENT_SECRET_KEY", "from-env")
	p, err := resolveProfile(cfg, "default")
	if err != nil {
		t.Fatalf("resolveProfile: %v", err)
	}
	if p.SecretKey != "from-env" {
		t.Errorf("$BASEMENT_SECRET_KEY should override on-disk secret; got %q", p.SecretKey)
	}
}

func TestResolveProfileMissing(t *testing.T) {
	tempConfigPath(t)
	cfg := &Config{Profiles: map[string]Profile{
		"work": {Endpoint: "https://e.test"},
	}}
	if _, err := resolveProfile(cfg, "default"); err == nil {
		t.Fatal("resolveProfile on missing name should fail")
	}
}
