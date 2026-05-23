package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// AdminSessionTTLSec bounds for the v1.3.0a.4 operator-configurable
// admin session timeout (ADR-0003 amendment). Default 15 min; range
// 60s – 24h. Stored in org_capabilities.json so a restart preserves
// it; gated on host:manage_org_caps so only host admins can change it.
const (
	AdminSessionTTLDefaultSec = 900    // 15 min
	AdminSessionTTLMinSec     = 60     // 1 min — anything shorter is useless
	AdminSessionTTLMaxSec     = 86_400 // 24 h — anything longer defeats the safety
)

// OrgCapabilities represents org-level feature flags and configuration.
type OrgCapabilities struct {
	SignupMode         string   `json:"signupMode"`         // "closed" | "invite" | "open"
	EnabledDrivers     []string `json:"enabledDrivers"`     // list of driver names
	AllowUserBackends  bool     `json:"allowUserBackends"`  // whether users can register their own clusters
	UserBackendDrivers []string `json:"userBackendDrivers"` // subset of enabled drivers for user backends
	OIDCOnly           bool     `json:"oidcOnly"`           // hide local password login, OIDC only
	// AdminSessionTTLSec is the per-elevation TTL (in seconds) the
	// /auth/elevate endpoint stamps on the cookie. Per ADR-0003 v1.3.0a.4
	// amendment this is operator-configurable from /admin/system instead
	// of env-only. Defaults to AdminSessionTTLDefaultSec when zero (older
	// org_capabilities.json files predate this field).
	AdminSessionTTLSec int `json:"adminSessionTtlSec,omitempty"`
	// Gateways holds per-protocol gateway toggles + overrides. v1.9.0b
	// introduces this nest so the operator can disable a shipped gateway
	// (WebDAV) without re-deploying. Each protocol defaults to "on" for
	// upgraded installs — see normalizeGateways() for the legacy-file
	// migration rule that flips a zero-value WebDAV into the default-on
	// shape rather than silently disabling a working gateway.
	Gateways GatewaySettings `json:"gateways"`
}

// GatewaySettings groups the per-protocol gateway toggles. Each nested
// struct lives under Gateways.{Protocol} so the on-disk JSON reads
// naturally: gateways.webdav.enabled, gateways.smb.* (future), etc.
//
// We model SMB explicitly as "not supported" rather than omitting it so
// the UI can render a deliberate "not supported natively" panel + link
// to docs/integrations/time-machine.md instead of an empty section.
type GatewaySettings struct {
	WebDAV WebDAVSettings `json:"webdav"`
}

// WebDAVSettings is the operator-facing config for the v1.9.0a gateway.
//
// Enabled defaults to true on a fresh install so the gateway works the
// moment basement comes up — operators who want to lock it down flip
// the toggle in /admin/system and the handler returns 403 GATEWAY_DISABLED
// from then on.
//
// BaseURL is an optional override for the URL the UI displays in the
// "connect from your platform" hint. Empty (the default) means the FE
// computes window.location.origin + "/webdav" — which is the right
// answer for the common single-origin deployment. Operators who front
// basement behind a reverse proxy with a different external WebDAV
// host can pin it here.
type WebDAVSettings struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"baseUrl,omitempty"`
}

// DefaultOrgCapabilities returns the default org capabilities.
func DefaultOrgCapabilities() OrgCapabilities {
	return OrgCapabilities{
		SignupMode:         "invite",
		EnabledDrivers:     []string{"garage", "garage-v1", "aws-s3", "minio"},
		AllowUserBackends:  false,
		UserBackendDrivers: []string{},
		OIDCOnly:           false,
		AdminSessionTTLSec: AdminSessionTTLDefaultSec,
		Gateways: GatewaySettings{
			WebDAV: WebDAVSettings{Enabled: true},
		},
	}
}

// normalizeGateways migrates a legacy on-disk file that predates
// v1.9.0b — no Gateways nest, no WebDAV.Enabled field — into the
// default-on shape. Without this a fresh upgrade would read
// Enabled=false (the zero value) and silently disable the working
// WebDAV gateway. The migration flag is the legacy marker we use: if
// the on-disk JSON has no gateways key at all, MigratedFromLegacy
// stays at its tag-default false → we treat the whole block as
// missing and substitute defaults.
//
// We do NOT mutate the on-disk file here — that happens lazily on the
// next Update() call. Read paths get the live defaults, write paths
// persist them, and an operator hand-editing the JSON between reads
// and writes still wins.
func normalizeGateways(g GatewaySettings, hadField bool) GatewaySettings {
	if !hadField {
		// Legacy file, no gateways block at all: default on.
		return GatewaySettings{WebDAV: WebDAVSettings{Enabled: true}}
	}
	return g
}

// ClampAdminSessionTTL returns the input clamped into the
// [AdminSessionTTLMinSec, AdminSessionTTLMaxSec] window. Zero (or any
// sub-min value) snaps to the default — that lets older
// org_capabilities.json files (pre-v1.3.0a.4, no field) read as
// "use the default" without a separate migration pass.
func ClampAdminSessionTTL(v int) int {
	if v <= 0 {
		return AdminSessionTTLDefaultSec
	}
	if v < AdminSessionTTLMinSec {
		return AdminSessionTTLMinSec
	}
	if v > AdminSessionTTLMaxSec {
		return AdminSessionTTLMaxSec
	}
	return v
}

// OrgCapabilitiesStore handles org capabilities persistence.
type OrgCapabilitiesStore struct {
	mu   sync.RWMutex
	path string
	data OrgCapabilities
}

// OpenOrgCapabilities opens or creates the org capabilities store.
func OpenOrgCapabilities(dataDir string) (*OrgCapabilitiesStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}

	path := filepath.Join(dataDir, "org_capabilities.json")
	s := &OrgCapabilitiesStore{
		path: path,
		data: DefaultOrgCapabilities(),
	}

	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return s, nil
}

// load reads capabilities from disk. If file doesn't exist or is empty, uses defaults.
func (s *OrgCapabilitiesStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // use defaults
		}
		return err
	}

	if len(data) == 0 {
		return nil // use defaults
	}

	if err := json.Unmarshal(data, &s.data); err != nil {
		return err
	}

	// Detect whether the on-disk file predates v1.9.0b's gateways
	// nest. We can't rely on Go's zero value (Enabled=false) because
	// a legacy file simply lacks the key entirely — we have to peek
	// at the raw JSON to tell "operator deliberately disabled" from
	// "field never existed". The raw map is cheap to allocate once
	// at boot; subsequent Get() / Update() cycles use the struct.
	var raw map[string]json.RawMessage
	hadGateways := false
	if err := json.Unmarshal(data, &raw); err == nil {
		_, hadGateways = raw["gateways"]
	}
	s.data.Gateways = normalizeGateways(s.data.Gateways, hadGateways)

	// Migrate legacy: ensure enabled drivers have defaults if empty
	if s.data.EnabledDrivers == nil || len(s.data.EnabledDrivers) == 0 {
		s.data.EnabledDrivers = []string{"garage", "garage-v1", "aws-s3", "minio"}
	}

	return nil
}

// Save persists capabilities to disk.
func (s *OrgCapabilitiesStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0644)
}

// Get returns a copy of the current capabilities. Legacy
// org_capabilities.json files predating v1.3.0a.4 lack the
// AdminSessionTTLSec field; we substitute the default at read time
// rather than mutating the on-disk file behind the operator's back —
// they'll see the default reflected in /admin/system and can persist
// a deliberate choice from there.
func (s *OrgCapabilitiesStore) Get() OrgCapabilities {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := s.data
	if out.AdminSessionTTLSec <= 0 {
		out.AdminSessionTTLSec = AdminSessionTTLDefaultSec
	}
	return out
}

// Update replaces all capabilities and persists. Per v1.3.0a.4 the
// admin session TTL is clamped into the supported range on the way in
// so an operator hand-editing the JSON (or a buggy FE) can't smuggle a
// 0-second or week-long TTL into the live config — the floor + ceiling
// are part of the contract, not advisory.
func (s *OrgCapabilitiesStore) Update(capabilities OrgCapabilities) error {
	capabilities.AdminSessionTTLSec = ClampAdminSessionTTL(capabilities.AdminSessionTTLSec)

	s.mu.Lock()
	s.data = capabilities
	s.mu.Unlock()

	return s.Save()
}
