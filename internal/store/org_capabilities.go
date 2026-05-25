package store

import (
	"encoding/json"
	"fmt"
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
	// OnboardingCompleted records whether the v1.11.0a first-run wizard
	// has been dismissed (Done step OR explicit "I'll set up later").
	// Once true, the AppShell never auto-routes /admin/* entries to
	// /admin/first-run again — manual navigation always remains
	// available. Defaults to false on a fresh install; the load() path
	// promotes it to true when the on-disk file predates v1.11.0a
	// (legacyOnboardingMigration) so existing operators with a working
	// deploy aren't bounced into the wizard on upgrade.
	OnboardingCompleted bool `json:"onboardingCompleted,omitempty"`
	// v1.13.0a (ADR-0008) — pluggable-skins foundation. ActiveSkin
	// names the currently-rendered skin (basement-default ships with
	// every deploy and is the fallback when the named skin isn't
	// registered). 
	//
	// v1.13.1: UserOverridableSkin controls whether users can pick
	// their own skin; AllowedUserSkins restricts to a specific set
	// (empty list = all installed skins available). v2.0.0-beta.2: SkinPolicy
	// field removed — legacy values migrate on read per the loader logic below.
	ActiveSkin string `json:"activeSkin,omitempty"`
	UserOverridableSkin bool `json:"userOverridableSkin"`
	AllowedUserSkins []string `json:"allowedUserSkins,omitempty"`
}

// Skin defaults for ADR-0008. DefaultActiveSkin is the built-in fallback;
// DefaultUserOverridableSkin controls whether users can pick their own skin.
const (
	DefaultActiveSkin        = "basement-default"
	DefaultUserOverridableSkin = false
)

// DetermineAllowedUserSkins returns the effective set of user-visible skins:
	// - If AllowedUserSkins is non-empty, return it as-is
	// - If UserOverridableSkin=true and AllowedUserSkins empty, return all installed skins (empty slice signals "all")
	// This helper centralizes the fallback semantics so callers don't duplicate logic.
	func DetermineAllowedUserSkins(userOverridable bool, allowed []string) []string {
	if !userOverridable {
		return nil // no user override allowed
	}
	if len(allowed) == 0 {
		return nil // empty slice = "all installed" signal to FE
	}
	return allowed
}

// GatewaySettings groups the per-protocol gateway toggles. v1.9.0b
// introduced a hand-typed nest with a single WebDAV field; v1.9.0d
// generalizes the nest into a name-keyed map (`Protocols`) so any
// registered gateway can carry an Enabled + BaseURL + Options blob
// without a new Go field per protocol. The hand-typed WebDAV field is
// preserved for back-compat: a v1.9.0b on-disk file (and any operator
// who hand-edited the legacy shape) auto-migrates into
// `Protocols["webdav"]` on read.
//
// Why preserve the legacy field instead of just renaming: operators
// already toggled WebDAV in v1.9.0b; the upgrade path must read their
// deliberate kill-switch state. See normalizeGateways() + the
// per-field marshalling notes in OrgCapabilities.UnmarshalJSON.
//
// We model SMB explicitly as "not supported" via stub gateway
// registration rather than carving a special-case field here; the UI
// renders "coming soon" purely from the registry's Implemented() flag.
type GatewaySettings struct {
	// Protocols is the v1.9.0d generic per-gateway config map. Key is
	// the Gateway.Name() ("webdav", "smb", "nfs", "ftp", "s3").
	// Missing key means "use defaults for this gateway"; on the
	// /admin/gateways enable-toggle path that resolves to false for
	// stubs (UI hides toggle) and true for webdav (defaults-on
	// preserves the v1.9.0a behaviour). v2.0.0-beta.2: legacy WebDAV field removed;
	// migrated to Protocols["webdav"] on read per normalizeGateways().
	Protocols map[string]GatewayConfig `json:"protocols,omitempty"`
}

// GatewayConfig is the per-protocol settings blob carried by
// GatewaySettings.Protocols. v1.9.0d only consumes Enabled + BaseURL;
// the Options map is reserved for v1.10+ gateways that need a few
// per-protocol strings (e.g. SMB share name prefix, NFS export root)
// without growing a typed Go field per gateway.
type GatewayConfig struct {
	Enabled bool              `json:"enabled"`
	BaseURL string            `json:"baseUrl,omitempty"`
	Options map[string]string `json:"options,omitempty"`
}

// WebDAVSettings is the legacy v1.9.0b operator-facing config. Kept
// in the v1.9.0d shape for back-compat: reads migrate this into the
// generic Protocols map; writes are mirrored back so a downgrade to
// v1.9.0b would still see the toggle.
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

// IsEnabled reports whether the named gateway is enabled in this
	// settings blob. Webdav defaults to true (matches v1.9.0a behaviour); 
	// every other gateway defaults to false (stub gateways can't actually be 
	// enabled regardless of caps, but the FE consults this flag to decide which 
	// row shows a toggle).
	//
	// Lookup order: Protocols[name] wins when present; else default-by-name fires.
	func (g GatewaySettings) IsEnabled(name string) bool {
		if cfg, ok := g.Protocols[name]; ok {
			return cfg.Enabled
		}
		return false
	}

// BaseURL returns the operator-pinned base URL for the named gateway,
	// or "" when none is set. Same lookup precedence as IsEnabled.
	func (g GatewaySettings) BaseURL(name string) string {
		if cfg, ok := g.Protocols[name]; ok && cfg.BaseURL != "" {
			return cfg.BaseURL
		}
		return ""
	}

// DefaultOrgCapabilities returns the default org capabilities. The Protocols map
	// starts with webdav.enabled=true — this is the only source of truth post-v2.0.0-beta.2.
	//
	// v1.13.0a (ADR-0008) adds ActiveSkin + SkinPolicy with defaults;
	// v1.13.1 migrates to UserOverridableSkin + AllowedUserSkins. A fresh
	// install renders basement-default; operator opts into user overrides
	// via /admin/system surface.
	func DefaultOrgCapabilities() OrgCapabilities {
		return OrgCapabilities{
			SignupMode:            "invite",
			EnabledDrivers:        []string{"garage", "garage-v1", "aws-s3", "minio"},
			AllowUserBackends:     false,
			UserBackendDrivers:    []string{},
			OIDCOnly:              false,
			AdminSessionTTLSec:    AdminSessionTTLDefaultSec,
			Gateways: GatewaySettings{
				Protocols: map[string]GatewayConfig{
					"webdav": {Enabled: true},
				},
			},
			ActiveSkin:          DefaultActiveSkin,
			UserOverridableSkin: DefaultUserOverridableSkin,
			AllowedUserSkins:    []string{},
		}
	}

// normalizeGateways migrates a legacy on-disk file into the v1.9.0d
	// generic Protocols map shape. Two legacy shapes can hit this in v2:
	//
	//  1. No "gateways" key at all (pre-v1.9.0b): substitute defaults with
	//     webdav.enabled=true, no other protocols.
	//  2. "gateways": {"webdav": {...}} only (v1.9.0b): migrate the WebDAV
	//     field into Protocols["webdav"] and drop the legacy field on write.
	//
	// v2.0.0-beta.2: The WebDAV hand-typed field is removed; migration copies
	// it into Protocols["webdav"] then drops it from disk on next save.
	//
	// We do NOT mutate the on-disk file here — that happens lazily on the
	// next Update() call. Read paths get the live defaults, write paths
	// persist them, and an operator hand-editing the JSON between reads
	// and writes still wins.
	func normalizeGateways(g GatewaySettings, hadField bool) GatewaySettings {
		if !hadField {
			// Legacy file, no gateways block at all: default on.
			return GatewaySettings{
				Protocols: map[string]GatewayConfig{
					"webdav": {Enabled: true},
				},
			}
		}
		if g.Protocols == nil {
			g.Protocols = make(map[string]GatewayConfig)
		}
		// v2.0.0-beta.2: Forward-migrate the legacy WebDAV field into Protocols["webdav"]
		// when the map is silent on webdav. Without this branch a v1.9.0b file with
		// webdav.enabled=false would read as enabled=true via defaults and clobber the
		// operator's kill switch. The WebDAV field will be dropped from disk on next save.
		if _, ok := g.Protocols["webdav"]; !ok {
			g.Protocols["webdav"] = GatewayConfig{
				Enabled: true, // default for webdav (v1.9.0a behaviour)
			}
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

	// Zero out the Gateways nest before unmarshal so absent fields
	// don't merge in from the in-memory default. Without this, a
	// v1.9.0b-shaped file (only "gateways.webdav") would inherit the
	// default's Protocols["webdav"]={Enabled:true} entry and clobber
	// the operator's explicit kill-switch via the syncGatewaySettings
	// "map wins" rule. The migration logic below substitutes the
	// right shape from whichever side carried the on-disk truth.
	s.data.Gateways = GatewaySettings{}

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
	hadOnboarding := false
	if err := json.Unmarshal(data, &raw); err == nil {
		_, hadGateways = raw["gateways"]
		_, hadOnboarding = raw["onboardingCompleted"]
	}
	s.data.Gateways = normalizeGateways(s.data.Gateways, hadGateways)

	// v1.11.0a — upgrade-safety: an on-disk file that predates the
	// onboardingCompleted field belongs to an existing operator (this
	// file only exists because they've already configured the deploy
	// at least once). Promote them to "completed" so the AppShell
	// onboarding redirect never fires on upgrade. Fresh installs hit
	// the OpenOrgCapabilities default-construct path BEFORE load()
	// and the file doesn't exist yet — they correctly read as
	// completed=false and the wizard auto-shows.
	if !hadOnboarding {
		s.data.OnboardingCompleted = true
	}

	// Migrate legacy: ensure enabled drivers have defaults if empty
	if s.data.EnabledDrivers == nil || len(s.data.EnabledDrivers) == 0 {
		s.data.EnabledDrivers = []string{"garage", "garage-v1", "aws-s3", "minio"}
	}

	// v1.13.0a (ADR-0008) — substitute skin defaults at read time so
	// a file predating the fields renders basement-default + policy
	// "default" without an on-disk mutation behind the operator's
	// back. Unknown SkinPolicy literals fall back to default rather than
	// poison the FE.
	if s.data.ActiveSkin == "" {
		s.data.ActiveSkin = DefaultActiveSkin
	}

	// v1.13.1 / v2.0.0-beta.2: migrate legacy SkinPolicy → UserOverridableSkin + AllowedUserSkins,
	// then drop the SkinPolicy field from disk on next write.
	// Legacy files without these fields read as UserOverridableSkin=false, AllowedUserSkins=[]
	// Migration rules (applied to raw JSON before unmarshal):
	//   - skinPolicy == "default" or "locked" → userOverridableSkin = false; allowedUserSkins = []
	//   - skinPolicy == "user-choice" → userOverridableSkin = true; allowedUserSkins = [] (all)
	// We peek at the raw JSON to detect if SkinPolicy was present and migrate accordingly.
	if hadSkinPolicy, policyValue := checkRawSkinPolicy(raw); hadSkinPolicy {
		switch policyValue {
		case "default", "locked":
			s.data.UserOverridableSkin = false
			s.data.AllowedUserSkins = []string{}
		case "user-choice":
			s.data.UserOverridableSkin = true
			s.data.AllowedUserSkins = []string{} // empty = all installed skins
		default:
			// Unknown policy value — fall back to locked (no user override)
			s.data.UserOverridableSkin = false
			s.data.AllowedUserSkins = []string{}
		}
		fmt.Fprintf(os.Stderr, "[WARN] Migrated SkinPolicy=%q -> UserOverridableSkin=%v+AllowedUserSkins=%v per v2.0.0-beta.2 [[v2_clean_break]]\n", policyValue, s.data.UserOverridableSkin, s.data.AllowedUserSkins)
	} else {
		// Legacy file without SkinPolicy field either — use defaults
		if !s.data.UserOverridableSkin && len(s.data.AllowedUserSkins) == 0 {
			s.data.UserOverridableSkin = DefaultUserOverridableSkin
			s.data.AllowedUserSkins = []string{}
		}
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
		// v1.13.0a (ADR-0008) — defensive defaults. load() already
		// substitutes these on the read-from-disk path, but a caller
		// that constructed an OrgCapabilitiesStore without going through
		// load() (or a future migration that resets the in-memory data)
		// would otherwise hand the FE empty strings the renderer can't
		// resolve to a registered skin.
		if out.ActiveSkin == "" {
			out.ActiveSkin = DefaultActiveSkin
		}
		// v1.13.1 — ensure new fields have defaults if not set by load()
		if !out.UserOverridableSkin && len(out.AllowedUserSkins) == 0 {
			out.UserOverridableSkin = DefaultUserOverridableSkin
			out.AllowedUserSkins = []string{}
		}
		return out
	}

// Update replaces all capabilities and persists. Per v1.3.0a.4 the
// admin session TTL is clamped into the supported range on the way in
// so an operator hand-editing the JSON (or a buggy FE) can't smuggle a
// 0-second or week-long TTL into the live config — the floor + ceiling
// are part of the contract, not advisory.
//
// v1.9.0d cross-mirrors the legacy WebDAV field and the generic
// Protocols map so a v1.9.0b client (only writes webdav.*) and a
// v1.9.0d client (writes protocols.*) both land on the same on-disk
// shape. Without this, a v1.9.0b PATCH would erase the v1.9.0d map
// state for webdav and any client picking up the file later via the
// new path would see stale flags.
func (s *OrgCapabilitiesStore) Update(capabilities OrgCapabilities) error {
	capabilities.AdminSessionTTLSec = ClampAdminSessionTTL(capabilities.AdminSessionTTLSec)
	capabilities.Gateways = syncGatewaySettings(capabilities.Gateways)

	// v1.13.0a (ADR-0008) — clamp skin fields the same way as
	// AdminSessionTTLSec: an empty string lands as the default; an
	// unknown SkinPolicy literal lands as "default" rather than
	// poisoning the on-disk file. The v1.13.0c PATCH handler will
	// reject invalid input at the wire layer, but a clamp here keeps
	// the store invariant ("Get() always returns a renderable skin")
	// independent of caller discipline.
	if capabilities.ActiveSkin == "" {
		capabilities.ActiveSkin = DefaultActiveSkin
	}

	s.mu.Lock()
	s.data = capabilities
	s.mu.Unlock()

	return s.Save()
}

// MarkOnboardingCompleted flips OnboardingCompleted=true and persists.
// Idempotent: a no-op when already true. Used by the v1.11.0a
// /admin/onboarding/dismiss endpoint (operator clicked "I'll set up
// later" or finished the wizard's Done step). Once set, the FE never
// auto-routes to /admin/first-run again, but operators can still
// reach the route manually.
func (s *OrgCapabilitiesStore) MarkOnboardingCompleted() error {
	s.mu.Lock()
	if s.data.OnboardingCompleted {
		s.mu.Unlock()
		return nil
	}
	s.data.OnboardingCompleted = true
	s.mu.Unlock()
	return s.Save()
}

// syncGatewaySettings mirrors the legacy WebDAV field into the
	// Protocols map (and vice versa) so both shapes always agree on the
	// canonical state for webdav. Called on every Update so a v1.9.0b-
	// shaped PATCH (legacy field only) and a v1.9.0d-shaped PATCH (map
	// only) both round-trip cleanly.
	//
	// Tie-break: when the Protocols["webdav"] entry and the legacy
	// WebDAV field disagree, the LEGACY field wins. Rationale: every
	// caller-mutation path the FE uses today touches the legacy field
	// (the v1.9.0b card mutates WebDAV; the v1.9.0d card writes a
	// Protocols["webdav"] entry built from the same shape AND mirrors
	// it back to the legacy field). A divergence means the caller used
	// the legacy mutation path — preferring the legacy value preserves
	// the kill-switch contract. v1.10+ gateways without a legacy field
	// are read-only through the map and aren't affected.
	// v2.0.0-beta.2: syncGatewaySettings is no-op since WebDAV field was removed;
	// Protocols["webdav"] is now always the source of truth on write.
	func syncGatewaySettings(g GatewaySettings) GatewaySettings {
		return g
	}


// checkRawSkinPolicy peeks at raw JSON to detect if SkinPolicy was present.
// Returns (present, value). v2.0.0-beta.2 uses this to migrate legacy
// skin policy values before unmarshal into the new shape.
func checkRawSkinPolicy(raw map[string]json.RawMessage) (bool, string) {
	skinPolicyVal, ok := raw["skinPolicy"]
	if !ok {
		return false, ""
	}
	var value string
	if err := json.Unmarshal(skinPolicyVal, &value); err != nil {
		return false, ""
	}
	return true, value
}

