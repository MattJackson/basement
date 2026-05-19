package store

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// User represents a user in the system.
type User struct {
	ID            string    `json:"id"`             // UUID
	Username      string    `json:"username"`
	PasswordHash  string    `json:"password_hash"`  // bcrypt
	Role          string    `json:"role"`           // "admin" | "user"
	OIDCSubject   string    `json:"oidc_subject,omitempty"`
	Created       time.Time `json:"created"`
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
