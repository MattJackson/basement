// Package store: persistent invite-token store (v1.3.0d).
//
// An Invite binds a one-shot, time-limited token to an optional invitee
// label so a Host Admin can hand a redemption URL to another user (a
// partner, family member, teammate) without needing to set their
// password directly. The token is generated server-side, returned to
// the admin once (so they can copy + send it), and stored as a bcrypt
// hash on disk — at redemption time the public endpoint takes the
// plaintext, hashes it, and compares against every active invite.
//
// On-disk file: {dataDir}/invites.json. Atomic write via the shared
// saveJSON helper (tmp + fsync + rename). Concurrency: a per-store
// RWMutex guards both the in-memory cache and the disk write.
//
// Lifecycle:
//   - CreateInvite mints a fresh token, hashes it, persists the
//     Invite record (without the plaintext), and returns the plaintext
//     to the caller exactly once.
//   - ListInvites returns every active (not-yet-redeemed, not-revoked)
//     invite. Expired invites stay in the list so the operator can see
//     which ones lapsed and reach for "Generate new token".
//   - RedeemInvite is called by the public redemption endpoint: it
//     looks up the matching active invite by hashing the supplied
//     plaintext, checks expiry, and on success marks it redeemed and
//     returns the Invite. The caller then provisions a User record.
//   - RevokeInvite removes the invite by ID — no soft-state, no audit
//     trail in this layer (audit happens at the API layer).
//   - RotateInvite replaces the token on an existing invite with a
//     fresh one (refreshing the expiry too). Useful if the original
//     token leaked or expired. Returns the new plaintext.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// Invite is one pending invite-token record. The plaintext Token never
// lives on disk — only TokenHash does — so a future op-DB dump can't be
// replayed to spoof invites. CreatedBy carries the host-admin UserID
// for the audit trail; Label is an optional human note (e.g. "wife").
type Invite struct {
	ID         string    `json:"id"`         // UUID
	TokenHash  string    `json:"tokenHash"`  // bcrypt(plaintext)
	TokenLast4 string    `json:"tokenLast4"` // last 4 chars of plaintext, for UI display
	Label      string    `json:"label,omitempty"`
	CreatedBy  string    `json:"createdBy,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// DefaultInviteTTL is the default expiry window for new invite tokens.
// Selected for the multi-user onboarding flow: long enough for the
// operator to text the link to their partner across timezones, short
// enough that a stale token can't sit valid forever.
const DefaultInviteTTL = 30 * 24 * time.Hour

// ErrInviteNotFound is returned when a lookup misses (by ID or by
// plaintext token). The redemption path collapses "no matching token"
// and "token expired" to the same error so the public endpoint never
// leaks which one of the two it was.
var ErrInviteNotFound = errors.New("invite not found")

// ErrInviteExpired is returned by Redeem when a matching invite exists
// but its ExpiresAt has passed. Kept as a separate sentinel so the
// admin-side endpoints (list / rotate) can show a precise reason; the
// public redemption endpoint translates it to "INVALID_OR_EXPIRED" so
// it doesn't leak invite existence.
var ErrInviteExpired = errors.New("invite expired")

// Invites is the CRUD surface for invite-token records. Mirrors the
// shape of UserRegions / Connections so a future store-wide refactor
// (e.g. SQLite) has a uniform set of interfaces to swap.
type Invites interface {
	Create(label, createdBy string, ttl time.Duration) (Invite, string, error)
	List() ([]Invite, error)
	Get(id string) (Invite, error)
	Redeem(plaintextToken string) (Invite, error)
	Revoke(id string) error
	Rotate(id string, ttl time.Duration) (Invite, string, error)
}

// invitesStore implements Invites on top of a JSON file.
type invitesStore struct {
	path string

	mu    sync.RWMutex
	cache []Invite
}

// OpenInvites opens or creates the invite-token store at dataDir. Empty
// or missing file is fine — first boot starts with no pending invites.
func OpenInvites(dataDir string) (Invites, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}

	s := &invitesStore{
		path:  filepath.Join(dataDir, "invites.json"),
		cache: make([]Invite, 0),
	}

	if err := s.load(); err != nil {
		return nil, fmt.Errorf("loading invites: %w", err)
	}

	return s, nil
}

func (s *invitesStore) load() error {
	rows, err := loadJSON[[]Invite](s.path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if rows == nil {
		s.cache = make([]Invite, 0)
	} else {
		s.cache = rows
	}
	return nil
}

func (s *invitesStore) save() error {
	return saveJSON(s.path, s.cache)
}

// generateInviteToken returns a 32-byte hex-encoded random token (64
// chars). High enough entropy (~256 bits) that an attacker can't
// brute-force the active-invite set even with a long expiry window.
func generateInviteToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Create mints a fresh invite token. Returns the Invite record and the
// plaintext token; the plaintext is the ONLY copy of the token and is
// never persisted, so the caller must surface it to the admin (who
// then hands it to the invitee). label is optional; ttl<=0 falls back
// to DefaultInviteTTL.
func (s *invitesStore) Create(label, createdBy string, ttl time.Duration) (Invite, string, error) {
	if ttl <= 0 {
		ttl = DefaultInviteTTL
	}

	plain, err := generateInviteToken()
	if err != nil {
		return Invite{}, "", fmt.Errorf("generating invite token: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return Invite{}, "", fmt.Errorf("hashing invite token: %w", err)
	}

	now := time.Now().UTC()
	inv := Invite{
		ID:         uuid.NewString(),
		TokenHash:  string(hash),
		TokenLast4: plain[len(plain)-4:],
		Label:      label,
		CreatedBy:  createdBy,
		CreatedAt:  now,
		ExpiresAt:  now.Add(ttl),
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.cache = append(s.cache, inv)
	if err := s.save(); err != nil {
		// Roll back the cache append so an in-memory failure doesn't
		// stay observable.
		s.cache = s.cache[:len(s.cache)-1]
		return Invite{}, "", fmt.Errorf("persisting invite: %w", err)
	}
	return inv, plain, nil
}

// List returns a copy of every persisted invite. Callers can mutate
// the returned slice freely.
func (s *invitesStore) List() ([]Invite, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Invite, len(s.cache))
	copy(out, s.cache)
	return out, nil
}

// Get returns one invite by ID.
func (s *invitesStore) Get(id string) (Invite, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, inv := range s.cache {
		if inv.ID == id {
			return inv, nil
		}
	}
	return Invite{}, ErrInviteNotFound
}

// Redeem looks up the invite whose hash matches the supplied plaintext,
// checks expiry, removes it from the store (one-shot), and returns the
// matched record. Missing / wrong-token returns ErrInviteNotFound;
// matched-but-expired returns ErrInviteExpired (and also removes the
// expired row, since the operator already has a list view to see what
// lapsed and the redemption path's job is to clean up after itself).
//
// Comparing bcrypt hashes is O(n) in the number of active invites,
// which is fine for a single-operator basement — the hot path here
// is "matthew has 1-5 pending invites at a time" not "1000-tenant
// SaaS." If invite counts grow we'd add a per-token random-prefix
// lookup, but that's a future-cycle concern.
func (s *invitesStore) Redeem(plaintextToken string) (Invite, error) {
	if plaintextToken == "" {
		return Invite{}, ErrInviteNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, inv := range s.cache {
		if err := bcrypt.CompareHashAndPassword([]byte(inv.TokenHash), []byte(plaintextToken)); err != nil {
			continue
		}
		// Hash matched. Now check expiry.
		if time.Now().UTC().After(inv.ExpiresAt) {
			// Expired: remove the row so the public redemption flow
			// doesn't keep matching it forever, and surface the
			// distinct error so admin-side callers can show the
			// precise reason.
			s.cache = append(s.cache[:i], s.cache[i+1:]...)
			if err := s.save(); err != nil {
				return Invite{}, fmt.Errorf("persisting expired-invite cleanup: %w", err)
			}
			return Invite{}, ErrInviteExpired
		}
		// One-shot: consume the invite on successful match.
		s.cache = append(s.cache[:i], s.cache[i+1:]...)
		if err := s.save(); err != nil {
			// Restore for atomicity — we don't want a half-redeemed
			// invite where the user creation might still succeed but
			// the invite reappears on reload.
			s.cache = append(s.cache, inv)
			return Invite{}, fmt.Errorf("persisting invite redemption: %w", err)
		}
		return inv, nil
	}
	return Invite{}, ErrInviteNotFound
}

// Revoke removes the invite by ID. Returns ErrInviteNotFound if no
// matching row exists (idempotency-friendly: caller can ignore the
// error if they don't care about distinguishing).
func (s *invitesStore) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, inv := range s.cache {
		if inv.ID == id {
			s.cache = append(s.cache[:i], s.cache[i+1:]...)
			return s.save()
		}
		_ = inv
	}
	return ErrInviteNotFound
}

// Rotate replaces the token on an existing invite with a fresh
// plaintext + hash + last-4, and refreshes ExpiresAt to now+ttl
// (ttl<=0 falls back to DefaultInviteTTL). The Label / CreatedBy
// fields are preserved. Returns the updated Invite + new plaintext.
func (s *invitesStore) Rotate(id string, ttl time.Duration) (Invite, string, error) {
	if ttl <= 0 {
		ttl = DefaultInviteTTL
	}

	plain, err := generateInviteToken()
	if err != nil {
		return Invite{}, "", fmt.Errorf("generating rotated token: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return Invite{}, "", fmt.Errorf("hashing rotated token: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.cache {
		if s.cache[i].ID != id {
			continue
		}
		now := time.Now().UTC()
		// Capture the original so we can roll back on save failure.
		original := s.cache[i]
		s.cache[i].TokenHash = string(hash)
		s.cache[i].TokenLast4 = plain[len(plain)-4:]
		s.cache[i].ExpiresAt = now.Add(ttl)

		if err := s.save(); err != nil {
			s.cache[i] = original
			return Invite{}, "", fmt.Errorf("persisting invite rotation: %w", err)
		}
		return s.cache[i], plain, nil
	}
	return Invite{}, "", ErrInviteNotFound
}
