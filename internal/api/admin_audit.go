// Package api: audit-log query handler (v1.0.0c).
//
// Mutating handlers across this package record events into s.audit
// (see audit_helpers.go); this handler is the read-side surface so
// /admin/audit can render the history. Gated on host:manage_policies
// — same persona that owns the role/permission matrix.
//
// The handler is intentionally thin: it parses the query string,
// delegates to audit.Logger.Query, and wraps the result in
// {events, total, truncated}. Filter semantics + limit caps live
// in the audit package so the wire shape and the unit-test shape
// stay aligned.

package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/mattjackson/basement/internal/audit"
)

// auditResponse is the wire shape for GET /api/v1/admin/audit. The
// `truncated` boolean signals "there were more matching events but
// the limit cut the result short" so the UI can render a "load more"
// affordance honestly. `total` mirrors len(events) (the audit logger
// doesn't pre-count without scanning).
type auditResponse struct {
	Events    []audit.Event `json:"events"`
	Total     int           `json:"total"`
	Truncated bool          `json:"truncated"`
}

// listAuditHandler handles GET /api/v1/admin/audit.
//
// Query params:
//   - from      ISO-8601 timestamp; defaults to 24h before `to`.
//   - to        ISO-8601 timestamp; defaults to now.
//   - actor     exact-match userID filter.
//   - action    substring filter, case-insensitive.
//   - resource  substring filter, case-insensitive.
//   - result    "success" | "failure" | "" (any).
//   - limit     int, default 200, max 1000.
//
// 503 AUDIT_NOT_WIRED if the logger hasn't been configured (matches
// the POLICY_NOT_WIRED pattern from policy_gates.go — surface
// misconfigured-boot loudly rather than silently returning [].)
func (s *Server) listAuditHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	if _, ok := s.requireCapability(w, r, "host:manage_policies", "host:*"); !ok {
		return
	}

	if s.audit == nil {
		writeErrorSimple(w, http.StatusServiceUnavailable, "AUDIT_NOT_WIRED",
			"Audit subsystem is not configured on this deployment.")
		return
	}

	q := r.URL.Query()

	// Parse `from` / `to`. Accept both RFC3339 (with timezone) and
	// the bare ISO date "YYYY-MM-DD" that the UI date-picker emits
	// — the UI is calendar-day driven; demanding a timezone there
	// is hostile.
	from, fromErr := parseAuditTime(q.Get("from"), false)
	if fromErr != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_FROM",
			"`from` must be RFC3339 (e.g. 2026-05-21T00:00:00Z) or YYYY-MM-DD")
		return
	}
	to, toErr := parseAuditTime(q.Get("to"), true)
	if toErr != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_TO",
			"`to` must be RFC3339 (e.g. 2026-05-21T23:59:59Z) or YYYY-MM-DD")
		return
	}

	filter := audit.QueryFilter{
		Actor:    q.Get("actor"),
		Action:   q.Get("action"),
		Resource: q.Get("resource"),
		Result:   q.Get("result"),
	}

	if limStr := q.Get("limit"); limStr != "" {
		if n, err := strconv.Atoi(limStr); err == nil && n > 0 {
			filter.Limit = n
		}
	}

	events, err := s.audit.Query(from, to, filter)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "AUDIT_QUERY_FAILED", err.Error())
		return
	}
	if events == nil {
		events = []audit.Event{}
	}

	// `truncated` is true when the caller's effective limit equals
	// the actual returned count — we cannot tell from here whether
	// there were MORE events; the calling UI uses this as a hint
	// to render "load more, narrow your window" copy.
	effectiveLimit := filter.Limit
	if effectiveLimit <= 0 {
		effectiveLimit = 200
	}
	if effectiveLimit > 1000 {
		effectiveLimit = 1000
	}
	truncated := len(events) >= effectiveLimit

	writeJSON(w, http.StatusOK, auditResponse{
		Events:    events,
		Total:     len(events),
		Truncated: truncated,
	})
}

// parseAuditTime accepts:
//
//   - "" (empty) — returns the zero time, which audit.Query
//     interprets as "use the default" (-24h for from, now for to).
//   - RFC3339 — full timestamps with timezone.
//   - YYYY-MM-DD — calendar date in UTC; treated as 00:00:00Z (start
//     of day) when isUpperBound is false, and 23:59:59.999999999Z
//     when true so an operator's "to=2026-05-21" picks up that
//     entire day.
func parseAuditTime(raw string, isUpperBound bool) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		if isUpperBound {
			return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, time.UTC), nil
		}
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	return time.Time{}, errInvalidTime
}

// errInvalidTime is the sentinel parse error returned by
// parseAuditTime. The handler maps to INVALID_FROM / INVALID_TO with
// a helpful message rather than the raw error.
var errInvalidTime = &invalidTimeErr{}

type invalidTimeErr struct{}

func (*invalidTimeErr) Error() string { return "invalid time format" }
