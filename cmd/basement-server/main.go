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
	"github.com/mattjackson/basement/internal/backup"
	"github.com/mattjackson/basement/internal/config"
	driverpkg "github.com/mattjackson/basement/internal/driver"
	_ "github.com/mattjackson/basement/internal/drivers/aws_s3"
	_ "github.com/mattjackson/basement/internal/drivers/garage"
	_ "github.com/mattjackson/basement/internal/drivers/garage_v1"
	_ "github.com/mattjackson/basement/internal/drivers/minio"
	"github.com/mattjackson/basement/internal/federation"
	"github.com/mattjackson/basement/internal/federationwire"
	"github.com/mattjackson/basement/internal/gateway"
	"github.com/mattjackson/basement/internal/gateway/ftp"
	"github.com/mattjackson/basement/internal/gateway/nfs"
	"github.com/mattjackson/basement/internal/gateway/s3"
	"github.com/mattjackson/basement/internal/gateway/smb"
	"github.com/mattjackson/basement/internal/gateway/webdav"
	"github.com/mattjackson/basement/internal/metrics"
	"github.com/mattjackson/basement/internal/store"
	"github.com/mattjackson/basement/internal/version"
	"github.com/mattjackson/basement/internal/webhook"
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

	// v1.11.0f: select slog handler format from BASEMENT_LOG_FORMAT.
	// JSON is the production default — every log line is one parseable
	// record (filebeat / loki / stackdriver all consume it directly).
	// Text mode is a developer convenience that produces human-friendly
	// timestamp+level+msg+kv lines.
	handlerOpts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	switch cfg.LogFormat {
	case "text":
		handler = slog.NewTextHandler(os.Stdout, handlerOpts)
	default:
		handler = slog.NewJSONHandler(os.Stdout, handlerOpts)
	}
	logger := slog.New(handler)
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
	} else if cfg.Driver.Name != "" {
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
	} else {
		// v1.11.0c — no env-derived driver and no persisted connection.
		// This is the 5-minute-install posture: boot to the dashboard
		// with no default cluster wired. Operator adds clusters via
		// /admin/clusters → New cluster. defaultDrv stays nil; the
		// api/handler layer is tolerant of a nil default driver
		// (everything routes through the per-connection registry once
		// the operator adds one).
		slog.Info("no BASEMENT_DRIVER set and no persisted connections — booting without a default cluster; add one via /admin/clusters")
	}

	st, err := store.Open(cfg.DataDir, cfg.AuditRetention)
	if err != nil {
		slog.Error("failed to open store", "error", err)
		os.Exit(1)
	}

	// Per ADR-0002 (v1.1.0a): per-user S3 region keychain, AES-GCM
	// encrypted at rest with a key derived from the JWT secret. The
	// region-tier abstraction supersedes per-bucket grants at the user
	// persona; the legacy bucket_grants.json store retired in v1.1.0e.
	if err := st.WireUserRegions(cfg.JWT.Secret); err != nil {
		slog.Error("failed to wire user-region store", "error", err)
		os.Exit(1)
	}

	// Per ADR-0005 (v1.6.0a): open the federated-bucket store. This is
	// the data layer only — the replication engine, API surface and
	// frontend land in v1.6.0b/c/d. A missing federated_buckets.json
	// is fine; OpenFederated treats it as an empty store.
	if err := st.OpenFederated(); err != nil {
		slog.Error("failed to open federated-bucket store", "error", err)
		os.Exit(1)
	}

	// v1.7.0a: basement-issued long-lived service-account access
	// keys (CI, k8s, CLI, MCP). Data layer only this cycle; the
	// v1.7.0b SigV4 middleware will read VerifySecret + TouchLastUsed
	// off the same store. Missing service_accounts.json is fine —
	// WireServiceAccounts treats it as empty.
	if err := st.WireServiceAccounts(); err != nil {
		slog.Error("failed to wire service-account store", "error", err)
		os.Exit(1)
	}

	// Per ADR-0002 (v1.1.0b): the driver registry needs a handle to
	// the region keychain so ForUserRegion can refuse to operate when
	// the store is unwired (returns ErrUnsupported). Production always
	// has a non-nil store here because WireUserRegions above would
	// have os.Exit(1)'d on failure.
	reg.SetUserRegionsStore(st.UserRegions())

	// Per ADR-0002 (v1.1.0e): if a legacy bucket_grants.json file is
	// still present from the pre-region-tier era, rename it aside so
	// nothing reads it again. The v1.1.0d migration already copied any
	// rows into user_regions.json; this step retires the source file
	// without deleting bytes (operators rolling back can rename the
	// .migrated suffix off and pick up the legacy code from git).
	// Idempotent: no-op when the source is absent or already archived.
	if moved, err := st.ArchiveLegacyBucketGrants(); err != nil {
		slog.Error("failed to archive legacy bucket_grants.json", "error", err)
		os.Exit(1)
	} else if moved {
		slog.Info("moved legacy bucket_grants.json aside; data has been migrated to user_regions.json")
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
	//
	// v1.11.0f: wrap the file logger in metrics.AuditCollector so
	// every event also drives the Prometheus counters
	// (auth_attempts_total, audit_events_total, federation_replicate,
	// backup_runs, webhook_deliveries). The collector forwards every
	// Log call to the wrapped logger so the on-disk log is unchanged.
	promCollector := metrics.NewCollector()
	promCollector.SetBuildInfo(version.Version, version.Commit)
	fileAuditLogger := audit.NewFileLogger(cfg.DataDir)
	auditLogger := metrics.NewAuditCollector(fileAuditLogger, promCollector)
	srv.SetAuditLogger(auditLogger)
	srv.SetPromCollector(promCollector, cfg.MetricsToken)

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

	// v1.5.0a: scheduled backup subsystem. The store persists
	// operator-defined Backup records under {dataDir}/backups.json;
	// the scheduler wraps robfig/cron/v3 and dispatches into the
	// existing sync engine through a runner closure on srv. Both
	// are opened before Start so the API handlers always have a
	// non-nil reference, and LoadAll registers every persisted
	// schedule before cron.Start fires the first tick.
	backupStore, err := backup.NewFileStore(cfg.DataDir)
	if err != nil {
		slog.Error("failed to open backup store", "error", err)
		os.Exit(1)
	}
	backupSched := backup.NewScheduler(backupStore, srv.NewBackupRunner(), slog.Default())
	if err := backupSched.LoadAll(context.Background()); err != nil {
		slog.Warn("backup scheduler: LoadAll returned an error — some schedules may not fire", "error", err)
	}
	srv.SetBackups(backupStore, backupSched)
	backupSched.Start()
	defer backupSched.Stop()

	// Per ADR-0005 (v1.6.0b): federation replication engine. Polls each
	// continuous-sync FederatedBucket every 10s, replicates primary →
	// replica deltas, and surfaces per-replica health. Boot wiring:
	// resolver bridges the engine's narrow ReplicationClient surface to
	// the production driver.Registry + per-user region keychain. Start
	// fans out one goroutine per persisted federation; Stop drains
	// in-flight replicates cleanly on shutdown.
	fedResolver := federationwire.NewResolver(st.UserRegions(), reg)
	fedEngine := federation.NewEngine(st.Federated(), fedResolver, auditLogger, slog.Default())

	// v1.6.0c FEDERATION.API: hand the store + engine to the API
	// server so /api/v1/user/federated-buckets/* handlers can persist
	// records (store) and signal the engine for EnsureLoop / RemoveLoop
	// / TriggerNow on each mutation. Order matters: SetFederation must
	// run BEFORE srv.Start so the routes have non-nil dependencies on
	// first request.
	srv.SetFederation(st.Federated(), fedEngine)

	ctxSignal, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fedEngine.Start(ctxSignal)
	defer fedEngine.Stop()

	// v1.7.0d WEBHOOK.SUBSCRIPTIONS: persistent operator-configured
	// HTTP POST hooks for bucket events. Store persists to
	// {dataDir}/webhooks.json; engine fans out per-event deliveries
	// with HMAC-signed bodies, retries, and audit logging. Wired
	// before srv.Start so the /user/webhooks handlers have non-nil
	// dependencies on first request.
	webhookStore, err := webhook.Open(cfg.DataDir)
	if err != nil {
		slog.Error("failed to open webhook store", "error", err)
		os.Exit(1)
	}
	webhookEngine := webhook.NewEngine(webhookStore, auditLogger, slog.Default())
	srv.SetWebhooks(webhookStore, webhookEngine)
	webhookEngine.Start(ctxSignal)
	defer webhookEngine.Stop()

	// v1.9.0c GATEWAY registry. The protocol surface (WebDAV today;
	// SMB / NFS / FTP / S3 in v1.10+) is now pluggable via the
	// gateway.Registry interface. main.go assembles a production
	// Backend (composing existing primitives) and registers the
	// WebDAV gateway (real impl) plus four stubs that surface in
	// /admin/gateways with "coming soon" badges.
	//
	// The Backend wraps store + driver + SA so the protocol code
	// never reaches into those packages directly — adding a new
	// gateway in v1.10+ is a Registry.Register call plus the
	// protocol-specific http.Handler / port-bound bind.
	gwBackend := gateway.NewProductionBackend(gateway.BackendDeps{
		Cfg:         cfg,
		Users:       st,
		SAs:         st.ServiceAccounts(),
		Regions:     st.UserRegions(),
		DriverReg:   reg,
		Connections: connStore,
	})
	gwRegistry := gateway.New()

	webdavGW := webdav.New(webdav.Deps{
		Backend: gwBackend,
		// v1.9.0b: gate the gateway on the operator-configurable
		// toggle at /admin/system → Gateways → WebDAV.Enabled.
		// Bridge through a tiny adapter so the gateway package
		// doesn't depend on internal/store.
		OrgCaps: webdavOrgCapsBridge{caps: st.OrgCapabilities()},
		Audit:   auditLogger,
		Logger:  slog.Default(),
	})
	if err := gwRegistry.Register(webdavGW); err != nil {
		slog.Error("failed to register webdav gateway", "error", err)
		os.Exit(1)
	}
	// Stub registrations — v1.9.0c lights up /admin/gateways with the
	// full protocol roster ahead of v1.10+'s real implementations.
	for _, g := range []gateway.Gateway{smb.New(), nfs.New(), ftp.New(), s3.New()} {
		if err := gwRegistry.Register(g); err != nil {
			slog.Error("failed to register gateway stub", "name", g.Name(), "error", err)
			os.Exit(1)
		}
	}
	if err := gwRegistry.StartAll(ctxSignal); err != nil {
		// StartAll continues past the first failure; log and proceed
		// so the rest of the gateways come up.
		slog.Warn("gateway registry: StartAll surfaced an error", "error", err)
	}
	defer func() {
		_ = gwRegistry.StopAll(context.Background())
	}()

	srv.SetGatewayRegistry(gwRegistry)
	// The WebDAV gateway is the only HTTP-mounted gateway today; wire
	// its handler into the legacy SetWebDAVHandler slot so the chi
	// route under /webdav/ keeps dispatching. When more HTTP-mounted
	// gateways land (v2.0 S3 gateway) we'll iterate the registry.
	srv.SetWebDAVHandler(webdavGW.HTTPHandler())

	// v1.7.0f FEDERATION.EVENT-DRIVEN: subscribe the federation engine
	// to the webhook event bus so writes hitting the primary trigger
	// an immediate replicate to each replica instead of waiting for
	// the 10s polling tick. Polling continues as a fallback for
	// backends without webhook-source coverage (the v2.0 gateway will
	// extend coverage to every mutation path; until then, real
	// real-time signal only fires for the user-region DELETE handler
	// the v1.7.0d cycle wired into the bus).
	fedEngine.SubscribeToEvents(webhookEngine)

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

	// v1.11.0f: 30s gauge refresher. Walks the federation store +
	// service-account store to populate the Prometheus gauges that
	// aren't naturally event-driven (federation lag, SA count). Kept
	// out of the request hot path; one tick per 30s is rounding-error
	// load for a store of dozens of federations.
	go runPromGaugeRefresher(ctxSignal, promCollector, st.Federated(), st.ServiceAccounts())

	if err := srv.Start(ctxSignal); err != nil {
		slog.Error("http server error", "error", err)
		os.Exit(1)
	}

	slog.Info("server shutdown complete")
}

// webdavOrgCapsBridge adapts *store.OrgCapabilitiesStore (which has a
// Get() returning store.OrgCapabilities) to the webdav package's
// narrow IsEnabled() interface. Keeps the gateway package free of any
// internal/store dependency — the bridge lives entirely in main.go.
type webdavOrgCapsBridge struct {
	caps *store.OrgCapabilitiesStore
}

func (b webdavOrgCapsBridge) IsEnabled() bool {
	if b.caps == nil {
		return true
	}
	return b.caps.Get().Gateways.WebDAV.Enabled
}

// runPromGaugeRefresher ticks every 30s and refreshes the Prometheus
// gauges that aren't naturally event-driven: per-replica federation
// lag, service-account count. Returns on ctx.Done().
//
// Bucket + object gauges are populated lazily by the per-bucket
// snapshot scheduler (internal/metrics/scheduler.go) — those touch
// every connection/bucket already and updating Prometheus there would
// double the per-tick fan-out cost. Keeping them as a v1.11.x follow-up
// once the snapshot scheduler grows a "this tick observed these
// numbers" hook.
func runPromGaugeRefresher(ctx context.Context, c *metrics.Collector, fedStore federation.FederatedBuckets, saStore serviceAccountsLister) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	// Fire once immediately so a fresh boot has gauges before the first
	// tick; otherwise the first 30s of /metrics shows no federation lag.
	refreshPromGauges(c, fedStore, saStore)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			refreshPromGauges(c, fedStore, saStore)
		}
	}
}

// refreshPromGauges walks the federation store + SA store and updates
// every gauge that derives from them. Best-effort: store errors log
// and continue so a transient JSON-load issue doesn't poison metrics.
func refreshPromGauges(c *metrics.Collector, fedStore federation.FederatedBuckets, saStore serviceAccountsLister) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if fedStore != nil {
		fbs, err := fedStore.All(ctx)
		if err != nil {
			slog.Warn("prom gauge refresh: federation list failed", "error", err)
		} else {
			now := time.Now().UTC()
			for _, fb := range fbs {
				for _, rep := range fb.Replicas {
					var lag float64
					if !rep.LastSync.IsZero() {
						lag = now.Sub(rep.LastSync).Seconds()
					}
					replicaKey := rep.RegionID + ":" + rep.Bucket
					c.SetFederationLag(fb.ID, replicaKey, lag)
				}
			}
		}
	}

	if saStore != nil {
		count, err := saStore.CountAll(ctx)
		if err != nil {
			slog.Warn("prom gauge refresh: service-account count failed", "error", err)
		} else {
			c.SetServiceAccountsTotal(count)
		}
	}
}

// serviceAccountsLister is the narrow interface refreshPromGauges
// needs. The real service-account store satisfies it via the
// CountAll method added in v1.11.0f; tests can substitute a fake.
type serviceAccountsLister interface {
	CountAll(ctx context.Context) (int, error)
}
