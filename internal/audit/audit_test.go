package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestFileLogger_AppendAndReadBack exercises the basic write path:
// log three events on the same UTC day, then Query them back and
// assert the file on disk has three JSON Lines.
func TestFileLogger_AppendAndReadBack(t *testing.T) {
	tmp := t.TempDir()
	logger := NewFileLogger(tmp)
	defer logger.Close()

	// Pin time so all three events land in the same daily file.
	fixed := time.Date(2026, 5, 21, 14, 30, 0, 0, time.UTC)
	logger.now = func() time.Time { return fixed }

	logger.Log(Event{Actor: "matthew", Action: "cluster:create", Resource: "cluster:abc", Result: ResultSuccess})
	logger.Log(Event{Actor: "matthew", Action: "bucket:create", Resource: "bucket:abc:foo", Result: ResultSuccess})
	logger.Log(Event{Actor: "matthew", Action: "bucket:delete", Resource: "bucket:abc:foo", Result: ResultFailure, Detail: "still has objects"})

	from := fixed.Add(-time.Hour)
	to := fixed.Add(time.Hour)
	events, err := logger.Query(from, to, QueryFilter{Limit: 50})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	// Newest-first ordering: bucket:delete should be at index 0.
	if events[0].Action != "bucket:delete" {
		t.Errorf("events[0].Action = %q, want bucket:delete", events[0].Action)
	}
	if events[2].Action != "cluster:create" {
		t.Errorf("events[2].Action = %q, want cluster:create", events[2].Action)
	}

	// File on disk should be parseable JSON Lines.
	path := filepath.Join(tmp, "audit", "2026-05-21.log")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	for i, ln := range lines {
		var e Event
		if err := json.Unmarshal([]byte(ln), &e); err != nil {
			t.Errorf("line %d not valid JSON: %v (%q)", i, err, ln)
		}
	}
}

// TestFileLogger_Rotation verifies that events on different UTC
// dates land in different files. Inject a clock that ticks across
// midnight on the second event.
func TestFileLogger_Rotation(t *testing.T) {
	tmp := t.TempDir()
	logger := NewFileLogger(tmp)
	defer logger.Close()

	day1 := time.Date(2026, 5, 21, 23, 59, 0, 0, time.UTC)
	day2 := time.Date(2026, 5, 22, 0, 1, 0, 0, time.UTC)

	logger.now = func() time.Time { return day1 }
	logger.Log(Event{Actor: "matthew", Action: "test", Resource: "x", Result: ResultSuccess})

	logger.now = func() time.Time { return day2 }
	logger.Log(Event{Actor: "matthew", Action: "test", Resource: "y", Result: ResultSuccess})

	for _, day := range []string{"2026-05-21", "2026-05-22"} {
		path := filepath.Join(tmp, "audit", day+".log")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
		if len(lines) != 1 {
			t.Errorf("file for %s: expected 1 line, got %d", day, len(lines))
		}
	}

	// Query across both days returns both events.
	from := day1.Add(-time.Hour)
	to := day2.Add(time.Hour)
	logger.now = func() time.Time { return day2 }

	// Use a fresh logger to bypass the in-memory cache so we
	// exercise the file-scan path.
	logger2 := NewFileLogger(tmp)
	logger2.now = func() time.Time { return day2 }
	events, err := logger2.Query(from, to, QueryFilter{Limit: 50})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events across rotation, got %d", len(events))
	}
}

// TestFileLogger_QueryFilter exercises actor / action / result
// filtering against a populated log.
func TestFileLogger_QueryFilter(t *testing.T) {
	tmp := t.TempDir()
	logger := NewFileLogger(tmp)
	defer logger.Close()

	fixed := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return fixed }

	logger.Log(Event{Actor: "matthew", Action: "cluster:create", Resource: "cluster:abc", Result: ResultSuccess})
	logger.Log(Event{Actor: "alice", Action: "bucket:create", Resource: "bucket:abc:foo", Result: ResultSuccess})
	logger.Log(Event{Actor: "matthew", Action: "bucket:delete", Resource: "bucket:abc:foo", Result: ResultFailure})

	from := fixed.Add(-time.Hour)
	to := fixed.Add(time.Hour)

	tests := []struct {
		name   string
		filter QueryFilter
		wantN  int
	}{
		{"actor matthew", QueryFilter{Actor: "matthew"}, 2},
		{"actor alice", QueryFilter{Actor: "alice"}, 1},
		{"action bucket: substring", QueryFilter{Action: "bucket:"}, 2},
		{"result failure", QueryFilter{Result: ResultFailure}, 1},
		{"resource bucket: substring", QueryFilter{Resource: "bucket:"}, 2},
		{"no match", QueryFilter{Actor: "nonexistent"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, err := logger.Query(from, to, tt.filter)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(events) != tt.wantN {
				t.Errorf("got %d events, want %d", len(events), tt.wantN)
			}
		})
	}
}

// TestFileLogger_LimitCap ensures the default + max limits are
// honoured even when more events exist on disk.
func TestFileLogger_LimitCap(t *testing.T) {
	tmp := t.TempDir()
	logger := NewFileLogger(tmp)
	defer logger.Close()

	fixed := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return fixed }

	for i := 0; i < 300; i++ {
		logger.Log(Event{Actor: "matthew", Action: "test", Resource: "r", Result: ResultSuccess})
	}

	from := fixed.Add(-time.Hour)
	to := fixed.Add(time.Hour)

	// Default limit caps to 200.
	events, err := logger.Query(from, to, QueryFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(events) != defaultLimit {
		t.Errorf("default limit: got %d, want %d", len(events), defaultLimit)
	}

	// Caller request of 999 fits inside maxLimit (1000).
	events, err = logger.Query(from, to, QueryFilter{Limit: 999})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(events) != 300 {
		t.Errorf("requested 999, have 300, got %d", len(events))
	}

	// Caller request of 100000 caps to maxLimit (1000) but we
	// only have 300 events, so the actual returned count is 300.
	events, err = logger.Query(from, to, QueryFilter{Limit: 100000})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(events) != 300 {
		t.Errorf("max limit cap: got %d, want 300", len(events))
	}
}

// TestFileLogger_ConcurrentLog drives Log() from many goroutines
// against the race detector. Each goroutine writes N events and the
// test asserts the on-disk line count matches.
func TestFileLogger_ConcurrentLog(t *testing.T) {
	tmp := t.TempDir()
	logger := NewFileLogger(tmp)
	defer logger.Close()

	fixed := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return fixed }

	const workers = 8
	const perWorker = 50

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				logger.Log(Event{
					Actor:    "matthew",
					Action:   "test:concurrent",
					Resource: "r",
					Result:   ResultSuccess,
				})
			}
		}(w)
	}
	wg.Wait()

	path := filepath.Join(tmp, "audit", "2026-05-21.log")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if got, want := len(lines), workers*perWorker; got != want {
		t.Errorf("disk line count = %d, want %d", got, want)
	}
}

// TestFileLogger_CacheBypassesFileScan asserts the in-memory cache
// short-circuits Query when the requested window fits. We confirm
// this by writing events with a working logger, then NUKING the
// audit dir from underneath it — Query should still return the
// cached events.
func TestFileLogger_CacheBypassesFileScan(t *testing.T) {
	tmp := t.TempDir()
	logger := NewFileLogger(tmp)
	defer logger.Close()

	fixed := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return fixed }

	for i := 0; i < 5; i++ {
		logger.Log(Event{Actor: "matthew", Action: "test:cache", Resource: "r", Result: ResultSuccess})
	}

	// Reach into the cache via the internal API path. After this
	// Close+RemoveAll, the file is gone but the cache is intact.
	logger.Close()
	if err := os.RemoveAll(filepath.Join(tmp, "audit")); err != nil {
		t.Fatalf("removing audit dir: %v", err)
	}

	// The recent cache is in-memory, so reopening the file handle
	// has no effect. Query the original logger and verify it still
	// returns the cached rows.
	from := fixed.Add(-time.Hour)
	to := fixed.Add(time.Hour)
	events, err := logger.Query(from, to, QueryFilter{Limit: 100})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(events) != 5 {
		t.Errorf("cache should still hold 5 events post-dir-nuke, got %d", len(events))
	}
}

// TestNewNoop verifies the no-op stub returned by NewNoop() does
// nothing and never errors — used by tests that don't care about
// audit but instantiate api.Server.
func TestNewNoop(t *testing.T) {
	l := NewNoop()
	l.Log(Event{Actor: "matthew", Action: "noop", Resource: "r", Result: ResultSuccess})
	events, err := l.Query(time.Now().Add(-time.Hour), time.Now(), QueryFilter{})
	if err != nil {
		t.Fatalf("noop Query: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("noop Query returned %d events, want 0", len(events))
	}
}

// BenchmarkFileLogger_Log measures the per-call latency of Log().
// The hard constraint is <5ms per write; this benchmark exists so
// future cycles can detect regressions.
func BenchmarkFileLogger_Log(b *testing.B) {
	tmp := b.TempDir()
	logger := NewFileLogger(tmp)
	defer logger.Close()

	e := Event{Actor: "matthew", Action: "bench:log", Resource: "r", Result: ResultSuccess}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Log(e)
	}
}
