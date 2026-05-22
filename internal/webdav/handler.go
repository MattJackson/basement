// Package webdav: HTTP handler that scopes golang.org/x/net/webdav.Handler
// to a per-request, per-user FileSystem rooted at /webdav/ (v1.9.0a).
//
// Wire model:
//
//	ServeHTTP
//	  → authenticate (Basic, env-admin / user / service-account)
//	  → build a per-request fs scoped to the caller's userID
//	  → hand off to golang.org/x/net/webdav.Handler.ServeHTTP
//
// LOCK and UNLOCK return 501 by short-circuiting before the webdav
// Handler is invoked. The upstream package mandates a LockSystem
// reference even when LOCK isn't used (the no-op confirmLocks path),
// so we pass webdav.NewMemLS() as a stub — it costs nothing per
// request and the 501 short-circuit prevents real lock state from
// accumulating.
//
// Configuration: the production wiring (cmd/basement-server/main.go)
// builds a single Handler at startup and mounts it under /webdav/.
// Tests can construct one directly with stub dependencies.

package webdav

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	wdav "golang.org/x/net/webdav"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/serviceaccount"
	"github.com/mattjackson/basement/internal/store"
)

// Deps wraps the production dependencies the Handler needs. The
// fields mirror the names used in internal/api so an operator
// grepping for "regions" + "registry" lands on consistent terminology.
//
// Any field may be nil in tests; callers that nil out Users + SAs
// effectively disable both auth paths, which is fine when the test
// constructs its own pre-resolved actor and bypasses authenticate().
type Deps struct {
	Cfg      *config.Config
	Regions  store.UserRegions
	Registry *driver.Registry
	Users    userLookup
	SAs      serviceaccount.ServiceAccounts
	Audit    audit.Logger
	Logger   *slog.Logger

	// Connections is consulted by the Garage admin bridge — the same
	// lookup the /api/v1/user/regions/{id}/buckets handler runs. May
	// be nil; in that case the bridge is skipped and the user-key
	// ListBuckets answers directly (matching AWS S3 + MinIO).
	Connections store.Connections
}

// Handler is the HTTP handler mounted at /webdav/. Build one at boot,
// not per-request — it caches the underlying webdav.LockSystem.
//
// regionLookupOverride / driverFactoryOverride are test seams: when
// non-nil they replace the production lookups that walk the deps
// store + registry. The fields are private + only ever set by code
// inside this package, so a production wiring path always uses the
// real Deps-backed lookups.
type Handler struct {
	deps   Deps
	auth   *authResolver
	locks  wdav.LockSystem
	logger *slog.Logger
	prefix string // always "/webdav"; constant for the v1.9.0a layout.

	regionLookupOverride  regionLookup
	driverFactoryOverride driverFactory
}

// New constructs a Handler. Returns nil only if the dependency bag is
// itself nil — every field is permitted to be nil individually so test
// setups can pick what to wire.
func New(deps Deps) *Handler {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		deps:   deps,
		auth:   &authResolver{cfg: deps.Cfg, users: deps.Users, sas: deps.SAs},
		locks:  wdav.NewMemLS(),
		logger: logger,
		prefix: "/webdav",
	}
}

// ServeHTTP is the entry point. Auth → fs → upstream webdav.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Short-circuit LOCK / UNLOCK with 501 per the v1.9.0a spec —
	// most read+write clients (Finder, Explorer, Nautilus, rclone)
	// tolerate the absence of locking.
	if r.Method == "LOCK" || r.Method == "UNLOCK" {
		http.Error(w, "LOCK/UNLOCK not implemented", http.StatusNotImplemented)
		return
	}

	// Pre-auth OPTIONS so an unauth'd discovery probe doesn't trigger
	// a password prompt. macOS Finder hits OPTIONS / first.
	if r.Method == http.MethodOptions {
		h.writeOptions(w)
		return
	}

	actor, ok := h.auth.authenticate(w, r)
	if !ok {
		// 401 already written by authenticate().
		return
	}

	if h.deps.Regions == nil && h.regionLookupOverride == nil {
		http.Error(w, "regions store not configured", http.StatusServiceUnavailable)
		return
	}

	// Build a per-request fs. Cheap; just struct allocation. Test
	// overrides take priority so a test can drive the verbs without
	// wiring a real driver registry.
	lookup := h.regionLookup
	if h.regionLookupOverride != nil {
		lookup = h.regionLookupOverride
	}
	factory := h.driverFactory(actor)
	if h.driverFactoryOverride != nil {
		factory = h.driverFactoryOverride
	}
	fsys := newFS(
		r.Context(),
		actor.UserID,
		lookup,
		factory,
		h.adminBridge,
	)

	wdh := &wdav.Handler{
		Prefix:     h.prefix,
		FileSystem: fsys,
		LockSystem: h.locks,
		Logger:     h.requestLogger(actor),
	}

	// Audit-emit before handing off; the upstream Handler doesn't tell
	// us the outcome of the verb in a structured way, so we treat the
	// dispatch as the auditable event ("matthew did PROPFIND on /alias").
	h.auditDispatch(r, actor)

	wdh.ServeHTTP(w, r)
}

// writeOptions emits a DAV-compatible OPTIONS response without
// authenticating. Advertises DAV class 1 + 3 (read+write), with no
// LOCK (no class 2). The Allow list covers every verb the upstream
// Handler implements.
func (h *Handler) writeOptions(w http.ResponseWriter) {
	w.Header().Set("DAV", "1, 3")
	w.Header().Set("Allow", "OPTIONS, GET, HEAD, POST, PUT, DELETE, PROPFIND, PROPPATCH, MKCOL, COPY, MOVE")
	w.Header().Set("MS-Author-Via", "DAV")
	w.WriteHeader(http.StatusOK)
}

// regionLookup is the regionLookup func injected into fs. Pulls from
// the persistent store; never returns nil — at worst returns an empty
// slice.
func (h *Handler) regionLookup(ctx context.Context, userID string) ([]store.UserRegion, error) {
	if h.deps.Regions == nil {
		return nil, nil
	}
	return h.deps.Regions.ListForUser(ctx, userID)
}

// driverFactory builds the per-region driver. Bumps LastUsedAt best-
// effort on each build so the operator can see "this region was
// touched via WebDAV at 10:42" in the keychain UI.
func (h *Handler) driverFactory(actor resolvedActor) driverFactory {
	return func(ctx context.Context, region store.UserRegion) (driver.Driver, error) {
		if h.deps.Registry == nil {
			return nil, driver.ErrUnsupported
		}
		secret, err := h.deps.Regions.Decrypt(region)
		if err != nil {
			return nil, err
		}
		d, err := h.deps.Registry.ForUserRegion(ctx, region.Endpoint, region.AccessKeyID, secret, region.Region, region.AddressingStyle)
		if err != nil {
			return nil, err
		}
		_ = h.deps.Regions.TouchLastUsed(ctx, region.ID)
		return d, nil
	}
}

// adminBridge replicates internal/api.garageRegionBucketsBridge for
// the WebDAV listing path. Without this, a Garage-backed region
// shows zero buckets at /webdav/{alias} because Garage's data-plane
// ListBuckets is unimplemented. We intentionally mirror the API-side
// shape rather than share code — keeps the package boundary clean
// and the WebDAV path doesn't depend on internal/api at all.
func (h *Handler) adminBridge(ctx context.Context, region store.UserRegion, userDrv driver.Driver) ([]driver.Bucket, bool, error) {
	if h.deps.Connections == nil || h.deps.Registry == nil {
		return nil, false, nil
	}
	conns, err := h.deps.Connections.List(ctx)
	if err != nil {
		// Don't fail the listing for a hiccup in admin lookups; fall
		// through and let the user driver answer (which is the right
		// behaviour for non-Garage backends anyway).
		h.logger.Warn("webdav: failed to list admin connections", "error", err.Error())
		return nil, false, nil
	}
	target := region.Endpoint
	var match *store.Connection
	for i := range conns {
		c := &conns[i]
		raw := strings.TrimSpace(c.Config["s3_endpoint"])
		if raw == "" {
			raw = strings.TrimSpace(c.Config["endpoint"])
		}
		if raw == "" {
			continue
		}
		canon, err := store.NormalizeEndpoint(raw)
		if err != nil || canon != target {
			continue
		}
		if c.Driver != store.DriverGarage && c.Driver != store.DriverGarageV1 {
			continue
		}
		match = c
		break
	}
	if match == nil {
		return nil, false, nil
	}
	adminDrv, err := h.deps.Registry.For(ctx, match.ID)
	if err != nil {
		return nil, true, err
	}
	buckets, err := adminDrv.ListBuckets(ctx)
	if err != nil {
		return nil, true, err
	}
	return buckets, true, nil
}

// requestLogger threads request errors into slog. The upstream
// webdav package emits one err per request (nil for success) with
// the *http.Request. We coalesce into a single structured log line
// so a failing PROPFIND shows up in the operator's logs alongside
// /api/v1 traffic.
func (h *Handler) requestLogger(actor resolvedActor) func(*http.Request, error) {
	return func(r *http.Request, err error) {
		if err == nil {
			h.logger.Debug("webdav request",
				"method", r.Method,
				"path", r.URL.Path,
				"actor", actor.ActorLabel)
			return
		}
		h.logger.Warn("webdav request error",
			"method", r.Method,
			"path", r.URL.Path,
			"actor", actor.ActorLabel,
			"error", err.Error())
	}
}

// auditDispatch emits one audit event per WebDAV verb. The shape
// mirrors the /api/v1 conventions: action="webdav:{verb}",
// resource="webdav:{path}". This is a "dispatch" event, not a
// "success" event — the actual verb outcome isn't known until the
// upstream Handler finishes, but the trail still answers "who hit
// what path with which actor."
func (h *Handler) auditDispatch(r *http.Request, actor resolvedActor) {
	if h.deps.Audit == nil {
		return
	}
	verb := strings.ToLower(r.Method)
	h.deps.Audit.Log(audit.Event{
		Actor:     actor.ActorLabel,
		ActorRole: "user",
		Action:    "webdav:" + verb,
		Resource:  "webdav:" + r.URL.Path,
		Result:    audit.ResultSuccess,
		Detail:    "",
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	})
}

// clientIP extracts the request's remote address sans port. Same
// shape as internal/api.clientIP — duplicated here so the webdav
// package doesn't depend on internal/api.
func clientIP(r *http.Request) string {
	ra := r.RemoteAddr
	if ra == "" {
		return ""
	}
	for i := len(ra) - 1; i >= 0; i-- {
		if ra[i] == ':' {
			return ra[:i]
		}
		if ra[i] < '0' || ra[i] > '9' {
			break
		}
	}
	return ra
}
