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

// Middleware returns an HTTP middleware that validates the session JWT cookie.
// On success, it stores *Claims in the request context. On failure, it writes a 401 error.
func Middleware(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(CookieName)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "SESSION_REQUIRED", "Session cookie not found")
				return
			}

			claims, err := ParseToken(secret, cookie.Value)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "INVALID_SESSION", "Invalid or expired session")
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			ctx = context.WithValue(ctx, uiAdminKey, claims.UIAdmin)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// FromContext retrieves *Claims from the request context.
func FromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsKey).(*Claims)
	if !ok {
		return nil, false
	}
	return claims, true
}

// IsUIAdmin returns whether the current user is a UI Admin.
func IsUIAdmin(ctx context.Context) bool {
	val := ctx.Value(uiAdminKey)
	if val == nil {
		return false
	}
	b, ok := val.(bool)
	return ok && b
}

// RequireUIAdmin returns an HTTP middleware that requires UI Admin status.
func RequireUIAdmin() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !IsUIAdmin(r.Context()) {
				writeError(w, http.StatusForbidden, "INSUFFICIENT_ROLE", "Insufficient permissions")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
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
