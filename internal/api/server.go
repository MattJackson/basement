package api

import (
	"context"
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
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/metrics"
	"github.com/mattjackson/basement/internal/store"
	"github.com/mattjackson/basement/internal/sync"
	"github.com/mattjackson/basement/internal/web"
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
	oidc       oidcProvider
	policy     policy.Enforcer
	audit      audit.Logger
	metrics    metrics.Recorder
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
func (s *Server) routes() {
	r := s.router

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(s.logHandler)
	r.Use(middleware.AllowContentType("application/json"))

	r.Route("/api/v1", func(apiR chi.Router) {
		apiR.Use(xBuildMiddleware)

		// Public routes — no auth required.
		apiR.Get("/health", s.healthHandler)
		apiR.Get("/version", s.versionHandler)
		apiR.Get("/auth/methods", s.authMethodsHandler)
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
			authG.Use(auth.Middleware(s.cfg.JWT.Secret))

			authG.Post("/auth/logout", s.logoutHandler)
			authG.Get("/auth/me", s.meHandler)
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
		})

		// Admin routes — admin role required.
		apiR.Group(func(adminG chi.Router) {
			adminG.Use(auth.Middleware(s.cfg.JWT.Secret))
			adminG.Use(auth.RequireRole("admin"))

			adminG.Get("/admin/clusters/{cid}/nodes", s.listNodesHandler)
			adminG.Get("/admin/clusters/{cid}/layout", s.getLayoutHandler)
			adminG.Post("/admin/clusters/{cid}/layout/stage", s.stageLayoutHandler)
			adminG.Post("/admin/clusters/{cid}/layout/apply", s.applyLayoutHandler)
			adminG.Post("/admin/clusters/{cid}/layout/revert", s.revertLayoutHandler)

			// Connection CRUD
			adminG.Get("/admin/clusters", s.listClustersHandler)
			adminG.Post("/admin/clusters", s.createClusterHandler)
			adminG.Get("/admin/clusters/{cid}", s.getClusterHandler)
			adminG.Patch("/admin/clusters/{cid}", s.updateClusterHandler)
			adminG.Delete("/admin/clusters/{cid}", s.deleteClusterHandler)
			adminG.Post("/admin/clusters/{cid}/_arm-delete", s.armDeleteClusterHandler)
			adminG.Post("/admin/clusters/{cid}/_test", s.testClusterHandler)

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

			// Cross-cluster aggregated reads (legacy paths, now return wrapped responses)
			adminG.Get("/admin/buckets", s.listAllBucketsHandler)
			adminG.Get("/admin/keys", s.listAllKeysHandler)
		})

		// UI Admin routes — require uiAdmin=true.
		apiR.Group(func(uiAdminG chi.Router) {
			uiAdminG.Use(auth.Middleware(s.cfg.JWT.Secret))
			uiAdminG.Use(auth.RequireUIAdmin())

			// Org capabilities management
			uiAdminG.Get("/admin/system", s.getOrgCapabilitiesHandler)
			uiAdminG.Patch("/admin/system", s.updateOrgCapabilitiesHandler)

			// User management (global, UI Admin only)
			uiAdminG.Get("/admin/users", s.listAllUsersHandler)
			uiAdminG.Post("/admin/users", s.createUserHandler)
			uiAdminG.Delete("/admin/users/{id}", s.deleteUserHandler)

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
			userG.Use(auth.Middleware(s.cfg.JWT.Secret))

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

			// User region keychain endpoints (ADR-0002, cycle
			// v1.1.0b). The region's S3 key IS the permission —
			// audit is via the owner-check (404 on mismatch) and
			// the backend's own access enforcement, not via
			// requireCapability. See internal/api/user_regions.go.
			userG.Post("/user/regions", s.userCreateRegionHandler)
			userG.Get("/user/regions", s.userListRegionsHandler)
			userG.Get("/user/regions/{regionId}", s.userGetRegionHandler)
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
		})
	})

	r.Handle("/*", web.Handler())
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
