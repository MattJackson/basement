package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	stdsync "sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/oauth2"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/backup"
	"github.com/mattjackson/basement/internal/clustersecret"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/federation"
	"github.com/mattjackson/basement/internal/gateway"
	"github.com/mattjackson/basement/internal/metrics"
	"github.com/mattjackson/basement/internal/skin"
	"github.com/mattjackson/basement/internal/store"
	"github.com/mattjackson/basement/internal/sync"
	"github.com/mattjackson/basement/internal/web"
	"github.com/mattjackson/basement/internal/webhook"

	"github.com/mattjackson/basement/internal/api/docs"
)

// oidcProvider is the subset of *auth.OIDCProvider the API server needs.
// Defined as an interface here so tests can substitute a fake.
type oidcProvider interface {
	AuthCodeURL(state, nonce string) string
	// ElevationAuthCodeURL builds the authorize URL for the ADR-0003
	// v1.2.0c sudo-style elevation flow — adds `prompt=<promptParam>`
	// and `max_age=0` so the IdP forces a fresh re-auth.
	ElevationAuthCodeURL(state, nonce, promptParam string) string
	Exchange(ctx context.Context, code string) (*oauth2.Token, error)
	VerifyIDToken(ctx context.Context, rawIDToken, expectedNonce string) (*auth.OIDCClaims, error)
	// VerifyIDTokenWithAuthTime is like VerifyIDToken but also returns
	// the `auth_time` claim from the ID token; used by the elevation
	// callback to confirm freshness.
	VerifyIDTokenWithAuthTime(ctx context.Context, rawIDToken, expectedNonce string) (*auth.OIDCClaims, int64, error)
	// VerifyIDTokenWithAllClaims is like VerifyIDToken but also returns
	// the full decoded claim map; used by the v1.3.0a OIDC group-claim
	// -> role auto-mapping sync so the callback can read provider
	// claims like "groups" / "roles" without re-parsing the JWT.
	VerifyIDTokenWithAllClaims(ctx context.Context, rawIDToken, expectedNonce string) (*auth.OIDCClaims, map[string]interface{}, error)
	Issuer() string
	AutoProvision() bool
}

// Server holds the HTTP server and its dependencies.
type Server struct {
	cfg        *config.Config
	store      *store.Store
	conns      store.Connections
	drv        driver.Driver
	reg        *driver.Registry
	syncStore  sync.Store
	// v1.5.0a backup subsystem. Both are nil in tests that don't
	// wire them, and the user_backups handlers treat nil as
	// "subsystem disabled" rather than crashing.
	backups     backup.Backups
	backupSched *backup.Scheduler
	// v1.6.0c federation subsystem. federations is the store handle;
	// federationEngine is the narrow interface the handlers need
	// (EnsureLoop / RemoveLoop / TriggerNow). Both are nil in tests
	// that don't wire them; handlers return 503 FEDERATIONS_NOT_WIRED
	// when the store is nil, and silently skip engine pokes when the
	// engine is nil (the store still persists, just no live ticking).
	federations      federation.FederatedBuckets
	federationEngine federationEngine
	// v1.7.0d webhook subsystem. Both can be nil — handlers return
	// 503 WEBHOOKS_NOT_WIRED, and emission sites silently skip when
	// the engine is missing. Production main.go wires both.
	webhooks      webhook.Store
	webhookEngine webhookEmitter
	// v1.9.0a WebDAV gateway. Optional; when nil the /webdav/ tree
	// returns 503 WEBDAV_NOT_WIRED. Production main.go wires a
	// *webdav.Handler before Start(); tests that don't care about the
	// gateway leave the field unset.
	webdav      http.Handler
	// v1.9.0c gateway registry. The /admin/gateways endpoint reads
	// from this to render every registered gateway (real + stub).
	// Nil in tests that don't care about the multi-gateway surface;
	// the handler returns 503 GATEWAYS_NOT_WIRED in that case.
	gateways    *gateway.Registry
	// v1.13.0a (ADR-0008) skin registry. The /skins endpoint reads
	// from this to enumerate every registered skin. Nil in tests
	// that don't care about skins; the handler returns 503
	// SKINS_NOT_WIRED in that case. Production main.go always
	// supplies a populated registry (basement-default + any
	// user-uploaded skins v1.13.0b+ adds).
	skins       *skin.Registry
	oidc        oidcProvider
	policy     policy.Enforcer
	audit      audit.Logger
	metrics    metrics.Recorder
	// v1.11.0f: Prometheus collector + optional bearer token gate
	// for /metrics. Nil collector means the endpoint returns 503
	// METRICS_NOT_WIRED. Token is enforced (constant-time compare)
	// only when non-empty — when empty, /metrics is open per the
	// Prometheus convention of "operator fronts with allowlist".
	promCollector *metrics.Collector
	promToken     string
	// v1.12.0a (ADR-0007) per-cluster envelope encryption manager.
	// Nil in tests that don't care about CSK; handlers return 503
	// CLUSTER_SECRETS_NOT_WIRED in that case. Production main.go
	// wires a FileStore-backed manager before Start. See
	// admin_cluster_secrets.go for the HTTP surface and
	// docs/adr/0007-per-cluster-envelope-encryption.md for the
	// model.
	clusterSecrets *clustersecret.ClusterSecretManager
	router     chi.Router
	httpServer *http.Server
	logger     *slog.Logger

	// oidcElevState backs /api/v1/auth/elevate/oidc/start +
	// /callback (ADR-0003, v1.2.0c). 5min TTL'd, cleaned up on each
	// insert via the store's own sweep. Allocated lazily so tests
	// that never touch the OIDC elevation path don't pay for it.
	oidcElevMu    stdsync.Mutex
	oidcElevState *oidcElevationStateStore
}

// New creates a new Server instance with both legacy single-driver (for back-compat) and registry.
//
// OIDC is wired separately via SetOIDC() so that callers and tests that
// don't care about OIDC don't have to thread a nil through. When OIDC
// isn't set, the /auth/oidc/* routes return 501 OIDC_NOT_CONFIGURED and
// local-password login remains the only auth path.
//
// Policy is wired similarly via SetPolicy(). To keep older tests that
// don't care about RBAC working (and to avoid a thundering-herd 503
// when an operator misconfigures), New() installs an internal
// "permissive" enforcer that grants every capability at every scope
// to the JWT's UserID. Production main.go REPLACES this with a real
// file-backed enforcer before Start().
func New(cfg *config.Config, store *store.Store, conns store.Connections, drv driver.Driver, reg *driver.Registry) *Server {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	syncStore := sync.NewFileStore(cfg.DataDir)

	srv := &Server{
		cfg:       cfg,
		store:     store,
		conns:     conns,
		drv:       drv,
		reg:       reg,
		syncStore: syncStore,
		router:    chi.NewRouter(),
		logger:    logger,
		policy:    permissiveEnforcer{},
		// Default to a no-op audit logger so the many existing
		// api.New(...) callers (tests, fixtures) don't have to
		// thread a logger through. Production main.go replaces
		// this with a real FileLogger via SetAuditLogger().
		audit: audit.NewNoop(),
		// Same pattern for the metrics recorder: tests get a
		// no-op so /admin/usage/series returns an empty result;
		// production wires a FileRecorder via SetMetricsRecorder.
		metrics: metrics.NewNoop(),
	}

	srv.routes()

	srv.httpServer = &http.Server{
		Addr:         cfg.Listen,
		Handler:      srv.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return srv
}

// SetOIDC wires an OIDC provider into the server. Must be called before
// Start. Passing nil is equivalent to leaving OIDC unconfigured (the
// /auth/oidc/* routes will return 501).
func (s *Server) SetOIDC(p oidcProvider) {
	s.oidc = p
}

// SetPolicy wires the policy enforcer into the server (ADR-0001).
// Must be called before Start when the policy subsystem is enabled.
// Handlers that need RoleAssignments nil-check s.policy and return
// 503 POLICY_NOT_WIRED when this hasn't been called.
func (s *Server) SetPolicy(p policy.Enforcer) {
	s.policy = p
}

// SetAuditLogger wires the audit log writer into the server (v1.0.0c).
// Must be called before Start in production so every mutating handler
// records its action; tests that don't care leave the no-op default
// installed by New().
func (s *Server) SetAuditLogger(l audit.Logger) {
	if l == nil {
		s.audit = audit.NewNoop()
		return
	}
	s.audit = l
}

// SetBackups wires the v1.5.0a backup store + scheduler into the
// server. Both must be set together — handlers that touch one also
// touch the other. Passing nil for either disables the
// /user/backups endpoint family (handlers return 503 BACKUPS_NOT_WIRED).
// Production main.go always supplies both; tests that don't care
// about backups leave the defaults unset.
func (s *Server) SetBackups(store backup.Backups, sched *backup.Scheduler) {
	s.backups = store
	s.backupSched = sched
}

// SetFederation wires the v1.6.0c federation store + replication engine
// into the server. Both can be passed independently (e.g. tests pass a
// store-only configuration where the engine pokes silently no-op), but
// production main.go always wires both. Passing nil for the store
// disables the /user/federated-buckets endpoint family (handlers return
// 503 FEDERATIONS_NOT_WIRED).
//
// The engine arg accepts federationEngine — production passes a
// *federation.Engine (which already satisfies the interface);
// user_federated_buckets_test.go substitutes a recording mock so it
// can assert "EnsureLoop was called for ID X" without spinning up real
// per-federation goroutines.
func (s *Server) SetFederation(store federation.FederatedBuckets, engine federationEngine) {
	s.federations = store
	s.federationEngine = engine
}

// SetWebhooks wires the v1.7.0d webhook store + delivery engine into
// the server. Passing nil for the store disables the /user/webhooks
// endpoint family (handlers return 503 WEBHOOKS_NOT_WIRED). The engine
// arg accepts webhookEmitter — production passes a *webhook.Engine;
// tests substitute a recording mock that captures Emit calls without
// actually POSTing anywhere.
func (s *Server) SetWebhooks(store webhook.Store, engine webhookEmitter) {
	s.webhooks = store
	s.webhookEngine = engine
}

// SetWebDAVHandler wires the v1.9.0a WebDAV gateway handler. Mounted
// under /webdav/ on the root chi router. Passing nil disables the
// route and any request to /webdav/* returns 503 WEBDAV_NOT_WIRED so
// a Finder probe surfaces an actionable error rather than a silent
// 404. Production main.go always supplies a non-nil http.Handler;
// tests that don't exercise WebDAV leave this unset.
func (s *Server) SetWebDAVHandler(h http.Handler) {
	s.webdav = h
}

// SetGatewayRegistry wires the v1.9.0c gateway registry. Read by the
// /admin/gateways endpoint to enumerate the protocol surface;
// production main.go always supplies a populated registry, tests
// that don't care leave it unset and the handler returns 503
// GATEWAYS_NOT_WIRED.
func (s *Server) SetGatewayRegistry(r *gateway.Registry) {
	s.gateways = r
}

// SetSkinRegistry wires the v1.13.0a (ADR-0008) skin registry. Read
// by /api/v1/skins to enumerate every registered skin (built-in +
// user-uploaded). Production main.go always supplies a registry
// populated with at least basement-default; tests that don't care
// leave it unset and the handler returns 503 SKINS_NOT_WIRED.
func (s *Server) SetSkinRegistry(r *skin.Registry) {
	s.skins = r
}

// SetMetricsRecorder wires the metrics-snapshot recorder into the
// server (v1.0.0d). The recorder is consumed by /admin/usage/series
// for the time-series view. Production wires a FileRecorder; tests
// leave the no-op default installed by New().
func (s *Server) SetMetricsRecorder(r metrics.Recorder) {
	if r == nil {
		s.metrics = metrics.NewNoop()
		return
	}
	s.metrics = r
}

// SetPromCollector wires the v1.11.0f Prometheus collector and the
// token gate read from BASEMENT_METRICS_TOKEN. When nil, /metrics
// returns 503 METRICS_NOT_WIRED. main.go always wires a non-nil
// collector; tests that don't care leave it unset.
//
// MUST be called before Start — the middleware that records HTTP
// counters is installed at routes() time.
func (s *Server) SetPromCollector(c *metrics.Collector, token string) {
	s.promCollector = c
	s.promToken = token
}

// authMiddleware returns the auth middleware to install on protected
// route groups. Production wires the v1.7.0b bearer path here by
// passing the wired service-account store as the BearerVerifier;
// tests + setups without WireServiceAccounts() get cookie-only auth
// (verifier is nil, the path silently degrades).
//
// Looked up at request time via s.store rather than captured here so
// a later WireServiceAccounts() call still takes effect — but in
// production the store is always wired before New() (see main.go).
func (s *Server) authMiddleware() func(http.Handler) http.Handler {
	var verifier auth.BearerVerifier
	if s.store != nil {
		if sas := s.store.ServiceAccounts(); sas != nil {
			verifier = sas
		}
	}
	return auth.MiddlewareWithBearer(s.cfg.JWT.Secret, verifier)
}

// Start starts the HTTP server and blocks until context is canceled or error.
func (s *Server) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		s.logger.Info("shutdown signal received, initiating graceful shutdown")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("http server shutdown error", "error", err)
		}
	}()

	s.logger.Info("starting http server", "addr", s.cfg.Listen)

	return s.httpServer.ListenAndServe()
}

// routes registers chi middleware and the /api/v1/* route group.
//
// v1.9.0a: the JSON-only AllowContentType middleware moved off the
// root router into the /api/v1 sub-router so /webdav/* requests (which
// carry XML on PROPFIND and arbitrary content types on PUT) aren't
// rejected with 415 before they reach the WebDAV handler. Every
// /api/v1 endpoint still enforces JSON via the apiR.Use call below.
func (s *Server) routes() {
	r := s.router

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(s.logHandler)
	// v1.11.0f: Prometheus HTTP counters + latency histogram. Installed
	// at the root so /metrics, /api/v1/*, /webdav/* all get observed.
	// The wrapper checks s.promCollector at request time so callers can
	// SetPromCollector after New() (the chi router is built once in
	// New() and middleware chains aren't re-runnable).
	r.Use(s.promMiddlewareDeferred)

	r.Route("/api/v1", func(apiR chi.Router) {
		apiR.Use(middleware.AllowContentType("application/json"))
		apiR.Use(xBuildMiddleware)

		// Public routes — no auth required.
		apiR.Get("/health", s.healthHandler)
		apiR.Get("/version", s.versionHandler)
		apiR.Get("/auth/methods", s.authMethodsHandler)
		// v1.3.0b: per-driver placeholder + hint catalogue used by
		// the cluster + key forms. Public — config hints, not secrets;
		// FE caches forever.
		apiR.Get("/system/driver-defaults", s.driverDefaultsHandler)
		apiR.Post("/auth/login", s.loginHandler)
		apiR.Get("/auth/oidc/start", s.oidcStartHandler)
		apiR.Get("/auth/oidc/callback", s.oidcCallbackHandler)

		// Invite redemption (public, no auth required)
		apiR.Post("/invites/{token}/redeem", s.inviteRedeemHandler)

		// Public share routes — no auth required (v0.7.0h SHARE.PUBLIC).
		apiR.Get("/share/{token}/info", s.shareInfoHandler)
		apiR.Post("/share/{token}/auth", s.shareAuthHandler)
		apiR.Get("/share/{token}/list", s.shareListHandler)
		apiR.Get("/share/{token}/get", s.shareGetHandler)

		// Authenticated routes — JWT cookie required.
		apiR.Group(func(authG chi.Router) {
			authG.Use(s.authMiddleware())

			authG.Post("/auth/logout", s.logoutHandler)
			authG.Get("/auth/me", s.meHandler)
			authG.Put("/auth/active-role", s.activeRoleHandler)
			authG.Post("/auth/elevate", s.elevateHandler)
			authG.Post("/auth/logout-elevation", s.logoutElevationHandler)
			// ADR-0003 v1.2.0c: OIDC step-up elevation. Start mints
			// a state token + returns the IdP authorize URL with
			// `prompt=login` + `max_age=0`; callback exchanges the
			// code, checks auth_time freshness, and mints the
			// elevated cookie before redirecting to "/?elevated=...".
			authG.Post("/auth/elevate/oidc/start", s.elevateOIDCStartHandler)
			authG.Get("/auth/elevate/oidc/callback", s.elevateOIDCCallbackHandler)
			authG.Get("/auth/org-capabilities", s.getCurrentOrgCapabilities)
			authG.Get("/capabilities", s.capabilitiesHandler)
// v1.13.0a (ADR-0008): pluggable skins read endpoint.
			// Every logged-in user can enumerate all registered skins;
			// the active skin is rendered at boot from registry tokens.
			authG.Get("/skins", s.listSkinsHandler)
			// v1.13.1: GET /api/v1/skins/active — fetch current active skin for live re-skinning
			authG.Get("/skins/active", s.getActiveSkinHandler)
			// v1.13.0c: user skin override — PUT /api/v1/user/skin allows
			// authenticated users to pick their own skin if org policy permits.
			authG.Put("/user/skin", s.setUserSkinHandler)
		})

		// Admin routes — admin role required.
		apiR.Group(func(adminG chi.Router) {
			adminG.Use(s.authMiddleware())
			adminG.Use(auth.RequireRole("admin"))

			// Cluster admin routes — require activeRole.kind == "cluster-admin" && activeRole.cluster == cid
			adminG.Group(func(clusterG chi.Router) {
				clusterG.Use(auth.ActiveRoleClusterMiddleware("{cid}"))
				clusterG.Get("/admin/clusters/{cid}/nodes", s.listNodesHandler)
			adminG.Get("/admin/clusters/{cid}/layout", s.getLayoutHandler)
			adminG.Post("/admin/clusters/{cid}/layout/stage", s.stageLayoutHandler)
			adminG.Post("/admin/clusters/{cid}/layout/apply", s.applyLayoutHandler)
			adminG.Post("/admin/clusters/{cid}/layout/revert", s.revertLayoutHandler)

			// v1.4.0c SCRUB.MAINT — block-scrub maintenance surface.
			// Both reads + writes gated on cluster:edit (per-handler)
			// so admin-role users without cluster:edit on this cluster
			// can't probe scrub state on a cluster they don't own.
			adminG.Get("/admin/clusters/{cid}/scrub", s.getClusterScrubHandler)
			adminG.Post("/admin/clusters/{cid}/scrub", s.postClusterScrubHandler)

			// Connection CRUD
			adminG.Get("/admin/clusters", s.listClustersHandler)
			adminG.Post("/admin/clusters", s.createClusterHandler)
			adminG.Get("/admin/clusters/{cid}", s.getClusterHandler)
			adminG.Patch("/admin/clusters/{cid}", s.updateClusterHandler)
			adminG.Delete("/admin/clusters/{cid}", s.deleteClusterHandler)
			adminG.Post("/admin/clusters/{cid}/_arm-delete", s.armDeleteClusterHandler)
			adminG.Post("/admin/clusters/{cid}/_test", s.testClusterHandler)

			// v1.12.0a (ADR-0007) per-cluster envelope encryption.
			// Five endpoints power the unlock-modal + multi-admin
			// password management flow. Other handlers that touch
			// CSK-protected secrets gate on s.requireUnlocked(w, cid)
			// to return 423 LOCKED with a structured hint.
			adminG.Post("/admin/clusters/{cid}/unlock", s.unlockClusterHandler)
			adminG.Post("/admin/clusters/{cid}/lock", s.lockClusterHandler)
			adminG.Get("/admin/clusters/{cid}/lock-status", s.lockStatusHandler)
			adminG.Post("/admin/clusters/{cid}/admins", s.addAdminHandler)
			adminG.Delete("/admin/clusters/{cid}/admins/{adminUserId}", s.removeAdminHandler)

			// v1.11.0.6 — per-cluster capability matrix. The global
			// /api/v1/capabilities endpoint reads from s.drv (default
			// driver) only; this per-cluster variant runs the active
			// driver for {cid} so a deploy with mixed drivers can
			// render the right UI per cluster. Routes through
			// driverForRouteCluster per the v1.11.0.2 dispatch fix.
			adminG.Get("/admin/clusters/{cid}/driver-info", s.driverInfoHandler)

			// v1.3.0e CLUSTER.ADMINS — convenience read for the
			// cluster detail page that filters the global assignment
			// list down to this cluster (including wildcard
			// inheritance). Writes still go through the global
			// /admin/policies/assignments endpoints.
			adminG.Get("/admin/clusters/{cid}/admins", s.listClusterAdminsHandler)

			// Connection-scoped bucket operations
			adminG.Get("/admin/clusters/{cid}/buckets", s.listBucketsByClusterHandler)
			adminG.Post("/admin/clusters/{cid}/buckets", s.createBucketHandler)
			adminG.Get("/admin/clusters/{cid}/buckets/{id}", s.getBucketHandler)
			adminG.Patch("/admin/clusters/{cid}/buckets/{id}", s.updateBucketHandler)
			adminG.Delete("/admin/clusters/{cid}/buckets/{id}", s.deleteBucketHandler)
			adminG.Post("/admin/clusters/{cid}/buckets/{id}/_arm-delete", s.armDeleteBucketHandler)

			// Connection-scoped key operations
			adminG.Get("/admin/clusters/{cid}/keys", s.listKeysByClusterHandler)
			adminG.Post("/admin/clusters/{cid}/keys", s.createKeyHandler)
			adminG.Get("/admin/clusters/{cid}/keys/{id}", s.getKeyHandler)
			adminG.Patch("/admin/clusters/{cid}/keys/{id}", s.updateKeyHandler)
			adminG.Delete("/admin/clusters/{cid}/keys/{id}", s.deleteKeyHandler)
			adminG.Post("/admin/clusters/{cid}/keys/{id}/_arm-delete", s.armDeleteKeyHandler)
			}) // end cluster admin group with active role middleware

			// Cross-cluster aggregated reads (legacy paths, now return wrapped responses)
			adminG.Get("/admin/buckets", s.listAllBucketsHandler)

			// v1.11.0.15: /admin/keys removed. Keys are inherently
			// per-cluster (Garage admin model); a flat global list
			// strips that context and the route had no FE consumer
			// after the per-cluster route model landed in v1.11.0.3.
			// Operators that bookmarked it now hit 404 — the
			// canonical per-cluster list lives at
			// /admin/clusters/{cid}/keys.
		})

		// UI Admin routes — require activeRole.kind == "ui-admin"
		apiR.Group(func(uiAdminG chi.Router) {
			uiAdminG.Use(s.authMiddleware())
			uiAdminG.Use(auth.ActiveRoleUIAdminMiddleware())

			// Org capabilities management
			uiAdminG.Get("/admin/system", s.getOrgCapabilitiesHandler)
			uiAdminG.Patch("/admin/system", s.updateOrgCapabilitiesHandler)

			// v1.11.0a ONBOARDING — first-run wizard support.
			// /state reports {needsOnboarding, completed} so the FE
			// AppShell can auto-route fresh admin logins to
			// /admin/first-run; /dismiss latches OnboardingCompleted
			// so the wizard never auto-shows again. Both gated under
			// the uiAdminG group (the wizard is host-admin-only) and
			// dismiss carries an explicit host:manage_org_caps check.
			uiAdminG.Get("/admin/onboarding/state", s.getOnboardingStateHandler)
			uiAdminG.Post("/admin/onboarding/dismiss", s.dismissOnboardingHandler)

			// v1.9.0c GATEWAYS: per-protocol gateway roster (real +
			// stub) for the generalized /admin/gateways UI. Read-only;
			// per-protocol toggles still go through PATCH /admin/system.
			uiAdminG.Get("/admin/gateways", s.listGatewaysHandler)

			// User management (global, UI Admin only)
			uiAdminG.Get("/admin/users", s.listAllUsersHandler)
			uiAdminG.Post("/admin/users", s.createUserHandler)
			uiAdminG.Delete("/admin/users/{id}", s.deleteUserHandler)

			// Persistent invite tokens (v1.3.0d). The /admin/users
			// invite-only flow above writes user records eagerly;
			// these endpoints manage standalone invite tokens that
			// only materialize into User records on redemption. Both
			// surfaces are gated on host:manage_users (per-handler
			// requireCapability), so the routes can sit next to each
			// other under uiAdminG.
			uiAdminG.Get("/admin/invites", s.listInvitesHandler)
			uiAdminG.Post("/admin/invites", s.createInvitePersistedHandler)
			uiAdminG.Delete("/admin/invites/{id}", s.revokeInviteHandler)
			uiAdminG.Post("/admin/invites/{id}/rotate", s.rotateInviteHandler)

			// v1.7.0a SERVICE_ACCOUNTS — basement-issued long-lived
			// access keys for automated clients (CI, k8s, MCP, CLI).
			// Same host:manage_users gate as the invite family above;
			// each handler runs its own per-call requireCapability so
			// the uiAdminG middleware is defense-in-depth only.
			// Cross-user GET / PUT / DELETE collapse to 404 so the
			// wire shape doesn't leak IDs across owners.
			uiAdminG.Get("/admin/service-accounts", s.listServiceAccountsHandler)
			uiAdminG.Post("/admin/service-accounts", s.createServiceAccountHandler)
			uiAdminG.Get("/admin/service-accounts/{id}", s.getServiceAccountHandler)
			uiAdminG.Put("/admin/service-accounts/{id}", s.updateServiceAccountHandler)
			uiAdminG.Delete("/admin/service-accounts/{id}", s.deleteServiceAccountHandler)
			uiAdminG.Post("/admin/service-accounts/{id}/rotate", s.rotateServiceAccountHandler)

			// Policy matrix editor (ADR-0001 cycle v0.9.0g). Each
			// handler runs its own capability gate so the legacy
			// UIAdmin middleware is purely defense-in-depth; once
			// the matrix lets operators rebalance assignments,
			// UIAdmin can retire.
			uiAdminG.Get("/admin/policies", s.listPoliciesHandler)
			uiAdminG.Post("/admin/policies/roles", s.upsertRoleHandler)
			uiAdminG.Delete("/admin/policies/roles/{id}", s.deleteRoleHandler)
			uiAdminG.Post("/admin/policies/assignments", s.assignRoleHandler)
			uiAdminG.Delete("/admin/policies/assignments", s.unassignRoleHandler)

			// POLICY.SIM (v0.9.0j): what-if simulator that walks
			// Enforcer.CanWithReason and returns the reasoning trail.
			// Same policy:view_matrix gate as the matrix GET — pure
			// inspector, no enforcement-logic changes.
			uiAdminG.Post("/admin/policies/simulate", s.simulatePolicyHandler)

			// v1.3.0a: OIDC group-claim -> role auto-mapping config.
			// Same persona that owns /admin/policies owns this — gated
			// on host:manage_policies inside the handler. Mappings
			// apply on each user's next OIDC login.
			uiAdminG.Get("/admin/oidc-group-mappings", s.listOIDCGroupMappingsHandler)
			uiAdminG.Put("/admin/oidc-group-mappings", s.updateOIDCGroupMappingsHandler)

			// v1.13.0b: Skin management endpoints (upload, activate, delete, policy).
			uiAdminG.Get("/admin/skins", s.listAdminSkinsHandler)
			uiAdminG.Post("/admin/skins/upload", s.uploadSkinHandler)
			uiAdminG.Put("/admin/skins/{id}/activate", s.activateSkinHandler)
			uiAdminG.Delete("/admin/skins/{id}", s.deleteSkinHandler)
			uiAdminG.Get("/admin/skins/{id}/policy", s.getSkinPolicyHandler)
			uiAdminG.Put("/admin/skins/{id}/policy", s.updateSkinPolicyHandler)

			// Bucket lifecycle (v0.9.0i LIFECYCLE.WIZARD). UIAdmin
			// middleware is belt-and-braces; the actual enforcement
			// is the per-handler bucket:view / bucket:edit_alias gate.
			uiAdminG.Get("/admin/clusters/{cid}/buckets/{bid}/lifecycle", s.getBucketLifecycleHandler)
			uiAdminG.Put("/admin/clusters/{cid}/buckets/{bid}/lifecycle", s.putBucketLifecycleHandler)

			// OBS.USAGE (v0.9.0k): storage overview dashboard.
			// Read-only snapshot aggregated from existing per-cluster
			// reads; per-handler gate is host:manage_users so any Host
			// Admin sees it without needing a new capability.
			uiAdminG.Get("/admin/usage/overview", s.getUsageOverviewHandler)

			// OBS.USAGE.SERIES (v1.0.0d): per-bucket time series from
			// the metrics recorder. Same host:manage_users gate as the
			// snapshot view above.
			uiAdminG.Get("/admin/usage/series", s.getUsageSeriesHandler)

			// AUDIT.LOG (v1.0.0c): query the append-only event log.
			// Per-handler gate is host:manage_policies — the same
			// persona that controls the matrix is the one who needs
			// the accountability view (and seeing audit data without
			// the gate would leak who-did-what across the matrix).
			uiAdminG.Get("/admin/audit", s.listAuditHandler)
		})

		// User routes — authenticated users only. Visibility derives
		// from each user's region keychain (ADR-0002); see user_filter.go.
		apiR.Group(func(userG chi.Router) {
			userG.Use(s.authMiddleware())

			// User shares endpoints (v0.7.0g USER.SHARES).
			userG.Post("/user/shares", s.userCreateShareHandler)
			userG.Get("/user/shares", s.userListSharesHandler)
			userG.Delete("/user/shares/{token}", s.userRevokeShareHandler)

			// User sync endpoints (v0.8.0c SYNC.ENGINE.PULL).
			userG.Post("/user/syncs", s.userCreateSyncHandler)
			userG.Get("/user/syncs", s.userListSyncsHandler)
			userG.Get("/user/syncs/{id}", s.userGetSyncHandler)
			userG.Delete("/user/syncs/{id}", s.userDeleteSyncHandler)
			userG.Post("/user/syncs/{id}/pause", s.userPauseSyncHandler)
			userG.Post("/user/syncs/{id}/resume", s.userResumeSyncHandler)

			// User backup endpoints (v1.5.0a BACKUP.SCHEDULED).
			// Promotes the sync engine into a recurring, named
			// backup with a cron schedule. Handlers return 503
			// when the backup subsystem isn't wired (tests).
			userG.Post("/user/backups", s.userCreateBackupHandler)
			userG.Get("/user/backups", s.userListBackupsHandler)
			userG.Get("/user/backups/{id}", s.userGetBackupHandler)
			userG.Put("/user/backups/{id}", s.userUpdateBackupHandler)
			userG.Delete("/user/backups/{id}", s.userDeleteBackupHandler)
			userG.Post("/user/backups/{id}/run", s.userRunBackupHandler)
			// v1.5.0b: list the snapshot timestamps the backup
			// currently has on disk. Used by the detail page to
			// render the "browse this snapshot" table. Returns an
			// empty array for mirror-mode backups.
			userG.Get("/user/backups/{id}/snapshots", s.userListBackupSnapshotsHandler)
			// v1.5.0c: restore a snapshot back to a chosen target.
			// Synchronous — the wizard wants the per-object summary
			// inline. See backup_restore.go for the engine.
			userG.Post("/user/backups/{id}/restore", s.userRestoreBackupHandler)

			// v1.6.0c FEDERATION.API — user-tier CRUD + failover +
			// resync over the FederatedBucket store + replication
			// engine (ADR-0005). Handlers return 503
			// FEDERATIONS_NOT_WIRED when the store wasn't opened at
			// boot (tests).
			userG.Post("/user/federated-buckets", s.userCreateFederationHandler)
			userG.Get("/user/federated-buckets", s.userListFederationsHandler)
			// v1.6.0e — reverse-lookup endpoint: "is this (region, bucket)
			// part of a federation I own?" The bucket browser calls this
			// speculatively to render a federation badge + link. Registered
			// before the /{id} route so chi matches the literal segment.
			userG.Get("/user/federated-buckets/by-target", s.userFindFederationByTargetHandler)
			userG.Get("/user/federated-buckets/{id}", s.userGetFederationHandler)
			userG.Put("/user/federated-buckets/{id}", s.userUpdateFederationHandler)
			userG.Delete("/user/federated-buckets/{id}", s.userDeleteFederationHandler)
			userG.Post("/user/federated-buckets/{id}/failover", s.userFailoverFederationHandler)
			userG.Post("/user/federated-buckets/{id}/resync", s.userResyncFederationHandler)

			// v1.7.0d WEBHOOK.SUBSCRIPTIONS — user-tier CRUD over
			// bucket-event webhook subscriptions. Handlers return
			// 503 WEBHOOKS_NOT_WIRED when the store wasn't opened
			// at boot (tests).
			userG.Post("/user/webhooks", s.userCreateWebhookHandler)
			userG.Get("/user/webhooks", s.userListWebhooksHandler)
			userG.Get("/user/webhooks/{id}", s.userGetWebhookHandler)
			userG.Put("/user/webhooks/{id}", s.userUpdateWebhookHandler)
			userG.Delete("/user/webhooks/{id}", s.userDeleteWebhookHandler)
			userG.Post("/user/webhooks/{id}/test", s.userTestWebhookHandler)
			userG.Post("/user/webhooks/{id}/enable", s.userEnableWebhookHandler)
			userG.Post("/user/webhooks/{id}/disable", s.userDisableWebhookHandler)

			// User region keychain endpoints (ADR-0002, cycle
			// v1.1.0b). The region's S3 key IS the permission —
			// audit is via the owner-check (404 on mismatch) and
			// the backend's own access enforcement, not via
			// requireCapability. See internal/api/user_regions.go.
			userG.Post("/user/regions", s.userCreateRegionHandler)
			// v1.3.0d: bulk-import — create N regions in one call,
			// per-row error reporting (no abort-on-first). Same USER
			// auth gate as single create.
			userG.Post("/user/regions/bulk", s.userBulkCreateRegionsHandler)
			userG.Get("/user/regions", s.userListRegionsHandler)
			userG.Get("/user/regions/{regionId}", s.userGetRegionHandler)
			userG.Post("/user/regions/{regionId}/rotate", s.userRotateRegionHandler)
			userG.Delete("/user/regions/{regionId}", s.userDeleteRegionHandler)
			userG.Get("/user/regions/{regionId}/buckets", s.userListRegionBucketsHandler)
			userG.Get("/user/regions/{regionId}/buckets/{bid}/objects", s.userListRegionBucketObjectsHandler)
			userG.Get("/user/regions/{regionId}/buckets/{bid}/objects/{key}/presign-get", s.userPresignGetRegionObjectHandler)
			userG.Post("/user/regions/{regionId}/buckets/{bid}/objects/{key}/presign-put", s.userPresignPutRegionObjectHandler)
			userG.Post("/user/regions/{regionId}/buckets/{bid}/multipart/init", s.userInitRegionMultipartHandler)
			userG.Post("/user/regions/{regionId}/buckets/{bid}/multipart/{uploadId}/part/{partNum}/presign", s.userPresignRegionUploadPartHandler)
			userG.Post("/user/regions/{regionId}/buckets/{bid}/multipart/{uploadId}/complete", s.userCompleteRegionMultipartHandler)
			userG.Delete("/user/regions/{regionId}/buckets/{bid}/multipart/{uploadId}", s.userAbortRegionMultipartHandler)
			userG.Delete("/user/regions/{regionId}/buckets/{bid}/objects/{key}", s.userDeleteRegionObjectHandler)

			// v1.10.0a VERSIONING — bucket-level toggle + per-object
			// version history. Capability-gated (the driver's
			// VersioningSupport() returns 501 NOT_SUPPORTED on
			// Garage variants today). Audit events:
			//   bucket:versioning_get        — read
			//   bucket:versioning_enabled    — flip to Enabled
			//   bucket:versioning_suspended  — flip to Suspended
			//   object:version_list/get      — read-with-trail
			//   object:version_delete        — destructive (always audited)
			userG.Get("/user/regions/{regionId}/buckets/{bid}/versioning", s.userGetBucketVersioningHandler)
			userG.Put("/user/regions/{regionId}/buckets/{bid}/versioning", s.userPutBucketVersioningHandler)
			userG.Get("/user/regions/{regionId}/buckets/{bid}/o/{key}/versions", s.userListObjectVersionsHandler)
			userG.Get("/user/regions/{regionId}/buckets/{bid}/o/{key}/versions/{versionId}", s.userGetObjectVersionHandler)
			userG.Delete("/user/regions/{regionId}/buckets/{bid}/o/{key}/versions/{versionId}", s.userDeleteObjectVersionHandler)

			// v1.10.0c OBJECT_LOCK — bucket-level config + per-version
			// retention + legal hold. Layered on top of versioning per
			// the S3 spec. Capability-gated (drivers without Object
			// Lock support return 501 NOT_SUPPORTED on the mutating
			// paths, and supported=false on the GET so the FE can
			// hide the card). Audit events:
			//   bucket:object_lock_enabled
			//   bucket:object_lock_default_retention_set
			//   object:retention_set / _extended / _reduced
			//   object:legal_hold_set / _released
			userG.Get("/user/regions/{regionId}/buckets/{bid}/object-lock", s.userGetBucketObjectLockHandler)
			userG.Put("/user/regions/{regionId}/buckets/{bid}/object-lock", s.userPutBucketObjectLockHandler)
			userG.Get("/user/regions/{regionId}/buckets/{bid}/o/{key}/retention", s.userGetObjectRetentionHandler)
			userG.Put("/user/regions/{regionId}/buckets/{bid}/o/{key}/retention", s.userPutObjectRetentionHandler)
			userG.Get("/user/regions/{regionId}/buckets/{bid}/o/{key}/legal-hold", s.userGetObjectLegalHoldHandler)
			userG.Put("/user/regions/{regionId}/buckets/{bid}/o/{key}/legal-hold", s.userPutObjectLegalHoldHandler)

			// v1.10.0d ENCRYPTION — bucket-level default server-side
			// encryption. SSE-S3 (backend-managed key) + SSE-KMS
			// (operator-supplied KMS key). Capability-gated per axis
			// (driver returns (s3, kms) capability bits; API rejects
			// algorithm requests that don't match an advertised axis
			// with 501 + the specific capability hint). Audit events:
			//   bucket:encryption_enabled
			//   bucket:encryption_disabled
			//   bucket:encryption_algorithm_changed
			//   bucket:encryption_kms_key_changed
			userG.Get("/user/regions/{regionId}/buckets/{bid}/encryption", s.userGetBucketEncryptionHandler)
			userG.Put("/user/regions/{regionId}/buckets/{bid}/encryption", s.userPutBucketEncryptionHandler)
			userG.Delete("/user/regions/{regionId}/buckets/{bid}/encryption", s.userDeleteBucketEncryptionHandler)
		})
	})

	// v1.9.0a WebDAV gateway. Mounted as a sub-tree so chi can dispatch
	// the full path (including verb-tagged children) into the webdav
	// handler. When SetWebDAVHandler hasn't been called we return a
	// typed 503 so a Finder probe surfaces "service not configured"
	// instead of falling through to the SPA's catchall 404.
	r.Handle("/webdav", s.webdavRouter())
	r.Handle("/webdav/*", s.webdavRouter())

	// v1.11.0f: Prometheus exporter. No auth by convention (operators
	// front this with a network allowlist) unless promToken is set, in
	// which case the handler enforces Bearer auth. Returns 503 when the
	// collector isn't wired so misconfigurations surface clearly.
	r.Handle("/metrics", s.metricsHandler())

	// v1.13.23: public docs at /docs/*.md rendered as HTML with basement chrome.
	// v1.13.25: chi requires "/docs/*" (wildcard) to match subpaths; bare
	// "/docs/" only matches the exact path and falls through to the SPA fallback.
	r.Handle("/docs/*", s.docsHandler())
	r.Handle("/docs/", s.docsHandler())

	r.Handle("/*", web.Handler())
}

// metricsHandler returns the Prometheus /metrics handler if the
// collector was wired, otherwise a 503 stub.
func (s *Server) metricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.promCollector == nil {
			writeErrorSimple(w, http.StatusServiceUnavailable, "METRICS_NOT_WIRED",
				"Prometheus exporter is not configured on this deployment.")
			return
		}
		s.promCollector.Handler(s.promToken).ServeHTTP(w, r)
	})
}

// promRouteFor resolves the chi route template (e.g. /api/v1/buckets/{id})
// from a request — gives the Prometheus HTTP counter labels a bounded
// cardinality (one row per declared route) rather than one row per
// concrete URL path.
//
// Falls back to the raw path when chi hasn't matched yet (root-level
// requests where chi's RouteContext is empty).
func promRouteFor(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return r.URL.Path
	}
	if p := rctx.RoutePattern(); p != "" {
		return p
	}
	return r.URL.Path
}

// promMiddlewareDeferred is a thin middleware that checks
// s.promCollector at request time and delegates to the collector's
// own middleware. When the collector hasn't been wired (tests), it
// passes through cleanly. Defining this as a method (rather than
// computing the middleware up front in routes()) means callers can
// SetPromCollector at any point after New() — the chi router is
// stamped once in New() and we can't re-run the .Use chain.
func (s *Server) promMiddlewareDeferred(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.promCollector == nil {
			next.ServeHTTP(w, r)
			return
		}
		s.promCollector.PromMiddleware(promRouteFor)(next).ServeHTTP(w, r)
	})
}

// webdavRouter returns the wired WebDAV handler if SetWebDAVHandler
// was called, otherwise a small 503-emitting stub. Centralised so
// both /webdav and /webdav/* mount points share the same fallback.
func (s *Server) webdavRouter() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.webdav == nil {
			writeErrorSimple(w, http.StatusServiceUnavailable, "WEBDAV_NOT_WIRED",
				"WebDAV gateway is not configured on this deployment.")
			return
		}

		// Browser navigation to /webdav returns a helpful message instead of 404.
		if r.Method == http.MethodGet && (r.URL.Path == "/webdav" || r.URL.Path == "/webdav/") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Basement WebDAV Gateway</title>
<style>body{font-family:system-ui,sans-serif;margin:4rem max(2rem,5vw);line-height:1.6}h1{margin:0 0 1rem}.code{background:#f4f4f4;padding:.2em .4rem;border-radius:3px;font-family:monospace}</style>
</head>
<body>
<h1>Basement WebDAV Gateway</h1>
<p>This endpoint is for <strong>WebDAV clients</strong>, not browsers.</p>
<p>To mount this location:</p>
<ul>
<li><span class="code">macOS Finder</span>: Go → Connect to Server… → <span class="code">dav://basement.pq.io/webdav</span></li>
<li><span class="code">Linux</span>: Use <span class="code">davfs2</span> or GNOME Files with <span class="code">dav://basement.pq.io/webdav</span></li>
<li><span class="code">Cyberduck</span>: Protocol WebDAV, Server <span class="code">basement.pq.io</span>, Path <span class="code">/webdav</span></li>
</ul>
<p>See <a href="/docs/integrations/webdav">/docs/integrations/webdav</a> for setup instructions.</p>
</body>
</html>`)
			return
		}

		s.webdav.ServeHTTP(w, r)
	})
}

// docsHandler returns the /docs/* handler that renders markdown files as HTML.
func (s *Server) docsHandler() http.Handler {
	return http.HandlerFunc(docs.HandleDocs)
}

// logHandler is a middleware equivalent to chi.Logger using slog.
func (s *Server) logHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		ww := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(ww, r)

		s.logger.Log(r.Context(), slog.LevelInfo, "request",
			"method", r.Method,
			"url", r.URL.Path,
			"status", ww.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
