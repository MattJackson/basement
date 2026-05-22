// Tests for ADR-0003 mode + mode-expires-at claims on the session JWT.
//
// IssueToken (the legacy entrypoint) MUST default to Mode="user",
// ModeExpiresAt=0 so existing callers don't accidentally mint elevated
// sessions. IssueTokenWithMode is the new explicit entrypoint used by
// the elevation handler.
package auth

import (
	"testing"
	"time"
)

// TestIssueToken_DefaultsToUserMode: any call through the legacy
// IssueToken signature gets Mode="user" + ModeExpiresAt=0. This is
// the ADR-0003 "default after login = USER" contract — every existing
// caller (loginHandler, oidcCallbackHandler, inviteRedeemHandler)
// continues to use IssueToken and inherits the safe default.
func TestIssueToken_DefaultsToUserMode(t *testing.T) {
	token, err := IssueToken(testSecret, "alice", "user", false, 24*time.Hour)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	claims, err := ParseToken(testSecret, token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}

	if claims.Mode != "user" {
		t.Errorf("Mode = %q, want %q", claims.Mode, "user")
	}
	if claims.ModeExpiresAt != 0 {
		t.Errorf("ModeExpiresAt = %d, want 0 (USER never expires at mode layer)", claims.ModeExpiresAt)
	}
}

// TestIssueTokenWithMode_RoundTrip: the explicit-mode entrypoint
// preserves both fields across sign + parse so the gate sees what the
// elevation handler put in.
func TestIssueTokenWithMode_RoundTrip(t *testing.T) {
	expiresAt := time.Now().Add(15 * time.Minute).Unix()

	token, err := IssueTokenWithMode(testSecret, "matthew", "admin", true,
		"admin", expiresAt, 24*time.Hour)
	if err != nil {
		t.Fatalf("IssueTokenWithMode: %v", err)
	}

	claims, err := ParseToken(testSecret, token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}

	if claims.UserID != "matthew" {
		t.Errorf("UserID = %q, want matthew", claims.UserID)
	}
	if claims.Role != "admin" {
		t.Errorf("Role = %q, want admin", claims.Role)
	}
	if !claims.UIAdmin {
		t.Errorf("UIAdmin = false, want true")
	}
	if claims.Mode != "admin" {
		t.Errorf("Mode = %q, want admin", claims.Mode)
	}
	if claims.ModeExpiresAt != expiresAt {
		t.Errorf("ModeExpiresAt = %d, want %d", claims.ModeExpiresAt, expiresAt)
	}
}

// TestIssueTokenWithMode_EmptyModeDefaultsToUser: an empty mode string
// becomes "user", matching the safe default the legacy IssueToken uses.
// Prevents an accidental zero-string from minting a privileged session.
func TestIssueTokenWithMode_EmptyModeDefaultsToUser(t *testing.T) {
	token, err := IssueTokenWithMode(testSecret, "alice", "user", false,
		"", 0, 24*time.Hour)
	if err != nil {
		t.Fatalf("IssueTokenWithMode: %v", err)
	}
	claims, err := ParseToken(testSecret, token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if claims.Mode != "user" {
		t.Errorf("empty mode minted as %q, want fallback to \"user\"", claims.Mode)
	}
}

// TestIssueTokenWithMode_LegacyElevatedRoundTrips: a v1.2-era cookie
// minted with mode="elevated" must still parse correctly so the gate's
// silent-migration logic (currentMode in policy_gates.go) can rewrite
// it to ADMIN on read. The JWT layer itself is mode-agnostic; the
// canonical-string normalisation happens above this layer.
func TestIssueTokenWithMode_LegacyElevatedRoundTrips(t *testing.T) {
	expiresAt := time.Now().Add(5 * time.Minute).Unix()

	token, err := IssueTokenWithMode(testSecret, "matthew", "admin", true,
		"elevated", expiresAt, 24*time.Hour)
	if err != nil {
		t.Fatalf("IssueTokenWithMode: %v", err)
	}
	claims, err := ParseToken(testSecret, token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if claims.Mode != "elevated" {
		t.Errorf("Mode = %q, want elevated (legacy claim must round-trip verbatim)", claims.Mode)
	}
	if claims.ModeExpiresAt != expiresAt {
		t.Errorf("ModeExpiresAt = %d, want %d", claims.ModeExpiresAt, expiresAt)
	}
}
