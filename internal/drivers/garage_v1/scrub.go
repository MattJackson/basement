// Package garage_v1: block-scrub admin support (v1.4.0c).
//
// Garage v1's admin API exposes scrub state under /v1/worker (the
// background-task introspection endpoint) and accepts on-demand kicks
// via /v1/worker/scrub-blocks. basement talks to those endpoints
// through the same admin client used elsewhere in this driver; the
// wire shapes are kept narrow because Garage's worker payload is large
// and only a handful of fields drive the operator UI.
//
// Driver-parity doctrine: the v2 sibling driver (internal/drivers/garage)
// ships an identical implementation. AWS S3 + MinIO advertise
// ScrubSupport{Supported: false} since neither backend exposes an
// operator-initiated durability scan.

package garage_v1

import (
	"context"
	"strings"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// ScrubSupport advertises this driver as scrub-capable. Garage v1
// exposes the scrub worker via its admin API; no operator config is
// needed beyond the standard admin token.
func (d *driver) ScrubSupport() driverpkg.ScrubCapability {
	return driverpkg.ScrubCapability{Supported: true}
}

// scrubWorkerResponseV1 mirrors a single entry in Garage v1's
// /v1/worker introspection list. Garage emits a free-form payload with
// many keys; we decode only the fields we surface in basement.
type scrubWorkerResponseV1 struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	Progress    string `json:"progress,omitempty"`
	Message     string `json:"message,omitempty"`
	Tranquility int    `json:"tranquility,omitempty"`
	LastSeen    string `json:"lastSeen,omitempty"`

	// Garage emits per-worker counters. The scrub worker carries
	// blocks_scanned + blocks_corrupt under "persistentErrors" /
	// "consecutiveErrors" depending on the variant; we accept both
	// shapes via separate fields and pick whichever populated.
	BlocksScanned int64 `json:"blocksScanned,omitempty"`
	BlocksCorrupt int64 `json:"blocksCorrupt,omitempty"`
}

// ScrubState fetches the current scrub worker status. Garage v1
// returns every background worker; we filter by name (the scrub worker
// is "block_scrub" in the v1.0.1 build). When no scrub has ever run,
// Garage omits the worker from the list — surface that as Running=false
// with no LastCompleted (the UI renders "never run" in that case).
func (d *driver) ScrubState(ctx context.Context) (driverpkg.ScrubState, error) {
	var workers []scrubWorkerResponseV1
	if err := d.client.do(ctx, "GET", "/v1/worker", nil, &workers); err != nil {
		// Some Garage builds gate the worker endpoint differently; on
		// 404 we surface an empty state so the UI doesn't blank — this
		// is the equivalent of "scrub has never been run on this node".
		if isNotFound(err) {
			return driverpkg.ScrubState{Message: "scrub status unavailable on this Garage build"}, nil
		}
		return driverpkg.ScrubState{}, err
	}

	for _, w := range workers {
		if !isScrubWorker(w.Name) {
			continue
		}
		st := driverpkg.ScrubState{
			Running:       strings.EqualFold(w.State, "busy") || strings.EqualFold(w.State, "running"),
			BlocksScanned: w.BlocksScanned,
			BlocksCorrupt: w.BlocksCorrupt,
			Message:       w.Message,
		}
		if w.LastSeen != "" {
			if t, perr := time.Parse(time.RFC3339, w.LastSeen); perr == nil && !st.Running {
				st.LastCompleted = t.UTC()
			}
		}
		// Garage emits progress as a fraction string "12345/56789"; if
		// we can divide the parts we surface the percent so the UI can
		// render a bar. Anything we can't parse stays at 0 — the UI
		// falls back to the spinner-only state.
		if w.Progress != "" {
			if num, den, ok := parseProgressFraction(w.Progress); ok && den > 0 {
				st.ProgressPercent = int((num * 100) / den)
				if st.ProgressPercent > 100 {
					st.ProgressPercent = 100
				}
			}
		}
		if !st.Running && st.ProgressPercent == 0 && st.BlocksScanned > 0 {
			// A finished scrub reports zero progress in the "ratio"
			// field — surface 100 so the bar renders complete.
			st.ProgressPercent = 100
		}
		return st, nil
	}

	// No scrub worker entry at all — Garage hasn't run one yet.
	return driverpkg.ScrubState{}, nil
}

// StartScrub kicks off a block scrub. Garage v1 exposes this via
// POST /v1/worker/scrub-blocks. The body is empty; Garage returns 204
// once the scrub task is enqueued (the actual scan runs async, observed
// via ScrubState).
func (d *driver) StartScrub(ctx context.Context) error {
	return d.client.do(ctx, "POST", "/v1/worker/scrub-blocks", nil, nil)
}

// isScrubWorker returns true if the Garage worker name corresponds to
// the block-scrub task. Garage has used "block_scrub" since v0.8 and
// "scrub" in earlier builds — we accept either spelling so a future
// build rename doesn't blank the UI.
func isScrubWorker(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "scrub")
}

// parseProgressFraction splits a "num/den" string into its parts. Used
// for the Garage progress strings ("12345/67890"). Returns ok=false on
// anything that doesn't match the simple shape.
func parseProgressFraction(s string) (int64, int64, bool) {
	slash := strings.Index(s, "/")
	if slash <= 0 {
		return 0, 0, false
	}
	num, ok1 := parseInt64(strings.TrimSpace(s[:slash]))
	den, ok2 := parseInt64(strings.TrimSpace(s[slash+1:]))
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	return num, den, true
}

// parseInt64 is a tiny dependency-free strconv shim — keeps the file
// free of the strconv import for one call site.
func parseInt64(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	return n, true
}

// isNotFound returns true if err is a driver.Error wrapping ErrNotFound.
// Used to soft-fail the scrub worker fetch on Garage builds that don't
// expose /v1/worker — the UI renders "unavailable" instead of erroring.
func isNotFound(err error) bool {
	type unwrapper interface{ Unwrap() error }
	for err != nil {
		if err == driverpkg.ErrNotFound {
			return true
		}
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
