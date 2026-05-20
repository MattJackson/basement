package store

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// User represents a user in the system.
//
// Provider + Subject are populated for OIDC-provisioned users (e.g.
// Provider="https://accounts.google.com", Subject="1234567890") and
// empty for local-password users. Existing local-password users
// continue to deserialize cleanly because both fields are omitempty.
type User struct {
	ID           string    `json:"id"` // UUID
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash,omitempty"` // bcrypt (empty for OIDC users)
	Role         string    `json:"role"`                    // "admin" | "user"
	UIAdmin      bool      `json:"uiAdmin,omitempty"`       // UI Admin axis: platform-level config access
	Provider     string    `json:"provider,omitempty"`      // OIDC issuer URL ("" = local password)
	Subject      string    `json:"subject,omitempty"`       // OIDC subject claim ("" = local password)
	OIDCSubject  string    `json:"oidc_subject,omitempty"`  // legacy field, kept for back-compat
	Email        string    `json:"email,omitempty"`
	Name         string    `json:"name,omitempty"`
	Created      time.Time `json:"created"`
}

// Users returns a deep copy of all users. Callers can mutate the returned slice freely.
func (s *Store) Users() []User {
	s.usersMu.RLock()
	defer s.usersMu.RUnlock()

	result := make([]User, len(s.usersCache))
	copy(result, s.usersCache)
	return result
}

// CreateUser creates a new user and persists it to disk.
func (s *Store) CreateUser(u User) error {
	s.usersMu.Lock()
	defer s.usersMu.Unlock()

	if u.ID == "" {
		u.ID = uuid.New().String()
	}

	now := time.Now()
	if u.Created.IsZero() {
		u.Created = now
	}

	s.usersCache = append(s.usersCache, u)

	return saveJSON(s.usersPath, s.usersCache)
}

// UpdateUser updates a user by ID using the provided mutation function.
func (s *Store) UpdateUser(id string, fn func(*User) error) error {
	s.usersMu.Lock()
	defer s.usersMu.Unlock()

	for i := range s.usersCache {
		if s.usersCache[i].ID == id {
			if err := fn(&s.usersCache[i]); err != nil {
				return err
			}
			return saveJSON(s.usersPath, s.usersCache)
		}
	}

	return fmt.Errorf("user not found: %s", id)
}

// DeleteUser deletes a user by ID.
func (s *Store) DeleteUser(id string) error {
	s.usersMu.Lock()
	defer s.usersMu.Unlock()

	for i := range s.usersCache {
		if s.usersCache[i].ID == id {
			s.usersCache = append(s.usersCache[:i], s.usersCache[i+1:]...)
			return saveJSON(s.usersPath, s.usersCache)
		}
	}

	return fmt.Errorf("user not found: %s", id)
}

// UserByUsername returns a user by username. Returns error if not found.
func (s *Store) UserByUsername(name string) (User, error) {
	s.usersMu.RLock()
	defer s.usersMu.RUnlock()

	for _, u := range s.usersCache {
		if u.Username == name {
			return u, nil
		}
	}

	return User{}, fmt.Errorf("user not found: %s", name)
}

// ErrUserNotFound is returned when a lookup does not find a matching user.
var ErrUserNotFound = fmt.Errorf("user not found")

// FindUserByProviderSubject returns a user whose (Provider, Subject) tuple
// matches the given identity. Both provider and subject must be non-empty;
// passing empty values returns ErrUserNotFound to prevent accidentally
// matching local-password users (whose Provider/Subject are both empty).
func (s *Store) FindUserByProviderSubject(provider, subject string) (User, error) {
	if provider == "" || subject == "" {
		return User{}, ErrUserNotFound
	}

	s.usersMu.RLock()
	defer s.usersMu.RUnlock()

	for _, u := range s.usersCache {
		if u.Provider == provider && u.Subject == subject {
			return u, nil
		}
	}

	return User{}, ErrUserNotFound
}
