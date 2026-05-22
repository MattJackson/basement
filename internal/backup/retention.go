package backup

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// SnapshotTimestampLayout is the on-disk timestamp format for snapshot
// prefixes. Sortable lexicographically (year-first, zero-padded) so a
// plain ListObjects + alphabetical sort orders snapshots chronologically.
// UTC is implied — the runner always formats now().UTC() and parses
// back through time.Parse which carries the timezone in the layout.
const SnapshotTimestampLayout = "2006-01-02_15:04:05"

// SnapshotTimestampPattern matches a 19-character timestamp like
// "2026-05-21_03:00:00". Used by listSnapshotTimestamps to filter out
// any unrelated keys an operator may have dropped alongside the
// backup-managed snapshots (we never want retention to delete those).
var SnapshotTimestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}_\d{2}:\d{2}:\d{2}$`)

// SlugifyName converts an operator-supplied backup Name into a
// path-safe slug for the snapshot prefix. Rule: lowercase ASCII
// letters, digits and dashes; everything else becomes a dash;
// repeated dashes collapse; leading/trailing dashes are trimmed.
//
// Empty / all-non-ASCII input falls back to "backup" so we never
// write to {DstBucket}//{ts}/ (an empty first segment).
func SlugifyName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	prevDash := true // suppress leading dashes
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		return "backup"
	}
	return out
}

// SnapshotPrefix returns the destination-bucket-relative prefix
// (no leading slash, trailing slash included) for a snapshot run.
// Centralised so the runner, the lister, and the test suite all
// agree on the on-disk layout.
//
// Shape: {slug(Name)}/{YYYY-MM-DD_HH:MM:SS}/
//
// Note: this is RELATIVE to DstBucket and ignores any DstPrefix the
// operator may have configured on the Backup record — snapshot mode
// owns the destination layout end-to-end, so layering a DstPrefix on
// top would only confuse the retention scan. The wizard hides the
// DstPrefix field when Mode=snapshot for the same reason.
func SnapshotPrefix(name string, t time.Time) string {
	return fmt.Sprintf("%s/%s/", SlugifyName(name), t.UTC().Format(SnapshotTimestampLayout))
}

// SnapshotRoot returns the per-backup root prefix that contains
// every snapshot for a given Name — i.e. SnapshotPrefix without the
// timestamp segment. Used by the retention lister to enumerate
// CommonPrefixes under one place.
func SnapshotRoot(name string) string {
	return SlugifyName(name) + "/"
}

// ParseSnapshotTimestamp inverts the on-disk layout for a single
// CommonPrefix entry like "2026-05-21_03:00:00/" or
// "{slug}/2026-05-21_03:00:00/". Returns ok=false for anything that
// doesn't match the expected pattern so the retention scan can
// ignore stray keys an operator may have dropped alongside the
// backup-managed snapshots.
func ParseSnapshotTimestamp(s string) (time.Time, bool) {
	s = strings.TrimSuffix(s, "/")
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		s = s[idx+1:]
	}
	if !SnapshotTimestampPattern.MatchString(s) {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(SnapshotTimestampLayout, s, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// PlanPrune computes the keep/prune split for a set of snapshot
// timestamps under a RetentionPolicy. Pure function: no IO, no
// side-effects. `now` is the reference clock for "today" / "this
// week" / "this month" — pass time.Now().UTC() in production and a
// fixed time in tests.
//
// Algorithm (Grandfather-Father-Son rotation):
//
//   - Daily bucket: walk back KeepDaily calendar days from `now`
//     (today, yesterday, ..., now-KeepDaily+1). For each day that has
//     at least one snapshot, keep the latest snapshot from that day.
//   - Weekly bucket: walk back KeepWeekly ISO-weeks from `now`. For
//     each week with snapshots, keep the latest.
//   - Monthly bucket: walk back KeepMonthly calendar months from `now`.
//     For each month with snapshots, keep the latest.
//   - The KEEP set is the UNION of the three buckets — a single
//     snapshot can satisfy all three simultaneously (e.g. the most
//     recent one usually does).
//   - The PRUNE set is everything not kept. Snapshots older than the
//     widest window get pruned; "future" snapshots (timestamp >
//     `now`) are always kept defensively so a clock-skew on the
//     server can't accidentally delete fresh data.
//
// Returned slices are sorted chronologically (oldest first) for
// log-friendliness. Both slices are non-nil; callers can len() them
// without a guard.
func PlanPrune(snapshots []time.Time, policy RetentionPolicy, now time.Time) (keep, prune []time.Time) {
	keep = []time.Time{}
	prune = []time.Time{}

	if len(snapshots) == 0 {
		return keep, prune
	}

	// Index snapshots by daily / weekly / monthly period key, mapping
	// each period to the LATEST snapshot timestamp it contains. We
	// then walk back the configured window from `now` and look each
	// candidate key up.
	type periodIndex map[string]time.Time
	latestPerDay := periodIndex{}
	latestPerWeek := periodIndex{}
	latestPerMonth := periodIndex{}

	for _, ts := range snapshots {
		tsU := ts.UTC()
		dayKey := tsU.Format("2006-01-02")
		if cur, ok := latestPerDay[dayKey]; !ok || tsU.After(cur) {
			latestPerDay[dayKey] = ts
		}
		isoYear, isoWeek := tsU.ISOWeek()
		weekKey := fmt.Sprintf("%04d-W%02d", isoYear, isoWeek)
		if cur, ok := latestPerWeek[weekKey]; !ok || tsU.After(cur) {
			latestPerWeek[weekKey] = ts
		}
		monthKey := tsU.Format("2006-01")
		if cur, ok := latestPerMonth[monthKey]; !ok || tsU.After(cur) {
			latestPerMonth[monthKey] = ts
		}
	}

	keptSet := map[int64]bool{}

	// Daily: today, today-1d, ..., today-(KeepDaily-1)d.
	nowU := now.UTC()
	today := time.Date(nowU.Year(), nowU.Month(), nowU.Day(), 0, 0, 0, 0, time.UTC)
	for i := 0; i < policy.KeepDaily; i++ {
		d := today.AddDate(0, 0, -i)
		key := d.Format("2006-01-02")
		if ts, ok := latestPerDay[key]; ok {
			keptSet[ts.UnixNano()] = true
		}
	}

	// Weekly: this ISO-week, last week, ..., back KeepWeekly-1.
	// time.AddDate(0,0,-7) preserves ISO-week math (no weird DST since UTC).
	for i := 0; i < policy.KeepWeekly; i++ {
		d := today.AddDate(0, 0, -7*i)
		y, w := d.ISOWeek()
		key := fmt.Sprintf("%04d-W%02d", y, w)
		if ts, ok := latestPerWeek[key]; ok {
			keptSet[ts.UnixNano()] = true
		}
	}

	// Monthly: this month, last month, ..., back KeepMonthly-1.
	thisMonth := time.Date(nowU.Year(), nowU.Month(), 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < policy.KeepMonthly; i++ {
		d := thisMonth.AddDate(0, -i, 0)
		key := d.Format("2006-01")
		if ts, ok := latestPerMonth[key]; ok {
			keptSet[ts.UnixNano()] = true
		}
	}

	// Future snapshots (clock skew) are always kept defensively —
	// we'd rather leave an orphaned snapshot on disk than delete data
	// the operator just wrote.
	for _, ts := range snapshots {
		if ts.After(nowU) {
			keptSet[ts.UnixNano()] = true
		}
	}

	// Build the returned slices in chronological order, deduplicating
	// against keptSet by UnixNano.
	sorted := make([]time.Time, len(snapshots))
	copy(sorted, snapshots)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })

	seen := map[int64]bool{}
	for _, ts := range sorted {
		k := ts.UnixNano()
		if seen[k] {
			continue
		}
		seen[k] = true
		if keptSet[k] {
			keep = append(keep, ts)
		} else {
			prune = append(prune, ts)
		}
	}
	return keep, prune
}
