package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/mattjackson/basement/internal/driver"
)

// resolveClusterDriver pulls cid from chi, resolves the registry, and
// either returns a per-cluster driver or writes the appropriate error
// response (400 INVALID / 404 CLUSTER_NOT_FOUND) and returns nil.
func (s *Server) resolveClusterDriver(w http.ResponseWriter, r *http.Request) driver.Driver {
	cid := chi.URLParam(r, "cid")
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id required")
		return nil
	}
	drv, err := s.reg.For(r.Context(), cid)
	if err != nil {
		writeRegistryForError(w, err)
		return nil
	}
	return drv
}

// listNodesHandler handles GET /api/v1/admin/clusters/{cid}/nodes.
func (s *Server) listNodesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}
	drv := s.resolveClusterDriver(w, r)
	if drv == nil {
		return
	}
	nodes, err := drv.ListNodes(r.Context())
	if err != nil {
		writeDriverError(w, "ListNodes", err)
		return
	}
	if nodes == nil {
		nodes = []driver.Node{}
	}
	writeJSON(w, http.StatusOK, nodes)
}

// getLayoutHandler handles GET /api/v1/admin/clusters/{cid}/layout.
func (s *Server) getLayoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}
	drv := s.resolveClusterDriver(w, r)
	if drv == nil {
		return
	}
	layout, err := drv.GetLayout(r.Context())
	if err != nil {
		writeDriverError(w, "GetLayout", err)
		return
	}
	writeJSON(w, http.StatusOK, layout)
}

// stageLayoutHandler handles POST /admin/clusters/{cid}/layout/stage.
func (s *Server) stageLayoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}
	drv := s.resolveClusterDriver(w, r)
	if drv == nil {
		return
	}
	var change driver.LayoutChange
	if err := json.NewDecoder(r.Body).Decode(&change); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID", "invalid request body", nil)
		return
	}
	diff, err := drv.StageLayout(r.Context(), change)
	if err != nil {
		writeDriverError(w, "StageLayout", err)
		return
	}
	writeJSON(w, http.StatusOK, diff)
}

// applyLayoutHandler handles POST /admin/clusters/{cid}/layout/apply.
func (s *Server) applyLayoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}
	drv := s.resolveClusterDriver(w, r)
	if drv == nil {
		return
	}
	if err := drv.ApplyLayout(r.Context()); err != nil {
		writeDriverError(w, "ApplyLayout", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// revertLayoutHandler handles POST /admin/clusters/{cid}/layout/revert.
func (s *Server) revertLayoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}
	drv := s.resolveClusterDriver(w, r)
	if drv == nil {
		return
	}
	if err := drv.RevertLayout(r.Context()); err != nil {
		writeDriverError(w, "RevertLayout", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
