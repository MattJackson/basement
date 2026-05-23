package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoad_AllRequiredPresent(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq") // 32 bytes base64

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Listen != ":8080" {
		t.Errorf("Listen=%q, want \":8080\"", cfg.Listen)
	}
	if cfg.DataDir != "/var/lib/basement" {
		t.Errorf("DataDir=%q, want \"/var/lib/basement\"", cfg.DataDir)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel=%q, want \"info\"", cfg.LogLevel)
	}
	if cfg.SessionTTL != 24*time.Hour {
		t.Errorf("SessionTTL=%v, want 24h", cfg.SessionTTL)
	}
	if cfg.AuditRetention != 90*24*time.Hour {
		t.Errorf("AuditRetention=%v, want 90 days", cfg.AuditRetention)
	}
	if cfg.Driver.Name != "garage" {
		t.Errorf("Driver.Name=%q, want \"garage\"", cfg.Driver.Name)
	}
	if cfg.Driver.Garage.AdminURL != "http://garage:3903" {
		t.Errorf("Driver.Garage.AdminURL=%q, want \"http://garage:3903\"", cfg.Driver.Garage.AdminURL)
	}
	if cfg.Driver.Garage.AdminToken != "testtoken123" {
		t.Errorf("Driver.Garage.AdminToken=%q, want \"testtoken123\"", cfg.Driver.Garage.AdminToken)
	}
	if cfg.Admin.User != "admin" {
		t.Errorf("Admin.User=%q, want \"admin\"", cfg.Admin.User)
	}
	if len(cfg.JWT.Secret) < 32 {
		t.Errorf("JWT.Secret length=%d, want >= 32", len(cfg.JWT.Secret))
	}
}

// TestLoad_MissingDriver_NowOptional — v1.11.0c made BASEMENT_DRIVER
// optional so the 5-minute-install path works with zero env vars.
// The operator adds clusters via /admin/clusters after first login.
// This test pins the new "no driver, no error" behaviour at the Load()
// boundary; main.go skips the legacy driver.Open fallback when the
// name is empty.
func TestLoad_MissingDriver_NowOptional(t *testing.T) {
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with no driver should succeed, got: %v", err)
	}
	if cfg.Driver.Name != "" {
		t.Errorf("Driver.Name=%q, want \"\" (empty when env not set)", cfg.Driver.Name)
	}
}

func TestLoad_MissingDriverGarageAdminURL(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "BASEMENT_DRIVER_GARAGE_ADMIN_URL") {
		t.Errorf("error missing BASEMENT_DRIVER_GARAGE_ADMIN_URL: %v", err)
	}
}

func TestLoad_MissingDriverGarageAdminToken(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN") {
		t.Errorf("error missing BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN: %v", err)
	}
}

func TestLoad_MissingAdminUser(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "BASEMENT_ADMIN_USER") {
		t.Errorf("error missing BASEMENT_ADMIN_USER: %v", err)
	}
}

// TestLoad_MissingAdminPasswordHash_BootstrapsInstead is the v1.11.0c
// successor to the original "missing hash is fatal" test. With the
// 5-minute-install bootstrap landed, an unset BASEMENT_ADMIN_PASSWORD_HASH
// (and unset BASEMENT_ADMIN_PASSWORD) no longer aborts boot — basement
// mints a random password, prints it to stdout, and persists the
// plaintext to {DataDir}/.initial-admin-password. Bootstrap is covered
// in detail by bootstrap_test.go; this test just pins the integration
// behaviour at the Load() boundary.
func TestLoad_MissingAdminPasswordHash_BootstrapsInstead(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")
	t.Setenv("BASEMENT_DATA_DIR", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Admin.PasswordHash == "" {
		t.Error("expected bootstrap to fill PasswordHash, got empty")
	}
}

// TestLoad_MissingJWTSecret_BootstrapsInstead — see comment on
// TestLoad_MissingAdminPasswordHash_BootstrapsInstead. Same shape:
// v1.11.0c bootstrap fills in the JWT secret instead of aborting boot.
func TestLoad_MissingJWTSecret_BootstrapsInstead(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_DATA_DIR", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.JWT.Secret) < 32 {
		t.Errorf("expected bootstrap to fill JWT.Secret with >= 32 bytes, got %d", len(cfg.JWT.Secret))
	}
}

func TestLoad_InvalidSessionTTL(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")
	t.Setenv("BASEMENT_SESSION_TTL", "invalid")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "BASEMENT_SESSION_TTL") {
		t.Errorf("error missing BASEMENT_SESSION_TTL: %v", err)
	}
}

func TestLoad_InvalidJWTSecretBase64(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "not-valid-base64!!!")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "BASEMENT_JWT_SECRET") {
		t.Errorf("error missing BASEMENT_JWT_SECRET: %v", err)
	}
}

func TestLoad_TOOShortJWTSecret(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	// Only 8 bytes base64 encoded = "dGVzdDEyMw=="
	t.Setenv("BASEMENT_JWT_SECRET", "dGVzdDEyMw==")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "BASEMENT_JWT_SECRET") || !strings.Contains(err.Error(), "32 bytes") {
		t.Errorf("error missing 32-byte check: %v", err)
	}
}

func TestLoad_UnknownDriverName(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "unknown-driver")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "BASEMENT_DRIVER") || !strings.Contains(err.Error(), "garage") {
		t.Errorf("error not mentioning garage: %v", err)
	}
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")
	t.Setenv("BASEMENT_LOG_LEVEL", "debugg")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "BASEMENT_LOG_LEVEL") {
		t.Errorf("error missing BASEMENT_LOG_LEVEL: %v", err)
	}
}

func TestLoad_CustomSessionTTL(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")
	t.Setenv("BASEMENT_SESSION_TTL", "2h30m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := 2*time.Hour + 30*time.Minute
	if cfg.SessionTTL != expected {
		t.Errorf("SessionTTL=%v, want %v", cfg.SessionTTL, expected)
	}
}

func TestLoad_CustomAuditRetentionDays(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")
	t.Setenv("BASEMENT_AUDIT_RETENTION_DAYS", "30")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := 30 * 24 * time.Hour
	if cfg.AuditRetention != expected {
		t.Errorf("AuditRetention=%v, want %v", cfg.AuditRetention, expected)
	}
}

func TestLoad_OIDCAutoProvision(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")
	t.Setenv("BASEMENT_OIDC_ISSUER", "https://oidc.example.com")
	t.Setenv("BASEMENT_OIDC_CLIENT_ID", "client123")
	t.Setenv("BASEMENT_OIDC_CLIENT_SECRET", "secret456")
	t.Setenv("BASEMENT_OIDC_REDIRECT_URL", "https://example.com/api/v1/auth/oidc/callback")
	t.Setenv("BASEMENT_OIDC_AUTO_PROVISION", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.OIDC.Issuer != "https://oidc.example.com" {
		t.Errorf("OIDC.Issuer=%q, want \"https://oidc.example.com\"", cfg.OIDC.Issuer)
	}
	if cfg.OIDC.ClientID != "client123" {
		t.Errorf("OIDC.ClientID=%q, want \"client123\"", cfg.OIDC.ClientID)
	}
	if cfg.OIDC.ClientSecret != "secret456" {
		t.Errorf("OIDC.ClientSecret=%q, want \"secret456\"", cfg.OIDC.ClientSecret)
	}
	if cfg.OIDC.RedirectURL != "https://example.com/api/v1/auth/oidc/callback" {
		t.Errorf("OIDC.RedirectURL=%q, want \"https://example.com/api/v1/auth/oidc/callback\"", cfg.OIDC.RedirectURL)
	}
	if !cfg.OIDC.AutoProvision {
		t.Error("OIDC.AutoProvision=false, want true")
	}
}

func TestLoad_OIDCRedirectURLDerivedFromPublicURL(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")
	t.Setenv("BASEMENT_PUBLIC_URL", "https://basement.example.com/")
	t.Setenv("BASEMENT_OIDC_ISSUER", "https://oidc.example.com")
	t.Setenv("BASEMENT_OIDC_CLIENT_ID", "client123")
	t.Setenv("BASEMENT_OIDC_CLIENT_SECRET", "secret456")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "https://basement.example.com/api/v1/auth/oidc/callback"
	if cfg.OIDC.RedirectURL != want {
		t.Errorf("OIDC.RedirectURL=%q, want %q", cfg.OIDC.RedirectURL, want)
	}
}

func TestLoad_OIDCMissingClientID(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")
	t.Setenv("BASEMENT_OIDC_ISSUER", "https://oidc.example.com")
	t.Setenv("BASEMENT_OIDC_CLIENT_SECRET", "secret456")
	t.Setenv("BASEMENT_OIDC_REDIRECT_URL", "https://example.com/api/v1/auth/oidc/callback")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "BASEMENT_OIDC_CLIENT_ID") {
		t.Errorf("error missing BASEMENT_OIDC_CLIENT_ID: %v", err)
	}
}

func TestLoad_OIDCMissingClientSecret(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")
	t.Setenv("BASEMENT_OIDC_ISSUER", "https://oidc.example.com")
	t.Setenv("BASEMENT_OIDC_CLIENT_ID", "client123")
	t.Setenv("BASEMENT_OIDC_REDIRECT_URL", "https://example.com/api/v1/auth/oidc/callback")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "BASEMENT_OIDC_CLIENT_SECRET") {
		t.Errorf("error missing BASEMENT_OIDC_CLIENT_SECRET: %v", err)
	}
}

func TestLoad_OIDCMissingRedirectURL(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")
	t.Setenv("BASEMENT_OIDC_ISSUER", "https://oidc.example.com")
	t.Setenv("BASEMENT_OIDC_CLIENT_ID", "client123")
	t.Setenv("BASEMENT_OIDC_CLIENT_SECRET", "secret456")
	// PublicURL not set, RedirectURL not set — should fail validation.

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "BASEMENT_OIDC_REDIRECT_URL") {
		t.Errorf("error missing BASEMENT_OIDC_REDIRECT_URL: %v", err)
	}
}

func TestLoad_OIDCDisabledByDefault(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OIDC.Issuer != "" {
		t.Errorf("OIDC.Issuer default=%q, want \"\"", cfg.OIDC.Issuer)
	}
	if cfg.OIDC.ClientID != "" {
		t.Errorf("OIDC.ClientID default=%q, want \"\"", cfg.OIDC.ClientID)
	}
}

func TestLoad_OIDCAutoProvisionInvalid(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")
	t.Setenv("BASEMENT_OIDC_AUTO_PROVISION", "maybe")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "BASEMENT_OIDC_AUTO_PROVISION") {
		t.Errorf("error missing BASEMENT_OIDC_AUTO_PROVISION: %v", err)
	}
}

func TestLoad_AggregatedErrors(t *testing.T) {
	// Only set the driver name and a tempdir; expect the driver-specific
	// vars (no auto-bootstrap path for those) to all surface in the
	// aggregated error. ADMIN_PASSWORD_HASH + JWT_SECRET are auto-
	// bootstrapped in v1.11.0c, so they no longer appear in the missing-
	// required-vars list. ADMIN_USER defaults to "admin" inside
	// bootstrap when admin auto-gen fires, so it likewise drops out.
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DATA_DIR", t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	errStr := err.Error()
	requiredVars := []string{
		"BASEMENT_DRIVER_GARAGE_ADMIN_URL",
		"BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN",
	}

	for _, varName := range requiredVars {
		if !strings.Contains(errStr, varName) {
			t.Errorf("aggregated error missing %s: %v", varName, err)
		}
	}
}

func TestLoad_InvalidAuditRetentionDays(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")
	t.Setenv("BASEMENT_AUDIT_RETENTION_DAYS", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "BASEMENT_AUDIT_RETENTION_DAYS") {
		t.Errorf("error missing BASEMENT_AUDIT_RETENTION_DAYS: %v", err)
	}
}

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "testtoken123")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check optional fields use defaults
	if cfg.Listen != ":8080" {
		t.Errorf("Listen default=%q, want \":8080\"", cfg.Listen)
	}
	if cfg.DataDir != "/var/lib/basement" {
		t.Errorf("DataDir default=%q, want \"/var/lib/basement\"", cfg.DataDir)
	}
	if cfg.PublicURL != "" {
		t.Errorf("PublicURL default=%q, want \"\"", cfg.PublicURL)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel default=%q, want \"info\"", cfg.LogLevel)
	}
	if cfg.SessionTTL != 24*time.Hour {
		t.Errorf("SessionTTL default=%v, want 24h", cfg.SessionTTL)
	}
	if cfg.AuditRetention != 90*24*time.Hour {
		t.Errorf("AuditRetention default=%v, want 90 days", cfg.AuditRetention)
	}
	if cfg.OIDC.AutoProvision != false {
		t.Errorf("OIDC.AutoProvision default=%v, want false", cfg.OIDC.AutoProvision)
	}
}

// TestLoad_AwsS3Driver_Happy covers DRIVER=aws-s3 with all required keys set.
func TestLoad_AwsS3Driver_Happy(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "aws-s3")
	t.Setenv("BASEMENT_DRIVER_AWS_S3_REGION", "us-west-2")
	t.Setenv("BASEMENT_DRIVER_AWS_S3_ACCESS_KEY", "AKIA-test")
	t.Setenv("BASEMENT_DRIVER_AWS_S3_SECRET_KEY", "secret/test")
	t.Setenv("BASEMENT_DRIVER_AWS_S3_ENDPOINT", "https://s3.example.com")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Driver.Name != "aws-s3" {
		t.Errorf("Driver.Name=%q, want aws-s3", cfg.Driver.Name)
	}
	if cfg.Driver.Aws.Region != "us-west-2" {
		t.Errorf("Aws.Region=%q", cfg.Driver.Aws.Region)
	}
	if cfg.Driver.Aws.AccessKey != "AKIA-test" {
		t.Errorf("Aws.AccessKey=%q", cfg.Driver.Aws.AccessKey)
	}
	if cfg.Driver.Aws.SecretKey != "secret/test" {
		t.Errorf("Aws.SecretKey=%q", cfg.Driver.Aws.SecretKey)
	}
	if cfg.Driver.Aws.Endpoint != "https://s3.example.com" {
		t.Errorf("Aws.Endpoint=%q", cfg.Driver.Aws.Endpoint)
	}
}

// TestLoad_AwsS3Driver_MissingRegion covers the missing-region branch.
func TestLoad_AwsS3Driver_MissingRegion(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "aws-s3")
	t.Setenv("BASEMENT_DRIVER_AWS_S3_ACCESS_KEY", "k")
	t.Setenv("BASEMENT_DRIVER_AWS_S3_SECRET_KEY", "s")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "BASEMENT_DRIVER_AWS_S3_REGION") {
		t.Errorf("error missing AWS region: %v", err)
	}
}

// TestLoad_AwsS3Driver_MissingAccessKey covers the missing-access-key branch.
func TestLoad_AwsS3Driver_MissingAccessKey(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "aws-s3")
	t.Setenv("BASEMENT_DRIVER_AWS_S3_REGION", "us-east-1")
	t.Setenv("BASEMENT_DRIVER_AWS_S3_SECRET_KEY", "s")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "BASEMENT_DRIVER_AWS_S3_ACCESS_KEY") {
		t.Errorf("error missing AWS access key: %v", err)
	}
}

// TestLoad_AwsS3Driver_MissingSecretKey covers the missing-secret-key branch.
func TestLoad_AwsS3Driver_MissingSecretKey(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "aws-s3")
	t.Setenv("BASEMENT_DRIVER_AWS_S3_REGION", "us-east-1")
	t.Setenv("BASEMENT_DRIVER_AWS_S3_ACCESS_KEY", "k")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "BASEMENT_DRIVER_AWS_S3_SECRET_KEY") {
		t.Errorf("error missing AWS secret key: %v", err)
	}
}

// TestLoad_GarageV1Driver covers DRIVER=garage-v1 (same garage validation
// applies — covers the "garage" || "garage-v1" branch path).
func TestLoad_GarageV1Driver(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "garage-v1")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage-v1:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "tok")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Driver.Name != "garage-v1" {
		t.Errorf("Driver.Name=%q, want garage-v1", cfg.Driver.Name)
	}
}

// TestLoad_MinioDriver covers DRIVER=minio (no minio-specific required-env
// validation in Load() at present — accepted as a valid name).
func TestLoad_MinioDriver(t *testing.T) {
	t.Setenv("BASEMENT_DRIVER", "minio")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Driver.Name != "minio" {
		t.Errorf("Driver.Name=%q, want minio", cfg.Driver.Name)
	}
}

// TestLoad_CustomListenAndDataDir covers BASEMENT_LISTEN, DATA_DIR, PUBLIC_URL.
func TestLoad_CustomListenAndDataDir(t *testing.T) {
	t.Setenv("BASEMENT_LISTEN", "127.0.0.1:9999")
	t.Setenv("BASEMENT_DATA_DIR", "/custom/data")
	t.Setenv("BASEMENT_PUBLIC_URL", "https://basement.example.com")
	t.Setenv("BASEMENT_DRIVER", "garage")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
	t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "tok")
	t.Setenv("BASEMENT_ADMIN_USER", "admin")
	t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
	t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Listen != "127.0.0.1:9999" {
		t.Errorf("Listen=%q", cfg.Listen)
	}
	if cfg.DataDir != "/custom/data" {
		t.Errorf("DataDir=%q", cfg.DataDir)
	}
	if cfg.PublicURL != "https://basement.example.com" {
		t.Errorf("PublicURL=%q", cfg.PublicURL)
	}
}

// TestLoad_LogLevelVariants covers each accepted log-level value.
func TestLoad_LogLevelVariants(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		t.Run(level, func(t *testing.T) {
			t.Setenv("BASEMENT_LOG_LEVEL", level)
			t.Setenv("BASEMENT_DRIVER", "garage")
			t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "http://garage:3903")
			t.Setenv("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "tok")
			t.Setenv("BASEMENT_ADMIN_USER", "admin")
			t.Setenv("BASEMENT_ADMIN_PASSWORD_HASH", "$2a$12$abcdefghijklmnopqrstuv")
			t.Setenv("BASEMENT_JWT_SECRET", "dGhpc2lzYXNlY3JldGtleTEyMzQ1Njc4OTBhYmNkZWZnaGlq")

			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.LogLevel != level {
				t.Errorf("LogLevel=%q, want %q", cfg.LogLevel, level)
			}
		})
	}
}

// TestEnvOr exercises the envOr helper directly.
func TestEnvOr(t *testing.T) {
	t.Setenv("ENV_OR_PRESENT", "found")
	if got := envOr("ENV_OR_PRESENT", "default"); got != "found" {
		t.Errorf("present=%q, want found", got)
	}
	if got := envOr("ENV_OR_MISSING_KEY_NOT_SET", "default"); got != "default" {
		t.Errorf("missing=%q, want default", got)
	}
	// Empty env var falls back to default per envOr semantics.
	t.Setenv("ENV_OR_EMPTY", "")
	if got := envOr("ENV_OR_EMPTY", "fallback"); got != "fallback" {
		t.Errorf("empty=%q, want fallback (envOr treats empty as unset)", got)
	}
}
