// Package webdav: HTTP handler shape — the ServeHTTP entrypoint, the
// short-circuits (LOCK/UNLOCK, OPTIONS, kill-switch), and the
// dispatch into the upstream golang.org/x/net/webdav.Handler.
//
// All of this code matches the v1.9.0a/b behaviour line-for-line;
// the only real change is that the per-request fs is now wired via
// gateway.Backend instead of (regionLookup, driverFactory, adminBridge)
// — see fs.go for the data-plane refactor.

package webdav

import (
	"net/http"
	"strings"
	"time"

	wdav "golang.org/x/net/webdav"

	"github.com/mattjackson/basement/internal/audit"
)

// ServeHTTP is the entry point for /webdav/* requests. Auth → fs →
// upstream webdav.Handler. Pre-auth short-circuits cover OPTIONS
// (Finder DAV discovery without a credentials prompt), LOCK / UNLOCK
// (return 501 — most clients tolerate the absence of locking), and
// the operator kill switch (Gateways.WebDAV.Enabled).
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.touchActivity()
	g.stats.totalRequests.Add(1)

	// Short-circuit LOCK / UNLOCK with 501. Read/write clients
	// (Finder, Explorer, Nautilus, rclone) tolerate the absence of
	// locking; the upstream wdav.Handler would otherwise issue
	// LOCK responses that we'd then have to back with real lock
	// state (memory leak across long-running clients).
	if r.Method == "LOCK" || r.Method == "UNLOCK" {
		http.Error(w, "LOCK/UNLOCK not implemented", http.StatusNotImplemented)
		return
	}

	// Operator-toggleable kill switch (v1.9.0b). When the operator
	// has flipped Gateways.WebDAV.Enabled off we refuse every request
	// — including OPTIONS — with a typed 403 so a probing client
	// surfaces "the operator turned this off" rather than a confusing
	// 401 cycle. Skip the check when no OrgCaps was wired (older
	// test setups + bare Gateway construction).
	if g.orgCaps != nil && !g.orgCaps.IsEnabled() {
		writeGatewayDisabled(w)
		return
	}

	// Pre-auth OPTIONS so an unauth'd discovery probe doesn't trigger
	// a password prompt. macOS Finder hits OPTIONS / first.
	if r.Method == http.MethodOptions {
		g.writeOptions(w)
		return
	}

	uctx, ok := g.auth.authenticate(w, r)
	if !ok {
		// 401 already written by authenticate().
		return
	}
	if uctx == nil {
		// Shouldn't happen — authenticate either writes 401 + returns
		// false, or returns a non-nil uctx — but defensive.
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if g.backend == nil {
		http.Error(w, "backend not configured", http.StatusServiceUnavailable)
		return
	}

	// Build a per-request fs scoped to the resolved UserContext. The
	// fs caches lookups within a single request; never reused across
	// requests.
	fsys := newFS(r.Context(), uctx.UserContext, g.backend)

	wdh := &wdav.Handler{
		Prefix:     g.prefix,
		FileSystem: fsys,
		LockSystem: g.locks,
		Logger:     g.requestLogger(uctx),
	}

	// Audit-emit before handing off; the upstream Handler doesn't
	// surface a structured outcome, so we treat the dispatch as the
	// auditable event ("alice did PROPFIND on /home").
	g.auditDispatch(r, uctx)

	wdh.ServeHTTP(w, r)
}

// writeGatewayDisabled emits a 403 with a JSON body matching the
// /api/v1 error shape. WebDAV clients won't parse the body but humans
// (and curl, rclone -vv) will.
func writeGatewayDisabled(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`{"code":"GATEWAY_DISABLED","message":"WebDAV gateway is disabled by the operator."}`))
}

// writeOptions emits a DAV-compatible OPTIONS response without
// authenticating. Advertises DAV class 1 + 3 (read+write), with no
// LOCK (no class 2). The Allow list covers every verb the upstream
// Handler implements.
func (g *Gateway) writeOptions(w http.ResponseWriter) {
	w.Header().Set("DAV", "1, 3")
	w.Header().Set("Allow", "OPTIONS, GET, HEAD, POST, PUT, DELETE, PROPFIND, PROPPATCH, MKCOL, COPY, MOVE")
	w.Header().Set("MS-Author-Via", "DAV")
	w.WriteHeader(http.StatusOK)
}

// requestLogger threads request errors into slog. The upstream
// webdav package emits one err per request (nil for success) with
// the *http.Request. We coalesce into a single structured log line
// so a failing PROPFIND shows up in the operator's logs alongside
// /api/v1 traffic — and bump the LastError stat for the
// /admin/gateways status pane.
func (g *Gateway) requestLogger(uctx interface{ Label() string }) func(*http.Request, error) {
	label := uctx.Label()
	return func(r *http.Request, err error) {
		if err == nil {
			g.logger.Debug("webdav request",
				"method", r.Method,
				"path", r.URL.Path,
				"actor", label)
			return
		}
		msg := err.Error()
		g.stats.lastError.Store(&msg)
		g.logger.Warn("webdav request error",
			"method", r.Method,
			"path", r.URL.Path,
			"actor", label,
			"error", msg)
	}
}

// auditDispatch emits one audit event per WebDAV verb. Same shape
// as v1.9.0a/b: action="webdav:{verb}", resource="webdav:{path}".
func (g *Gateway) auditDispatch(r *http.Request, uctx interface{ Label() string }) {
	if g.audit == nil {
		return
	}
	verb := strings.ToLower(r.Method)
	g.audit.Log(audit.Event{
		Actor:     uctx.Label(),
		ActorRole: "user",
		Action:    "webdav:" + verb,
		Resource:  "webdav:" + r.URL.Path,
		Result:    audit.ResultSuccess,
		Detail:    "",
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	})
}

// touchActivity bumps the LastActivity timestamp on every request.
// Atomic pointer swap so the /admin/gateways read can grab a
// consistent snapshot without a mutex.
func (g *Gateway) touchActivity() {
	t := time.Now().UTC()
	g.stats.lastActivity.Store(&t)
}

// clientIP extracts the request's remote address sans port. Same
// shape as internal/api.clientIP — duplicated here so the gateway
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
