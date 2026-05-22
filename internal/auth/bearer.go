// Package auth: bearer-token authentication for service accounts
// (v1.7.0b).
//
// Service accounts (internal/serviceaccount, v1.7.0a) are basement-
// issued long-lived credentials for non-interactive clients: CLI tools,
// CI runners, k8s controllers, MCP servers. They live alongside the
// JWT cookie path; the same MiddlewareWithBearer wraps both auth modes
// and falls through from one to the other in a strict priority order:
//
//   1. JWT cookie (__Host-basement_session) — cookie-bearing clients.
//      Cookies are more authoritative than headers (an attacker who
//      can set the Authorization header still can't override an
//      HttpOnly Secure cookie set by a previous login), so when both
//      are present we honour the cookie.
//   2. Bearer header — "Authorization: Bearer {AccessKeyID}:{Secret}".
//      Format is raw colon-separated rather than base64-encoded because
//      AKIDs are unambiguous (BMNT + 16 hex chars) and the colon-form
//      is easier to debug from a shell.
//   3. Neither — 401 SESSION_REQUIRED.
//
// The verifier interface is narrow on purpose: only the three hot-path
// methods from serviceaccount.ServiceAccounts that the middleware
// actually calls. This lets tests pass a tiny in-memory fake without
// implementing the entire CRUD surface.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/mattjackson/basement/internal/serviceaccount"
)

// BearerVerifier is the narrow subset of serviceaccount.ServiceAccounts
// the bearer middleware needs. serviceaccount.ServiceAccounts satisfies
// it directly; test fakes only need these three methods.
//
// GetByAccessKey returns serviceaccount.ErrNotFound if the AKID is
// unknown. VerifySecret returns (false, nil) for revoked / expired /
// mismatched secrets and (false, serviceaccount.ErrNotFound) when the
// AKID itself doesn't exist (the bcrypt comparison is never reached).
// TouchLastUsed is best-effort — a failed disk write must not 5xx an
// otherwise-valid request, so the middleware logs and continues.
type BearerVerifier interface {
	GetByAccessKey(ctx context.Context, akid string) (serviceaccount.ServiceAccount, error)
	VerifySecret(ctx context.Context, akid, candidateSecret string) (bool, error)
	TouchLastUsed(ctx context.Context, id string) error
}

// MiddlewareWithBearer returns an HTTP middleware that accepts either a
// session JWT cookie OR a service-account bearer header. Behaviour
// matches the doc-comment priority order: cookie wins if both present.
//
// secret is the JWT signing key for the cookie path. verifier may be
// nil — in which case the bearer path is disabled and the middleware
// degrades to the legacy JWT-only behaviour (same as Middleware). This
// lets test setups that don't care about bearer-auth re-use the
// existing wiring with a nil third arg.
func MiddlewareWithBearer(secret []byte, verifier BearerVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. JWT cookie — most authoritative path. We attempt this
			//    first and short-circuit on any error other than "no
			//    cookie present" so a malformed cookie isn't silently
			//    upgraded into a bearer-auth attempt.
			if cookie, err := r.Cookie(CookieName); err == nil {
				claims, parseErr := ParseToken(secret, cookie.Value)
				if parseErr != nil {
					writeError(w, http.StatusUnauthorized, "INVALID_SESSION", "Invalid or expired session")
					return
				}
				ctx := context.WithValue(r.Context(), claimsKey, claims)
				ctx = context.WithValue(ctx, uiAdminKey, claims.UIAdmin)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// 2. Bearer header — service-account path. Only attempted
			//    if the cookie was missing entirely (not malformed).
			if verifier != nil {
				if authz := r.Header.Get("Authorization"); strings.HasPrefix(authz, "Bearer ") {
					handleBearer(w, r, next, verifier, strings.TrimPrefix(authz, "Bearer "))
					return
				}
			}

			// 3. Neither — fall back to the legacy "session required"
			//    response so existing FE behaviour (redirect to login)
			//    is unchanged.
			writeError(w, http.StatusUnauthorized, "SESSION_REQUIRED", "Session cookie not found")
		})
	}
}

// handleBearer parses the bearer payload, resolves the SA, verifies
// the secret, debounce-touches LastUsedAt, and injects a Claims into
// the request context with ServiceAccountID set.
func handleBearer(w http.ResponseWriter, r *http.Request, next http.Handler, verifier BearerVerifier, payload string) {
	akid, secret, ok := splitBearerPayload(payload)
	if !ok {
		writeError(w, http.StatusUnauthorized, "MALFORMED_BEARER",
			"Bearer token must be of the form Bearer {AccessKeyID}:{Secret}")
		return
	}

	sa, err := verifier.GetByAccessKey(r.Context(), akid)
	if err != nil {
		if errors.Is(err, serviceaccount.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "INVALID_ACCESS_KEY",
				"Access key not recognized")
			return
		}
		writeError(w, http.StatusUnauthorized, "INVALID_ACCESS_KEY",
			"Access key lookup failed")
		return
	}

	// Revocation + expiry are checked BEFORE the bcrypt compare so a
	// revoked-credential attempt gets a precise reason in the wire
	// shape (operators triaging an alert want to see "this CI runner
	// is still using the key we revoked", not "bad creds").
	if sa.IsRevoked() {
		writeError(w, http.StatusUnauthorized, "SERVICE_ACCOUNT_REVOKED",
			"This service account has been revoked")
		return
	}
	if sa.IsExpired(nowFunc().UTC()) {
		writeError(w, http.StatusUnauthorized, "SERVICE_ACCOUNT_EXPIRED",
			"This service account has expired")
		return
	}

	matched, err := verifier.VerifySecret(r.Context(), akid, secret)
	if err != nil {
		// ErrNotFound shouldn't reach here (GetByAccessKey above
		// already resolved the row) but treat any verifier-side error
		// as a generic mismatch so we never leak internal state.
		writeError(w, http.StatusUnauthorized, "INVALID_SECRET",
			"Secret did not match")
		return
	}
	if !matched {
		writeError(w, http.StatusUnauthorized, "INVALID_SECRET",
			"Secret did not match")
		return
	}

	// Best-effort LastUsedAt bump. TouchLastUsed debounces internally
	// so back-to-back requests don't churn the JSON store; a failure
	// here is logged but does not 5xx the request the operator is
	// trying to make.
	_ = verifier.TouchLastUsed(r.Context(), sa.ID)

	// Construct a Claims shaped for the rest of the auth pipeline:
	// UserID = SA's owner so existing handlers that read claims.UserID
	// keep attributing actions to the right human; ServiceAccountID
	// = sa.ID so policy gates + audit can distinguish M2M from human.
	// Mode is "user" — bearer tokens cannot elevate to ADMIN at all
	// (sudo mode requires fresh password / OIDC, not a static secret),
	// so the SA's capability list is the floor AND the ceiling.
	claims := &Claims{
		UserID:           sa.OwnerUserID,
		Role:             "service_account",
		UIAdmin:          false,
		Mode:             "user",
		ModeExpiresAt:    0,
		ServiceAccountID: sa.ID,
	}
	ctx := context.WithValue(r.Context(), claimsKey, claims)
	ctx = context.WithValue(ctx, uiAdminKey, false)
	next.ServeHTTP(w, r.WithContext(ctx))
}

// splitBearerPayload separates "{AccessKeyID}:{Secret}" into its two
// halves. Rejects empty either-side (so "Bearer :secret" or
// "Bearer akid:" both fail). The AKID prefix discipline (BMNT + hex)
// is checked downstream by GetByAccessKey — this splitter is grammar-
// only so a future format change (e.g. base64 fallback) is one place.
func splitBearerPayload(payload string) (akid, secret string, ok bool) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "", "", false
	}
	idx := strings.IndexByte(payload, ':')
	if idx <= 0 || idx == len(payload)-1 {
		return "", "", false
	}
	return payload[:idx], payload[idx+1:], true
}
