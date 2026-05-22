// Package garage: block-scrub admin support (v1.4.0c).
//
// Garage v2 exposes the same worker introspection + on-demand kick
// endpoints as v1, just under the v2 admin path. The wire shapes are
// near-identical so we keep the implementation deliberately close to
// the v1 sibling — driver-parity doctrine means the FE must see the
// same ScrubState shape no matter which Garage generation answered.
//
// Garage v2 path naming:
//   GET  /v2/GetWorkers          — background worker status
//   POST /v2/LaunchRepairOperation { "scrub_blocks": true }
//
// Some v2 builds also expose POST /v2/LaunchScrubBlocks as a thin alias.
// We use LaunchRepairOperation since it's the canonical name in the
// upstream OpenAPI spec.

package garage

import (
	"context"
	"strings"
	"time"

	driverpkg "github.com/mattjackson/basement/internal/driver"
)

// ScrubSupport advertises this driver as scrub-capable. Garage v2
// owns its block store the same way v1 does; the maintenance UI lights
// up regardless of which Garage generation the operator picked.
func (d *driver) ScrubSupport() driverpkg.ScrubCapability {
	return driverpkg.ScrubCapability{Supported: true}
}

// scrubWorkerV2 mirrors a single entry in Garage v2's worker list. The
// upstream payload is wider; we decode only the fields the UI uses.
type scrubWorkerV2 struct {
	Name          string `json:"name"`
	State         string `json:"state"`
	Progress      string `json:"progress,omitempty"`
	Message       string `json:"message,omitempty"`
	LastSeen      string `json:"lastSeen,omitempty"`
	BlocksScanned int64  `json:"blocksScanned,omitempty"`
	BlocksCorrupt int64  `json:"blocksCorrupt,omitempty"`
}

// launchRepairBodyV2 is the body for POST /v2/LaunchRepairOperation.
// v2 packs every repair variant into one endpoint behind a boolean
// switch; we only set scrub_blocks for the scrub case.
type launchRepairBodyV2 struct {
	ScrubBlocks bool `json:"scrub_blocks"`
}

// ScrubState fetches the current scrub worker status from
// GET /v2/GetWorkers. Same semantics as the v1 sibling: missing worker
// entry maps to "never run" rather than an error.
func (d *driver) ScrubState(ctx context.Context) (driverpkg.ScrubState, error) {
	var workers []scrubWorkerV2
	if err := d.client.do(ctx, "GET", "/v2/GetWorkers", nil, &workers); err != nil {
		if isNotFoundV2(err) {
			return driverpkg.ScrubState{Message: "scrub status unavailable on this Garage build"}, nil
		}
		return driverpkg.ScrubState{}, err
	}

	for _, w := range workers {
		if !isScrubWorkerV2(w.Name) {
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
		if w.Progress != "" {
			if num, den, ok := parseProgressFractionV2(w.Progress); ok && den > 0 {
				st.ProgressPercent = int((num * 100) / den)
				if st.ProgressPercent > 100 {
					st.ProgressPercent = 100
				}
			}
		}
		if !st.Running && st.ProgressPercent == 0 && st.BlocksScanned > 0 {
			st.ProgressPercent = 100
		}
		return st, nil
	}

	return driverpkg.ScrubState{}, nil
}

// StartScrub kicks off a block scrub via POST /v2/LaunchRepairOperation
// with {scrub_blocks: true}.
func (d *driver) StartScrub(ctx context.Context) error {
	body := launchRepairBodyV2{ScrubBlocks: true}
	return d.client.do(ctx, "POST", "/v2/LaunchRepairOperation", body, nil)
}

func isScrubWorkerV2(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "scrub")
}

func parseProgressFractionV2(s string) (int64, int64, bool) {
	slash := strings.Index(s, "/")
	if slash <= 0 {
		return 0, 0, false
	}
	num, ok1 := parseInt64V2(strings.TrimSpace(s[:slash]))
	den, ok2 := parseInt64V2(strings.TrimSpace(s[slash+1:]))
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	return num, den, true
}

func parseInt64V2(s string) (int64, bool) {
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

func isNotFoundV2(err error) bool {
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
