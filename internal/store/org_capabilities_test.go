// org_capabilities_test.go covers the v1.9.0b Gateways nest:
//
//   - A legacy file (no "gateways" key at all) reads as
//     WebDAV.Enabled=true, not the Go zero-value false. This is
//     the safety net that stops a v1.9.0a → v1.9.0b upgrade from
//     silently disabling every working WebDAV mount.
//   - A file with "gateways": {"webdav": {"enabled": false}} reads
//     as Enabled=false — i.e. the operator's deliberate kill switch
//     is preserved.
//   - A round-trip (Update → Get) preserves the toggle.

package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOrgCapabilities_LegacyFileDefaultsWebDAVEnabled(t *testing.T) {
	dir := t.TempDir()
	// Hand-craft a legacy file that predates v1.9.0b: no gateways key.
	legacy := map[string]any{
		"signupMode":         "invite",
		"enabledDrivers":     []string{"garage", "aws-s3"},
		"allowUserBackends":  false,
		"userBackendDrivers": []string{},
		"oidcOnly":           false,
		"adminSessionTtlSec": 900,
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "org_capabilities.json"), data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	s, err := OpenOrgCapabilities(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	caps := s.Get()
	if !caps.Gateways.WebDAV.Enabled {
		t.Errorf("legacy file: WebDAV.Enabled = false, want true (default-on migration)")
	}
}

func TestOrgCapabilities_ExplicitDisablePreserved(t *testing.T) {
	dir := t.TempDir()
	withDisabled := map[string]any{
		"signupMode": "invite",
		"gateways": map[string]any{
			"webdav": map[string]any{"enabled": false},
		},
	}
	data, _ := json.Marshal(withDisabled)
	if err := os.WriteFile(filepath.Join(dir, "org_capabilities.json"), data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	s, err := OpenOrgCapabilities(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	caps := s.Get()
	if caps.Gateways.WebDAV.Enabled {
		t.Errorf("explicit disable: WebDAV.Enabled = true, want false (kill switch preserved)")
	}
}

func TestOrgCapabilities_GatewayToggleRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenOrgCapabilities(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	caps := s.Get()
	// Sanity: fresh defaults are gateway-on.
	if !caps.Gateways.WebDAV.Enabled {
		t.Fatalf("default: WebDAV.Enabled = false, want true")
	}

	// Operator flips it off.
	caps.Gateways.WebDAV.Enabled = false
	caps.Gateways.WebDAV.BaseURL = "https://files.example.test/webdav"
	if err := s.Update(caps); err != nil {
		t.Fatalf("update: %v", err)
	}

	// Re-open from disk → toggle survives.
	s2, err := OpenOrgCapabilities(dir)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	got := s2.Get()
	if got.Gateways.WebDAV.Enabled {
		t.Errorf("round-trip: Enabled = true, want false")
	}
	if got.Gateways.WebDAV.BaseURL != "https://files.example.test/webdav" {
		t.Errorf("round-trip: BaseURL = %q, want %q", got.Gateways.WebDAV.BaseURL, "https://files.example.test/webdav")
	}
}

// v1.9.0d: a legacy v1.9.0b file ({"gateways":{"webdav":{"enabled":false}}})
// must auto-migrate into the generic Protocols map. The v1.9.0d
// /admin/gateways UI reads through GatewaySettings.IsEnabled which
// consults Protocols first; without the migration, an operator who
// flipped the WebDAV kill switch in v1.9.0b would see it flip back to
// "enabled" on the new card.
func TestOrgCapabilities_V190b_MigratesToProtocolsMap(t *testing.T) {
	dir := t.TempDir()
	legacy := map[string]any{
		"signupMode": "invite",
		"gateways": map[string]any{
			"webdav": map[string]any{
				"enabled": false,
				"baseUrl": "https://files.example.test/webdav",
			},
		},
	}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(filepath.Join(dir, "org_capabilities.json"), data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	s, err := OpenOrgCapabilities(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	caps := s.Get()

	// Protocols map populated from legacy field — v1.9.0d code reads
	// IsEnabled("webdav") and must see the migrated value.
	cfg, ok := caps.Gateways.Protocols["webdav"]
	if !ok {
		t.Fatalf("Protocols[\"webdav\"]: missing after migration")
	}
	if cfg.Enabled {
		t.Errorf("Protocols[\"webdav\"].Enabled: want false (migrated from legacy kill switch)")
	}
	if cfg.BaseURL != "https://files.example.test/webdav" {
		t.Errorf("Protocols[\"webdav\"].BaseURL: want %q, got %q",
			"https://files.example.test/webdav", cfg.BaseURL)
	}

	// IsEnabled / BaseURL helpers return the canonical value.
	if caps.Gateways.IsEnabled("webdav") {
		t.Errorf("IsEnabled(webdav): want false")
	}
	if caps.Gateways.BaseURL("webdav") != "https://files.example.test/webdav" {
		t.Errorf("BaseURL(webdav): got %q", caps.Gateways.BaseURL("webdav"))
	}
	// Stubs: missing key → enabled=false.
	for _, name := range []string{"smb", "nfs", "ftp", "s3"} {
		if caps.Gateways.IsEnabled(name) {
			t.Errorf("IsEnabled(%s): want false (no entry → default off)", name)
		}
	}
}

// v1.9.0d: a v1.9.0b-shaped PATCH (mutates only the legacy WebDAV
// field) must end up with the Protocols["webdav"] entry kept in
// sync. Without syncGatewaySettings the new card would render stale.
func TestOrgCapabilities_V190b_PatchSyncsToProtocols(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenOrgCapabilities(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	caps := s.Get()
	// v1.9.0b client: only touches the WebDAV field.
	caps.Gateways.WebDAV.Enabled = false
	caps.Gateways.WebDAV.BaseURL = "https://override.example/webdav"
	if err := s.Update(caps); err != nil {
		t.Fatalf("update: %v", err)
	}

	// Re-open: both shapes must agree.
	s2, err := OpenOrgCapabilities(dir)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	got := s2.Get()
	if got.Gateways.WebDAV.Enabled {
		t.Errorf("WebDAV.Enabled: want false")
	}
	cfg := got.Gateways.Protocols["webdav"]
	if cfg.Enabled {
		t.Errorf("Protocols[webdav].Enabled: want false (sync from legacy)")
	}
	if cfg.BaseURL != "https://override.example/webdav" {
		t.Errorf("Protocols[webdav].BaseURL: got %q", cfg.BaseURL)
	}
}
