// Package main — config.go handles the on-disk profile store at
// ~/.config/basement/config.yaml.
//
// The CLI is multi-profile from day one: an operator manages many
// basement deployments (dev / staging / prod / a customer's
// install) and we don't want them re-passing --endpoint --key --secret
// on every invocation. `basement login` writes a profile; subsequent
// calls read --profile NAME (or $BASEMENT_PROFILE, or "default").
//
// Secrets on disk: stored in plaintext YAML. The file mode is 0600 so
// only the owner can read — same shape as ~/.aws/credentials. CI use
// case is covered separately via $BASEMENT_SECRET_KEY which overrides
// the file. We can revisit at-rest encryption when the threat model
// requires it (keychain integration, age-encrypted profiles, etc.).
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the root of config.yaml. The Profiles map is keyed by
// profile name so `basement --profile work` looks up Profiles["work"].
// The top-level shape is intentionally minimal so future additions
// (default region per profile, telemetry opt-in flag) don't need a
// migration step.
type Config struct {
	Profiles map[string]Profile `yaml:"profiles"`
}

// Profile is one configured basement deployment. Endpoint is the
// base URL (no trailing slash, no /api/v1 — the client adds that).
// AccessKeyID + SecretKey are the service-account creds from
// `/admin/service-accounts` (v1.7.0a). CurrentRegionID is a UX
// convenience — `basement objects list bucket` uses it as the default
// region when --region isn't passed.
type Profile struct {
	Endpoint        string `yaml:"endpoint"`
	AccessKeyID     string `yaml:"access_key_id"`
	SecretKey       string `yaml:"secret_key,omitempty"`
	CurrentRegionID string `yaml:"current_region_id,omitempty"`
}

// configPath returns the absolute path to config.yaml. It honours
// $XDG_CONFIG_HOME first (Linux convention), falling back to
// ~/.config/basement/config.yaml. Tests override via $BASEMENT_CONFIG.
func configPath() (string, error) {
	if override := os.Getenv("BASEMENT_CONFIG"); override != "" {
		return override, nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "basement", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, ".config", "basement", "config.yaml"), nil
}

// LoadConfig reads config.yaml from disk. Missing file returns an
// empty Config (not an error) — fresh installs need a valid zero value
// so `basement login` can append a profile to it.
func LoadConfig() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{Profiles: map[string]Profile{}}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(buf, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	return &cfg, nil
}

// SaveConfig writes cfg to config.yaml with 0600 mode. The parent
// directory is created with 0700 if missing — same one-shot setup the
// AWS / kubectl CLIs use.
func SaveConfig(cfg *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	buf, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// profileName resolves the active profile name from --profile, then
// $BASEMENT_PROFILE, then "default". Centralised so every subcommand
// gets the same precedence.
func profileName(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if env := os.Getenv("BASEMENT_PROFILE"); env != "" {
		return env
	}
	return "default"
}

// resolveProfile returns the active profile (looked up by name) plus
// any environment overrides applied. The secret override
// ($BASEMENT_SECRET_KEY) is the only CI-friendly bypass — we do NOT
// honour env vars for endpoint / akid because those should be
// baked into the on-disk profile so audit can pin a CI run to a
// known SA.
func resolveProfile(cfg *Config, name string) (Profile, error) {
	if cfg == nil || cfg.Profiles == nil {
		return Profile{}, fmt.Errorf("no profile %q — run `basement login` first", name)
	}
	p, ok := cfg.Profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("no profile %q — run `basement login` first (available: %v)",
			name, profileNames(cfg))
	}
	if env := os.Getenv("BASEMENT_SECRET_KEY"); env != "" {
		p.SecretKey = env
	}
	return p, nil
}

// profileNames lists the configured profile names — used in the error
// message above so the operator gets a hint when they typo a name.
func profileNames(cfg *Config) []string {
	if cfg == nil {
		return nil
	}
	out := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		out = append(out, name)
	}
	return out
}
