package api

import (
	"encoding/json"
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

// stageLayoutHandler handles POST /admin/layout/stage.
func (s *Server) stageLayoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	var change driver.LayoutChange
	if err := json.NewDecoder(r.Body).Decode(&change); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", "invalid request body", nil)
		return
	}

	diff, err := s.drv.StageLayout(r.Context(), change)
	if err != nil {
		writeDriverError(w, "StageLayout", err)
		return
	}

	writeJSON(w, http.StatusOK, diff)
}

// applyLayoutHandler handles POST /admin/layout/apply.
func (s *Server) applyLayoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	if err := s.drv.ApplyLayout(r.Context()); err != nil {
		writeDriverError(w, "ApplyLayout", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// revertLayoutHandler handles POST /admin/layout/revert.
func (s *Server) revertLayoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	if err := s.drv.RevertLayout(r.Context()); err != nil {
		writeDriverError(w, "RevertLayout", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
