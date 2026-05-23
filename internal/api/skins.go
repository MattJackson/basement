// Package api: GET /api/v1/skins — v1.13.0a (ADR-0008) read endpoint
// surfacing the full Skin registry to the operator + user UI.
//
// Authenticated (every logged-in user can see the skin list — the FE
// renders the active skin's tokens at boot, and per-user override
// lands in v1.13.0c). NOT admin-only: the user-side skin picker in
// v1.13.0c needs the same payload as the /admin/system surface.
//
// Shape: a JSON array of Skin objects, in registry order (alphabetical
// by Name). v1.13.0a always returns exactly one entry — the
// basement-default skin. v1.13.0b widens this once the uploader
// lands.

package api

import (
	"encoding/json"
	"net/http"

	"github.com/mattjackson/basement/internal/skin"
)

// listSkinsHandler handles GET /api/v1/skins. Wired into the
// authenticated group in server.go.
func (s *Server) listSkinsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}
	if s.skins == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "SKINS_NOT_WIRED",
			"Skin registry is not configured on this deployment.")
		return
	}

	out := s.skins.All()
	// Guarantee a non-nil slice so the JSON encoder writes "[]"
	// rather than "null" on an (impossible-in-prod) empty registry.
	// The FE's TanStack-query consumer treats null as "loading still"
	// and would spin forever.
	if out == nil {
		out = []skin.Skin{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
