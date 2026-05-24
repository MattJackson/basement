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

// v1.11.0a — an existing org_capabilities.json file that predates the
// onboardingCompleted field belongs to an already-configured operator
// and must read as completed=true on upgrade, otherwise the AppShell
// would bounce them into the first-run wizard at the next admin entry.
// Conversely, a fresh install has NO file on disk and reads from the
// in-memory default (completed=false) so the wizard auto-shows.
func TestOrgCapabilities_LegacyFile_OnboardingCompletedOnUpgrade(t *testing.T) {
	dir := t.TempDir()
	// Hand-craft a legacy file: no onboardingCompleted key.
	legacy := map[string]any{
		"signupMode": "invite",
		"gateways": map[string]any{
			"webdav": map[string]any{"enabled": true},
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
	if !caps.OnboardingCompleted {
		t.Errorf("OnboardingCompleted: want true on upgrade (legacy file present), got false — operator would be bounced into first-run wizard")
	}
}

// v1.11.0a — a brand new install has no on-disk file, so
// OpenOrgCapabilities returns the in-memory default. That default keeps
// OnboardingCompleted at its zero value (false) so the FE auto-routes
// into the wizard.
func TestOrgCapabilities_FreshInstall_OnboardingNotCompleted(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenOrgCapabilities(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	caps := s.Get()
	if caps.OnboardingCompleted {
		t.Errorf("OnboardingCompleted: want false on fresh install (no on-disk file), got true — wizard wouldn't auto-show")
	}
}

// v1.11.0a — MarkOnboardingCompleted is the dismiss latch the API
// handler calls. Persists across re-open and is idempotent.
func TestOrgCapabilities_MarkOnboardingCompletedPersists(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenOrgCapabilities(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.MarkOnboardingCompleted(); err != nil {
		t.Fatalf("mark: %v", err)
	}
	// Idempotent: second call is a no-op.
	if err := s.MarkOnboardingCompleted(); err != nil {
		t.Fatalf("mark (second): %v", err)
	}

	s2, err := OpenOrgCapabilities(dir)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	if !s2.Get().OnboardingCompleted {
		t.Errorf("after dismiss + re-open: OnboardingCompleted = false, want true (latch should persist)")
	}
}

// v1.13.0a (ADR-0008) — a legacy file without ActiveSkin / SkinPolicy
// reads as the basement-default pair on Get(). Substitutes at read time
// without mutating the on-disk file behind the operator.
func TestOrgCapabilities_LegacyFile_SkinDefaults(t *testing.T) {
	dir := t.TempDir()
	legacy := map[string]any{
		"signupMode": "invite",
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
	if caps.ActiveSkin != DefaultActiveSkin {
		t.Errorf("ActiveSkin: got %q, want %q", caps.ActiveSkin, DefaultActiveSkin)
	}
}

// v1.13.0a (ADR-0008) — operator-set ActiveSkin value round-trips through
// Update → re-open. The field survives the JSON serialisation.
func TestOrgCapabilities_ActiveSkinRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenOrgCapabilities(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	caps := s.Get()
	caps.ActiveSkin = "acme-corp"
	if err := s.Update(caps); err != nil {
		t.Fatalf("update: %v", err)
	}

	// Re-open from disk — value survives.
	s2, err := OpenOrgCapabilities(dir)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	got := s2.Get()
	if got.ActiveSkin != "acme-corp" {
		t.Errorf("ActiveSkin round-trip: got %q, want %q", got.ActiveSkin, "acme-corp")
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

// v1.13.1: SkinPolicy → UserOverridableSkin + AllowedUserSkins migration tests

func TestOrgCapabilities_SkinPolicyMigration_Default(t *testing.T) {
	dir := t.TempDir()
	legacy := map[string]any{
		"signupMode": "invite",
		"activeSkin": "basement-default",
		"skinPolicy": "default",
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

	// "default" → UserOverridableSkin = false, AllowedUserSkins = []
	if caps.UserOverridableSkin {
		t.Errorf("UserOverridableSkin: want false for skinPolicy=default, got true")
	}
	if len(caps.AllowedUserSkins) != 0 {
		t.Errorf("AllowedUserSkins: want empty slice, got %v", caps.AllowedUserSkins)
	}
}

func TestOrgCapabilities_SkinPolicyMigration_Locked(t *testing.T) {
	dir := t.TempDir()
	legacy := map[string]any{
		"signupMode": "invite",
		"activeSkin": "basement-default",
		"skinPolicy": "locked",
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

	// "locked" → UserOverridableSkin = false, AllowedUserSkins = []
	if caps.UserOverridableSkin {
		t.Errorf("UserOverridableSkin: want false for skinPolicy=locked, got true")
	}
	if len(caps.AllowedUserSkins) != 0 {
		t.Errorf("AllowedUserSkins: want empty slice, got %v", caps.AllowedUserSkins)
	}
}

func TestOrgCapabilities_SkinPolicyMigration_UserChoice(t *testing.T) {
	dir := t.TempDir()
	legacy := map[string]any{
		"signupMode": "invite",
		"activeSkin": "basement-default",
		"skinPolicy": "user-choice",
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

	// "user-choice" → UserOverridableSkin = true, AllowedUserSkins = [] (all)
	if !caps.UserOverridableSkin {
		t.Errorf("UserOverridableSkin: want true for skinPolicy=user-choice, got false")
	}
	if len(caps.AllowedUserSkins) != 0 {
		t.Errorf("AllowedUserSkins: want empty slice (all), got %v", caps.AllowedUserSkins)
	}
}

func TestOrgCapabilities_SkinPolicyMigration_UnknownValue(t *testing.T) {
	dir := t.TempDir()
	legacy := map[string]any{
		"signupMode": "invite",
		"activeSkin": "basement-default",
		"skinPolicy": "UNKNOWN_POLICY",
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

	// Unknown → falls back to locked (UserOverridableSkin = false)
	if caps.UserOverridableSkin {
		t.Errorf("UserOverridableSkin: want false for unknown policy, got true")
	}
	if len(caps.AllowedUserSkins) != 0 {
		t.Errorf("AllowedUserSkins: want empty slice, got %v", caps.AllowedUserSkins)
	}
}

func TestOrgCapabilities_FreshInstall_NewFields(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenOrgCapabilities(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	caps := s.Get()

	// Fresh install → defaults
	if caps.UserOverridableSkin {
		t.Errorf("UserOverridableSkin: want false (default), got true")
	}
	if len(caps.AllowedUserSkins) != 0 {
		t.Errorf("AllowedUserSkins: want empty slice, got %v", caps.AllowedUserSkins)
	}
}

func TestOrgCapabilities_UserOverridableTrue_AllowedList(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenOrgCapabilities(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	caps := s.Get()
	caps.UserOverridableSkin = true
	caps.AllowedUserSkins = []string{"basement-default", "basement-95"}
	if err := s.Update(caps); err != nil {
		t.Fatalf("update: %v", err)
	}

	s2, err := OpenOrgCapabilities(dir)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	got := s2.Get()

	if !got.UserOverridableSkin {
		t.Errorf("UserOverridableSkin: want true, got false")
	}
	if len(got.AllowedUserSkins) != 2 {
		t.Errorf("AllowedUserSkins length: want 2, got %d", len(got.AllowedUserSkins))
	}
}

