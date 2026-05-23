// Package webdav: the WebDAV gateway (v1.9.0c refactor of the legacy
// internal/webdav package onto the Gateway + Backend interfaces).
//
// Wire model (unchanged from v1.9.0a/b):
//
//	ServeHTTP
//	  → check Gateways.WebDAV.Enabled
//	  → short-circuit OPTIONS pre-auth (Finder DAV discovery)
//	  → authenticate (Basic, env-admin / user / SA-bearer)
//	  → build a per-request fs scoped to the caller's UserContext
//	  → hand off to golang.org/x/net/webdav.Handler.ServeHTTP
//
// The refactor moves the data plane onto gateway.Backend. fs.go no
// longer knows about driver.Registry or store.UserRegions — every
// data-plane call goes through Backend.{ListRegions, ListBuckets,
// ListObjects, HeadObject, GetObject, PutObject, DeleteObject,
// CopyObject, CreateBucket, DeleteBucket}.
//
// What still lives in this package (versus a shared helper): the
// WebDAV-specific path parsing (alias / bucket / key), the folder-
// marker convention (zero-byte object with trailing /), the Finder-
// friendly Stat fallback to "is this a folder prefix", and the auth
// HTTP shape. None of that is reusable across protocols — SMB has
// a different namespace model, NFS has handles, FTP has stateful
// CWD. Each gateway owns its protocol's path semantics; the Backend
// is the shared data-plane.

package webdav

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	wdav "golang.org/x/net/webdav"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/gateway"
)

// Gateway is the WebDAV protocol handler. Implements
// gateway.Gateway + http.Handler so the registry can hand it back to
// the chi router under the /webdav/ prefix.
type Gateway struct {
	backend gateway.Backend
	orgCaps OrgCapsLookup
	audit   audit.Logger
	logger  *slog.Logger

	auth   *authResolver
	locks  wdav.LockSystem
	prefix string

	// stats is a thin runtime counter the /admin/gateways endpoint
	// surfaces. We keep it on the gateway, not the registry, so
	// per-protocol counters stay close to the code that increments
	// them.
	stats *runtimeStats

	// running flips on Start; the gateway is no-op-safe to re-Start
	// (StartAll calls every gateway, including ones already running
	// in test loops).
	running atomic.Bool
	mu      sync.Mutex
}

// runtimeStats tracks the Status() fields the gateway reports back
// to the registry. Atomics so concurrent requests can increment
// without a per-request mutex acquire.
type runtimeStats struct {
	totalRequests atomic.Int64
	lastActivity  atomic.Pointer[time.Time]
	lastError     atomic.Pointer[string]
}

// Deps wraps the gateway's dependencies. Mirrors the v1.9.0a/b
// internal/webdav.Deps shape so the production main.go wiring
// translates almost line-for-line.
type Deps struct {
	Backend gateway.Backend
	OrgCaps OrgCapsLookup
	Audit   audit.Logger
	Logger  *slog.Logger
}

// OrgCapsLookup is the narrow read interface the gateway uses to
// resolve the WebDAV.Enabled toggle. *store.OrgCapabilitiesStore
// satisfies the equivalent shape in main.go (one indirection).
type OrgCapsLookup interface {
	IsEnabled() bool
}

// New constructs a WebDAV gateway. The returned value implements
// both gateway.Gateway and http.Handler.
//
// Backend MUST be non-nil — without it the gateway can't serve any
// verb. The other fields may be nil for tests.
func New(deps Deps) *Gateway {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	g := &Gateway{
		backend: deps.Backend,
		orgCaps: deps.OrgCaps,
		audit:   deps.Audit,
		logger:  logger,
		locks:   wdav.NewMemLS(),
		prefix:  "/webdav",
		stats:   &runtimeStats{},
	}
	g.auth = &authResolver{backend: deps.Backend}
	return g
}

// --- gateway.Gateway interface ------------------------------------

// Name returns the stable identifier "webdav".
func (g *Gateway) Name() string { return "webdav" }

// DisplayName returns the human-friendly label.
func (g *Gateway) DisplayName() string { return "WebDAV" }

// Description is the one-sentence operator-facing what-it-is.
func (g *Gateway) Description() string {
	return "Mount basement as a native filesystem in Finder, Explorer, Nautilus or any WebDAV-aware client."
}

// Capabilities reports the WebDAV protocol surface. WebDAV supports
// every basic verb; LOCK is advertised false because the gateway
// short-circuits LOCK/UNLOCK with 501 (same v1.9.0a behaviour).
//
// Auth methods: Basic (password) and Bearer-shaped Basic (BMNT
// access key in the username slot) are both accepted; SigV4 isn't a
// WebDAV thing.
func (g *Gateway) Capabilities() gateway.Capabilities {
	return gateway.Capabilities{
		Read:       true,
		Write:      true,
		Delete:     true,
		Move:       true,
		Lock:       false,
		BasicAuth:  true,
		BearerAuth: true,
	}
}

// Status returns the runtime stats. Always populated; the absent
// fields stay at their zero values.
func (g *Gateway) Status() gateway.Status {
	st := gateway.Status{
		Running:       g.running.Load(),
		TotalRequests: g.stats.totalRequests.Load(),
	}
	if t := g.stats.lastActivity.Load(); t != nil {
		st.LastActivity = t
	}
	if s := g.stats.lastError.Load(); s != nil {
		st.LastError = *s
	}
	return st
}

// Implemented returns true — WebDAV is the v1.9.0a/b production
// implementation; the other four gateways registered in v1.9.0c are
// stubs.
func (g *Gateway) Implemented() bool { return true }

// Start brings the gateway online. HTTP-mounted gateway: no-op
// beyond setting the running flag. The real mount happens in main.go
// via HTTPHandler() + chi.
func (g *Gateway) Start(_ context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.running.Store(true)
	return nil
}

// Stop tears the gateway down. Sets running to false; the chi mount
// continues to dispatch requests after Stop, so we don't try to tear
// down the mount itself. Future enhancement: refuse requests when
// !running.
func (g *Gateway) Stop(_ context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.running.Store(false)
	return nil
}

// HTTPHandler returns the gateway itself — Gateway implements
// http.Handler.
func (g *Gateway) HTTPHandler() http.Handler { return g }

// ListenAddress returns "" because WebDAV is HTTP-mounted: the
// basement server already binds. Operators read the externally-
// visible URL from window.location.origin + /webdav in the UI, or
// from the WebDAVSettings.BaseURL override.
func (g *Gateway) ListenAddress() string { return "" }
