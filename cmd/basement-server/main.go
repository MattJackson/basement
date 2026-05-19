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
	_ "github.com/mattjackson/basement/internal/drivers/garage"
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

	st, err := store.Open(cfg.DataDir, cfg.AuditRetention)
	if err != nil {
		slog.Error("failed to open store", "error", err)
		os.Exit(1)
	}

	driverCfg := map[string]string{
		"admin_url":    cfg.Driver.Garage.AdminURL,
		"admin_token":  cfg.Driver.Garage.AdminToken,
		"s3_url":       cfg.Driver.Garage.S3URL,
		"s3_region":    cfg.Driver.Garage.S3Region,
		"s3_access_key": cfg.Driver.Garage.S3AccessKey,
		"s3_secret_key": cfg.Driver.Garage.S3SecretKey,
	}

	drv, err := driverpkg.Open(cfg.Driver.Name, driverCfg)
	if err != nil {
		slog.Error("failed to open driver", "driver", cfg.Driver.Name, "error", err)
		os.Exit(1)
	}

	srv := api.New(cfg, st, drv)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := srv.Start(ctx); err != nil {
		slog.Error("http server error", "error", err)
		os.Exit(1)
	}

	slog.Info("server shutdown complete")
}
