// Package store: OIDC group-claim -> role auto-mapping config (v1.3.0a).
//
// On every OIDC login basement reads the user's ID-token claims and,
// for each mapping in this file whose claim value matches what the IdP
// asserted, grants the user the corresponding (roleId, scope) assignment
// with Source="oidc" + AutoAssigned=true. Stale auto-assignments (no
// longer matching the user's current claims) are revoked on the next
// login. Manually-assigned roles (Source="manual" or absent) are never
// touched by this sync — they're sacred.
//
// The config is operator-editable via PUT /api/v1/admin/oidc-group-mappings
// (gated on host:manage_policies). Persisted atomically the same way as
// every other JSON store (tmp + fsync + rename).
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// OIDCGroupMapping declares "if the OIDC ID token's `<Claim>` includes
// `<ClaimValue>`, grant the user `<RoleID>` at `<Scope>`."
//
// Claim is the dotted-path-free top-level OIDC claim name; common
// values are "groups" (Authentik / Keycloak default) or "roles".
// ClaimValue is a single string compared verbatim against either a
// string scalar or any element of a string array under that claim.
// RoleID must reference a role in policies.json; the OIDC sync skips
// (with a slog warning) mappings whose role no longer exists.
// Scope follows the ADR-0001 grammar — typically "host:*" but the
// operator may scope OIDC-granted roles to a single cluster too.
type OIDCGroupMapping struct {
	Claim      string `json:"claim"`
	ClaimValue string `json:"claimValue"`
	RoleID     string `json:"roleId"`
	Scope      string `json:"scope"`
}

// OIDCGroupMappings is the on-disk shape — a flat list plus the
// last-update timestamp so the UI can surface "edited 3h ago" without
// us re-deriving from filesystem mtime.
type OIDCGroupMappings struct {
	Mappings  []OIDCGroupMapping `json:"mappings"`
	UpdatedAt time.Time          `json:"updatedAt"`
}

// OIDCGroupMappingsStore is the file-backed store for the mappings
// config. Same RWMutex + atomic-write pattern as OrgCapabilitiesStore.
type OIDCGroupMappingsStore struct {
	mu   sync.RWMutex
	path string
	data OIDCGroupMappings
}

// OpenOIDCGroupMappings opens or creates the mappings store. Missing
// or empty file is fine — returns an empty mapping list.
func OpenOIDCGroupMappings(dataDir string) (*OIDCGroupMappingsStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("oidc-group-mappings: creating data dir: %w", err)
	}

	s := &OIDCGroupMappingsStore{
		path: filepath.Join(dataDir, "oidc_group_mappings.json"),
		data: OIDCGroupMappings{Mappings: []OIDCGroupMapping{}},
	}

	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return s, nil
}

func (s *OIDCGroupMappingsStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("oidc-group-mappings: reading %s: %w", s.path, err)
	}
	if len(data) == 0 {
		return nil
	}

	var on OIDCGroupMappings
	if err := json.Unmarshal(data, &on); err != nil {
		return fmt.Errorf("oidc-group-mappings: parsing %s: %w", s.path, err)
	}
	if on.Mappings == nil {
		on.Mappings = []OIDCGroupMapping{}
	}
	s.data = on
	return nil
}

// Get returns a defensive copy of the current mappings.
func (s *OIDCGroupMappingsStore) Get() OIDCGroupMappings {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := OIDCGroupMappings{
		UpdatedAt: s.data.UpdatedAt,
		Mappings:  make([]OIDCGroupMapping, len(s.data.Mappings)),
	}
	copy(out.Mappings, s.data.Mappings)
	return out
}

// Replace overwrites the full mapping list and persists atomically.
// Stamps UpdatedAt to now (UTC). nil slices are normalised to empty.
func (s *OIDCGroupMappingsStore) Replace(mappings []OIDCGroupMapping) error {
	// Reject mappings with any empty field. An empty ClaimValue or Claim is
	// the dangerous case: depending on the matcher it could match EVERY
	// login (auto-granting the mapped role to all OIDC users). RoleID/Scope
	// must be present so a mapping can't silently grant nothing-or-anything.
	for i, m := range mappings {
		if strings.TrimSpace(m.Claim) == "" || strings.TrimSpace(m.ClaimValue) == "" ||
			strings.TrimSpace(m.RoleID) == "" || strings.TrimSpace(m.Scope) == "" {
			return fmt.Errorf("oidc-group-mappings: mapping %d has an empty claim/claimValue/roleId/scope", i)
		}
	}

	s.mu.Lock()
	if mappings == nil {
		mappings = []OIDCGroupMapping{}
	}
	s.data = OIDCGroupMappings{
		Mappings:  mappings,
		UpdatedAt: time.Now().UTC(),
	}
	pending := s.data
	s.mu.Unlock()

	data, err := json.MarshalIndent(pending, "", "  ")
	if err != nil {
		return fmt.Errorf("oidc-group-mappings: marshaling: %w", err)
	}
	data = append(data, '\n')

	tmp := s.path + ".tmp"
	// 0600: this is authorization config (which group claim grants which
	// role) — must not be world-readable.
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("oidc-group-mappings: writing tmp file: %w", err)
	}
	f, err := os.OpenFile(tmp, os.O_RDONLY, 0600)
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("oidc-group-mappings: opening tmp for fsync: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("oidc-group-mappings: fsyncing tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("oidc-group-mappings: closing tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("oidc-group-mappings: renaming tmp to final: %w", err)
	}
	return nil
}
