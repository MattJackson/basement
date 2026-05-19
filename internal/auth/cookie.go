package auth

import (
	"net/http"
	"time"
)

// CookieName is the session cookie key.
const CookieName = "__Host-basement_session"

// SetSessionCookie sets a session cookie with __Host- prefix requirements:
// HttpOnly, Secure, SameSite=Strict, Path=/, no Domain.
func SetSessionCookie(w http.ResponseWriter, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(ttl),
	})
}

// ClearSessionCookie removes the session cookie by setting it with immediate expiry.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}
