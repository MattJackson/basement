// Package webdav: HTTP Basic auth → UserContext resolution. v1.9.0c
// refactor: instead of reaching into config/users/service-accounts
// directly, this thin layer delegates to gateway.Backend.AuthBasic /
// .AuthBearer so the protocol code is decoupled from identity
// plumbing.
//
// Two creds shapes on the same Basic-auth line:
//
//  1. username + password: env-admin or persisted user.
//  2. BMNT-prefixed AccessKeyID + service-account secret: Finder /
//     Explorer enter "BMNT...:secret" as user/pass, and the bearer
//     middleware accepts the same shape at /api/v1/*.
//
// On success: callers get a *gateway.UserContext + a label string for
// audit. On failure: 401 + WWW-Authenticate: Basic and ok=false; the
// verb handler must just return.
//
// Discipline: no session cookie. WebDAV clients re-send the Basic
// header on every request; each verb resolves creds from scratch —
// same model AWS S3 / Garage / MinIO use for their S3 wire.

package webdav

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mattjackson/basement/internal/gateway"
)

// resolvedUser wraps gateway.UserContext with a precomputed label
// the audit emitter + logger use. Keeps the request-time Label()
// method cheap (no allocation).
type resolvedUser struct {
	*gateway.UserContext
	label string
}

func (r *resolvedUser) Label() string { return r.label }

// authResolver wraps the Backend so authenticate() can stay terse.
type authResolver struct {
	backend gateway.Backend
}

// authenticate parses Authorization: Basic, dispatches to either
// AuthBasic or AuthBearer (BMNT-prefixed username triggers the
// bearer path), and returns the resolvedUser on success.
//
// On any failure it writes a 401 + WWW-Authenticate so the client
// prompts for creds. Callers must return immediately when ok=false.
func (a *authResolver) authenticate(w http.ResponseWriter, r *http.Request) (*resolvedUser, bool) {
	if a.backend == nil {
		// No backend wired → can't authenticate. Treat as 503 so an
		// operator probing sees "service not configured" instead of
		// a generic 401.
		http.Error(w, "backend not configured", http.StatusServiceUnavailable)
		return nil, false
	}

	hdr := r.Header.Get("Authorization")
	if hdr == "" {
		return a.challenge(w)
	}
	if !strings.HasPrefix(hdr, "Basic ") {
		return a.challenge(w)
	}
	user, pass, err := decodeBasic(strings.TrimPrefix(hdr, "Basic "))
	if err != nil || user == "" || pass == "" {
		return a.challenge(w)
	}

	ctx := r.Context()

	// Service-account branch: BMNT-prefixed AKID in the user slot.
	// The bearer Backend method expects the "AKID:secret" shape, so
	// we glue them back together with a colon — same payload the
	// /api/v1 bearer middleware sees.
	if strings.HasPrefix(user, "BMNT") {
		uctx, err := a.backend.AuthBearer(ctx, user+":"+pass)
		if err != nil || uctx == nil {
			return a.challenge(w)
		}
		return &resolvedUser{
			UserContext: uctx,
			label:       "sa:" + uctx.ServiceAccountID,
		}, true
	}

	uctx, err := a.backend.AuthBasic(ctx, user, pass)
	if err != nil || uctx == nil {
		return a.challenge(w)
	}
	return &resolvedUser{
		UserContext: uctx,
		label:       uctx.UserID,
	}, true
}

// challenge writes a 401 + WWW-Authenticate so Finder / Explorer
// pop a password prompt.
func (a *authResolver) challenge(w http.ResponseWriter) (*resolvedUser, bool) {
	w.Header().Set("WWW-Authenticate", `Basic realm="basement"`)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
	return nil, false
}

// decodeBasic decodes "user:pass" from the base64-encoded payload
// found after "Basic ". Mirrors the legacy implementation; accepts
// URL-safe base64 as a fallback for non-standard clients.
func decodeBasic(encoded string) (string, string, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return "", "", errors.New("empty payload")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(encoded)
		if err != nil {
			return "", "", fmt.Errorf("decodeBasic: %w", err)
		}
	}
	s := string(raw)
	idx := strings.IndexByte(s, ':')
	if idx < 0 {
		return "", "", errors.New("missing colon separator")
	}
	return s[:idx], s[idx+1:], nil
}
