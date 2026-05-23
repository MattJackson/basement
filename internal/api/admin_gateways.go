// Package api: GET /api/v1/admin/gateways — the v1.9.0c read endpoint
// that surfaces the full Gateway registry to the operator UI.
//
// Shape: one JSON entry per registered Gateway, in registry order
// (alphabetical by Name()). Each entry includes the protocol's
// advertised Capabilities, the runtime Status, an Implemented flag,
// and the Enabled flag from org capabilities. The v1.9.0d cycle
// (generalized /admin/gateways UI) consumes this verbatim — the
// generalized card layout is "trust the registry, render the rows".
//
// Mounted under /admin/gateways inside the uiAdminG group; the
// per-handler capability gate is host:manage_org_caps so the same
// persona that flips per-gateway toggles via PATCH /admin/system
// owns the read view too.

package api

import (
	"encoding/json"
	"net/http"

	"github.com/mattjackson/basement/internal/gateway"
	"github.com/mattjackson/basement/internal/store"
)

// gatewayResponse is one row in the GET /admin/gateways response.
// Mirrors gateway.Gateway's accessor shape so the FE consumes a
// flat object rather than re-deriving fields from a registry blob.
type gatewayResponse struct {
	Name         string               `json:"name"`
	DisplayName  string               `json:"displayName"`
	Description  string               `json:"description"`
	Capabilities gateway.Capabilities `json:"capabilities"`
	Status       gateway.Status       `json:"status"`
	Implemented  bool                 `json:"implemented"`
	Enabled      bool                 `json:"enabled"`
	ListenAddr   string               `json:"listenAddress,omitempty"`
}

// listGatewaysHandler handles GET /api/v1/admin/gateways.
func (s *Server) listGatewaysHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}
	if _, ok := s.requireCapability(w, r, "host:manage_org_caps", "host:*"); !ok {
		return
	}
	if s.gateways == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "GATEWAYS_NOT_WIRED",
			"Gateway registry is not configured on this deployment.")
		return
	}

	// Pull the org caps once so the Enabled column reads
	// consistently across every row in this response.
	var caps store.OrgCapabilities
	if s.store != nil {
		caps = s.store.OrgCapabilities().Get()
	}

	all := s.gateways.All()
	out := make([]gatewayResponse, 0, len(all))
	for _, g := range all {
		out = append(out, gatewayResponse{
			Name:         g.Name(),
			DisplayName:  g.DisplayName(),
			Description:  g.Description(),
			Capabilities: g.Capabilities(),
			Status:       g.Status(),
			Implemented:  g.Implemented(),
			Enabled:      gatewayEnabled(g.Name(), caps),
			ListenAddr:   g.ListenAddress(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// gatewayEnabled bridges the gateway name → org capabilities Enabled
// flag. v1.9.0d generalized the lookup onto the GatewaySettings
// Protocols map so any registered gateway (real or future) reads
// through the same path. Stub gateways still report enabled=false by
// default because their Protocols entry defaults missing — but the FE
// uses Implemented() to decide whether to render the toggle at all,
// so the data shape is forward-compatible with the v1.10+ "enable
// SMB once it ships" cycle.
func gatewayEnabled(name string, caps store.OrgCapabilities) bool {
	return caps.Gateways.IsEnabled(name)
}
