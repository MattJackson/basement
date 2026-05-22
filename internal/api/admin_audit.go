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

// auditResponse is the wire shape for GET /api/v1/admin/audit.
//
// v1.4.0a: pagination. `total` is now the FULL count of matches over
// the filter window (across all pages), and `offset` + `limit` echo
// the page the caller saw. `truncated` stays for one release as a
// deprecated hint so older FE builds don't break — it now mirrors
// (offset + len(events) < total). Newer FE renders the page footer
// + Prev/Next purely from total + offset + limit.
type auditResponse struct {
	Events    []audit.Event `json:"events"`
	Total     int           `json:"total"`
	Offset    int           `json:"offset"`
	Limit     int           `json:"limit"`
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
//   - limit     int, default 50 (v1.4.0a), max 1000.
//   - offset    int, default 0 (v1.4.0a). Rows skipped from the
//                 newest-first ordering before the page is sliced.
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
	if offStr := q.Get("offset"); offStr != "" {
		if n, err := strconv.Atoi(offStr); err == nil && n >= 0 {
			filter.Offset = n
		}
	}
	// v1.4.0a: handler-level default of 50, overriding the audit
	// package's 200. Long unfiltered scrolls were the operator pain
	// point flagged in the v1.3.0f cycle report; the new page size
	// + Prev/Next chrome replaces them.
	if filter.Limit <= 0 {
		filter.Limit = 50
	}

	events, total, err := s.audit.QueryWithTotal(from, to, filter)
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "AUDIT_QUERY_FAILED", err.Error())
		return
	}
	if events == nil {
		events = []audit.Event{}
	}

	// Effective limit echoed to the FE so the page-count math is
	// stable when the caller sent no limit (or one over the cap).
	// v1.4.0a: default flipped from 200 to 50 — long unfiltered
	// scrolls were a known UX wart per the v1.3.0f cycle report.
	effectiveLimit := filter.Limit
	if effectiveLimit <= 0 {
		effectiveLimit = 50
	}
	if effectiveLimit > 1000 {
		effectiveLimit = 1000
	}
	effectiveOffset := filter.Offset
	if effectiveOffset < 0 {
		effectiveOffset = 0
	}
	// Deprecated `truncated` hint: there are more rows past this
	// page's window. The new FE drives Prev/Next from total alone;
	// the field stays one release for in-flight clients.
	truncated := effectiveOffset+len(events) < total

	writeJSON(w, http.StatusOK, auditResponse{
		Events:    events,
		Total:     total,
		Offset:    effectiveOffset,
		Limit:     effectiveLimit,
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
