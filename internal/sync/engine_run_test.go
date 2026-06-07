package sync

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/driver"
)

// fakeDriver is a minimal in-memory driver.Driver for engine/plan tests. Only
// the methods the sync engine actually calls are functional; everything else
// returns ErrUnsupported / zero values. baseDriver supplies the no-op
// implementations so this file stays focused on the behaviour under test.
type fakeDriver struct {
	baseDriver
	name string

	mu      sync.Mutex
	objects map[string]driver.ObjectInfo // key -> info (used for List + Stat)

	// counters
	putCount  atomic.Int64
	sscCount  atomic.Int64
	statCount atomic.Int64

	// behaviour hooks
	caps        driver.Caps
	streamDelay time.Duration                               // delay inside StreamObject
	onStream    func(ctx context.Context, key string) error // optional pre-stream hook
	statResults map[string]driver.ObjectInfo                // dest stat overrides
	statErrKeys map[string]bool                             // keys that StatObject should miss
}

func newFakeDriver(name string) *fakeDriver {
	return &fakeDriver{
		name:        name,
		objects:     map[string]driver.ObjectInfo{},
		statResults: map[string]driver.ObjectInfo{},
		statErrKeys: map[string]bool{},
		caps:        driver.Caps{Driver: name},
	}
}

func (d *fakeDriver) Capabilities(ctx context.Context) (driver.Caps, error) {
	return d.caps, nil
}

func (d *fakeDriver) ListObjects(ctx context.Context, bucket, prefix, continuation, delimiter string, limit int) (driver.ObjectPage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var objs []driver.ObjectInfo
	for _, o := range d.objects {
		if prefix == "" || strings.HasPrefix(o.Key, prefix) {
			objs = append(objs, o)
		}
	}
	return driver.ObjectPage{Objects: objs, IsTruncated: false}, nil
}

func (d *fakeDriver) StatObject(ctx context.Context, bucket, key string) (driver.ObjectInfo, error) {
	d.statCount.Add(1)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.statErrKeys[key] {
		return driver.ObjectInfo{}, driver.ErrNotFound
	}
	if o, ok := d.statResults[key]; ok {
		return o, nil
	}
	return driver.ObjectInfo{}, driver.ErrNotFound
}

func (d *fakeDriver) StreamObject(ctx context.Context, bucket, key, rng string) (driver.StreamResult, error) {
	if d.onStream != nil {
		if err := d.onStream(ctx, key); err != nil {
			return driver.StreamResult{}, err
		}
	}
	if d.streamDelay > 0 {
		select {
		case <-time.After(d.streamDelay):
		case <-ctx.Done():
			return driver.StreamResult{}, ctx.Err()
		}
	}
	body := io.NopCloser(strings.NewReader("payload-" + key))
	return driver.StreamResult{Body: body, ContentType: "application/octet-stream", ContentLength: int64(len("payload-" + key))}, nil
}

func (d *fakeDriver) PutObjectStream(ctx context.Context, bucket, key string, reader io.Reader, contentType string, size int64) (driver.PutResult, error) {
	// Drain the body so the stream contract is honoured.
	_, _ = io.Copy(io.Discard, reader)
	d.putCount.Add(1)
	return driver.PutResult{ETag: "etag-" + key}, nil
}

func (d *fakeDriver) ServerSideCopy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	d.sscCount.Add(1)
	return nil
}

// ---- tests ----

func waitForState(t *testing.T, store Store, jobID, want string, timeout time.Duration) *SyncJob {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last *SyncJob
	for time.Now().Before(deadline) {
		j, err := store.Load(jobID)
		if err == nil {
			last = j
			if j.State == want {
				return j
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	gotState := "<load-failed>"
	if last != nil {
		gotState = last.State
	}
	t.Fatalf("job %s did not reach state %q within %s (last state %q)", jobID, want, timeout, gotState)
	return nil
}

// TestPlanSkipVsCopy verifies Plan classifies an etag-matched object as "skip"
// and a non-matching / missing object as "copy".
func TestPlanSkipVsCopy(t *testing.T) {
	src := newFakeDriver("garage")
	dst := newFakeDriver("garage")

	src.objects["a.txt"] = driver.ObjectInfo{Key: "a.txt", Size: 10, ETag: "match"}
	src.objects["b.txt"] = driver.ObjectInfo{Key: "b.txt", Size: 20, ETag: "src-only"}

	// Dest has a.txt with the SAME etag (=> skip) and b.txt is absent (=> copy).
	dst.statResults["a.txt"] = driver.ObjectInfo{Key: "a.txt", Size: 10, ETag: "match"}
	dst.statErrKeys["b.txt"] = true

	actions, err := Plan(context.Background(), src, dst, "src", "srcbucket", "", "dst", "dstbucket", "")
	if err != nil {
		t.Fatalf("Plan error: %v", err)
	}

	byKey := map[string]string{}
	for _, a := range actions {
		byKey[a.SrcKey] = a.ActionType
	}
	if byKey["a.txt"] != "skip" {
		t.Errorf("a.txt: want action skip, got %q", byKey["a.txt"])
	}
	if byKey["b.txt"] != "copy" {
		t.Errorf("b.txt: want action copy, got %q", byKey["b.txt"])
	}
}

// TestEngineSkipsDoNotCopy verifies a "skip" action does NOT trigger any dest
// Put / ServerSideCopy, while a "copy" action does — proving the etag-match
// optimisation is actually wired into the worker.
func TestEngineSkipsDoNotCopy(t *testing.T) {
	store := NewFileStore(t.TempDir())
	src := newFakeDriver("garage")
	dst := newFakeDriver("garage")
	// Force the streaming path (not SSC) so we count PutObjectStream.
	dst.caps = driver.Caps{Driver: "garage", ServerSideCopy: false}

	// a.txt = etag match (skip); b.txt = missing at dest (copy).
	src.objects["a.txt"] = driver.ObjectInfo{Key: "a.txt", Size: 10, ETag: "match"}
	src.objects["b.txt"] = driver.ObjectInfo{Key: "b.txt", Size: 20, ETag: "x"}
	dst.statResults["a.txt"] = driver.ObjectInfo{Key: "a.txt", Size: 10, ETag: "match"}
	dst.statErrKeys["b.txt"] = true

	job := &SyncJob{ID: GenerateID(), OwnerUserID: "u", State: "queued", SrcBucket: "s", DstBucket: "d"}
	if err := store.Save(job); err != nil {
		t.Fatalf("save: %v", err)
	}

	eng := NewEngine(store, 2)
	if err := eng.Run(context.Background(), job, src, dst); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if got := dst.putCount.Load(); got != 1 {
		t.Errorf("PutObjectStream call count = %d, want 1 (only the copy action)", got)
	}
	if got := dst.sscCount.Load(); got != 0 {
		t.Errorf("ServerSideCopy call count = %d, want 0", got)
	}

	final, _ := store.Load(job.ID)
	if final.Progress.ObjectsCopied != 1 {
		t.Errorf("ObjectsCopied = %d, want 1", final.Progress.ObjectsCopied)
	}
	if final.Progress.ObjectsSkipped != 1 {
		t.Errorf("ObjectsSkipped = %d, want 1", final.Progress.ObjectsSkipped)
	}
	if final.Progress.ObjectsTotal != 2 {
		t.Errorf("ObjectsTotal = %d, want 2", final.Progress.ObjectsTotal)
	}
	// BytesTotal should sum both objects (10 + 20); BytesCopied only the copy (20).
	if final.Progress.BytesTotal != 30 {
		t.Errorf("BytesTotal = %d, want 30", final.Progress.BytesTotal)
	}
	if final.Progress.BytesCopied != 20 {
		t.Errorf("BytesCopied = %d, want 20", final.Progress.BytesCopied)
	}
	if final.State != "done" {
		t.Errorf("State = %q, want done", final.State)
	}
}

// TestPauseStopsRunAndPersists verifies that Pause on a live run stops the
// worker and the persisted state stays "paused" (it is NOT overwritten to
// "done" when the run loop unwinds).
func TestPauseStopsRunAndPersists(t *testing.T) {
	store := NewFileStore(t.TempDir())
	src := newFakeDriver("garage")
	dst := newFakeDriver("garage")
	dst.caps = driver.Caps{Driver: "garage", ServerSideCopy: false}

	// Many objects, each stream blocks long enough for Pause to land.
	gate := make(chan struct{})
	var firstSeen sync.Once
	src.streamDelay = 200 * time.Millisecond
	src.onStream = func(ctx context.Context, key string) error {
		firstSeen.Do(func() { close(gate) })
		return nil
	}
	for i := 0; i < 50; i++ {
		k := fmt.Sprintf("obj-%02d.bin", i)
		src.objects[k] = driver.ObjectInfo{Key: k, Size: 100, ETag: "e"}
		dst.statErrKeys[k] = true // all copies
	}

	job := &SyncJob{ID: GenerateID(), OwnerUserID: "u", State: "queued", SrcBucket: "s", DstBucket: "d"}
	if err := store.Save(job); err != nil {
		t.Fatalf("save: %v", err)
	}

	eng := NewEngine(store, 2)
	go func() { _ = eng.Run(context.Background(), job, src, dst) }()

	// Wait until at least one worker has started streaming, then pause.
	// Generous timeout: the happy path fires in milliseconds; the long
	// deadline only guards against a genuine hang on a heavily loaded box.
	select {
	case <-gate:
	case <-time.After(30 * time.Second):
		t.Fatal("run never started streaming")
	}

	// Pause via a SEPARATE engine instance to mirror the per-request-engine
	// pattern in the HTTP layer — proves the registry is process-global.
	pauseEng := NewEngine(store, 2)
	if err := pauseEng.Pause(context.Background(), job.ID); err != nil {
		t.Fatalf("Pause error: %v", err)
	}

	final := waitForState(t, store, job.ID, "paused", 30*time.Second)
	if final.State != "paused" {
		t.Fatalf("state = %q, want paused", final.State)
	}

	// The run must have stopped well short of all 50 objects.
	if final.Progress.ObjectsCopied >= 50 {
		t.Errorf("ObjectsCopied = %d; expected the pause to stop the run early", final.Progress.ObjectsCopied)
	}

	// Give any straggler goroutines time to (incorrectly) overwrite state,
	// then re-check it is still paused.
	time.Sleep(300 * time.Millisecond)
	again, _ := store.Load(job.ID)
	if again.State != "paused" {
		t.Errorf("state after settle = %q, want paused (run overwrote the pause)", again.State)
	}
}

// TestResumeSurvivesCancelledCallerContext proves the resumed run is detached
// from the caller's context: we pass an already-cancelled context and the run
// still completes.
func TestResumeSurvivesCancelledCallerContext(t *testing.T) {
	store := NewFileStore(t.TempDir())
	src := newFakeDriver("garage")
	dst := newFakeDriver("garage")
	dst.caps = driver.Caps{Driver: "garage", ServerSideCopy: false}

	for i := 0; i < 5; i++ {
		k := fmt.Sprintf("k-%d", i)
		src.objects[k] = driver.ObjectInfo{Key: k, Size: 5, ETag: "e"}
		dst.statErrKeys[k] = true
	}

	job := &SyncJob{ID: GenerateID(), OwnerUserID: "u", State: "paused", SrcBucket: "s", DstBucket: "d"}
	if err := store.Save(job); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Caller context is cancelled BEFORE Resume returns — emulates the HTTP
	// handler returning and chi cancelling r.Context().
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	eng := NewEngine(store, 2)
	if err := eng.Resume(ctx, job.ID, src, dst); err != nil {
		t.Fatalf("Resume error: %v", err)
	}

	final := waitForState(t, store, job.ID, "done", 30*time.Second)
	if final.Progress.ObjectsCopied != 5 {
		t.Errorf("ObjectsCopied = %d, want 5 (resume died with the caller ctx?)", final.Progress.ObjectsCopied)
	}
	if got := dst.putCount.Load(); got != 5 {
		t.Errorf("PutObjectStream count = %d, want 5", got)
	}
}
