// Package api: /admin/usage/series — per-bucket time-series read
// from the metrics recorder (v1.0.0d).
//
// The companion to /admin/usage/overview (v0.9.0k). Where overview
// is a CURRENT snapshot aggregated on demand, series returns
// historical samples persisted by the hourly snapshot scheduler in
// cmd/basement-server/main.go.
//
// Wire shape kept deliberately small so the frontend can render an
// inline SVG sparkline without further transformation:
//
//   {
//     "snapshots":  [ { "time": iso, "bytes": n, "objects": n }, ... ],
//     "bucketAlias": "photos",
//     "range":       "7d"
//   }
//
// Default range is 7 days; max is 90 days. Range parameters are
// validated and clamped server-side so a malformed URL can't trigger
// a multi-year file scan.
package api

import (
	"net/http"
	"time"

	"github.com/mattjackson/basement/internal/metrics"
)

// usageSeriesPoint is one chart point. Time is RFC3339 (UTC).
type usageSeriesPoint struct {
	Time    time.Time `json:"time"`
	Bytes   int64     `json:"bytes"`
	Objects int64     `json:"objects"`
}

// usageSeriesResponse is the wire shape returned by GET
// /admin/usage/series.
type usageSeriesResponse struct {
	Snapshots   []usageSeriesPoint `json:"snapshots"`
	BucketAlias string             `json:"bucketAlias,omitempty"`
	Range       string             `json:"range"`
}

// usageSeriesMaxRange caps how far back a series query may look.
// 90 days at hourly cadence is 2160 points — already past what a
// sparkline can render legibly; this is a guard against operator
// typos in the URL.
const usageSeriesMaxRange = 90 * 24 * time.Hour

// usageSeriesDefaultRange is the window when from/to are absent.
const usageSeriesDefaultRange = 7 * 24 * time.Hour

// getUsageSeriesHandler handles GET /api/v1/admin/usage/series.
//
// Query params:
//   - cid   connection ID (required).
//   - bid   bucket ID (required).
//   - from  RFC3339; defaults to to-7d.
//   - to    RFC3339; defaults to now.
//
// Gated on host:manage_users at host:* — same coarse Host Admin gate
// the overview handler uses. There's no per-bucket "view metrics"
// capability yet; reusing the existing one keeps the matrix small.
func (s *Server) getUsageSeriesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErrorSimple(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "GET required")
		return
	}

	if _, ok := s.requireCapability(w, r, "host:manage_users", "host:*"); !ok {
		return
	}

	q := r.URL.Query()
	cid := q.Get("cid")
	bid := q.Get("bid")
	if cid == "" || bid == "" {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_REQUEST",
			"cid and bid query parameters are required")
		return
	}

	now := time.Now().UTC()
	from, fromErr := parseUsageSeriesTime(q.Get("from"))
	if fromErr != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_FROM",
			"`from` must be RFC3339 (e.g. 2026-05-21T00:00:00Z)")
		return
	}
	to, toErr := parseUsageSeriesTime(q.Get("to"))
	if toErr != nil {
		writeErrorSimple(w, http.StatusBadRequest, "INVALID_TO",
			"`to` must be RFC3339 (e.g. 2026-05-21T23:59:59Z)")
		return
	}

	if to.IsZero() {
		to = now
	}
	if from.IsZero() {
		from = to.Add(-usageSeriesDefaultRange)
	}

	// Range clamping. If the caller asks for more than the max,
	// shrink `from` toward `to`. Doing it this way (rather than
	// returning 400) keeps the chart useful when a stale bookmark
	// asks for a year of data.
	if to.Sub(from) > usageSeriesMaxRange {
		from = to.Add(-usageSeriesMaxRange)
	}

	if s.metrics == nil {
		// Should be impossible — New() installs a no-op default —
		// but defensive in case future refactors break the wiring.
		writeJSON(w, http.StatusOK, usageSeriesResponse{
			Snapshots: []usageSeriesPoint{},
			Range:     formatRange(to.Sub(from)),
		})
		return
	}

	snaps, err := s.metrics.Query(from, to, metrics.Filter{
		ConnectionID: cid,
		BucketID:     bid,
	})
	if err != nil {
		writeErrorSimple(w, http.StatusInternalServerError, "METRICS_QUERY_FAILED", err.Error())
		return
	}

	// Shape down to the chart-friendly subset. The Snapshot wire
	// type from the metrics package carries the connection/bucket
	// IDs on every entry; those are already in the URL, so we
	// strip them out of the response payload to keep it tight.
	out := make([]usageSeriesPoint, 0, len(snaps))
	alias := ""
	for _, s := range snaps {
		out = append(out, usageSeriesPoint{
			Time:    s.Time,
			Bytes:   s.Bytes,
			Objects: s.Objects,
		})
		// First non-empty alias wins; alias doesn't change across
		// a single bucket's lifetime but on rename the most recent
		// label is what the operator wants displayed.
		if s.BucketAlias != "" {
			alias = s.BucketAlias
		}
	}

	writeJSON(w, http.StatusOK, usageSeriesResponse{
		Snapshots:   out,
		BucketAlias: alias,
		Range:       formatRange(to.Sub(from)),
	})
}

// parseUsageSeriesTime is the strict RFC3339-only parser used by the
// series handler. Date-only ("YYYY-MM-DD") is NOT accepted here —
// the UI always emits a full timestamp for series queries (unlike
// the audit page's date-picker which goes day-at-a-time).
func parseUsageSeriesTime(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

// formatRange returns the duration as a short label for the
// response envelope (e.g. "7d", "24h"). Used by the UI to title
// the chart without re-parsing the timestamps.
func formatRange(d time.Duration) string {
	if d <= 0 {
		return "0d"
	}
	days := int(d.Hours()) / 24
	if days >= 1 {
		return formatInt(days) + "d"
	}
	hours := int(d.Hours())
	if hours >= 1 {
		return formatInt(hours) + "h"
	}
	mins := int(d.Minutes())
	return formatInt(mins) + "m"
}

// formatInt is a tiny inline int-to-string helper that keeps the
// admin_usage_series.go file free of the strconv import (and free
// of fmt — which would pull in reflect for a one-liner). Sized for
// the small positive integers this handler emits.
func formatInt(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
