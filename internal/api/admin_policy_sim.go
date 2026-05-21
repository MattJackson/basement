// Package api: /admin/policies/simulate — the POLICY.SIM what-if
// inspector (ADR-0001, cycle v0.9.0j).
//
// Operators staring at the role/capability matrix need a way to answer
// "given this user, can they do X on this scope, and why?". The
// simulator is pure analysis on the existing policy + assignment data —
// no enforcement-logic changes, no side effects, no audit trail. It's
// the matrix UI's debugger.
//
// The handler is a thin shim over Enforcer.CanWithReason, which records
// every step Can() takes so the UI can render an ordered explanation.
package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/mattjackson/basement/internal/auth/policy"
)

// simulateRequest is the POST body shape. Three required fields —
// userId, capability, scope — exactly matching the args to
// Enforcer.Can. Mismatch with the registry is intentionally NOT
// rejected here so the simulator can also be used to debug typos
// (the reasoning will say "no assignment grants foo:bar at …").
type simulateRequest struct {
	UserID     string `json:"userId"`
	Capability string `json:"capability"`
	Scope      string `json:"scope"`
}

// simulateResponse mirrors the brief's JSON shape exactly:
//
//	{
//	  "allowed": bool,
//	  "reasoning": [{step, detail}, ...],
//	  "matchingAssignments": [Assignment]
//	}
type simulateResponse struct {
	Allowed             bool                    `json:"allowed"`
	Reasoning           []policy.ReasoningStep  `json:"reasoning"`
	MatchingAssignments []policy.RoleAssignment `json:"matchingAssignments"`
}

// simulatePolicyHandler implements POST /api/v1/admin/policies/simulate.
// Gated on policy:view_matrix @ host:* — same gate as the matrix GET,
// because the simulator only exposes information the operator can
// already see by browsing the matrix manually.
func (s *Server) simulatePolicyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	if _, ok := s.requireCapability(w, r, "policy:view_matrix", "host:*"); !ok {
		return
	}

	if s.policy == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "POLICY_NOT_WIRED",
			"Policy subsystem is not configured on this deployment.")
		return
	}

	var req simulateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON body")
		return
	}

	req.UserID = strings.TrimSpace(req.UserID)
	req.Capability = strings.TrimSpace(req.Capability)
	req.Scope = strings.TrimSpace(req.Scope)
	if req.UserID == "" || req.Capability == "" || req.Scope == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			"userId, capability, and scope are all required")
		return
	}

	allowed, matches, reasoning := s.policy.CanWithReason(req.UserID, req.Capability, req.Scope)

	// Never return null arrays — the UI expects empty arrays so it can
	// .map() unconditionally.
	if reasoning == nil {
		reasoning = []policy.ReasoningStep{}
	}
	if matches == nil {
		matches = []policy.RoleAssignment{}
	}

	writeJSON(w, http.StatusOK, simulateResponse{
		Allowed:             allowed,
		Reasoning:           reasoning,
		MatchingAssignments: matches,
	})
}
