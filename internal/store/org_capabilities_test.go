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
