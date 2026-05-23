package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"golang.org/x/crypto/bcrypt"
)

// jwtSecretFile is the on-disk persistence path (relative to DataDir)
// for an auto-generated JWT signing secret. Created with 0600 perms so
// only the owning UID can read it. Format: hex-encoded raw bytes.
const jwtSecretFile = ".jwt-secret"

// initialAdminPasswordFile is the on-disk record of an auto-generated
// admin password. The plaintext lives here exactly long enough for the
// operator to read it on first boot; once they change the password via
// /admin/users the file is irrelevant and can be deleted (basement never
// reads it back). 0600 perms.
const initialAdminPasswordFile = ".initial-admin-password"

// bootstrapCost is the bcrypt cost used when hashing an auto-generated
// or env-supplied plaintext admin password. Mirrors auth.bcryptCost
// (=12); duplicated here to avoid an import cycle (auth depends on
// config, not the other way round).
const bootstrapCost = 12

// applyBootstrap fills in any missing pieces of the config from
// auto-generated secrets persisted under cfg.DataDir. The four
// behaviours:
//
//  1. BASEMENT_JWT_SECRET unset: read .jwt-secret if present, otherwise
//     generate 32 random bytes + write the file (0600) + log a warning
//     that the operator should set the env var explicitly for production.
//  2. BASEMENT_ADMIN_PASSWORD_HASH unset AND BASEMENT_ADMIN_PASSWORD set:
//     bcrypt-hash the plaintext at boot, never persist. Convenience for
//     `docker run -e BASEMENT_ADMIN_PASSWORD=...`.
//  3. Both unset: read .initial-admin-password if present (reuse the hash
//     basement minted on a prior boot), otherwise generate a random
//     password, bcrypt it, log it to stdout + write plaintext to
//     .initial-admin-password (0600) for operator retrieval.
//  4. BASEMENT_ADMIN_USER unset when 2 or 3 fires: default to "admin".
//
// Returns the first error encountered. Bootstrap is a no-op for an
// operator who set every env var explicitly — zero behavioural change.
//
// Called from Load() before validation, after env parsing, so the
// validator sees the post-bootstrap state.
func applyBootstrap(cfg *Config) error {
	if cfg.DataDir == "" {
		// Defensive: Load() always sets a default, but guard anyway so
		// a misuse from tests doesn't write to the filesystem root.
		return errors.New("bootstrap: DataDir is empty")
	}

	// Best-effort MkdirAll. If the host hasn't given us permission, the
	// existing main.go warn-not-exit path will surface that diagnostic
	// after Load returns; bootstrap silently degrades to "no auto-gen"
	// rather than failing the whole boot.
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		slog.Warn("bootstrap: data dir not creatable — auto-bootstrap disabled, basement will refuse to start unless env vars are set explicitly", "dir", cfg.DataDir, "error", err)
		return nil
	}

	if err := bootstrapJWTSecret(cfg); err != nil {
		return err
	}
	if err := bootstrapAdminPassword(cfg); err != nil {
		return err
	}
	return nil
}

// bootstrapJWTSecret implements the JWT-secret auto-generation path.
// No-op when the env var was set. Reads .jwt-secret if present;
// otherwise generates 32 random bytes, persists them, and logs a
// production-posture warning.
func bootstrapJWTSecret(cfg *Config) error {
	if len(cfg.JWT.Secret) != 0 {
		slog.Info("JWT signing secret loaded from BASEMENT_JWT_SECRET env var")
		return nil
	}

	path := filepath.Join(cfg.DataDir, jwtSecretFile)
	existing, err := os.ReadFile(path)
	if err == nil && len(existing) > 0 {
		decoded, decodeErr := hex.DecodeString(string(existing))
		if decodeErr != nil {
			return fmt.Errorf("bootstrap: %s exists but is not valid hex: %w", path, decodeErr)
		}
		if len(decoded) < 32 {
			return fmt.Errorf("bootstrap: %s is only %d bytes; expected >= 32", path, len(decoded))
		}
		cfg.JWT.Secret = decoded
		slog.Info("auto-generated JWT signing secret loaded from disk", "path", path)
		return nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("bootstrap: read %s: %w", path, err)
	}

	// Fresh generate.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("bootstrap: crypto/rand: %w", err)
	}
	if err := writeSecret(path, []byte(hex.EncodeToString(raw))); err != nil {
		return fmt.Errorf("bootstrap: write %s: %w", path, err)
	}
	cfg.JWT.Secret = raw
	slog.Warn("auto-generated JWT signing secret persisted to disk; set BASEMENT_JWT_SECRET explicitly for production", "path", path)
	return nil
}

// bootstrapAdminPassword implements the three admin-password paths in
// priority order: explicit hash > plaintext convenience > full auto-gen.
func bootstrapAdminPassword(cfg *Config) error {
	if cfg.Admin.PasswordHash != "" {
		// Operator set the hash explicitly — nothing to do.
		return nil
	}

	// Default the username when bootstrap is going to mint a hash.
	// (Validation still catches the case where the operator set HASH
	// but not USER — that's a misconfiguration, not a bootstrap path.)
	if cfg.Admin.User == "" {
		cfg.Admin.User = "admin"
	}

	plaintext := os.Getenv("BASEMENT_ADMIN_PASSWORD")
	if plaintext != "" {
		hash, err := bcryptHash(plaintext)
		if err != nil {
			return fmt.Errorf("bootstrap: bcrypt BASEMENT_ADMIN_PASSWORD: %w", err)
		}
		cfg.Admin.PasswordHash = hash
		slog.Info("admin password hashed from BASEMENT_ADMIN_PASSWORD env var (plaintext never persisted)", "user", cfg.Admin.User)
		return nil
	}

	// Both env vars unset — full auto-generate path. Reuse the on-disk
	// password if one already exists (operator may have already seen
	// it on a prior boot); otherwise mint a fresh one.
	path := filepath.Join(cfg.DataDir, initialAdminPasswordFile)
	existing, err := os.ReadFile(path)
	if err == nil && len(existing) > 0 {
		hash, hashErr := bcryptHash(string(existing))
		if hashErr != nil {
			return fmt.Errorf("bootstrap: bcrypt persisted initial admin password: %w", hashErr)
		}
		cfg.Admin.PasswordHash = hash
		slog.Info("initial admin password loaded from disk (operator should change via /admin/users)", "user", cfg.Admin.User, "path", path)
		return nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("bootstrap: read %s: %w", path, err)
	}

	// Fresh mint.
	pw, err := randomPassword(24)
	if err != nil {
		return fmt.Errorf("bootstrap: generate random password: %w", err)
	}
	hash, err := bcryptHash(pw)
	if err != nil {
		return fmt.Errorf("bootstrap: bcrypt generated password: %w", err)
	}
	if err := writeSecret(path, []byte(pw)); err != nil {
		return fmt.Errorf("bootstrap: write %s: %w", path, err)
	}
	cfg.Admin.PasswordHash = hash

	// Stdout banner so `docker logs basement | grep "INITIAL ADMIN
	// PASSWORD"` works for the install-script handoff. Printed via
	// fmt.Println (not slog) so the line is human-readable plain text
	// regardless of BASEMENT_LOG_LEVEL or the JSON handler installed
	// in main.go.
	fmt.Println("INITIAL ADMIN PASSWORD: " + pw)
	slog.Warn("auto-generated INITIAL ADMIN PASSWORD printed to stdout and persisted to disk; change it via /admin/users after first login", "user", cfg.Admin.User, "path", path)
	return nil
}

// writeSecret atomically writes data to path with 0600 perms. Uses a
// tmp+rename so a crash mid-write can't leave a half-written file.
func writeSecret(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// bcryptHash wraps bcrypt.GenerateFromPassword to keep call sites tidy.
// Cost mirrors internal/auth.bcryptCost (=12).
func bcryptHash(plaintext string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plaintext), bootstrapCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// randomPassword returns a URL-safe random password of the requested
// rune length. Uses base32-style alphabet (no padding, no ambiguous
// characters) so the operator can read + retype the password from a
// terminal without misreading 0/O or 1/l.
func randomPassword(length int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"
	out := make([]byte, length)
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out), nil
}
