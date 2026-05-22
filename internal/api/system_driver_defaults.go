package api

import (
	"encoding/json"
	"net/http"

	"github.com/mattjackson/basement/internal/driver"
)

// driverDefaultsHandler returns the curated EndpointDefaults entries
// for every registered driver. Public — these are config hints, not
// secrets. The frontend caches the response forever (driver hints
// don't change at runtime, only with a new binary).
//
// Shape: an array of driver.EndpointDefaults, one entry per driver
// the registry knows about. See internal/driver/defaults.go for the
// curated table.
func (s *Server) driverDefaultsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(driver.Defaults())
}
