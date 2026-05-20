package store

import (
	"strings"
)

// Grant represents an access grant for a user on a bucket/key prefix.
type Grant struct {
	UserID      string   `json:"user_id"`
	Scope       string   `json:"scope,omitempty"`     // "bucket" | "cluster" | "cluster_admin"; defaults to "bucket" for legacy
	Bucket      string   `json:"bucket,omitempty"`    // used when scope="bucket" or ""
	Prefix      string   `json:"prefix,omitempty"`    // "" matches whole bucket (used with bucket scope)
	ConnectionID string  `json:"connection_id,omitempty"` // used when scope="cluster" or "cluster_admin"
	Permissions []string `json:"permissions,omitempty"` // subset of {list,read,write,delete,share}
}

// LegacyGrant is the old schema for migration.
type LegacyGrant struct {
	UserID      string   `json:"user_id"`
	Bucket      string   `json:"bucket"`
	Prefix      string   `json:"prefix"`
	Permissions []string `json:"permissions"`
}

// Grants returns all grants for a specific user. Returns an empty slice if none found.
func (s *Store) Grants(userID string) []Grant {
	s.grantsMu.RLock()
	defer s.grantsMu.RUnlock()

	userGrants, ok := s.grantsCache[userID]
	if !ok {
		return nil
	}

	result := make([]Grant, len(userGrants))
	copy(result, userGrants)

	// Migrate legacy grants on read: set scope="bucket" for entries without it
	for i := range result {
		if result[i].Scope == "" && result[i].Bucket != "" {
			result[i].Scope = "bucket"
		}
	}

	return result
}

// SetGrants replaces all grants for a user.
func (s *Store) SetGrants(userID string, grants []Grant) error {
	s.grantsMu.Lock()
	defer s.grantsMu.Unlock()

	if s.grantsCache == nil {
		s.grantsCache = make(map[string][]Grant)
	}

	// Deep copy the grants slice
	newGrants := make([]Grant, len(grants))
	copy(newGrants, grants)

	s.grantsCache[userID] = newGrants

	return saveJSON(s.grantsPath, s.grantsCache)
}

// MatchGrant finds the longest-prefix matching grant for a user on bucket+key.
// Returns the matching Grant and true if found, otherwise zero Grant and false.
func (s *Store) MatchGrant(userID, bucket, key string) (Grant, bool) {
	s.grantsMu.RLock()
	defer s.grantsMu.RUnlock()

	userGrants, ok := s.grantsCache[userID]
	if !ok || len(userGrants) == 0 {
		return Grant{}, false
	}

	var bestMatch Grant
	bestPrefixLen := -1

	for _, g := range userGrants {
		if g.Bucket != bucket {
			continue
		}

		// If grant prefix is empty, it matches the whole bucket
		if g.Prefix == "" {
			prefixLen := len(bucket) + 1 // bucket/
			if bestPrefixLen < prefixLen {
				bestMatch = g
				bestPrefixLen = prefixLen
			}
			continue
		}

		// Check if key starts with bucket/prefix
		expectedPath := bucket + "/" + g.Prefix
		if strings.HasPrefix(key, expectedPath) {
			prefixLen := len(expectedPath)
			if bestPrefixLen < prefixLen {
				bestMatch = g
				bestPrefixLen = prefixLen
			}
		}
	}

	return bestMatch, bestPrefixLen >= 0
}

// ClusterAdminGrants returns all connection IDs where the user has cluster_admin scope.
func (s *Store) ClusterAdminGrants(userID string) []string {
	s.grantsMu.RLock()
	defer s.grantsMu.RUnlock()

	var result []string
	userGrants, ok := s.grantsCache[userID]
	if !ok {
		return result
	}

	for _, g := range userGrants {
		if g.Scope == "cluster_admin" && g.ConnectionID != "" {
			result = append(result, g.ConnectionID)
		}
	}

	return result
}

// ClusterReadGrants returns all connection IDs where the user has cluster scope.
func (s *Store) ClusterReadGrants(userID string) []string {
	s.grantsMu.RLock()
	defer s.grantsMu.RUnlock()

	var result []string
	userGrants, ok := s.grantsCache[userID]
	if !ok {
		return result
	}

	for _, g := range userGrants {
		if g.Scope == "cluster" && g.ConnectionID != "" {
			result = append(result, g.ConnectionID)
		}
	}

	return result
}

// BucketGrants returns all connection IDs where the user has bucket-level grants.
func (s *Store) BucketGrants(userID string) []string {
	s.grantsMu.RLock()
	defer s.grantsMu.RUnlock()

	connIDs := make(map[string]bool)
	userGrants, ok := s.grantsCache[userID]
	if !ok {
		return nil
	}

	for _, g := range userGrants {
		if g.Scope == "bucket" && g.Bucket != "" && g.ConnectionID != "" {
			connIDs[g.ConnectionID] = true
		}
	}

	result := make([]string, 0, len(connIDs))
	for cid := range connIDs {
		result = append(result, cid)
	}

	return result
}
