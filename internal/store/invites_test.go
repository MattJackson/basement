package store

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func newInvitesTestStore(t *testing.T) (Invites, func()) {
	t.Helper()
	tmp, err := os.MkdirTemp("", "invites-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	s, err := OpenInvites(tmp)
	if err != nil {
		_ = os.RemoveAll(tmp)
		t.Fatalf("OpenInvites: %v", err)
	}
	return s, func() { _ = os.RemoveAll(tmp) }
}

// TestInvites_CreateAndRedeem walks the happy path: create returns the
// plaintext token + persists a hashed record; redeem with that
// plaintext returns the record + removes it from the store.
func TestInvites_CreateAndRedeem(t *testing.T) {
	s, cleanup := newInvitesTestStore(t)
	defer cleanup()

	inv, plain, err := s.Create("wife", "matthew", 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if plain == "" {
		t.Fatalf("expected non-empty plaintext token")
	}
	if inv.ID == "" {
		t.Errorf("expected non-empty invite ID")
	}
	if inv.Label != "wife" {
		t.Errorf("expected Label=wife, got %q", inv.Label)
	}
	if inv.CreatedBy != "matthew" {
		t.Errorf("expected CreatedBy=matthew, got %q", inv.CreatedBy)
	}
	// Default TTL of 30 days
	want := 30 * 24 * time.Hour
	gotTTL := time.Until(inv.ExpiresAt)
	if gotTTL < want-time.Minute || gotTTL > want+time.Minute {
		t.Errorf("expected ~30d expiry, got %v", gotTTL)
	}
	if !strings.HasSuffix(plain, inv.TokenLast4) {
		t.Errorf("expected TokenLast4 to match plaintext suffix")
	}

	// Redeem with the plaintext token
	redeemed, err := s.Redeem(plain)
	if err != nil {
		t.Fatalf("Redeem: %v", err)
	}
	if redeemed.ID != inv.ID {
		t.Errorf("expected matched invite ID %q, got %q", inv.ID, redeemed.ID)
	}

	// One-shot: second redemption should fail
	if _, err := s.Redeem(plain); !errors.Is(err, ErrInviteNotFound) {
		t.Errorf("expected second redemption to fail with ErrInviteNotFound, got %v", err)
	}

	list, _ := s.List()
	if len(list) != 0 {
		t.Errorf("expected empty list after redemption, got %d", len(list))
	}
}

// TestInvites_Revoke removes the invite by ID. Subsequent redemption
// with the same plaintext fails.
func TestInvites_Revoke(t *testing.T) {
	s, cleanup := newInvitesTestStore(t)
	defer cleanup()

	inv, plain, err := s.Create("partner", "matthew", 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.Revoke(inv.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	if _, err := s.Redeem(plain); !errors.Is(err, ErrInviteNotFound) {
		t.Errorf("expected revoked token to fail redemption, got %v", err)
	}

	// Idempotency hint: revoking again returns not-found
	if err := s.Revoke(inv.ID); !errors.Is(err, ErrInviteNotFound) {
		t.Errorf("expected second revoke to return ErrInviteNotFound, got %v", err)
	}
}

// TestInvites_Rotate replaces the token + extends the expiry; the old
// plaintext stops redeeming, the new plaintext redeems successfully.
func TestInvites_Rotate(t *testing.T) {
	s, cleanup := newInvitesTestStore(t)
	defer cleanup()

	inv, oldPlain, err := s.Create("father", "matthew", 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	oldExpiry := inv.ExpiresAt

	// Sleep a tick so the new expiry strictly differs from the old one.
	time.Sleep(10 * time.Millisecond)

	rotated, newPlain, err := s.Rotate(inv.ID, 0)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if newPlain == oldPlain {
		t.Errorf("expected rotated token to differ from original")
	}
	if rotated.ID != inv.ID {
		t.Errorf("expected rotation to preserve ID")
	}
	if rotated.Label != "father" {
		t.Errorf("expected rotation to preserve Label")
	}
	if !rotated.ExpiresAt.After(oldExpiry) {
		t.Errorf("expected rotated expiry %v to be after %v", rotated.ExpiresAt, oldExpiry)
	}

	// Old token no longer redeems
	if _, err := s.Redeem(oldPlain); !errors.Is(err, ErrInviteNotFound) {
		t.Errorf("expected old token to stop working after rotate, got %v", err)
	}
	// New token redeems
	if _, err := s.Redeem(newPlain); err != nil {
		t.Errorf("expected new token to redeem, got %v", err)
	}
}

// TestInvites_ExpiryRejection: a token whose ExpiresAt is in the past
// fails redemption with ErrInviteExpired and is cleaned up from the
// store.
func TestInvites_ExpiryRejection(t *testing.T) {
	s, cleanup := newInvitesTestStore(t)
	defer cleanup()

	// Create with a tiny TTL, then sleep past it.
	_, plain, err := s.Create("expired", "matthew", 10*time.Millisecond)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	_, err = s.Redeem(plain)
	if !errors.Is(err, ErrInviteExpired) {
		t.Errorf("expected ErrInviteExpired, got %v", err)
	}

	// Expired row should have been cleaned up.
	list, _ := s.List()
	if len(list) != 0 {
		t.Errorf("expected expired invite to be removed, got %d rows", len(list))
	}
}

// TestInvites_RoundTripPersistence: state survives a reopen of the
// store (atomic-write disk path actually works).
func TestInvites_RoundTripPersistence(t *testing.T) {
	tmp, err := os.MkdirTemp("", "invites-persist-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	s1, err := OpenInvites(tmp)
	if err != nil {
		t.Fatalf("OpenInvites 1: %v", err)
	}
	inv, plain, err := s1.Create("partner", "matthew", 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Reopen — same data dir.
	s2, err := OpenInvites(tmp)
	if err != nil {
		t.Fatalf("OpenInvites 2: %v", err)
	}

	list, _ := s2.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 invite after reopen, got %d", len(list))
	}
	if list[0].ID != inv.ID {
		t.Errorf("expected same invite ID across reopen")
	}

	// Token still redeems on the reopened store.
	if _, err := s2.Redeem(plain); err != nil {
		t.Errorf("expected plaintext to still redeem after reopen, got %v", err)
	}
}

// TestInvites_WrongTokenFails: a bogus / never-issued token doesn't
// match any persisted row.
func TestInvites_WrongTokenFails(t *testing.T) {
	s, cleanup := newInvitesTestStore(t)
	defer cleanup()

	if _, _, err := s.Create("real", "matthew", 0); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := s.Redeem("bogus-token"); !errors.Is(err, ErrInviteNotFound) {
		t.Errorf("expected bogus token to fail with ErrInviteNotFound, got %v", err)
	}
}
