package backup

import (
	"strings"
	"testing"
	"time"
)

// mustParse is a tiny test helper so the table-driven cases below
// stay readable. Panics on malformed input — only used with literal
// strings in this file so a panic is a test bug, not a data bug.
func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	ts, ok := ParseSnapshotTimestamp(s)
	if !ok {
		t.Fatalf("ParseSnapshotTimestamp(%q) failed", s)
	}
	return ts
}

// TestSlugifyName covers the rules the retention layout depends on:
// lowercased, alphanumeric + dashes, collapsing, no leading/trailing
// dashes, empty -> "backup".
func TestSlugifyName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"lsi to cheshire weekly", "lsi-to-cheshire-weekly"},
		{"Photos!!!", "photos"},
		{"  spaced  ", "spaced"},
		{"two___under", "two-under"},
		{"slash/in/name", "slash-in-name"},
		{"---dashes---", "dashes"},
		{"", "backup"},
		{"   ", "backup"},
		{"!!@@##", "backup"},
		{"MiXeDcAsE", "mixedcase"},
		{"unicode-é-fallback", "unicode-fallback"},
	}
	for _, tc := range cases {
		got := SlugifyName(tc.in)
		if got != tc.want {
			t.Errorf("SlugifyName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSnapshotPrefix verifies the on-disk shape — one place encodes
// the layout the runner + lister + retention scan all assume.
func TestSnapshotPrefix(t *testing.T) {
	ts := time.Date(2026, 5, 21, 3, 0, 0, 0, time.UTC)
	got := SnapshotPrefix("LSI -> Cheshire", ts)
	want := "lsi-cheshire/2026-05-21_03:00:00/"
	if got != want {
		t.Fatalf("SnapshotPrefix = %q, want %q", got, want)
	}
}

// TestParseSnapshotTimestamp_RoundTrip ensures the layout/format pair
// round-trip cleanly; ParseSnapshotTimestamp is the inverse the
// retention scan relies on.
func TestParseSnapshotTimestamp_RoundTrip(t *testing.T) {
	ts := time.Date(2026, 5, 21, 3, 14, 15, 0, time.UTC)
	s := ts.Format(SnapshotTimestampLayout)
	got, ok := ParseSnapshotTimestamp(s)
	if !ok {
		t.Fatalf("ParseSnapshotTimestamp(%q) ok=false", s)
	}
	if !got.Equal(ts) {
		t.Fatalf("round-trip mismatch: got %s, want %s", got, ts)
	}
}

// TestParseSnapshotTimestamp_Variants covers the input shapes the
// scanner sees in practice: with/without trailing slash, with the
// {slug}/ prefix the S3 CommonPrefixes payload includes when the
// list query uses the per-backup root.
func TestParseSnapshotTimestamp_Variants(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want string
	}{
		{"2026-05-21_03:00:00", true, "2026-05-21T03:00:00Z"},
		{"2026-05-21_03:00:00/", true, "2026-05-21T03:00:00Z"},
		{"lsi-cheshire/2026-05-21_03:00:00/", true, "2026-05-21T03:00:00Z"},
		{"lsi-cheshire/2026-05-21_03:00:00", true, "2026-05-21T03:00:00Z"},
		{"some-other-key.json", false, ""},
		{"2026-13-99_99:99:99", false, ""}, // pattern matches, time.Parse rejects
		{"", false, ""},
	}
	for _, tc := range cases {
		got, ok := ParseSnapshotTimestamp(tc.in)
		if ok != tc.ok {
			t.Errorf("ParseSnapshotTimestamp(%q) ok=%v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		if got.Format(time.RFC3339) != tc.want {
			t.Errorf("ParseSnapshotTimestamp(%q) = %s, want %s", tc.in, got.Format(time.RFC3339), tc.want)
		}
	}
}

// TestPlanPrune_Empty: no snapshots in, no keep/prune out.
func TestPlanPrune_Empty(t *testing.T) {
	keep, prune := PlanPrune(nil, RetentionPolicy{KeepDaily: 7}, time.Now())
	if len(keep) != 0 || len(prune) != 0 {
		t.Fatalf("expected empty slices, got keep=%v prune=%v", keep, prune)
	}
}

// TestPlanPrune_AllInDailyWindow: every snapshot is within the
// KeepDaily window so nothing gets pruned. Exercises the daily
// bucket in isolation.
func TestPlanPrune_AllInDailyWindow(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	snaps := []time.Time{
		now.AddDate(0, 0, -1),
		now.AddDate(0, 0, -2),
		now.AddDate(0, 0, -3),
	}
	keep, prune := PlanPrune(snaps, RetentionPolicy{KeepDaily: 7}, now)
	if len(keep) != 3 || len(prune) != 0 {
		t.Fatalf("expected all kept, got keep=%d prune=%d", len(keep), len(prune))
	}
}

// TestPlanPrune_MultipleSnapshotsSameDay: a day with 3 snapshots
// keeps only the latest (when daily is the only matching bucket).
// The latest-of-day also happens to be latest-of-week and
// latest-of-month — but adding policy here only enables daily, so
// the older two are pruned outright.
func TestPlanPrune_MultipleSnapshotsSameDay(t *testing.T) {
	now := time.Date(2026, 5, 21, 23, 59, 59, 0, time.UTC)
	day := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	snaps := []time.Time{
		day.Add(1 * time.Hour),
		day.Add(7 * time.Hour),
		day.Add(15 * time.Hour),
	}
	keep, prune := PlanPrune(snaps, RetentionPolicy{KeepDaily: 1}, now)
	if len(keep) != 1 {
		t.Fatalf("expected 1 kept, got %d", len(keep))
	}
	if !keep[0].Equal(day.Add(15 * time.Hour)) {
		t.Fatalf("expected latest-of-day kept, got %s", keep[0])
	}
	if len(prune) != 2 {
		t.Fatalf("expected 2 pruned, got %d", len(prune))
	}
}

// TestPlanPrune_GapsInDays: KeepDaily=7 but only 3 of those 7 days
// have snapshots. Those 3 stay; nothing pruned by the daily bucket
// alone, since each day's latest survives.
func TestPlanPrune_GapsInDays(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	snaps := []time.Time{
		now.AddDate(0, 0, -1),
		now.AddDate(0, 0, -3),
		now.AddDate(0, 0, -5),
	}
	keep, prune := PlanPrune(snaps, RetentionPolicy{KeepDaily: 7}, now)
	if len(keep) != 3 {
		t.Fatalf("expected 3 kept, got %d", len(keep))
	}
	if len(prune) != 0 {
		t.Fatalf("expected 0 pruned, got %d (%v)", len(prune), prune)
	}
}

// TestPlanPrune_OldSnapshotsBeyondAllBuckets: snapshots older than
// the widest window get pruned. Default policy {7,4,12} reaches
// roughly 14 months back; a 2-year-old snapshot is out.
func TestPlanPrune_OldSnapshotsBeyondAllBuckets(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	twoYearsAgo := now.AddDate(-2, 0, 0)
	threeYearsAgo := now.AddDate(-3, 0, 0)
	snaps := []time.Time{
		twoYearsAgo,
		threeYearsAgo,
		now.AddDate(0, 0, -1),
	}
	policy := DefaultRetention()
	keep, prune := PlanPrune(snaps, policy, now)
	if len(keep) != 1 {
		t.Fatalf("expected 1 kept (yesterday), got %d", len(keep))
	}
	if len(prune) != 2 {
		t.Fatalf("expected 2 pruned, got %d", len(prune))
	}
	for _, p := range prune {
		if !p.Equal(twoYearsAgo) && !p.Equal(threeYearsAgo) {
			t.Errorf("unexpected pruned timestamp: %s", p)
		}
	}
}

// TestPlanPrune_GFSUnion: a snapshot from 6 weeks ago is outside
// the daily window (7 days) but inside the weekly window (4 weeks)
// — wait, 6 weeks is outside 4 too. Pick something inside weekly
// but outside daily: 10 days ago. It's not the latest in its week
// only if another snapshot landed later in the same ISO-week.
func TestPlanPrune_GFSUnion(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC) // Thursday
	snaps := []time.Time{
		now.AddDate(0, 0, -10), // ~last-last Monday — outside daily, in weekly
		now.AddDate(0, 0, -45), // ~6.5 weeks ago — outside daily+weekly, in monthly
		now.AddDate(0, 0, -1),  // yesterday — daily
	}
	keep, prune := PlanPrune(snaps, RetentionPolicy{KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 12}, now)
	if len(keep) != 3 {
		t.Fatalf("expected all 3 kept (one per bucket), got keep=%d prune=%d (%v)", len(keep), len(prune), prune)
	}
}

// TestPlanPrune_SortedChronologically: regardless of input order,
// outputs come back oldest-first.
func TestPlanPrune_SortedChronologically(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	snaps := []time.Time{
		now.AddDate(0, 0, -3),
		now.AddDate(0, 0, -1),
		now.AddDate(0, 0, -2),
	}
	keep, _ := PlanPrune(snaps, RetentionPolicy{KeepDaily: 7}, now)
	if len(keep) != 3 {
		t.Fatalf("expected 3 kept, got %d", len(keep))
	}
	for i := 1; i < len(keep); i++ {
		if keep[i].Before(keep[i-1]) {
			t.Errorf("not chronological: %s before %s", keep[i], keep[i-1])
		}
	}
}

// TestPlanPrune_FuturePreserved: clock-skew defensive case — a
// snapshot dated after `now` must never be pruned regardless of
// policy.
func TestPlanPrune_FuturePreserved(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	future := now.Add(2 * time.Hour)
	snaps := []time.Time{future}
	keep, prune := PlanPrune(snaps, RetentionPolicy{}, now) // all-zero policy
	if len(prune) != 0 {
		t.Fatalf("future snapshot pruned: %v", prune)
	}
	if len(keep) != 1 {
		t.Fatalf("future snapshot not kept: keep=%v", keep)
	}
}

// TestPlanPrune_ZeroPolicy: all-zero policy prunes everything in
// the past — operator opt-out from retention entirely. Useful in
// case the wizard accidentally writes zeros; we still want explicit
// behaviour rather than a silent default.
func TestPlanPrune_ZeroPolicy(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	snaps := []time.Time{
		now.AddDate(0, 0, -1),
		now.AddDate(0, 0, -7),
	}
	keep, prune := PlanPrune(snaps, RetentionPolicy{}, now)
	if len(keep) != 0 {
		t.Fatalf("expected nothing kept, got %v", keep)
	}
	if len(prune) != 2 {
		t.Fatalf("expected 2 pruned, got %d", len(prune))
	}
}

// TestPlanPrune_DenseDays: 30 daily snapshots, policy {7,0,0} keeps
// exactly 7 (today + 6 prior days).
func TestPlanPrune_DenseDays(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	snaps := make([]time.Time, 30)
	for i := range snaps {
		snaps[i] = now.AddDate(0, 0, -i)
	}
	keep, prune := PlanPrune(snaps, RetentionPolicy{KeepDaily: 7}, now)
	if len(keep) != 7 {
		t.Fatalf("expected 7 kept, got %d", len(keep))
	}
	if len(prune) != 23 {
		t.Fatalf("expected 23 pruned, got %d", len(prune))
	}
}

// TestPlanPrune_GFSDoesNotMutateInput: PlanPrune copies before it
// sorts so the caller's slice is unaffected. Cheap defensive check
// against a future refactor.
func TestPlanPrune_GFSDoesNotMutateInput(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	original := []time.Time{
		now.AddDate(0, 0, -3),
		now.AddDate(0, 0, -1),
		now.AddDate(0, 0, -2),
	}
	snapshot := make([]time.Time, len(original))
	copy(snapshot, original)
	_, _ = PlanPrune(original, RetentionPolicy{KeepDaily: 7}, now)
	for i, want := range snapshot {
		if !original[i].Equal(want) {
			t.Fatalf("PlanPrune mutated input at index %d: %s != %s", i, original[i], want)
		}
	}
}

// TestPlanPrune_AcceptanceScenario follows the prompt's verification
// step (#9): two snapshots on different days, retention {7,4,12} —
// both must survive because each is the latest of its own day and
// both days are inside the 7-day window.
func TestPlanPrune_AcceptanceScenario(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	snaps := []time.Time{
		now.AddDate(0, 0, -1), // first run, yesterday
		now,                   // second run, today
	}
	keep, prune := PlanPrune(snaps, RetentionPolicy{KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 12}, now)
	if len(prune) != 0 {
		t.Fatalf("expected zero pruned for two consecutive-day runs, got %d (%v)", len(prune), prune)
	}
	if len(keep) != 2 {
		t.Fatalf("expected both kept, got keep=%d", len(keep))
	}
}

// TestPlanPrune_OnlyLatestPerDayCounts: two snapshots same day,
// daily=1, plus an older single-snapshot day. Daily window covers
// today's latest + yesterday's single — but the older same-day
// snapshot can NOT be saved by the weekly/monthly buckets because
// today's latest already represents the week/month.
func TestPlanPrune_OnlyLatestPerDayCounts(t *testing.T) {
	now := time.Date(2026, 5, 21, 23, 0, 0, 0, time.UTC)
	day := func(d int, h int) time.Time {
		return time.Date(2026, 5, d, h, 0, 0, 0, time.UTC)
	}
	snaps := []time.Time{
		day(21, 3),  // today's morning
		day(21, 22), // today's evening — this is the kept-latest
		day(20, 12), // yesterday
	}
	keep, prune := PlanPrune(snaps, RetentionPolicy{KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 12}, now)
	if len(keep) != 2 {
		t.Fatalf("expected 2 kept, got %d (%v)", len(keep), keep)
	}
	if len(prune) != 1 || !prune[0].Equal(day(21, 3)) {
		t.Fatalf("expected today's morning pruned, got %v", prune)
	}
}

// TestSnapshotRoot keeps the layout helper that the lister uses
// pinned to "{slug}/" so refactors don't accidentally diverge.
func TestSnapshotRoot(t *testing.T) {
	if got := SnapshotRoot("LSI Photos"); got != "lsi-photos/" {
		t.Errorf("SnapshotRoot = %q, want %q", got, "lsi-photos/")
	}
}

// (kept to silence unused-import warnings when someone adds a future
// helper with `strings.` usage in this file).
var _ = strings.HasPrefix

// TestPlanPrune_DateFormatInvariant guards against the day key
// using local-time formatting by accident — every snapshot below is
// 23:00 UTC, which is the next calendar day in many western
// timezones. Result must be insensitive to the runtime's TZ.
func TestPlanPrune_DateFormatInvariant(t *testing.T) {
	now := time.Date(2026, 5, 21, 23, 0, 0, 0, time.UTC)
	snap := mustParse(t, "2026-05-21_23:00:00")
	keep, prune := PlanPrune([]time.Time{snap}, RetentionPolicy{KeepDaily: 1}, now)
	if len(keep) != 1 || len(prune) != 0 {
		t.Fatalf("UTC date key drifted: keep=%v prune=%v", keep, prune)
	}
}
