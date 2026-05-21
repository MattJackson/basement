// Package metrics implements per-bucket bytes/objects snapshots
// persisted to JSON-Lines files, plus a Query path for the
// /admin/usage time-series view (v1.0.0d).
//
// Storage format mirrors internal/audit/audit.go: one append-only
// file per UTC day at {dataDir}/metrics/YYYY-MM-DD.jsonl. Rotation
// is a path change when the UTC date rolls over; old files stay on
// disk indefinitely so the operator can keep arbitrary history.
//
// Write path: every Snapshot call serialises the entry, appends one
// line + newline under a single mutex, and fsyncs. The hourly
// snapshot scheduler in cmd/basement-server/main.go writes one Snapshot
// per (connection, bucket) per cycle, so write volume is tiny
// (dozens-to-hundreds of lines per hour) and the fsync-per-write
// cost is negligible.
//
// Read path: Query() scans the per-day files in the requested range
// and filters in memory. A 10k-entry in-memory ring caches the most
// recent snapshots so the common "last 7 days for one bucket" Query
// is served without touching disk.
//
// Failure mode: any I/O error during Snapshot() is logged via slog
// and swallowed — the calling scheduler must never crash the server
// because the metrics dir wasn't writable. Same contract audit
// follows.
package metrics

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Snapshot is one per-bucket sample at a point in time. Time is
// always UTC; the Recorder normalises and sets it if zero.
type Snapshot struct {
	Time         time.Time `json:"time"`
	ConnectionID string    `json:"connectionId"`
	BucketID     string    `json:"bucketId"`
	BucketAlias  string    `json:"bucketAlias,omitempty"`
	Bytes        int64     `json:"bytes"`
	Objects      int64     `json:"objects"`
}

// Filter narrows the rows returned by Query. Empty fields are
// "match anything". Limit caps results regardless of range.
type Filter struct {
	ConnectionID string
	BucketID     string
	Limit        int
}

// Recorder is the metrics-storage interface. Production wires
// *FileRecorder; tests can substitute a memory-only fake.
type Recorder interface {
	Snapshot(snap Snapshot) error
	Query(from, to time.Time, filter Filter) ([]Snapshot, error)
}

// defaultLimit is the rows-returned cap when Filter.Limit is 0
// or negative. Sized for "show 7 days x 1 bucket" (168 entries).
const defaultLimit = 1000

// maxLimit caps Query results regardless of the caller's request.
// 90 days of hourly snapshots for one bucket is 2160 entries;
// the cap leaves room for multi-bucket queries too.
const maxLimit = 10000

// recentCacheSize is the in-memory ring buffer that short-circuits
// recent-window queries. 10k entries handles ~5 weeks of hourly
// snapshots across 60 buckets without touching disk.
const recentCacheSize = 10000

// FileRecorder writes snapshots as JSON Lines to
// {dir}/YYYY-MM-DD.jsonl, rotating by UTC date. Thread-safe; one
// mutex serialises all writes.
type FileRecorder struct {
	dir string

	mu     sync.Mutex
	cur    *os.File
	curDay string // YYYY-MM-DD of the open file; "" means cur is nil
	bw     *bufio.Writer

	recent    []Snapshot // ring; len ≤ recentCacheSize
	recentIdx int        // next write position
	recentN   int        // number of valid entries (≤ recentCacheSize)

	now func() time.Time // injectable for tests
}

// NewFileRecorder constructs a Recorder backed by
// {dataDir}/metrics/. The directory is created lazily on first
// write — if the data dir isn't writable, callers still see no
// error here; the slog warning fires on first Snapshot.
func NewFileRecorder(dataDir string) *FileRecorder {
	return &FileRecorder{
		dir:    filepath.Join(dataDir, "metrics"),
		recent: make([]Snapshot, recentCacheSize),
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// Snapshot writes one entry to disk + the in-memory recent cache.
// Errors are logged at warn level and returned to the caller so
// the scheduler can decide whether to continue — unlike audit
// (which never blocks the request path), metrics scheduling is a
// pure background task and the caller may want to abort the cycle
// if writes are failing.
func (r *FileRecorder) Snapshot(s Snapshot) error {
	if s.Time.IsZero() {
		s.Time = r.now()
	} else {
		s.Time = s.Time.UTC()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Cache first — even a disk-write failure should surface
	// recently-collected snapshots via the query cache, matching
	// audit's "cache before persist" guarantee.
	r.recent[r.recentIdx] = s
	r.recentIdx = (r.recentIdx + 1) % recentCacheSize
	if r.recentN < recentCacheSize {
		r.recentN++
	}

	day := s.Time.Format("2006-01-02")
	if err := r.openForDay(day); err != nil {
		slog.Warn("metrics: open file failed", "day", day, "error", err)
		return err
	}

	line, err := json.Marshal(s)
	if err != nil {
		slog.Warn("metrics: marshal failed", "error", err)
		return err
	}

	if _, err := r.bw.Write(append(line, '\n')); err != nil {
		slog.Warn("metrics: write failed", "error", err)
		return err
	}
	if err := r.bw.Flush(); err != nil {
		slog.Warn("metrics: flush failed", "error", err)
		return err
	}
	if err := r.cur.Sync(); err != nil {
		slog.Warn("metrics: fsync failed", "error", err)
		return err
	}
	return nil
}

// openForDay ensures r.cur points at {dir}/YYYY-MM-DD.jsonl for
// `day`. Caller must hold r.mu.
func (r *FileRecorder) openForDay(day string) error {
	if r.curDay == day && r.cur != nil {
		return nil
	}

	if r.cur != nil {
		_ = r.bw.Flush()
		_ = r.cur.Close()
		r.cur = nil
		r.bw = nil
	}

	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return fmt.Errorf("creating metrics dir: %w", err)
	}

	path := filepath.Join(r.dir, day+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}

	r.cur = f
	r.curDay = day
	r.bw = bufio.NewWriter(f)
	return nil
}

// Close flushes the open file and releases the fd. Safe to call
// from shutdown paths; the Recorder is unusable after Close.
func (r *FileRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cur == nil {
		return nil
	}
	if err := r.bw.Flush(); err != nil {
		return err
	}
	if err := r.cur.Close(); err != nil {
		return err
	}
	r.cur = nil
	r.curDay = ""
	r.bw = nil
	return nil
}

// Query returns snapshots in [from, to] that match the filter,
// oldest-first (chart-friendly), up to filter.Limit (capped at
// maxLimit). Default limit is defaultLimit.
//
// Strategy: try the recent-events ring first; if the requested
// window is fully covered by the cache, return without touching
// disk. Otherwise fall through to a per-day file scan.
func (r *FileRecorder) Query(from, to time.Time, filter Filter) ([]Snapshot, error) {
	if to.IsZero() {
		to = r.now()
	}
	if from.IsZero() {
		from = to.Add(-7 * 24 * time.Hour)
	}
	from = from.UTC()
	to = to.UTC()

	limit := filter.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	if cached, fullyCovered := r.queryCache(from, to, filter, limit); fullyCovered {
		return cached, nil
	}

	return r.queryFiles(from, to, filter, limit)
}

// queryCache walks the in-memory ring buffer oldest-first.
func (r *FileRecorder) queryCache(from, to time.Time, filter Filter, limit int) ([]Snapshot, bool) {
	r.mu.Lock()
	if r.recentN == 0 {
		r.mu.Unlock()
		return nil, false
	}

	snapshot := make([]Snapshot, r.recentN)
	start := (r.recentIdx - r.recentN + recentCacheSize) % recentCacheSize
	for i := 0; i < r.recentN; i++ {
		snapshot[i] = r.recent[(start+i)%recentCacheSize]
	}
	cacheFull := r.recentN == recentCacheSize
	oldest := snapshot[0].Time
	r.mu.Unlock()

	// If the ring is full AND the oldest cached entry is newer
	// than `from`, the cache cannot guarantee coverage of the
	// requested window — fall through to file scan.
	fullyCovered := !cacheFull || !oldest.After(from)

	out := make([]Snapshot, 0, limit)
	// Walk oldest-first so the returned slice is chart-ready.
	for i := 0; i < len(snapshot) && len(out) < limit; i++ {
		e := snapshot[i]
		if e.Time.Before(from) || e.Time.After(to) {
			continue
		}
		if !matchFilter(e, filter) {
			continue
		}
		out = append(out, e)
	}

	return out, fullyCovered
}

// queryFiles scans the per-day jsonl files covering [from, to].
func (r *FileRecorder) queryFiles(from, to time.Time, filter Filter, limit int) ([]Snapshot, error) {
	dayFiles, err := r.fileNamesInRange(from, to)
	if err != nil {
		return nil, err
	}

	// Oldest day first so the appended output is chart-ordered.
	sort.Strings(dayFiles)

	out := make([]Snapshot, 0, limit)
	for _, name := range dayFiles {
		if len(out) >= limit {
			break
		}
		path := filepath.Join(r.dir, name)
		snaps, err := readDayFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		for _, s := range snaps {
			if len(out) >= limit {
				break
			}
			if s.Time.Before(from) || s.Time.After(to) {
				continue
			}
			if !matchFilter(s, filter) {
				continue
			}
			out = append(out, s)
		}
	}
	return out, nil
}

// fileNamesInRange returns the YYYY-MM-DD.jsonl files that fall
// within [from, to]. Filters to existing files only.
func (r *FileRecorder) fileNamesInRange(from, to time.Time) ([]string, error) {
	if _, err := os.Stat(r.dir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var names []string
	day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	end := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	for !day.After(end) {
		name := day.Format("2006-01-02") + ".jsonl"
		path := filepath.Join(r.dir, name)
		if _, err := os.Stat(path); err == nil {
			names = append(names, name)
		}
		day = day.Add(24 * time.Hour)
	}
	return names, nil
}

// readDayFile loads every snapshot in one file in arrival order.
// Malformed lines are skipped with a slog warning so one corrupt
// line never blocks the rest.
func readDayFile(path string) ([]Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	out := make([]Snapshot, 0, len(lines))
	for i, line := range lines {
		if line == "" {
			continue
		}
		var s Snapshot
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			slog.Warn("metrics: skipping malformed line", "path", path, "line_num", i+1, "error", err)
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// matchFilter returns true iff the snapshot passes every
// non-empty filter field. ConnectionID and BucketID are
// exact-match.
func matchFilter(s Snapshot, f Filter) bool {
	if f.ConnectionID != "" && s.ConnectionID != f.ConnectionID {
		return false
	}
	if f.BucketID != "" && s.BucketID != f.BucketID {
		return false
	}
	return true
}

// noopRecorder is the silent Recorder installed when metrics
// isn't wired (e.g. tests that don't care about persistence).
type noopRecorder struct{}

// NewNoop returns a Recorder that drops every snapshot and returns
// empty Query results. Used by tests and by api.New() before
// SetMetricsRecorder() is called.
func NewNoop() Recorder { return noopRecorder{} }

func (noopRecorder) Snapshot(Snapshot) error                              { return nil }
func (noopRecorder) Query(_, _ time.Time, _ Filter) ([]Snapshot, error)   { return nil, nil }
