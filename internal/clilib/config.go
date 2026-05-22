// Package clilib is the shared library for basement's out-of-process
// clients — the `basement-mcp` MCP server (cmd/basement-mcp/) and,
// optionally, anything else the operator scripts that needs to talk
// to basement-server over the bearer-auth path. It bundles the two
// pieces every such client wants:
//
//  1. A profile-backed on-disk config (~/.config/basement/config.yaml)
//     so the operator doesn't keep re-typing endpoint + credentials.
//  2. A bearer-auth HTTP client that talks to basement-server's JSON
//     API through the v1.7.0b service-account middleware.
//
// History: v1.8 was originally scoped with a dedicated `basement`
// CLI in addition to the MCP server. v1.8.0d dropped that plan —
// object-store CRUD is covered by aws-cli against the SigV4 endpoint
// and basement-specific control-plane work belongs in the web UI —
// so this package now serves the MCP binary alone. The shape stays
// generic on purpose: any future out-of-process client can adopt
// the same YAML schema + `$BASEMENT_CONFIG` override without a
// migration step.
package clilib

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// Config is the root of config.yaml. The Profiles map is keyed by
// profile name so `--profile work` looks up Profiles["work"]. The
// top-level shape is intentionally minimal so future additions
// (default region per profile, telemetry opt-in flag) don't need a
// migration step.
type Config struct {
	Profiles map[string]Profile `yaml:"profiles"`
}

// Profile is one configured basement deployment. Endpoint is the
// base URL (no trailing slash, no /api/v1 — the client adds that).
// AccessKeyID + SecretKey are the service-account credentials from
// /admin/service-accounts (v1.7.0a). CurrentRegionID is a UX
// convenience some clients use as a default when --region isn't
// passed.
type Profile struct {
	Endpoint        string `yaml:"endpoint"`
	AccessKeyID     string `yaml:"access_key_id"`
	SecretKey       string `yaml:"secret_key,omitempty"`
	CurrentRegionID string `yaml:"current_region_id,omitempty"`
}

// ConfigPath returns the absolute path to config.yaml. It honours
// $XDG_CONFIG_HOME first (Linux convention), falling back to
// ~/.config/basement/config.yaml. Tests override via
// $BASEMENT_CONFIG.
func ConfigPath() (string, error) {
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
// empty Config (not an error) — fresh installs need a valid zero
// value so any future client that wants to append a profile (for
// example a `basement-mcp init` subcommand) has something to start
// from instead of erroring out on a missing path.
func LoadConfig() (*Config, error) {
	path, err := ConfigPath()
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
// directory is created with 0700 if missing — same one-shot setup
// the AWS / kubectl CLIs use.
func SaveConfig(cfg *Config) error {
	path, err := ConfigPath()
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

// ProfileName resolves the active profile name from the explicit
// argument (e.g. a --profile flag value), then $BASEMENT_PROFILE,
// then "default". Centralised so every client gets the same
// precedence.
func ProfileName(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if env := os.Getenv("BASEMENT_PROFILE"); env != "" {
		return env
	}
	return "default"
}

// ResolveProfile returns the active profile (looked up by name)
// with environment overrides applied. The secret override
// ($BASEMENT_SECRET_KEY) is the only CI-friendly bypass — we do
// NOT honour env vars for endpoint / akid because those should be
// baked into the on-disk profile so audit can pin a CI run to a
// known SA.
func ResolveProfile(cfg *Config, name string) (Profile, error) {
	if cfg == nil || cfg.Profiles == nil {
		return Profile{}, fmt.Errorf("no profile %q — configure ~/.config/basement/config.yaml first", name)
	}
	p, ok := cfg.Profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("no profile %q — available profiles: %v", name, profileNames(cfg))
	}
	if env := os.Getenv("BASEMENT_SECRET_KEY"); env != "" {
		p.SecretKey = env
	}
	return p, nil
}

// profileNames lists the configured profile names — used in the
// error message above so the operator gets a hint when they typo a
// name. Output is sorted so tests can assert deterministically.
func profileNames(cfg *Config) []string {
	if cfg == nil {
		return nil
	}
	out := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
