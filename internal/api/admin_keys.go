package api

import (
	"net/http"

	"github.com/mattjackson/basement/internal/driver"
)

// listKeysHandler handles GET /api/v1/admin/keys.
// Calls driver.ListKeys and returns JSON []Key per OpenAPI schema.
func (s *Server) listKeysHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	keys, err := s.drv.ListKeys(r.Context())
	if err != nil {
		writeDriverError(w, "ListKeys", err)
		return
	}

	if keys == nil {
		keys = []driver.Key{}
	}

	writeJSON(w, http.StatusOK, keys)
}
