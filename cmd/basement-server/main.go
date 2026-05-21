// Command basement-server starts the admin + user server.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mattjackson/basement/internal/api"
	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/config"
	driverpkg "github.com/mattjackson/basement/internal/driver"
	_ "github.com/mattjackson/basement/internal/drivers/aws_s3"
	_ "github.com/mattjackson/basement/internal/drivers/garage"
	_ "github.com/mattjackson/basement/internal/drivers/garage_v1"
	_ "github.com/mattjackson/basement/internal/drivers/minio"
	"github.com/mattjackson/basement/internal/metrics"
	"github.com/mattjackson/basement/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// Warn loud if the data dir isn't writable — saves will fail.
	// Don't EXIT on this (v0.8.0d.21 did, which turned a broken-
	// writes deploy into a fully-down site for the operator). Reads
	// still work even when writes fail, so the server stays up to
	// let the operator at least navigate while they fix host perms.
	mkErr := os.MkdirAll(cfg.DataDir, 0755)
	if mkErr != nil {
		slog.Warn("DATA DIR NOT CREATABLE — saves will fail until host perms fixed (chown to UID 65532 if running the distroless image)", "dir", cfg.DataDir, "error", mkErr)
	} else {
		probe := cfg.DataDir + "/.write-probe"
		if err := os.WriteFile(probe, []byte("ok"), 0644); err != nil {
			slog.Warn("DATA DIR NOT WRITABLE by this process — saves will fail until host perms fixed (chown to UID 65532 if running the distroless image; binary runs as 65532:65532)", "dir", cfg.DataDir, "error", err)
		} else {
			_ = os.Remove(probe)
		}
	}

	// v1.0.0a: at-rest encryption of admin_token / secret_key /
	// s3_secret_key / auth_token. Key derived from the JWT signing
	// secret (same as bucket_grants — see ADR-0001). On first boot
	// after upgrade, load() silently rewrites connections.json with
	// the sensitive subset moved into ConfigEnc. Idempotent on reboot.
	connStore, err := store.OpenConnectionsWithKey(cfg.DataDir, cfg.JWT.Secret)
	if err != nil {
		slog.Error("failed to open connections store", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	count, err := connStore.Count(ctx)
	if err != nil {
		slog.Error("failed to count connections", "error", err)
		os.Exit(1)
	}

	if count == 0 && cfg.Driver.Name != "" {
		connConfig := map[string]string{}

		switch cfg.Driver.Name {
		case "garage", "garage-v1":
			connConfig["admin_url"] = cfg.Driver.Garage.AdminURL
			connConfig["admin_token"] = cfg.Driver.Garage.AdminToken
			if cfg.Driver.Garage.S3URL != "" {
				connConfig["s3_url"] = cfg.Driver.Garage.S3URL
			}
			if cfg.Driver.Garage.S3Region != "" {
				connConfig["s3_region"] = cfg.Driver.Garage.S3Region
			}
			if cfg.Driver.Garage.S3AccessKey != "" {
				connConfig["s3_access_key"] = cfg.Driver.Garage.S3AccessKey
			}
			if cfg.Driver.Garage.S3SecretKey != "" {
				connConfig["s3_secret_key"] = cfg.Driver.Garage.S3SecretKey
			}
		case "aws-s3":
			connConfig["region"] = cfg.Driver.Aws.Region
			connConfig["access_key"] = cfg.Driver.Aws.AccessKey
			connConfig["secret_key"] = cfg.Driver.Aws.SecretKey
			if cfg.Driver.Aws.Endpoint != "" {
				connConfig["endpoint"] = cfg.Driver.Aws.Endpoint
			}
		}

		newConn := store.Connection{
			Label:  "default",
			Driver: cfg.Driver.Name,
			Config: connConfig,
			Owner:  "org",
		}

		if _, err := connStore.Create(ctx, newConn); err != nil {
			slog.Error("failed to auto-seed default connection", "error", err)
			os.Exit(1)
		}

		slog.Info("auto-seeded 'default' connection from env-var config — manage via /admin/clusters")
	}

	reg := driverpkg.NewRegistry(connStore)

	defaultConn, err := connStore.Get(ctx, "")
	if err != nil {
		list, _ := connStore.List(ctx)
		if len(list) > 0 {
			defaultConn = list[0]
		} else if cfg.Driver.Name != "" {
			defaultConn = store.Connection{Driver: cfg.Driver.Name, Config: map[string]string{}}
		}
	}

	var defaultDrv driverpkg.Driver
	if defaultConn.ID != "" || defaultConn.Driver != "" {
		defaultDrv, err = reg.For(ctx, defaultConn.ID)
		if err != nil {
			slog.Warn("falling back to legacy single-driver mode", "error", err)
			driverCfg := map[string]string{
				"admin_url":    cfg.Driver.Garage.AdminURL,
				"admin_token":  cfg.Driver.Garage.AdminToken,
				"s3_url":       cfg.Driver.Garage.S3URL,
				"s3_region":    cfg.Driver.Garage.S3Region,
				"s3_access_key": cfg.Driver.Garage.S3AccessKey,
				"s3_secret_key": cfg.Driver.Garage.S3SecretKey,
			}

			if cfg.Driver.Name == "aws-s3" {
				driverCfg["region"] = cfg.Driver.Aws.Region
				driverCfg["access_key"] = cfg.Driver.Aws.AccessKey
				driverCfg["secret_key"] = cfg.Driver.Aws.SecretKey
				if cfg.Driver.Aws.Endpoint != "" {
					driverCfg["endpoint"] = cfg.Driver.Aws.Endpoint
				}
			}

			defaultDrv, err = driverpkg.Open(cfg.Driver.Name, driverCfg)
			if err != nil {
				slog.Error("failed to open legacy driver", "driver", cfg.Driver.Name, "error", err)
				os.Exit(1)
			}
		}
	} else {
		driverCfg := map[string]string{
			"admin_url":    cfg.Driver.Garage.AdminURL,
			"admin_token":  cfg.Driver.Garage.AdminToken,
			"s3_url":       cfg.Driver.Garage.S3URL,
			"s3_region":    cfg.Driver.Garage.S3Region,
			"s3_access_key": cfg.Driver.Garage.S3AccessKey,
			"s3_secret_key": cfg.Driver.Garage.S3SecretKey,
		}

		if cfg.Driver.Name == "aws-s3" {
			driverCfg["region"] = cfg.Driver.Aws.Region
			driverCfg["access_key"] = cfg.Driver.Aws.AccessKey
			driverCfg["secret_key"] = cfg.Driver.Aws.SecretKey
			if cfg.Driver.Aws.Endpoint != "" {
				driverCfg["endpoint"] = cfg.Driver.Aws.Endpoint
			}
		}

		defaultDrv, err = driverpkg.Open(cfg.Driver.Name, driverCfg)
		if err != nil {
			slog.Error("failed to open legacy driver", "driver", cfg.Driver.Name, "error", err)
			os.Exit(1)
		}
	}

	st, err := store.Open(cfg.DataDir, cfg.AuditRetention)
	if err != nil {
		slog.Error("failed to open store", "error", err)
		os.Exit(1)
	}

	// Per ADR-0001 (v0.9.0c): per-user per-bucket S3 credential grants,
	// encrypted at rest with a key derived from the JWT secret.
	if err := st.WireBucketGrants(cfg.JWT.Secret); err != nil {
		slog.Error("failed to wire bucket-grant store", "error", err)
		os.Exit(1)
	}

	srv := api.New(cfg, st, connStore, defaultDrv, reg)

	// Per ADR-0001 (v0.9.0b/e): policy enforcer + matthew->host_admin
	// seed assignment. The user-tier "Add bucket access" endpoint and
	// future RBAC gates depend on this being wired before Start.
	enforcer, err := policy.Open(cfg.DataDir)
	if err != nil {
		slog.Error("failed to open policy enforcer", "error", err)
		os.Exit(1)
	}

	// Per v0.9.0f cycle prompt: ensure the env-seeded admin keeps
	// host/cluster/bucket access when the new capability gates land.
	// Idempotent — re-running on each boot is safe.
	if err := enforcer.SeedEnvAdmin(cfg.Admin.User); err != nil {
		slog.Error("failed to seed env-admin policy assignments", "error", err)
		os.Exit(1)
	}

	srv.SetPolicy(enforcer)

	// v1.0.0c: append-only audit log of every mutating handler.
	// FileLogger appends one JSON line per event to
	// {dataDir}/audit/YYYY-MM-DD.log and fsyncs each write so a
	// crashing process never loses a recorded action. Tests that
	// instantiate api.New() don't get this wiring — they fall
	// through to the no-op default installed by New().
	auditLogger := audit.NewFileLogger(cfg.DataDir)
	srv.SetAuditLogger(auditLogger)

	// v1.0.0d: per-bucket bytes+objects snapshots, persisted hourly
	// to {dataDir}/metrics/YYYY-MM-DD.jsonl. Powers the time-series
	// chart at /admin/usage. The scheduler runs in its own goroutine
	// with the lifetime of ctxSignal (below) — graceful shutdown
	// stops it cleanly. Survives every per-cluster error (logs +
	// continues) so a flaky backend doesn't crash the server.
	metricsRec := metrics.NewFileRecorder(cfg.DataDir)
	srv.SetMetricsRecorder(metricsRec)

	// Optional: wire up OIDC if BASEMENT_OIDC_ISSUER is set. When unset,
	// local-password remains the only login path.
	if cfg.OIDC.Issuer != "" {
		oidcProv, err := auth.NewOIDCProvider(ctx, cfg.OIDC)
		if err != nil {
			slog.Error("failed to initialise OIDC provider", "error", err)
			os.Exit(1)
		}
		srv.SetOIDC(oidcProv)
		slog.Info("OIDC enabled", "issuer", cfg.OIDC.Issuer, "auto_provision", cfg.OIDC.AutoProvision)
	}

	ctxSignal, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// v1.0.0d: kick off the hourly metrics snapshot scheduler. Fires
	// once immediately so first-time deploys get a data point without
	// waiting an hour, then on Interval + Jitter cadence. The goroutine
	// returns on ctxSignal cancel.
	go metrics.RunScheduler(ctxSignal, metrics.SchedulerConfig{
		Conns:     connStore,
		Reg:       reg,
		Recorder:  metricsRec,
		Interval:  time.Hour,
		MaxJitter: 90 * time.Second,
	})

	if err := srv.Start(ctxSignal); err != nil {
		slog.Error("http server error", "error", err)
		os.Exit(1)
	}

	slog.Info("server shutdown complete")
}
