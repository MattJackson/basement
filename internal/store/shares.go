package store

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Share represents a shareable link with optional expiration, password, and download limits.
type Share struct {
	Token         string     `json:"token"`
	OwnerUserID   string     `json:"ownerUserId"`
	ConnectionID  string     `json:"connectionId"`
	BucketID      string     `json:"bucketId"`
	Prefix        string     `json:"prefix,omitempty"`
	Key           string     `json:"key,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	ExpiresAt     *time.Time `json:"expiresAt,omitempty"`
	DownloadLimit *int       `json:"downloadLimit,omitempty"`
	DownloadsUsed int        `json:"downloadsUsed"`
	PasswordHash  string     `json:"passwordHash,omitempty"`
	Revoked       bool       `json:"revoked"`
}

// generateToken generates a URL-safe random token.
func generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// hashPassword hashes a plaintext password using bcrypt.
func hashPassword(password string) (string, error) {
	if password == "" {
		return "", nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(hash), nil
}

// CreateShare creates a new share with an auto-generated token.
func (s *Store) CreateShare(sh Share) error {
	s.sharesMu.Lock()
	defer s.sharesMu.Unlock()

	if sh.Token == "" {
		token, err := generateToken()
		if err != nil {
			return fmt.Errorf("generating token: %w", err)
		}
		sh.Token = token
	}

	now := time.Now()
	if sh.CreatedAt.IsZero() {
		sh.CreatedAt = now
	}

	s.sharesCache = append(s.sharesCache, sh)

	return saveJSON(s.sharesPath, s.sharesCache)
}

// Share returns a share by token. Returns error if not found or revoked.
func (s *Store) Share(token string) (Share, error) {
	s.sharesMu.RLock()
	defer s.sharesMu.RUnlock()

	for _, sh := range s.sharesCache {
		if sh.Token == token {
			if sh.Revoked {
				return Share{}, fmt.Errorf("share revoked: %s", token)
			}
			return sh, nil
		}
	}

	return Share{}, fmt.Errorf("share not found: %s", token)
}

// SharesByUser returns all non-revoked shares created by a specific user.
func (s *Store) SharesByUser(userID string) []Share {
	s.sharesMu.RLock()
	defer s.sharesMu.RUnlock()

	var result []Share
	for _, sh := range s.sharesCache {
		if sh.OwnerUserID == userID && !sh.Revoked {
			result = append(result, sh)
		}
	}

	return result
}

// RevokeShare marks a share as revoked. Returns error if not found.
func (s *Store) RevokeShare(token string) error {
	s.sharesMu.Lock()
	defer s.sharesMu.Unlock()

	for i := range s.sharesCache {
		if s.sharesCache[i].Token == token {
			s.sharesCache[i].Revoked = true
			return saveJSON(s.sharesPath, s.sharesCache)
		}
	}

	return fmt.Errorf("share not found: %s", token)
}

// IncrementDownloads increments the download count for a share. Returns error if not found or limit reached.
func (s *Store) IncrementDownloads(token string) error {
	s.sharesMu.Lock()
	defer s.sharesMu.Unlock()

	for i := range s.sharesCache {
		if s.sharesCache[i].Token == token {
			sh := &s.sharesCache[i]
			
			if sh.DownloadLimit != nil && sh.DownloadsUsed >= *sh.DownloadLimit {
				return fmt.Errorf("download limit reached for share: %s", token)
			}

			sh.DownloadsUsed++
			return saveJSON(s.sharesPath, s.sharesCache)
		}
	}

	return fmt.Errorf("share not found: %s", token)
}

// VerifyPassword checks if the provided password matches the share's password hash.
func (s *Store) VerifyPassword(token string, password string) error {
	sh, err := s.Share(token)
	if err != nil {
		return err
	}

	if sh.PasswordHash == "" {
		return fmt.Errorf("share does not require password")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(sh.PasswordHash), []byte(password)); err != nil {
		return fmt.Errorf("invalid password")
	}

	return nil
}

// IsExpired checks if a share has expired.
func (s *Store) IsExpired(token string) (bool, error) {
	sh, err := s.Share(token)
	if err != nil {
		return false, err
	}

	if sh.ExpiresAt == nil {
		return false, nil
	}

	return time.Now().After(*sh.ExpiresAt), nil
}

// loadShares loads shares from disk.
func loadShares(path string) ([]Share, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Share{}, nil
		}
		return nil, fmt.Errorf("reading shares file: %w", err)
	}

	var shares []Share
	if err := json.Unmarshal(data, &shares); err != nil {
		return nil, fmt.Errorf("unmarshaling shares: %w", err)
	}

	return shares, nil
}
