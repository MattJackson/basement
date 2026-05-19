package api

import (
	"encoding/json"
	"net/http"

	"github.com/mattjackson/basement/internal/version"
)

// HealthResponse is the response shape for /api/v1/health.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// healthHandler handles GET /api/v1/health requests.
func (s *Server) healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	resp := HealthResponse{
		Status:  "ok",
		Version: version.Version,
	}

	_ = json.NewEncoder(w).Encode(resp)
}

// versionHandler handles GET /api/v1/version. Public (no auth) so
// operators can verify which build is deployed without logging in.
func (s *Server) versionHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(version.Get())
}
