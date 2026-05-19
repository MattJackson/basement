package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var confirmSecret = []byte("test-secret-bytes-do-not-use-in-production-32-bytes-min")

func TestConfirmToken_RoundTrip(t *testing.T) {
	tok := MintConfirmToken(confirmSecret, "delete:bucket", "bucket-id-1", "user-42", 60*time.Second)
	if err := VerifyConfirmToken(confirmSecret, tok, "delete:bucket", "bucket-id-1", "user-42"); err != nil {
		t.Fatalf("expected verify success, got %v", err)
	}
}

func TestConfirmToken_ExpiredRejected(t *testing.T) {
	tok := MintConfirmToken(confirmSecret, "delete:bucket", "bucket-id-1", "user-42", -1*time.Second)
	err := VerifyConfirmToken(confirmSecret, tok, "delete:bucket", "bucket-id-1", "user-42")
	if !errors.Is(err, ErrConfirmInvalid) {
		t.Fatalf("expected ErrConfirmInvalid for expired token, got %v", err)
	}
}

func TestConfirmToken_WrongTargetRejected(t *testing.T) {
	tok := MintConfirmToken(confirmSecret, "delete:bucket", "bucket-id-1", "user-42", 60*time.Second)
	err := VerifyConfirmToken(confirmSecret, tok, "delete:bucket", "bucket-id-2", "user-42")
	if !errors.Is(err, ErrConfirmMismatch) {
		t.Fatalf("expected ErrConfirmMismatch for wrong target, got %v", err)
	}
}

func TestConfirmToken_WrongUserRejected(t *testing.T) {
	tok := MintConfirmToken(confirmSecret, "delete:bucket", "bucket-id-1", "user-42", 60*time.Second)
	err := VerifyConfirmToken(confirmSecret, tok, "delete:bucket", "bucket-id-1", "user-99")
	if !errors.Is(err, ErrConfirmMismatch) {
		t.Fatalf("expected ErrConfirmMismatch for wrong user, got %v", err)
	}
}

func TestConfirmToken_WrongOpRejected(t *testing.T) {
	tok := MintConfirmToken(confirmSecret, "delete:bucket", "bucket-id-1", "user-42", 60*time.Second)
	err := VerifyConfirmToken(confirmSecret, tok, "delete:key", "bucket-id-1", "user-42")
	if !errors.Is(err, ErrConfirmMismatch) {
		t.Fatalf("expected ErrConfirmMismatch for wrong op, got %v", err)
	}
}

func TestConfirmToken_BadSignatureRejected(t *testing.T) {
	tok := MintConfirmToken(confirmSecret, "delete:bucket", "bucket-id-1", "user-42", 60*time.Second)
	// Flip the last hex char of the signature.
	parts := strings.SplitN(tok, ".", 2)
	if len(parts) != 2 {
		t.Fatalf("expected payload.sig format, got %q", tok)
	}
	sig := parts[1]
	flipped := sig[:len(sig)-1]
	if sig[len(sig)-1] == 'a' {
		flipped += "b"
	} else {
		flipped += "a"
	}
	err := VerifyConfirmToken(confirmSecret, parts[0]+"."+flipped, "delete:bucket", "bucket-id-1", "user-42")
	if !errors.Is(err, ErrConfirmInvalid) {
		t.Fatalf("expected ErrConfirmInvalid for tampered sig, got %v", err)
	}
}

func TestConfirmToken_WrongSecretRejected(t *testing.T) {
	tok := MintConfirmToken(confirmSecret, "delete:bucket", "bucket-id-1", "user-42", 60*time.Second)
	otherSecret := []byte("a-totally-different-32-byte-test-secret-value-here-yep")
	err := VerifyConfirmToken(otherSecret, tok, "delete:bucket", "bucket-id-1", "user-42")
	if !errors.Is(err, ErrConfirmInvalid) {
		t.Fatalf("expected ErrConfirmInvalid for wrong secret, got %v", err)
	}
}

func TestConfirmToken_MalformedRejected(t *testing.T) {
	cases := []string{
		"",
		"no-dot",
		"only.one.thing",
		"abc.def",
		"123:op:tgt:user.notHexAtAll",
	}
	for _, tc := range cases {
		err := VerifyConfirmToken(confirmSecret, tc, "delete:bucket", "tgt", "user")
		if !errors.Is(err, ErrConfirmInvalid) {
			t.Errorf("expected ErrConfirmInvalid for %q, got %v", tc, err)
		}
	}
}
