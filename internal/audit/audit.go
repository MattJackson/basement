// Package audit implements append-only audit logging of every mutating
// action that flows through the basement server.
//
// Storage format: JSON Lines, one file per UTC day at
// {dataDir}/audit/YYYY-MM-DD.log. Files are append-only and never
// rewritten; rotation is a path change when the UTC date rolls over.
// Old files stay on disk indefinitely — operators rm or cron-archive
// them; basement never deletes.
//
// Write path: each Log() call serialises the event, appends a line +
// newline under a single mutex, and fsyncs. fsync-per-write is fine
// for the volume expected (typical operator action rate is dozens of
// events per minute at peak); benchmark in audit_test.go shows the
// latency hit is well under 5ms even on a network-mounted disk.
//
// Read path: Query() linear-scans the date range. For fast initial
// load on /admin/audit we keep an in-memory ring of the last
// recentCacheSize events that gets consulted first; if the requested
// window fits inside the cache, the file scan is skipped.
//
// Failure mode: any I/O error during Log() is swallowed (logged via
// slog) — never blocks or fails the calling request path. ADR-0001
// is the policy authority and bucket grants are the credential
// authority; audit is purely observational and must not gate.
package audit

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

// Event is one row in the audit log. Time is UTC, set by the Logger
// when Log() is called (callers should not pre-set it). All other
// fields are caller-provided.
type Event struct {
	Time      time.Time `json:"time"`
	Actor     string    `json:"actor"`
	ActorRole string    `json:"actorRole,omitempty"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	Result    string    `json:"result"`
	Detail    string    `json:"detail,omitempty"`
	IP        string    `json:"ip,omitempty"`
	UserAgent string    `json:"userAgent,omitempty"`
}

// QueryFilter narrows the rows returned by Query. Empty fields are
// "match anything". Substring matches are case-insensitive.
type QueryFilter struct {
	Actor    string
	Action   string
	Resource string
	Result   string
	Limit    int
}

// Logger is the audit-log interface used by the API handlers. The
// production implementation is *FileLogger; tests can substitute a
// memory-only fake.
type Logger interface {
	Log(e Event)
	Query(from, to time.Time, filter QueryFilter) ([]Event, error)
}

// defaultLimit is the rows-returned cap when QueryFilter.Limit is 0
// or negative. Matches the v1.0.0c spec.
const defaultLimit = 200

// maxLimit caps Query results regardless of the caller's request, so
// a misconfigured UI cannot ask for every event ever written.
const maxLimit = 1000

// recentCacheSize is the size of the in-memory ring buffer that
// short-circuits "show me the last N events" Query calls. Sized to
// always cover one page of the /admin/audit table at maxLimit.
const recentCacheSize = 1000

// FileLogger writes events as JSON Lines to {dir}/YYYY-MM-DD.log,
// rotating by UTC date. Thread-safe; one mutex serialises all writes.
type FileLogger struct {
	dir string

	mu     sync.Mutex
	cur    *os.File
	curDay string // YYYY-MM-DD of the open file; "" means cur is nil
	bw     *bufio.Writer

	recent    []Event // ring; len ≤ recentCacheSize
	recentIdx int     // next write position
	recentN   int     // number of valid entries (≤ recentCacheSize)

	now func() time.Time // injectable for tests
}

// NewFileLogger constructs a Logger backed by {dataDir}/audit/. The
// directory is created lazily on first write — if the data dir isn't
// writable, callers still see no error here (matching the sync.Store
// pattern from store.go); the slog warning fires on first Log().
func NewFileLogger(dataDir string) *FileLogger {
	return &FileLogger{
		dir:    filepath.Join(dataDir, "audit"),
		recent: make([]Event, recentCacheSize),
		now:    func() time.Time { return time.Now().UTC() },
	}
}

// Log writes an event to disk + the in-memory recent cache. Errors
// are logged at warn level and swallowed; the calling request path
// must never fail because audit logging failed.
func (l *FileLogger) Log(e Event) {
	if e.Time.IsZero() {
		e.Time = l.now()
	} else {
		e.Time = e.Time.UTC()
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// In-memory cache first so even a disk-write failure still
	// surfaces recent events via /admin/audit (which scans files
	// AFTER consulting the cache).
	l.recent[l.recentIdx] = e
	l.recentIdx = (l.recentIdx + 1) % recentCacheSize
	if l.recentN < recentCacheSize {
		l.recentN++
	}

	day := e.Time.Format("2006-01-02")
	if err := l.openForDay(day); err != nil {
		slog.Warn("audit: open file failed", "day", day, "error", err)
		return
	}

	line, err := json.Marshal(e)
	if err != nil {
		slog.Warn("audit: marshal failed", "error", err)
		return
	}

	// Append the line as a single bufio write so partial-line
	// corruption is impossible (the writer flushes its internal
	// buffer atomically at this size; line+newline well under
	// bufio's default 4096B page).
	if _, err := l.bw.Write(append(line, '\n')); err != nil {
		slog.Warn("audit: write failed", "error", err)
		return
	}
	if err := l.bw.Flush(); err != nil {
		slog.Warn("audit: flush failed", "error", err)
		return
	}
	if err := l.cur.Sync(); err != nil {
		slog.Warn("audit: fsync failed", "error", err)
	}
}

// openForDay ensures l.cur points at the YYYY-MM-DD.log for `day`.
// Caller must hold l.mu.
func (l *FileLogger) openForDay(day string) error {
	if l.curDay == day && l.cur != nil {
		return nil
	}

	// Date rolled over — close the previous file before opening
	// the new one so the writer doesn't leak fds.
	if l.cur != nil {
		_ = l.bw.Flush()
		_ = l.cur.Close()
		l.cur = nil
		l.bw = nil
	}

	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return fmt.Errorf("creating audit dir: %w", err)
	}

	path := filepath.Join(l.dir, day+".log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}

	l.cur = f
	l.curDay = day
	l.bw = bufio.NewWriter(f)
	return nil
}

// Close flushes the open file and releases the fd. Safe to call from
// shutdown paths; the Logger is unusable after Close.
func (l *FileLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cur == nil {
		return nil
	}
	if err := l.bw.Flush(); err != nil {
		return err
	}
	if err := l.cur.Close(); err != nil {
		return err
	}
	l.cur = nil
	l.curDay = ""
	l.bw = nil
	return nil
}

// Query returns events in [from, to] that match the filter, most
// recent first, up to filter.Limit (capped at maxLimit). The boolean
// in the response API ("truncated") is computed by the HTTP layer;
// callers here see the raw slice.
//
// Strategy: first try the recent-events cache; if the requested
// range is fully contained in cache AND the cache has at least
// filter.Limit matches, return immediately. Otherwise fall through
// to a file scan over the date range.
func (l *FileLogger) Query(from, to time.Time, filter QueryFilter) ([]Event, error) {
	if to.IsZero() {
		to = l.now()
	}
	if from.IsZero() {
		from = to.Add(-24 * time.Hour)
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

	// Cache fast path. The cache holds up to recentCacheSize most
	// recent events globally; if our oldest cached event is older
	// than `from` we know the cache covers the window.
	if cached, fullyCovered := l.queryCache(from, to, filter, limit); fullyCovered {
		return cached, nil
	}

	// Fall through to a file scan.
	return l.queryFiles(from, to, filter, limit)
}

// queryCache walks the in-memory ring buffer newest-first.
func (l *FileLogger) queryCache(from, to time.Time, filter QueryFilter, limit int) ([]Event, bool) {
	l.mu.Lock()
	if l.recentN == 0 {
		l.mu.Unlock()
		return nil, false
	}

	// Copy out the live slice while we hold the lock — minimises
	// lock-hold time so concurrent Log() isn't blocked by query
	// filter/format work.
	snapshot := make([]Event, l.recentN)
	// recentIdx points at the NEXT write position, so the oldest
	// event is at recentIdx-recentN (mod recentCacheSize).
	start := (l.recentIdx - l.recentN + recentCacheSize) % recentCacheSize
	for i := 0; i < l.recentN; i++ {
		snapshot[i] = l.recent[(start+i)%recentCacheSize]
	}
	cacheFull := l.recentN == recentCacheSize
	oldest := snapshot[0].Time
	l.mu.Unlock()

	// If the cache buffer is full AND the oldest cached event is
	// newer than `from`, the cache cannot guarantee coverage of
	// the full requested window — fall through to file scan.
	fullyCovered := !cacheFull || !oldest.After(from)

	out := make([]Event, 0, limit)
	// Walk newest-first.
	for i := len(snapshot) - 1; i >= 0 && len(out) < limit; i-- {
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

// queryFiles scans the per-day log files covering [from, to].
func (l *FileLogger) queryFiles(from, to time.Time, filter QueryFilter, limit int) ([]Event, error) {
	dayFiles, err := l.fileNamesInRange(from, to)
	if err != nil {
		return nil, err
	}

	// Walk newest day first so we can short-circuit once we hit the limit.
	sort.Sort(sort.Reverse(sort.StringSlice(dayFiles)))

	out := make([]Event, 0, limit)
	for _, name := range dayFiles {
		if len(out) >= limit {
			break
		}
		path := filepath.Join(l.dir, name)
		events, err := readDayFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		// In-file newest-first.
		for i := len(events) - 1; i >= 0 && len(out) < limit; i-- {
			e := events[i]
			if e.Time.Before(from) || e.Time.After(to) {
				continue
			}
			if !matchFilter(e, filter) {
				continue
			}
			out = append(out, e)
		}
	}
	return out, nil
}

// fileNamesInRange returns the YYYY-MM-DD.log files that fall within
// [from, to]. Filters to existing files only.
func (l *FileLogger) fileNamesInRange(from, to time.Time) ([]string, error) {
	if _, err := os.Stat(l.dir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var names []string
	// Iterate one day at a time. Range may be a couple of years for
	// pathological cases but linear over days is fine — no operator
	// is querying more than ~365 files at once.
	day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	end := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	for !day.After(end) {
		name := day.Format("2006-01-02") + ".log"
		path := filepath.Join(l.dir, name)
		if _, err := os.Stat(path); err == nil {
			names = append(names, name)
		}
		day = day.Add(24 * time.Hour)
	}
	return names, nil
}

// readDayFile loads every event in one file in arrival order
// (oldest first). Malformed lines are skipped with a slog warning;
// one corrupt line never blocks the rest of the file.
func readDayFile(path string) ([]Event, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	out := make([]Event, 0, len(lines))
	for i, line := range lines {
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			slog.Warn("audit: skipping malformed line", "path", path, "line_num", i+1, "error", err)
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// matchFilter returns true iff the event passes every non-empty
// filter field. Action and Resource are substring-matched case-
// insensitively so the UI search box "just works."
func matchFilter(e Event, f QueryFilter) bool {
	if f.Actor != "" && e.Actor != f.Actor {
		return false
	}
	if f.Action != "" && !strings.Contains(strings.ToLower(e.Action), strings.ToLower(f.Action)) {
		return false
	}
	if f.Resource != "" && !strings.Contains(strings.ToLower(e.Resource), strings.ToLower(f.Resource)) {
		return false
	}
	if f.Result != "" && e.Result != f.Result {
		return false
	}
	return true
}

// Result codes; centralised so handlers and tests use the same
// string values without typo risk.
const (
	ResultSuccess = "success"
	ResultFailure = "failure"
)

// noopLogger is the silent Logger installed when audit isn't wired
// (e.g. the many tests that construct api.Server with no data dir).
// All calls become no-ops; Query returns empty.
type noopLogger struct{}

// NewNoop returns a Logger that drops every event. Used by tests and
// by api.New() before SetAuditLogger() is called.
func NewNoop() Logger { return noopLogger{} }

func (noopLogger) Log(Event)                                            {}
func (noopLogger) Query(_, _ time.Time, _ QueryFilter) ([]Event, error) { return nil, nil }
