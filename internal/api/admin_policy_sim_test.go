// Package api: tests for the POLICY.SIM what-if simulator
// (ADR-0001 cycle v0.9.0j).
//
// Coverage focuses on the four decision paths the UI surfaces to
// operators:
//
//   1. Allow — calling user holds an assignment that grants the
//      capability at a scope covering the request.
//   2. Deny — user has no assignments at all.
//   3. Deny — user has assignments but none cover the requested scope.
//   4. Deny — scope matches but no role grants the capability.
//
// Plus the gate + validation surface (403 without policy:view_matrix,
// 400 on missing fields, 405 on GET).
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mattjackson/basement/internal/auth/policy"
)

// postSim is a tiny helper for the JSON-POST + admin-cookie pattern
// used by every test in this file. Centralised so adding a new test
// doesn't drift from the existing ones.
func postSim(t *testing.T, srv *Server, body simulateRequest) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := adminPolicyReq(http.MethodPost, "/api/v1/admin/policies/simulate", raw)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)
	return rr
}

func decodeSimResponse(t *testing.T, rr *httptest.ResponseRecorder) simulateResponse {
	t.Helper()
	var resp simulateResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, rr.Body.String())
	}
	return resp
}

func TestSimulatePolicy_Allow(t *testing.T) {
	srv, enf, cleanup := newPolicyTestEnv(t, true)
	defer cleanup()

	// Set up the operator's example: matthew with host_admin@host:*
	// should be able to do host:manage_policies on host:*.
	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "matthew", RoleID: "host_admin", Scope: "host:*",
	}); err != nil {
		t.Fatalf("seed matthew assignment: %v", err)
	}

	rr := postSim(t, srv, simulateRequest{
		UserID:     "matthew",
		Capability: "host:manage_policies",
		Scope:      "host:*",
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}

	resp := decodeSimResponse(t, rr)
	if !resp.Allowed {
		t.Errorf("expected allowed=true, got false (reasoning=%+v)", resp.Reasoning)
	}
	if len(resp.MatchingAssignments) == 0 {
		t.Errorf("expected at least one matching assignment, got 0")
	} else {
		got := resp.MatchingAssignments[0]
		if got.UserID != "matthew" || got.RoleID != "host_admin" || got.Scope != "host:*" {
			t.Errorf("matching assignment = %+v, want matthew/host_admin/host:*", got)
		}
	}
	if len(resp.Reasoning) == 0 {
		t.Errorf("expected reasoning steps, got 0")
	}
}

func TestSimulatePolicy_DenyNoAssignments(t *testing.T) {
	// The operator's worked example: matthew asks "can wife do
	// objects:list on bucket:abc:family-photos?" — wife has zero
	// assignments yet.
	srv, _, cleanup := newPolicyTestEnv(t, true)
	defer cleanup()

	rr := postSim(t, srv, simulateRequest{
		UserID:     "wife",
		Capability: "objects:list",
		Scope:      "bucket:abc:family-photos",
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	resp := decodeSimResponse(t, rr)
	if resp.Allowed {
		t.Errorf("expected allowed=false for user with no assignments, got true")
	}
	if len(resp.MatchingAssignments) != 0 {
		t.Errorf("expected zero matching assignments on deny, got %d", len(resp.MatchingAssignments))
	}
	// Reasoning should mention "no assignments" so the UI can render
	// a useful explanation rather than a bare false.
	joined := strings.ToLower(joinSteps(resp.Reasoning))
	if !strings.Contains(joined, "no assignment") && !strings.Contains(joined, "zero role assignments") {
		t.Errorf("expected reasoning to mention no assignments, got %q", joined)
	}
}

func TestSimulatePolicy_DenyScopeMismatch(t *testing.T) {
	srv, enf, cleanup := newPolicyTestEnv(t, true)
	defer cleanup()

	// wife has bucket_user but only on family-photos, not lsi.
	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "wife", RoleID: "bucket_user", Scope: "bucket:cid-x:family-photos",
	}); err != nil {
		t.Fatalf("seed wife assignment: %v", err)
	}

	rr := postSim(t, srv, simulateRequest{
		UserID:     "wife",
		Capability: "objects:list",
		Scope:      "bucket:cid-x:lsi",
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	resp := decodeSimResponse(t, rr)
	if resp.Allowed {
		t.Errorf("expected allowed=false for scope mismatch, got true")
	}
	joined := strings.ToLower(joinSteps(resp.Reasoning))
	if !strings.Contains(joined, "scope") {
		t.Errorf("expected reasoning to mention scope, got %q", joined)
	}
}

func TestSimulatePolicy_DenyCapabilityNotInRole(t *testing.T) {
	srv, enf, cleanup := newPolicyTestEnv(t, true)
	defer cleanup()

	// bucket_user includes objects:list but NOT bucket:delete; assigning
	// wife bucket_user @ bucket:cid-x:* should leave bucket:delete denied
	// at that scope.
	if err := enf.AssignRole(policy.RoleAssignment{
		UserID: "wife", RoleID: "bucket_user", Scope: "bucket:cid-x:photos",
	}); err != nil {
		t.Fatalf("seed wife assignment: %v", err)
	}

	rr := postSim(t, srv, simulateRequest{
		UserID:     "wife",
		Capability: "bucket:delete",
		Scope:      "bucket:cid-x:photos",
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	resp := decodeSimResponse(t, rr)
	if resp.Allowed {
		t.Errorf("expected allowed=false (bucket_user has no bucket:delete), got true")
	}
	joined := strings.ToLower(joinSteps(resp.Reasoning))
	if !strings.Contains(joined, "capability") {
		t.Errorf("expected reasoning to mention capability, got %q", joined)
	}
}

func TestSimulatePolicy_GateForbidsWithoutCapability(t *testing.T) {
	srv, _, cleanup := newPolicyTestEnv(t, false) // no host_admin grant
	defer cleanup()

	rr := postSim(t, srv, simulateRequest{
		UserID:     "matthew",
		Capability: "host:manage_users",
		Scope:      "host:*",
	})

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without policy:view_matrix, got %d (body=%s)", rr.Code, rr.Body.String())
	}
}

func TestSimulatePolicy_RejectsEmptyFields(t *testing.T) {
	srv, _, cleanup := newPolicyTestEnv(t, true)
	defer cleanup()

	cases := []simulateRequest{
		{UserID: "", Capability: "objects:list", Scope: "bucket:a:b"},
		{UserID: "matthew", Capability: "", Scope: "bucket:a:b"},
		{UserID: "matthew", Capability: "objects:list", Scope: ""},
		{UserID: "  ", Capability: "  ", Scope: "  "},
	}
	for i, c := range cases {
		rr := postSim(t, srv, c)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("case %d (%+v): expected 400, got %d (body=%s)", i, c, rr.Code, rr.Body.String())
		}
	}
}

func TestSimulatePolicy_RejectsNonPost(t *testing.T) {
	srv, _, cleanup := newPolicyTestEnv(t, true)
	defer cleanup()

	req := adminPolicyReq(http.MethodGet, "/api/v1/admin/policies/simulate", nil)
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	// Chi returns 405 for a registered POST hit with a different method.
	if rr.Code != http.StatusMethodNotAllowed && rr.Code != http.StatusNotFound {
		t.Errorf("expected 405 or 404 for GET on simulate, got %d (body=%s)", rr.Code, rr.Body.String())
	}
}

func TestSimulatePolicy_RejectsInvalidJSON(t *testing.T) {
	srv, _, cleanup := newPolicyTestEnv(t, true)
	defer cleanup()

	req := adminPolicyReq(http.MethodPost,
		"/api/v1/admin/policies/simulate",
		[]byte("{not-json"))
	// Re-set the body explicitly to make the malformed payload obvious
	// in test failures.
	req.Body = http.NoBody
	req.ContentLength = 0
	req = adminPolicyReqWithRawBody(req, []byte("{not-json"))
	rr := httptest.NewRecorder()
	srv.router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed JSON, got %d (body=%s)", rr.Code, rr.Body.String())
	}
}

// adminPolicyReqWithRawBody attaches a raw byte body to an existing
// admin-authenticated request — used by the malformed-JSON test to
// keep the cookie/auth setup centralised in adminPolicyReq while
// still letting tests inject deliberately-invalid bytes.
func adminPolicyReqWithRawBody(r *http.Request, body []byte) *http.Request {
	r.Body = http.NoBody
	r.ContentLength = int64(len(body))
	r.Body = readCloser{bytes.NewReader(body)}
	return r
}

type readCloser struct{ *bytes.Reader }

func (readCloser) Close() error { return nil }

// joinSteps flattens a reasoning slice to a single string for
// substring assertions. The simulator's contract is "reasoning steps
// explain the decision in operator-readable English" — tests verify
// the key phrases land somewhere in the trail, not the exact phrasing.
func joinSteps(steps []policy.ReasoningStep) string {
	parts := make([]string, 0, len(steps)*2)
	for _, s := range steps {
		parts = append(parts, s.Step, s.Detail)
	}
	return strings.Join(parts, " | ")
}
