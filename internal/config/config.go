// Package config implements configuration loading and management.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds the application configuration loaded from environment variables.
type Config struct {
	Listen         string        // BASEMENT_LISTEN, default ":8080"
	DataDir        string        // BASEMENT_DATA_DIR, default "/var/lib/basement"
	PublicURL      string        // BASEMENT_PUBLIC_URL, optional
	LogLevel       string        // BASEMENT_LOG_LEVEL, default "info"
	SessionTTL     time.Duration // BASEMENT_SESSION_TTL, default 24h
	AuditRetention time.Duration // BASEMENT_AUDIT_RETENTION_DAYS (days), default 90 days

	Driver  DriverConfig
	Admin   AdminConfig
	JWT     JWTConfig
	OIDC    OIDCConfig // optional, v1.3
}

// DriverConfig holds driver-specific configuration.
type DriverConfig struct {
	Name   string
	Garage GarageConfig
	// Future: Basement BasementConfig
}

// GarageConfig holds Garage driver configuration.
type GarageConfig struct {
	AdminURL    string // BASEMENT_DRIVER_GARAGE_ADMIN_URL, required if Driver=garage
	AdminToken  string // BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN, required if Driver=garage
	S3URL       string
	S3Region    string
	S3AccessKey string
	S3SecretKey string
}

// AdminConfig holds admin authentication configuration.
type AdminConfig struct {
	User         string // BASEMENT_ADMIN_USER, required
	PasswordHash string // BASEMENT_ADMIN_PASSWORD_HASH, required
}

// JWTConfig holds JWT signing configuration.
type JWTConfig struct {
	Secret []byte // BASEMENT_JWT_SECRET (base64-decoded), required, must be >= 32 bytes
}

// OIDCConfig holds OIDC configuration (v1.3).
type OIDCConfig struct {
	Issuer        string // BASEMENT_OIDC_ISSUER
	ClientID      string
	ClientSecret  string
	AutoProvision bool
}

// Load reads and validates configuration from environment variables.
// Returns (*Config, error) where error contains all validation failures (aggregated).
func Load() (*Config, error) {
	cfg := &Config{
		// Defaults per design.md § Configuration
		Listen:         ":8080",
		DataDir:        "/var/lib/basement",
		LogLevel:       "info",
		SessionTTL:     24 * time.Hour,
		AuditRetention: 90 * 24 * time.Hour,
		OIDC: OIDCConfig{
			AutoProvision: false,
		},
	}

	// Load optional scalar values with defaults
	cfg.Listen = envOr("BASEMENT_LISTEN", cfg.Listen)
	cfg.DataDir = envOr("BASEMENT_DATA_DIR", cfg.DataDir)
	cfg.PublicURL = envOr("BASEMENT_PUBLIC_URL", "")
	cfg.LogLevel = envOr("BASEMENT_LOG_LEVEL", cfg.LogLevel)

	// Parse SessionTTL (optional, default 24h)
	sessionTTLEnv := os.Getenv("BASEMENT_SESSION_TTL")
	if sessionTTLEnv != "" {
		parsed, err := time.ParseDuration(sessionTTLEnv)
		if err != nil {
			return nil, fmt.Errorf("invalid BASEMENT_SESSION_TTL: %w", err)
		}
		cfg.SessionTTL = parsed
	}

	// Parse AuditRetentionDays (optional, default 90 days)
	auditDaysEnv := os.Getenv("BASEMENT_AUDIT_RETENTION_DAYS")
	if auditDaysEnv != "" {
		days, err := strconv.Atoi(auditDaysEnv)
		if err != nil {
			return nil, fmt.Errorf("invalid BASEMENT_AUDIT_RETENTION_DAYS: %w", err)
		}
		cfg.AuditRetention = time.Duration(days) * 24 * time.Hour
	}

	// Load driver configuration
	cfg.Driver.Name = envOr("BASEMENT_DRIVER", "")

	// Load Garage-specific driver config (if Driver=garage)
	cfg.Driver.Garage.AdminURL = envOr("BASEMENT_DRIVER_GARAGE_ADMIN_URL", "")
	cfg.Driver.Garage.AdminToken = envOr("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN", "")
	cfg.Driver.Garage.S3URL = envOr("BASEMENT_DRIVER_GARAGE_S3_URL", "")
	cfg.Driver.Garage.S3Region = envOr("BASEMENT_DRIVER_GARAGE_S3_REGION", "")
	cfg.Driver.Garage.S3AccessKey = envOr("BASEMENT_DRIVER_GARAGE_S3_ACCESS_KEY", "")
	cfg.Driver.Garage.S3SecretKey = envOr("BASEMENT_DRIVER_GARAGE_S3_SECRET_KEY", "")

	// Load admin configuration (always required)
	cfg.Admin.User = os.Getenv("BASEMENT_ADMIN_USER")
	cfg.Admin.PasswordHash = os.Getenv("BASEMENT_ADMIN_PASSWORD_HASH")

	// Load JWT secret (required, base64-encoded in env)
	jwtSecretEnv := os.Getenv("BASEMENT_JWT_SECRET")
	if jwtSecretEnv != "" {
		decoded, err := base64.StdEncoding.DecodeString(jwtSecretEnv)
		if err != nil {
			return nil, fmt.Errorf("invalid BASEMENT_JWT_SECRET (not valid base64): %w", err)
		}
		cfg.JWT.Secret = decoded
	}

	// Load OIDC configuration (optional, v1.3)
	cfg.OIDC.Issuer = envOr("BASEMENT_OIDC_ISSUER", "")
	cfg.OIDC.ClientID = envOr("BASEMENT_OIDC_CLIENT_ID", "")
	cfg.OIDC.ClientSecret = envOr("BASEMENT_OIDC_CLIENT_SECRET", "")

	// Parse OIDC AutoProvision (optional, default false)
	oidcAutoProvEnv := os.Getenv("BASEMENT_OIDC_AUTO_PROVISION")
	if oidcAutoProvEnv != "" {
		parsed, err := strconv.ParseBool(oidcAutoProvEnv)
		if err != nil {
			return nil, fmt.Errorf("invalid BASEMENT_OIDC_AUTO_PROVISION: %w", err)
		}
		cfg.OIDC.AutoProvision = parsed
	}

	// Validation - aggregate all errors
	var errs []error

	// Validate driver name (required)
	if cfg.Driver.Name == "" {
		errs = append(errs, errors.New("BASEMENT_DRIVER is required"))
	} else if cfg.Driver.Name != "garage" && cfg.Driver.Name != "garage-v1" {
		errs = append(errs, fmt.Errorf("BASEMENT_DRIVER=%q: supported values are \"garage\" (v2 admin API) or \"garage-v1\" (v1 admin API)", cfg.Driver.Name))
	}

	// Validate Garage driver config (required if Driver=garage*)
	if cfg.Driver.Name == "garage" || cfg.Driver.Name == "garage-v1" {
		if cfg.Driver.Garage.AdminURL == "" {
			errs = append(errs, errors.New("BASEMENT_DRIVER_GARAGE_ADMIN_URL is required when DRIVER=garage"))
		}
		if cfg.Driver.Garage.AdminToken == "" {
			errs = append(errs, errors.New("BASEMENT_DRIVER_GARAGE_ADMIN_TOKEN is required when DRIVER=garage"))
		}
		// S3 fields are optional per design (can be omitted if not needed)
	}

	// Validate admin config (always required)
	if cfg.Admin.User == "" {
		errs = append(errs, errors.New("BASEMENT_ADMIN_USER is required"))
	}
	if cfg.Admin.PasswordHash == "" {
		errs = append(errs, errors.New("BASEMENT_ADMIN_PASSWORD_HASH is required"))
	}

	// Validate JWT secret (required)
	if len(cfg.JWT.Secret) == 0 {
		errs = append(errs, errors.New("BASEMENT_JWT_SECRET is required"))
	} else if len(cfg.JWT.Secret) < 32 {
		errs = append(errs, fmt.Errorf("BASEMENT_JWT_SECRET must be at least 32 bytes after base64 decoding (got %d)", len(cfg.JWT.Secret)))
	}

	// Validate log level (optional, must be one of debug|info|warn|error)
	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLogLevels[cfg.LogLevel] {
		errs = append(errs, fmt.Errorf("BASEMENT_LOG_LEVEL=%q: must be one of debug|info|warn|error", cfg.LogLevel))
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return cfg, nil
}

// envOr returns the environment variable value for key if set, otherwise def.
func envOr(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
}
