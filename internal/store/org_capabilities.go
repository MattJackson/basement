package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// OrgCapabilities represents org-level feature flags and configuration.
type OrgCapabilities struct {
	SignupMode         string   `json:"signupMode"`          // "closed" | "invite" | "open"
	EnabledDrivers     []string `json:"enabledDrivers"`      // list of driver names
	AllowUserBackends  bool     `json:"allowUserBackends"`   // whether users can register their own clusters
	UserBackendDrivers []string `json:"userBackendDrivers"`  // subset of enabled drivers for user backends
	OIDCOnly           bool     `json:"oidcOnly"`            // hide local password login, OIDC only
}

// DefaultOrgCapabilities returns the default org capabilities.
func DefaultOrgCapabilities() OrgCapabilities {
	return OrgCapabilities{
		SignupMode:         "invite",
		EnabledDrivers:     []string{"garage", "garage-v1", "aws-s3", "minio"},
		AllowUserBackends:  false,
		UserBackendDrivers: []string{},
		OIDCOnly:           false,
	}
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

// Get returns a copy of the current capabilities.
func (s *OrgCapabilitiesStore) Get() OrgCapabilities {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data
}

// Update replaces all capabilities and persists.
func (s *OrgCapabilitiesStore) Update(capabilities OrgCapabilities) error {
	s.mu.Lock()
	s.data = capabilities
	s.mu.Unlock()

	return s.Save()
}
