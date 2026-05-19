package store

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Share represents a shareable link with optional expiration and password.
type Share struct {
	Token          string    `json:"token"`
	UserID         string    `json:"user_id"`
	Bucket         string    `json:"bucket"`
	Key            string    `json:"key"`
	Expires        time.Time `json:"expires"`
	PasswordHash   string    `json:"password_hash,omitempty"`
	Revoked        time.Time `json:"revoked,omitempty"` // zero if not revoked
	LastAccessed   time.Time `json:"last_accessed,omitempty"`
}

// CreateShare creates a new share with an auto-generated token.
func (s *Store) CreateShare(sh Share) error {
	s.sharesMu.Lock()
	defer s.sharesMu.Unlock()

	if sh.Token == "" {
		sh.Token = uuid.New().String()
	}

	now := time.Now()
	if sh.LastAccessed.IsZero() {
		sh.LastAccessed = now
	}

	s.sharesCache = append(s.sharesCache, sh)

	return saveJSON(s.sharesPath, s.sharesCache)
}

// Share returns a share by token. Returns error if not found.
func (s *Store) Share(token string) (Share, error) {
	s.sharesMu.RLock()
	defer s.sharesMu.RUnlock()

	for _, sh := range s.sharesCache {
		if sh.Token == token {
			return sh, nil
		}
	}

	return Share{}, fmt.Errorf("share not found: %s", token)
}

// SharesByUser returns all shares created by a specific user.
func (s *Store) SharesByUser(userID string) []Share {
	s.sharesMu.RLock()
	defer s.sharesMu.RUnlock()

	var result []Share
	for _, sh := range s.sharesCache {
		if sh.UserID == userID {
			result = append(result, sh)
		}
	}

	return result
}

// RevokeShare marks a share as revoked.
func (s *Store) RevokeShare(token string) error {
	s.sharesMu.Lock()
	defer s.sharesMu.Unlock()

	now := time.Now()
	for i := range s.sharesCache {
		if s.sharesCache[i].Token == token {
			s.sharesCache[i].Revoked = now
			return saveJSON(s.sharesPath, s.sharesCache)
		}
	}

	return fmt.Errorf("share not found: %s", token)
}

// TouchShare updates the LastAccessed timestamp for a share.
func (s *Store) TouchShare(token string, accessedAt time.Time) error {
	s.sharesMu.Lock()
	defer s.sharesMu.Unlock()

	for i := range s.sharesCache {
		if s.sharesCache[i].Token == token {
			s.sharesCache[i].LastAccessed = accessedAt
			return saveJSON(s.sharesPath, s.sharesCache)
		}
	}

	return fmt.Errorf("share not found: %s", token)
}
