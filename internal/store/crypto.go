// Package store: at-rest encryption helpers for credential blobs.
//
// Per ADR-0001 (v0.9.0c), per-user per-bucket S3 secret keys must NEVER
// hit disk in plaintext. v0.9.x uses AES-GCM with a key derived from the
// JWT signing secret via SHA-256 — single-server self-hosted threat
// model. Revisit before multi-tenant SaaS (ADR-0002 candidate).
//
// Wire format (per ciphertext value):
//
//   nonce(12) || gcm-ciphertext(plaintext + tag)
//
// 12-byte nonce prepended; GCM auth tag is appended by the cipher. A
// tampered byte anywhere in the blob causes Decrypt to error
// (auth-tag mismatch).
package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// deriveKey turns an arbitrary-length raw secret into the 32-byte key
// AES-256 needs. SHA-256 gives us a fixed-size, well-distributed key
// regardless of input length.
func deriveKey(raw []byte) [32]byte {
	return sha256.Sum256(raw)
}

// encryptSecret AES-GCM encrypts plaintext with a key derived from
// the supplied raw secret. Returns nonce || ciphertext.
//
// key must be non-empty; the actual cipher key is sha256(key).
func encryptSecret(plaintext, key []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, errors.New("encryptSecret: empty key")
	}

	derived := deriveKey(key)

	block, err := aes.NewCipher(derived[:])
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce read: %w", err)
	}

	// Seal appends ciphertext+tag to nonce, giving us nonce||ct in one alloc.
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decryptSecret reverses encryptSecret. Returns the plaintext as a string
// for convenience (callers wipe it ASAP). Returns an error if the blob is
// truncated, the auth tag fails, or the key is wrong.
func decryptSecret(ciphertext, key []byte) (string, error) {
	if len(key) == 0 {
		return "", errors.New("decryptSecret: empty key")
	}

	derived := deriveKey(key)

	block, err := aes.NewCipher(derived[:])
	if err != nil {
		return "", fmt.Errorf("aes.NewCipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("cipher.NewGCM: %w", err)
	}

	if len(ciphertext) < gcm.NonceSize() {
		return "", errors.New("decryptSecret: ciphertext too short")
	}

	nonce := ciphertext[:gcm.NonceSize()]
	ct := ciphertext[gcm.NonceSize():]

	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("gcm.Open: %w", err)
	}

	return string(plain), nil
}
