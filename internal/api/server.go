package api

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/web"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// Server holds the HTTP server and its dependencies.
type Server struct {
	cfg    *config.Config
	store  *store.Store
	drv    driver.Driver
	router chi.Router
	httpServer *http.Server
	logger *slog.Logger
}

// New creates a new Server instance.
func New(cfg *config.Config, store *store.Store, drv driver.Driver) *Server {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	srv := &Server{
		cfg:    cfg,
		store:  store,
		drv:    drv,
		router: chi.NewRouter(),
		logger: logger,
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
			// Public routes (no auth required) - /health and /auth/login
			apiR.Get("/health", s.healthHandler)
			apiR.Post("/auth/login", s.loginHandler)

			// Authenticated group with middleware for protected routes
			apiR.Group(func(authG chi.Router) {
				authG.Use(auth.Middleware(s.cfg.JWT.Secret))

				authG.Post("/auth/logout", s.logoutHandler)
				authG.Get("/auth/me", s.meHandler)
				authG.Get("/capabilities", s.capabilitiesHandler)
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
