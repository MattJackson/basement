// Command basement-server starts the admin + user server.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mattjackson/basement/internal/api"
	"github.com/mattjackson/basement/internal/config"
	driverpkg "github.com/mattjackson/basement/internal/driver"
	_ "github.com/mattjackson/basement/internal/drivers/aws_s3"
	_ "github.com/mattjackson/basement/internal/drivers/garage"
	_ "github.com/mattjackson/basement/internal/drivers/garage_v1"
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

	connStore, err := store.OpenConnections(cfg.DataDir)
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

	srv := api.New(cfg, st, connStore, defaultDrv, reg)

	ctxSignal, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := srv.Start(ctxSignal); err != nil {
		slog.Error("http server error", "error", err)
		os.Exit(1)
	}

	slog.Info("server shutdown complete")
}
