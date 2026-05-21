package api

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/oauth2"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/auth/policy"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
	"github.com/mattjackson/basement/internal/sync"
	"github.com/mattjackson/basement/internal/web"
)

// oidcProvider is the subset of *auth.OIDCProvider the API server needs.
// Defined as an interface here so tests can substitute a fake.
type oidcProvider interface {
	AuthCodeURL(state, nonce string) string
	Exchange(ctx context.Context, code string) (*oauth2.Token, error)
	VerifyIDToken(ctx context.Context, rawIDToken, expectedNonce string) (*auth.OIDCClaims, error)
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
	router     chi.Router
	httpServer *http.Server
	logger     *slog.Logger
}

// New creates a new Server instance with both legacy single-driver (for back-compat) and registry.
//
// OIDC is wired separately via SetOIDC() so that callers and tests that
// don't care about OIDC don't have to thread a nil through. When OIDC
// isn't set, the /auth/oidc/* routes return 501 OIDC_NOT_CONFIGURED and
// local-password login remains the only auth path.
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
// Handlers that need RoleAssignments — e.g. POST /user/buckets/connect —
// nil-check s.policy and return 503 POLICY_NOT_WIRED when this hasn't
// been called.
func (s *Server) SetPolicy(p policy.Enforcer) {
	s.policy = p
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
		})

		// User routes — authenticated users only. Grants filtered server-side.
		apiR.Group(func(userG chi.Router) {
			userG.Use(auth.Middleware(s.cfg.JWT.Secret))

			userG.Get("/user/clusters", s.userListClustersHandler)
			userG.Post("/user/clusters", s.createUserClusterHandler)
			userG.Post("/user/clusters/_test", s.testUserClusterHandler)

			// Per ADR-0001 (v0.9.0e): user-persona "Add bucket access"
			// flow. The user brings their own S3 creds for a bucket
			// they're entitled to and basement stores a Grant + a
			// bucket_user RoleAssignment for them.
			userG.Post("/user/buckets/connect", s.userBucketsConnectHandler)
			userG.Get("/user/clusters/{cid}", s.userGetClusterHandler)
			userG.Get("/user/clusters/{cid}/buckets", s.userListClusterBucketsHandler)
			userG.Get("/user/clusters/{cid}/buckets/{bid}", s.userGetClusterBucketHandler)
			userG.Get("/user/keys", s.userListKeysHandler)

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

			// User object browser endpoints (v0.7.0d USER.OBJECTBROWSE).
			userG.Get("/user/clusters/{cid}/buckets/{bid}/objects", s.userListClusterBucketObjectsHandler)
			userG.Get("/user/clusters/{cid}/buckets/{bid}/objects/{key+}/stat", s.userStatClusterBucketObjectHandler)
			userG.Post("/user/clusters/{cid}/buckets/{bid}/objects/{key+}/presign-get", s.userPresignGetClusterBucketObjectHandler)

			// User upload endpoints (v0.7.0e USER.UPLOAD).
			userG.Post("/user/clusters/{cid}/buckets/{bid}/multipart/init", s.userInitMultipartUploadHandler)
			userG.Post("/user/clusters/{cid}/buckets/{bid}/multipart/{uploadId}/part/{partNum}/presign", s.userPresignUploadPartHandler)
			userG.Post("/user/clusters/{cid}/buckets/{bid}/multipart/{uploadId}/complete", s.userCompleteMultipartUploadHandler)
			userG.Delete("/user/clusters/{cid}/buckets/{bid}/multipart/{uploadId}", s.userAbortMultipartUploadHandler)
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
