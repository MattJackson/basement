package api

import (
	"encoding/json"
	"net/http"
)

// AuthMethodsResponse advertises which login methods are currently
// available on this server. The login page polls this on first
// render so it can render the right form (password + optional SSO
// button). Public — must not require auth, otherwise the login
// page can't reach it.
type AuthMethodsResponse struct {
	Password bool          `json:"password"`
	OIDC     OIDCAdvertise `json:"oidc"`
}

type OIDCAdvertise struct {
	Configured  bool   `json:"configured"`
	IssuerLabel string `json:"issuerLabel,omitempty"`
}

func (s *Server) authMethodsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	resp := AuthMethodsResponse{
		Password: true,
		OIDC: OIDCAdvertise{
			Configured: s.oidc != nil,
		},
	}
	if s.oidc != nil {
		// Use the configured issuer as the label fallback. The frontend
		// will only display this when OIDC is configured.
		resp.OIDC.IssuerLabel = s.oidc.Issuer()
	}
	_ = json.NewEncoder(w).Encode(resp)
}
