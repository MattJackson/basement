package config

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// setBootstrapEnv wipes the env vars that drive the bootstrap path so
// each test starts from a known-empty baseline. Driver vars are set to
// keep validation happy; the test under examination then overrides
// individual entries as needed via t.Setenv.
func setBootstrapEnv(t *testing.T, dataDir string) {
	t.Helper()
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "tok")
	t.Setenv("BASEMENT_DATA_DIR", dataDir)
	// Explicitly unset every var bootstrap reads — t.Setenv("", "")
	// records the cleanup, so a parent test's setenv doesn't leak in.
	t.Setenv("BASEMENT_ADMIN_USER", "")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "")
	t.Setenv("BASEMENT_ADMIN_PASSWORD", "")
	t.Setenv("BASEMENT_JWT_SECRET", "")
}

// TestBootstrap_NoEnvVars covers the headline "docker run with empty
// env" path: every secret missing → bootstrap mints + persists + sets
// admin defaults so Load() returns a usable config.
func TestBootstrap_NoEnvVars(t *testing.T) {
	dataDir := t.TempDir()
	setBootstrapEnv(t, dataDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// JWT secret minted + persisted.
	if len(cfg.JWT.Secret) != 32 {
		t.Errorf("JWT.Secret length=%d, want 32", len(cfg.JWT.Secret))
	}
	jwtPath := filepath.Join(dataDir, jwtSecretFile)
	if info, err := os.Stat(jwtPath); err != nil {
		t.Errorf("expected %s on disk: %v", jwtPath, err)
	} else if info.Mode().Perm() != 0600 {
		t.Errorf("%s perms=%v, want 0600", jwtPath, info.Mode().Perm())
	}
	// Disk content should hex-decode to exactly cfg.JWT.Secret.
	raw, err := os.ReadFile(jwtPath)
	if err != nil {
		t.Fatalf("read %s: %v", jwtPath, err)
	}
	decoded, err := hex.DecodeString(string(raw))
	if err != nil {
		t.Fatalf("hex decode %s: %v", jwtPath, err)
	}
	if string(decoded) != string(cfg.JWT.Secret) {
		t.Error("persisted JWT secret does not match cfg.JWT.Secret")
	}

	// Admin password hash minted + plaintext persisted.
	if cfg.Admin.User != "admin" {
		t.Errorf("Admin.User=%q, want \"admin\" (bootstrap default)", cfg.Admin.User)
	}
	if cfg.Admin.PasswordHash == "" {
		t.Error("Admin.PasswordHash is empty after bootstrap")
	}
	pwPath := filepath.Join(dataDir, initialAdminPasswordFile)
	pwBytes, err := os.ReadFile(pwPath)
	if err != nil {
		t.Fatalf("expected %s on disk: %v", pwPath, err)
	}
	if info, _ := os.Stat(pwPath); info.Mode().Perm() != 0600 {
		t.Errorf("%s perms=%v, want 0600", pwPath, info.Mode().Perm())
	}
	// Persisted plaintext must verify against the persisted hash.
	if err := bcrypt.CompareHashAndPassword([]byte(cfg.Admin.PasswordHash), pwBytes); err != nil {
		t.Errorf("bcrypt compare: persisted plaintext does not verify against hash: %v", err)
	}
	if len(pwBytes) < 16 {
		t.Errorf("persisted password length=%d, want >= 16", len(pwBytes))
	}
}

// TestBootstrap_PlaintextPasswordEnvVar covers the
// `docker run -e BASEMENT_ADMIN_PASSWORD=...` convenience path. Bootstrap
// must bcrypt the plaintext at boot, never persist it, and skip the
// auto-generation random-password code path.
func TestBootstrap_PlaintextPasswordEnvVar(t *testing.T) {
	dataDir := t.TempDir()
	setBootstrapEnv(t, dataDir)
	t.Setenv("BASEMENT_ADMIN_PASSWORD", "operator-supplied-pw")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Admin.PasswordHash == "" {
		t.Fatal("Admin.PasswordHash empty after plaintext bootstrap")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(cfg.Admin.PasswordHash), []byte("operator-supplied-pw")); err != nil {
		t.Errorf("bcrypt compare: hash does not verify against plaintext env var: %v", err)
	}
	// Plaintext must NOT have been persisted.
	pwPath := filepath.Join(dataDir, initialAdminPasswordFile)
	if _, err := os.Stat(pwPath); !os.IsNotExist(err) {
		t.Errorf("plaintext password file should not exist when BASEMENT_ADMIN_PASSWORD is supplied; got err=%v", err)
	}
}

// TestBootstrap_ExistingJWTSecretFile covers the reboot path: an
// existing .jwt-secret on disk must be reused, never rotated. Same
// secret across container restarts means existing user sessions
// survive.
func TestBootstrap_ExistingJWTSecretFile(t *testing.T) {
	dataDir := t.TempDir()
	setBootstrapEnv(t, dataDir)

	// Seed a known secret on disk.
	known := []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef") // 32 bytes hex
	if err := os.WriteFile(filepath.Join(dataDir, jwtSecretFile), known, 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	wantDecoded, _ := hex.DecodeString(string(known))
	if string(cfg.JWT.Secret) != string(wantDecoded) {
		t.Error("JWT.Secret was regenerated despite an existing .jwt-secret file")
	}
}

// TestBootstrap_ExistingInitialPasswordFile covers the same reboot
// path for the admin password: an existing .initial-admin-password
// reuses the same plaintext so the operator who saw the password on
// boot 1 can still log in after a container restart.
func TestBootstrap_ExistingInitialPasswordFile(t *testing.T) {
	dataDir := t.TempDir()
	setBootstrapEnv(t, dataDir)

	known := "operator-already-saw-this"
	if err := os.WriteFile(filepath.Join(dataDir, initialAdminPasswordFile), []byte(known), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(cfg.Admin.PasswordHash), []byte(known)); err != nil {
		t.Errorf("persisted password was not reused: %v", err)
	}
}

// TestBootstrap_ExplicitEnvVars_NoOp pins the most important guarantee:
// an operator who set every env var explicitly sees ZERO behavioural
// change. Bootstrap must early-return without touching disk.
func TestBootstrap_ExplicitEnvVars_NoOp(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "tok")
	t.Setenv("BASEMENT_DATA_DIR", dataDir)
	t.Setenv("BASEMENT_ADMIN_USER", "operator")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Admin.User != "operator" {
		t.Errorf("Admin.User=%q, want \"operator\" (env var not overridden)", cfg.Admin.User)
	}
	if cfg.Admin.PasswordHash != "$2a$12$abcdefghijklmnopqrstuv" {
		t.Error("PasswordHash was rewritten despite env var")
	}
	// Neither bootstrap file should have been created.
	for _, f := range []string{jwtSecretFile, initialAdminPasswordFile} {
		if _, err := os.Stat(filepath.Join(dataDir, f)); !os.IsNotExist(err) {
			t.Errorf("bootstrap created %s despite explicit env vars; err=%v", f, err)
		}
	}
}

// TestBootstrap_CorruptJWTFile asserts that a tampered .jwt-secret
// (non-hex content) surfaces a clean error rather than silently
// rotating the secret out from under existing sessions.
func TestBootstrap_CorruptJWTFile(t *testing.T) {
	dataDir := t.TempDir()
	setBootstrapEnv(t, dataDir)
	if err := os.WriteFile(filepath.Join(dataDir, jwtSecretFile), []byte("not-hex!!"), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := Load()
	if err == nil {
		t.Fatal("expected error from corrupt .jwt-secret")
	}
	if !strings.Contains(err.Error(), "hex") {
		t.Errorf("error should mention hex parsing: %v", err)
	}
}

// TestRandomPassword_AlphabetAndLength covers the alphabet contract
// (no ambiguous chars) and the requested length being honored.
func TestRandomPassword_AlphabetAndLength(t *testing.T) {
	pw, err := randomPassword(24)
	if err != nil {
		t.Fatal(err)
	}
	if len(pw) != 24 {
		t.Errorf("length=%d, want 24", len(pw))
	}
	for _, r := range pw {
		if strings.ContainsRune("0O1lI", r) {
			t.Errorf("ambiguous char %q in password %q", r, pw)
		}
	}
}
