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
	// lands. v1.13.1: each Skin includes a swatch field (primary color)
	// from palette.light so FE can render preview cards without re-fetching.

	package api

	import (
		"encoding/json"
		"net/http"

		"github.com/mattjackson/basement/internal/skin"
	)

	// skinListItem is the v1.13.1 response shape for GET /api/v1/skins.
	// Includes swatch field (palette.primary color) so FE can render
	// preview cards without additional requests. Built-in skins get a badge.
	type skinListItem struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Version     string `json:"version"`
		Swatch      string `json:"swatch"`
		BuiltIn     bool   `json:"builtIn"`
	}

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

		allSkins := s.skins.All()
		// Guarantee a non-nil slice so the JSON encoder writes "[]"
		// rather than "null" on an (impossible-in-prod) empty registry.
		if allSkins == nil {
			allSkins = []skin.Skin{}
		}

		// Build response with swatch + built-in badge
		out := make([]skinListItem, 0, len(allSkins))
		for _, sk := range allSkins {
			item := skinListItem{
				Name:        sk.Name,
				DisplayName: sk.DisplayName,
				Version:     sk.Version,
				BuiltIn:     false, // Will be set by caller if known
			}
			// Extract primary color from palette for swatch
			if sk.Palette.Light.Primary != "" {
				item.Swatch = sk.Palette.Light.Primary
			} else if sk.Palette.Dark.Primary != "" {
				item.Swatch = sk.Palette.Dark.Primary
			}
			out = append(out, item)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
