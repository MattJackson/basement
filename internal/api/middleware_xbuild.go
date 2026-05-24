package api

import (
	"net/http"

	"github.com/mattjackson/basement/internal/version"
)

// xBuildMiddleware stamps X-Build on every API response so clients can
// detect a server-build change and prompt for refresh. Paired with
// Cache-Control: no-store on index.html — see internal/web/web.go.
func xBuildMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Build", version.Commit)
		w.Header().Set("X-Version", version.Version)
		next.ServeHTTP(w, r)
	})
}
