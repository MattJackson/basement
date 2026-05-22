// Package api — block-scrub maintenance handlers (v1.4.0c).
//
// Two endpoints under the admin group:
//
//   GET  /api/v1/admin/clusters/{cid}/scrub  → scrubGetResponse
//   POST /api/v1/admin/clusters/{cid}/scrub  → kick off scrub
//
// The GET response folds the driver's ScrubCapability into the payload
// alongside ScrubState so the UI can decide "render the Run button or
// the explanation card" in a single round-trip — same pattern lifecycle
// uses (v0.9.0i).
//
// Capability gate: cluster:edit at "cluster:{cid}". Scrub is a topology-
// touching maintenance op; the gate matches layout apply/revert (also
// cluster:edit) so an operator with edit on a cluster can run scrub on
// it. We don't mint a new cluster:scrub capability — the matrix is
// already busy, and the operator-mention test (cycle prompt) explicitly
// allows host:manage_drivers OR cluster:edit.
package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/mattjackson/basement/internal/driver"
)

// scrubGetResponse is the GET shape. capabilities + state arrive
// together so the UI never has to choose between "render disabled"
// and "render real state" — both come from one fetch.
type scrubGetResponse struct {
	Capabilities driver.ScrubCapability `json:"capabilities"`
	State        driver.ScrubState      `json:"state"`
}

// getClusterScrubHandler handles GET /api/v1/admin/clusters/{cid}/scrub.
func (s *Server) getClusterScrubHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	cid := chi.URLParam(r, "cid")
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id required")
		return
	}

	if _, ok := s.requireCapability(w, r, "cluster:edit", scopeCluster(cid)); !ok {
		return
	}

	drv, err := s.reg.For(r.Context(), cid)
	if err != nil {
		writeRegistryForError(w, err)
		return
	}

	caps := drv.ScrubSupport()
	resp := scrubGetResponse{Capabilities: caps}

	if !caps.Supported {
		// Don't probe state on drivers that don't claim scrub — they
		// all return ErrUnsupported and the UI gates on caps.Supported
		// anyway. 200 with empty state keeps the screen rendering.
		writeJSON(w, http.StatusOK, resp)
		return
	}

	state, serr := drv.ScrubState(r.Context())
	if serr != nil {
		var de *driver.Error
		if errors.As(serr, &de) && errors.Is(serr, driver.ErrUnsupported) {
			// Supported flag flipped between the capability call and the
			// state call — possible during a hot driver swap. Render the
			// empty-state branch rather than blanking the screen.
			writeJSON(w, http.StatusOK, resp)
			return
		}
		writeDriverError(w, "ScrubState", serr)
		return
	}
	resp.State = state
	writeJSON(w, http.StatusOK, resp)
}

// postClusterScrubHandler handles POST /api/v1/admin/clusters/{cid}/scrub.
// Kicks off a scrub; returns 200 with the current ScrubState so the UI
// gets immediate feedback (Running=true).
func (s *Server) postClusterScrubHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	cid := chi.URLParam(r, "cid")
	if cid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID", "cluster id required")
		return
	}

	if _, ok := s.requireCapability(w, r, "cluster:edit", scopeCluster(cid)); !ok {
		return
	}

	drv, err := s.reg.For(r.Context(), cid)
	if err != nil {
		writeRegistryForError(w, err)
		return
	}

	caps := drv.ScrubSupport()
	if !caps.Supported {
		writeError(w, http.StatusConflict, "SCRUB_UNSUPPORTED",
			"This driver does not support block scrub.", nil)
		return
	}

	if err := drv.StartScrub(r.Context()); err != nil {
		writeDriverError(w, "StartScrub", err)
		return
	}

	s.auditSuccess(r, "cluster:scrub", resourceCluster(cid))

	// Round-trip: GET current state so the response carries the live
	// Running flag. If the state read fails we still return success
	// with the capability + a synthetic Running=true — the kick landed.
	state, serr := drv.ScrubState(r.Context())
	if serr != nil {
		state = driver.ScrubState{Running: true, Message: "scrub started"}
	}
	writeJSON(w, http.StatusOK, scrubGetResponse{
		Capabilities: caps,
		State:        state,
	})
}
