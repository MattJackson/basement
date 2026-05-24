package auth

import (
	"context"
	"net/http"
)

type contextKey string

const (
	claimsKey    = contextKey("claims")
	uiAdminKey   = contextKey("uiAdmin")
	clusterAdmin = contextKey("clusterAdmin")
	bucketGrant  = contextKey("bucketGrant")
)

// Middleware returns an HTTP middleware that validates the session JWT
// cookie. On success, it stores *Claims in the request context; on
// failure, it writes a 401 error.
//
// This is a thin convenience around MiddlewareWithBearer(secret, nil)
// for callers that haven't wired the service-account bearer path yet.
// Production wiring uses MiddlewareWithBearer directly so bearer tokens
// resolve alongside cookies; tests + legacy call sites keep using
// Middleware for cookie-only behaviour.
func Middleware(secret []byte) func(http.Handler) http.Handler {
	return MiddlewareWithBearer(secret, nil)
}

// FromContext retrieves *Claims from the request context.
func FromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsKey).(*Claims)
	if !ok {
		return nil, false
	}
	return claims, true
}

// RequireUIAdmin returns an HTTP middleware that requires UI Admin status.
func RequireUIAdmin() func(http.Handler) http.Handler {
	return ActiveRoleUIAdminMiddleware()
}

// RequireRole returns an HTTP middleware that requires a specific role.
// It writes 403 Forbidden if the user's role doesn't match.
func RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := FromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "SESSION_REQUIRED", "Session cookie not found")
				return
			}

			if claims.Role != role {
				writeError(w, http.StatusForbidden, "INSUFFICIENT_ROLE", "Insufficient permissions")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":{"code":"` + code + `","message":"` + message + `"}}`))
}
