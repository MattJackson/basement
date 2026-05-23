package api

import (
	"net/http"
)

// OnboardingState is the v1.11.0a wire shape consumed by the
// frontend AppShell admin-entry effect. needsOnboarding=true tells
// the FE to auto-route to /admin/first-run on the next /admin/* view;
// completed=true is the latch the operator (or the upgrade migration
// in store.OrgCapabilities.load) sets so the auto-route never fires
// again. Manual access to /admin/first-run remains available
// regardless of state — the route renders the same wizard so an
// operator who dismissed early can come back and finish.
type OnboardingState struct {
	NeedsOnboarding bool `json:"needsOnboarding"`
	Completed       bool `json:"completed"`
}

// getOnboardingStateHandler handles GET /api/v1/admin/onboarding/state.
//
// needsOnboarding is computed at request time:
//   - 0 clusters configured (s.conns.Count == 0) AND
//   - 0 users beyond the env-seeded admin (len(s.store.Users()) == 0)
//
// We don't gate on Completed when computing needsOnboarding — the FE
// gates on `needsOnboarding && !completed` for the auto-redirect, so
// the server can keep returning the live signal even after dismiss.
// That lets the wizard surface a "still no clusters" hint if the
// operator returns to /admin/first-run manually.
func (s *Server) getOnboardingStateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	// Cluster count. s.conns can be nil in older test harnesses that
	// don't wire the connections store; treat nil as "0 clusters" so
	// the handler stays useful for FE smoke-tests without forcing
	// every test to thread a real store through.
	clusterCount := 0
	if s.conns != nil {
		n, err := s.conns.Count(r.Context())
		if err != nil {
			writeErrorSimple(w, http.StatusInternalServerError, "STORE_ERROR", "Failed to count clusters")
			return
		}
		clusterCount = n
	}

	// User count beyond the env-seeded admin. The admin is synthesised
	// from cfg.Admin.User / cfg.Admin.Hash and does NOT appear in
	// s.store.Users() (see listAllUsersHandler), so users.json being
	// empty really does mean "no team members invited yet".
	userCount := 0
	if s.store != nil {
		userCount = len(s.store.Users())
	}

	caps := s.store.OrgCapabilities().Get()

	resp := OnboardingState{
		NeedsOnboarding: clusterCount == 0 && userCount == 0,
		Completed:       caps.OnboardingCompleted,
	}

	writeJSON(w, http.StatusOK, resp)
}

// dismissOnboardingHandler handles POST /api/v1/admin/onboarding/dismiss.
// Idempotent latch: sets OrgCapabilities.OnboardingCompleted=true. The
// FE calls this from the wizard's Done step AND from the "I'll set up
// later" Skip-everything path so both routes converge on the same
// "never auto-show again" state.
func (s *Server) dismissOnboardingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}

	if _, ok := s.requireCapability(w, r, "host:manage_org_caps", "host:*"); !ok {
		return
	}

	if err := s.store.OrgCapabilities().MarkOnboardingCompleted(); err != nil {
		s.auditFailure(r, "host:onboarding_dismissed", resourceHost, err)
		writeErrorSimple(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to persist onboarding state")
		return
	}

	s.auditSuccess(r, "host:onboarding_dismissed", resourceHost)
	writeJSON(w, http.StatusOK, OnboardingState{
		NeedsOnboarding: false,
		Completed:       true,
	})
}
