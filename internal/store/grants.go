package store

import (
	"strings"
)

// Grant represents an access grant for a user on a bucket/key prefix.
type Grant struct {
	UserID      string   `json:"user_id"`
	Bucket      string   `json:"bucket"`
	Prefix      string   `json:"prefix"`              // "" matches whole bucket
	Permissions []string `json:"permissions"`         // subset of {list,read,write,delete,share}
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
