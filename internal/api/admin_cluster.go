package api

import (
	"net/http"

	"github.com/mattjackson/basement/internal/driver"
)

// listNodesHandler handles GET /api/v1/admin/nodes.
// Calls driver.ListNodes and returns JSON []Node per OpenAPI schema.
func (s *Server) listNodesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	nodes, err := s.drv.ListNodes(r.Context())
	if err != nil {
		writeDriverError(w, "ListNodes", err)
		return
	}

	if nodes == nil {
		nodes = []driver.Node{}
	}

	writeJSON(w, http.StatusOK, nodes)
}

// getLayoutHandler handles GET /api/v1/admin/layout.
// Calls driver.GetLayout and returns JSON Layout per OpenAPI schema.
func (s *Server) getLayoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	layout, err := s.drv.GetLayout(r.Context())
	if err != nil {
		writeDriverError(w, "GetLayout", err)
		return
	}

	writeJSON(w, http.StatusOK, layout)
}
