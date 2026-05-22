package store

import (
	"bytes"
	"testing"
)

// testKey is a 32-byte secret that satisfies the JWT min-length rule
// applied in production. Previously lived in the v1.1.0e-retired
// bucket_grants_test.go; user_regions_test.go now owns the only
// remaining callers.
var testKey = []byte("01234567890123456789012345678901")

func TestCrypto_RoundTrip(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	plain := []byte("hello bucket grant")

	ct, err := encryptSecret(plain, key)
	if err != nil {
		t.Fatalf("encryptSecret: %v", err)
	}
	if bytes.Equal(ct, plain) {
		t.Fatal("ciphertext equals plaintext")
	}
	if bytes.Contains(ct, plain) {
		t.Fatal("ciphertext contains plaintext bytes")
	}

	got, err := decryptSecret(ct, key)
	if err != nil {
		t.Fatalf("decryptSecret: %v", err)
	}
	if got != string(plain) {
		t.Errorf("roundtrip mismatch: got %q want %q", got, string(plain))
	}
}

func TestCrypto_DifferentNonceEachTime(t *testing.T) {
	key := []byte("test-key-material-need-some-len")
	plain := []byte("same plaintext every time")

	ct1, err := encryptSecret(plain, key)
	if err != nil {
		t.Fatalf("encrypt 1: %v", err)
	}
	ct2, err := encryptSecret(plain, key)
	if err != nil {
		t.Fatalf("encrypt 2: %v", err)
	}

	if bytes.Equal(ct1, ct2) {
		t.Fatal("two encryptions of the same plaintext produced identical ciphertexts — nonce reused or RNG broken")
	}

	// First 12 bytes are the nonce; they should differ.
	if bytes.Equal(ct1[:12], ct2[:12]) {
		t.Fatal("nonces match across two encryptions")
	}

	// Both should still decrypt to the same plaintext.
	for i, ct := range [][]byte{ct1, ct2} {
		got, err := decryptSecret(ct, key)
		if err != nil {
			t.Fatalf("decrypt %d: %v", i, err)
		}
		if got != string(plain) {
			t.Errorf("decrypt %d mismatch: got %q", i, got)
		}
	}
}

func TestCrypto_TamperedCiphertextFails(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	plain := []byte("don't tamper with me")

	ct, err := encryptSecret(plain, key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Tamper with a byte in the ciphertext body (after the 12-byte nonce).
	tampered := append([]byte(nil), ct...)
	tampered[len(tampered)-1] ^= 0x01

	if _, err := decryptSecret(tampered, key); err == nil {
		t.Fatal("expected decrypt to fail on tampered ciphertext (GCM auth tag check), got nil")
	}

	// Tamper with the nonce too.
	tamperedNonce := append([]byte(nil), ct...)
	tamperedNonce[0] ^= 0x01
	if _, err := decryptSecret(tamperedNonce, key); err == nil {
		t.Fatal("expected decrypt to fail on tampered nonce, got nil")
	}
}

func TestCrypto_WrongKeyFails(t *testing.T) {
	plain := []byte("secret stuff")
	ct, err := encryptSecret(plain, []byte("right-key-with-enough-entropy!!!"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if _, err := decryptSecret(ct, []byte("wrong-key-with-enough-entropy!!!")); err == nil {
		t.Fatal("expected decrypt with wrong key to fail")
	}
}

func TestCrypto_EmptyKeyRejected(t *testing.T) {
	if _, err := encryptSecret([]byte("x"), nil); err == nil {
		t.Error("encryptSecret with nil key should error")
	}
	if _, err := decryptSecret([]byte("x"), nil); err == nil {
		t.Error("decryptSecret with nil key should error")
	}
}

func TestCrypto_ShortCiphertextRejected(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	if _, err := decryptSecret([]byte("short"), key); err == nil {
		t.Error("decryptSecret with too-short input should error")
	}
}

func TestCrypto_VariableLengthKeyAccepted(t *testing.T) {
	// SHA-256 derivation should accept any non-empty key length.
	plain := []byte("portable plaintext")
	for _, key := range [][]byte{
		[]byte("k"),
		[]byte("medium-length-key"),
		bytes.Repeat([]byte("X"), 256),
	} {
		ct, err := encryptSecret(plain, key)
		if err != nil {
			t.Fatalf("encrypt with %d-byte key: %v", len(key), err)
		}
		got, err := decryptSecret(ct, key)
		if err != nil {
			t.Fatalf("decrypt with %d-byte key: %v", len(key), err)
		}
		if got != string(plain) {
			t.Errorf("%d-byte key roundtrip mismatch", len(key))
		}
	}
}
