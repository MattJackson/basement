// Package webdav: HTTP Basic auth → user / service-account resolution
// (v1.9.0a).
//
// Two creds shapes are accepted on the same Basic-auth line:
//
//  1. username + password: env-admin (config.Admin.User + PasswordHash)
//     OR a store.User (bcrypt PasswordHash).
//  2. BMNT-prefixed AccessKeyID + service-account secret: same
//     plaintext-on-the-wire form the bearer middleware accepts at
//     /api/v1/*, but here it rides in the password slot so Finder /
//     Explorer can prompt for it as "username = AKID, password = secret".
//
// On success the caller gets a userID string and an audit actor label.
// On failure the helper writes the 401 WWW-Authenticate response and
// returns ok=false; the verb handler must just return.
//
// Discipline: we do NOT issue or set a session cookie — WebDAV clients
// re-send the Basic header on every request, so each verb resolves
// creds from scratch. This is the same model AWS S3 / Garage / MinIO
// use for their S3 wire (every signed request stands alone).

package webdav

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/mattjackson/basement/internal/auth"
	"github.com/mattjackson/basement/internal/config"
	"github.com/mattjackson/basement/internal/serviceaccount"
	"github.com/mattjackson/basement/internal/store"
)

// resolvedActor is the result of authenticate(). UserID is the user
// who owns the regions we will then look up; ActorLabel is what we
// stamp in audit Detail (so an SA-authed request shows up as
// "sa:{id}" while a human shows as their userID).
type resolvedActor struct {
	UserID     string
	ActorLabel string
	// SAName is non-empty when the request was authenticated via a
	// service-account; lets handlers include "sa=ci-prod" in log lines
	// without paying for a second lookup.
	SAName string
}

// authResolver wraps the dependencies needed to authenticate a Basic
// header. Pulled into a struct so the Handler can be constructed with
// nil-safety on stores not wired in tests.
type authResolver struct {
	// cfg supplies the env-admin username + bcrypt hash. The same
	// constants the /api/v1/auth/login handler uses; pulling from
	// config means no second source of truth.
	cfg *config.Config
	// users is the persisted user store (store.Store.UserByUsername).
	// May be nil — in which case only the env admin can log in by
	// password. SA path is unaffected.
	users userLookup
	// sas verifies BMNT bearer credentials. May be nil in tests; in
	// that case the SA path is disabled and only password works.
	sas serviceaccount.ServiceAccounts
}

// userLookup is the narrow surface webdav needs from store.Store. Tests
// can pass a tiny fake without implementing the full user CRUD.
type userLookup interface {
	UserByUsername(name string) (store.User, error)
}

// once-cached env-admin creds. Mirrors the lazy load in
// internal/api/auth.go's loginHandler — both call sites read the same
// config, so initialising once is safe.
var (
	envAdminOnce sync.Once
	envAdminUser string
	envAdminHash string
)

func loadEnvAdmin(cfg *config.Config) {
	envAdminOnce.Do(func() {
		envAdminUser = cfg.Admin.User
		envAdminHash = cfg.Admin.PasswordHash
	})
}

// authenticate parses the Authorization: Basic header, resolves the
// credentials against either the env-admin / user store or the
// service-account store, and returns the resolvedActor on success.
//
// On any failure (no header, malformed, wrong creds) it writes a
// 401 with WWW-Authenticate: Basic so the client prompts for creds,
// and returns ok=false. Callers must return immediately.
func (a *authResolver) authenticate(w http.ResponseWriter, r *http.Request) (resolvedActor, bool) {
	hdr := r.Header.Get("Authorization")
	if hdr == "" {
		return a.challenge(w, "missing credentials")
	}
	if !strings.HasPrefix(hdr, "Basic ") {
		return a.challenge(w, "only Basic auth is accepted")
	}

	user, pass, err := decodeBasic(strings.TrimPrefix(hdr, "Basic "))
	if err != nil {
		return a.challenge(w, "malformed Basic credentials")
	}
	if user == "" || pass == "" {
		return a.challenge(w, "username and password are required")
	}

	// Service-account branch: BMNT-prefixed access key in the username
	// slot. The secret is in the password slot. This shape lets a Mac
	// Finder user enter "BMNT...:..." as their username with the
	// secret in the password field — basement-issued machines + UI
	// see the same wire format whether they call /api/v1 with Bearer
	// or /webdav with Basic.
	if strings.HasPrefix(user, "BMNT") && a.sas != nil {
		return a.authSA(w, r.Context(), user, pass)
	}

	// Password branch: env-admin first (same constant-time compare as
	// the JSON login path), then the store user table.
	loadEnvAdmin(a.cfg)
	if user == envAdminUser && envAdminHash != "" {
		if auth.VerifyPassword(envAdminHash, pass) {
			return resolvedActor{UserID: envAdminUser, ActorLabel: envAdminUser}, true
		}
		return a.challenge(w, "invalid credentials")
	}

	if a.users == nil {
		return a.challenge(w, "invalid credentials")
	}
	u, err := a.users.UserByUsername(user)
	if err != nil {
		return a.challenge(w, "invalid credentials")
	}
	if u.PasswordHash == "" {
		// OIDC-only account — can't authenticate via Basic.
		return a.challenge(w, "this account requires OIDC; WebDAV needs a local password or service-account key")
	}
	if !auth.VerifyPassword(u.PasswordHash, pass) {
		return a.challenge(w, "invalid credentials")
	}
	return resolvedActor{UserID: u.Username, ActorLabel: u.Username}, true
}

// authSA verifies a BMNT bearer-shaped credential pair, returning a
// resolvedActor scoped to the SA's owner. Same revocation / expiry
// gating as the bearer middleware so an SA-authed WebDAV mount loses
// access the moment the operator revokes the key.
func (a *authResolver) authSA(w http.ResponseWriter, ctx context.Context, akid, secret string) (resolvedActor, bool) {
	sa, err := a.sas.GetByAccessKey(ctx, akid)
	if err != nil {
		if errors.Is(err, serviceaccount.ErrNotFound) {
			return a.challenge(w, "unknown access key")
		}
		return a.challenge(w, "credential lookup failed")
	}
	if sa.IsRevoked() {
		return a.challenge(w, "service account revoked")
	}
	// IsExpired uses a time we supply — passing the request's clock
	// keeps the test surface deterministic if a fake clock is wired
	// later. For now plain time.Now() inside the SA helper works.
	if sa.IsExpired(nowFunc()) {
		return a.challenge(w, "service account expired")
	}

	matched, err := a.sas.VerifySecret(ctx, akid, secret)
	if err != nil || !matched {
		return a.challenge(w, "invalid credentials")
	}
	_ = a.sas.TouchLastUsed(ctx, sa.ID)
	return resolvedActor{
		UserID:     sa.OwnerUserID,
		ActorLabel: "sa:" + sa.ID,
		SAName:     sa.Name,
	}, true
}

// challenge writes a 401 with WWW-Authenticate so Finder / Explorer
// pop a password prompt. Reason is logged via the request's slog
// handler (when wired) but never leaked in the body.
func (a *authResolver) challenge(w http.ResponseWriter, _ string) (resolvedActor, bool) {
	w.Header().Set("WWW-Authenticate", `Basic realm="basement"`)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
	return resolvedActor{}, false
}

// decodeBasic decodes "user:pass" from the base64-encoded payload
// found after "Basic ". Returns an error if the bytes aren't valid
// base64 or there's no colon. Leading / trailing whitespace on the
// encoded portion is trimmed first.
func decodeBasic(encoded string) (string, string, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return "", "", errors.New("empty payload")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		// Some clients use URL-safe base64; fall back before erroring.
		raw, err = base64.URLEncoding.DecodeString(encoded)
		if err != nil {
			return "", "", err
		}
	}
	s := string(raw)
	idx := strings.IndexByte(s, ':')
	if idx < 0 {
		return "", "", errors.New("missing colon separator")
	}
	return s[:idx], s[idx+1:], nil
}
