// Tests for the metrics package (v1.0.0d). Mirrors audit_test.go's
// shape: append/read back, rotation, filter, limit cap, concurrency,
// and cache-bypasses-file-scan path.
package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFileRecorder_AppendAndReadBack(t *testing.T) {
	tmp := t.TempDir()
	rec := NewFileRecorder(tmp)
	defer rec.Close()

	fixed := time.Date(2026, 5, 21, 14, 30, 0, 0, time.UTC)
	rec.now = func() time.Time { return fixed }

	if err := rec.Snapshot(Snapshot{ConnectionID: "ca", BucketID: "b1", BucketAlias: "photos", Bytes: 1024, Objects: 10}); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if err := rec.Snapshot(Snapshot{ConnectionID: "ca", BucketID: "b2", BucketAlias: "logs", Bytes: 2048, Objects: 5}); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if err := rec.Snapshot(Snapshot{ConnectionID: "cb", BucketID: "b3", Bytes: 4096, Objects: 100}); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	from := fixed.Add(-time.Hour)
	to := fixed.Add(time.Hour)

	snaps, err := rec.Query(from, to, Filter{Limit: 50})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(snaps) != 3 {
		t.Fatalf("expected 3 snapshots, got %d", len(snaps))
	}

	// Chart-friendly ordering: oldest first. All three share the
	// same clock so we check the connection IDs round-trip.
	if snaps[0].ConnectionID != "ca" || snaps[2].ConnectionID != "cb" {
		t.Errorf("ordering wrong: got %v", []string{snaps[0].ConnectionID, snaps[1].ConnectionID, snaps[2].ConnectionID})
	}

	// On-disk file should be three JSON Lines.
	path := filepath.Join(tmp, "metrics", "2026-05-21.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines on disk, got %d", len(lines))
	}
	for i, ln := range lines {
		var s Snapshot
		if err := json.Unmarshal([]byte(ln), &s); err != nil {
			t.Errorf("line %d not valid JSON: %v (%q)", i, err, ln)
		}
	}
}

func TestFileRecorder_FilterByBucket(t *testing.T) {
	tmp := t.TempDir()
	rec := NewFileRecorder(tmp)
	defer rec.Close()

	fixed := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	rec.now = func() time.Time { return fixed }

	for i := 0; i < 5; i++ {
		_ = rec.Snapshot(Snapshot{ConnectionID: "ca", BucketID: "b1", Bytes: int64(i * 100), Objects: int64(i)})
		_ = rec.Snapshot(Snapshot{ConnectionID: "ca", BucketID: "b2", Bytes: int64(i * 200), Objects: int64(i * 2)})
		_ = rec.Snapshot(Snapshot{ConnectionID: "cb", BucketID: "b3", Bytes: int64(i * 300), Objects: int64(i * 3)})
	}

	from := fixed.Add(-time.Hour)
	to := fixed.Add(time.Hour)

	// Filter on a specific bucket.
	snaps, err := rec.Query(from, to, Filter{ConnectionID: "ca", BucketID: "b1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(snaps) != 5 {
		t.Errorf("expected 5 snaps for ca/b1, got %d", len(snaps))
	}
	for _, s := range snaps {
		if s.ConnectionID != "ca" || s.BucketID != "b1" {
			t.Errorf("filter leak: got %+v", s)
		}
	}

	// Filter on connection only.
	snaps, err = rec.Query(from, to, Filter{ConnectionID: "ca"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(snaps) != 10 {
		t.Errorf("expected 10 snaps for ca, got %d", len(snaps))
	}
}

func TestFileRecorder_Rotation(t *testing.T) {
	tmp := t.TempDir()
	rec := NewFileRecorder(tmp)
	defer rec.Close()

	day1 := time.Date(2026, 5, 21, 23, 59, 0, 0, time.UTC)
	day2 := time.Date(2026, 5, 22, 0, 1, 0, 0, time.UTC)

	rec.now = func() time.Time { return day1 }
	_ = rec.Snapshot(Snapshot{ConnectionID: "ca", BucketID: "b1", Bytes: 1})

	rec.now = func() time.Time { return day2 }
	_ = rec.Snapshot(Snapshot{ConnectionID: "ca", BucketID: "b1", Bytes: 2})

	for _, day := range []string{"2026-05-21", "2026-05-22"} {
		path := filepath.Join(tmp, "metrics", day+".jsonl")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
		if len(lines) != 1 {
			t.Errorf("file for %s: expected 1 line, got %d", day, len(lines))
		}
	}

	// Fresh recorder to bypass cache and exercise the file scan.
	rec2 := NewFileRecorder(tmp)
	rec2.now = func() time.Time { return day2 }
	snaps, err := rec2.Query(day1.Add(-time.Hour), day2.Add(time.Hour), Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(snaps) != 2 {
		t.Errorf("expected 2 snaps across rotation, got %d", len(snaps))
	}
}

func TestFileRecorder_LimitCap(t *testing.T) {
	tmp := t.TempDir()
	rec := NewFileRecorder(tmp)
	defer rec.Close()

	fixed := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	rec.now = func() time.Time { return fixed }

	for i := 0; i < 50; i++ {
		_ = rec.Snapshot(Snapshot{ConnectionID: "ca", BucketID: "b1", Bytes: int64(i)})
	}

	from := fixed.Add(-time.Hour)
	to := fixed.Add(time.Hour)

	// Explicit small limit honoured.
	snaps, err := rec.Query(from, to, Filter{Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(snaps) != 10 {
		t.Errorf("Limit=10: got %d", len(snaps))
	}

	// Over-large limit clamps to maxLimit but only 50 exist.
	snaps, err = rec.Query(from, to, Filter{Limit: 100000})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(snaps) != 50 {
		t.Errorf("over-limit cap: got %d, want 50", len(snaps))
	}
}

func TestFileRecorder_ConcurrentSnapshot(t *testing.T) {
	tmp := t.TempDir()
	rec := NewFileRecorder(tmp)
	defer rec.Close()

	fixed := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	rec.now = func() time.Time { return fixed }

	const workers = 8
	const perWorker = 25

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				_ = rec.Snapshot(Snapshot{
					ConnectionID: "ca",
					BucketID:     "b1",
					Bytes:        int64(w*perWorker + i),
					Objects:      1,
				})
			}
		}(w)
	}
	wg.Wait()

	path := filepath.Join(tmp, "metrics", "2026-05-21.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if got, want := len(lines), workers*perWorker; got != want {
		t.Errorf("disk line count = %d, want %d", got, want)
	}
}

func TestFileRecorder_CacheBypassesFileScan(t *testing.T) {
	tmp := t.TempDir()
	rec := NewFileRecorder(tmp)
	defer rec.Close()

	fixed := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	rec.now = func() time.Time { return fixed }

	for i := 0; i < 5; i++ {
		_ = rec.Snapshot(Snapshot{ConnectionID: "ca", BucketID: "b1", Bytes: int64(i)})
	}

	// Nuke the dir; the cache should still serve the query.
	rec.Close()
	if err := os.RemoveAll(filepath.Join(tmp, "metrics")); err != nil {
		t.Fatalf("remove dir: %v", err)
	}

	snaps, err := rec.Query(fixed.Add(-time.Hour), fixed.Add(time.Hour), Filter{Limit: 100})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(snaps) != 5 {
		t.Errorf("cache should still hold 5 snapshots after dir nuke, got %d", len(snaps))
	}
}

func TestFileRecorder_DefaultRangeIs7Days(t *testing.T) {
	tmp := t.TempDir()
	rec := NewFileRecorder(tmp)
	defer rec.Close()

	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	rec.now = func() time.Time { return now }

	// Three snapshots: way old, just inside the default 7-day
	// window, and "now".
	old := now.Add(-30 * 24 * time.Hour)
	recent := now.Add(-3 * 24 * time.Hour)
	rec.now = func() time.Time { return old }
	_ = rec.Snapshot(Snapshot{ConnectionID: "ca", BucketID: "b1", Bytes: 1})
	rec.now = func() time.Time { return recent }
	_ = rec.Snapshot(Snapshot{ConnectionID: "ca", BucketID: "b1", Bytes: 2})
	rec.now = func() time.Time { return now }
	_ = rec.Snapshot(Snapshot{ConnectionID: "ca", BucketID: "b1", Bytes: 3})

	// Both from and to zero — default range is "last 7 days from now".
	snaps, err := rec.Query(time.Time{}, time.Time{}, Filter{Limit: 100})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(snaps) != 2 {
		t.Errorf("default-range Query: got %d snaps, want 2 (excluded 30-day-old)", len(snaps))
	}
}

func TestNewNoop(t *testing.T) {
	r := NewNoop()
	if err := r.Snapshot(Snapshot{ConnectionID: "ca"}); err != nil {
		t.Errorf("noop Snapshot: %v", err)
	}
	snaps, err := r.Query(time.Now().Add(-time.Hour), time.Now(), Filter{})
	if err != nil {
		t.Errorf("noop Query: %v", err)
	}
	if len(snaps) != 0 {
		t.Errorf("noop Query returned %d, want 0", len(snaps))
	}
}
