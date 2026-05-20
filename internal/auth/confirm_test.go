package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

// signPayload manually HMACs a payload so we can construct tokens that
// pass the signature check but have a malformed payload (covering the
// post-signature parse branches of VerifyConfirmToken).
func signPayload(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	return payload + "." + hex.EncodeToString(mac.Sum(nil))
}

// TestConfirmToken_SignedButTooFewFields covers the `len(fields) != 4`
// branch (signature OK, but payload has <4 pipe-separated fields).
func TestConfirmToken_SignedButTooFewFields(t *testing.T) {
	tok := signPayload(confirmSecret, "123|op|target") // only 3 fields
	err := VerifyConfirmToken(confirmSecret, tok, "op", "target", "user")
	if !errors.Is(err, ErrConfirmInvalid) {
		t.Fatalf("expected ErrConfirmInvalid for too-few-fields, got %v", err)
	}
}

// TestConfirmToken_SignedButBadExpInt covers the strconv.ParseInt failure
// branch (signature OK, but fields[0] isn't a valid int).
func TestConfirmToken_SignedButBadExpInt(t *testing.T) {
	tok := signPayload(confirmSecret, "not-an-int|op|target|user")
	err := VerifyConfirmToken(confirmSecret, tok, "op", "target", "user")
	if !errors.Is(err, ErrConfirmInvalid) {
		t.Fatalf("expected ErrConfirmInvalid for non-numeric exp, got %v", err)
	}
}

// TestConfirmToken_SignedButTooManyFields verifies that >4 fields are
// tolerated (SplitN(_, 4) caps at 4 — extra pipes end up in the userID
// field, so the userID match still succeeds when the request claims the
// full embedded-pipe userID).
func TestConfirmToken_SignedButTooManyFields(t *testing.T) {
	tok := signPayload(confirmSecret, "999999999999|op|target|userwith|pipes")
	err := VerifyConfirmToken(confirmSecret, tok, "op", "target", "userwith|pipes")
	if err != nil {
		t.Fatalf("expected nil (userID match including embedded pipes), got %v", err)
	}
}

// TestConfirmToken_ZeroSecret ensures a zero-length secret still produces
// a valid HMAC (Go's crypto/hmac handles zero-length keys).
func TestConfirmToken_ZeroSecret(t *testing.T) {
	zero := []byte{}
	tok := MintConfirmToken(zero, "op", "target", "user", 60*time.Second)
	if err := VerifyConfirmToken(zero, tok, "op", "target", "user"); err != nil {
		t.Errorf("zero-length secret roundtrip failed: %v", err)
	}
}

// TestConfirmToken_LongFields ensures very long fields don't break minting
// or verification.
func TestConfirmToken_LongFields(t *testing.T) {
	long := strings.Repeat("a", 4096)
	tok := MintConfirmToken(confirmSecret, "op", long, long, 60*time.Second)
	if err := VerifyConfirmToken(confirmSecret, tok, "op", long, long); err != nil {
		t.Errorf("long-fields roundtrip failed: %v", err)
	}
}
