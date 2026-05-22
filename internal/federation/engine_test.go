package federation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/audit"
)

// fakeObject is one object inside the fakeDriver's per-bucket store.
type fakeObject struct {
	body         []byte
	contentType  string
	etag         string
	lastModified time.Time
}

// fakeDriver is a minimal in-memory ReplicationClient implementation
// for the federation engine tests. Only the methods the engine actually
// calls are non-trivial.
//
// Bucket -> key -> object map. Mutex-guarded so tests can assert
// post-replicate state without flakiness when the engine is multi-
// goroutine.
//
// Counters: callers can read replicated / listed / streamed to assert
// engine behaviour. failPut / failStream let a test inject a per-call
// error without rebuilding the driver.
type fakeDriver struct {
	id   string
	mu   sync.Mutex
	data map[string]map[string]fakeObject // bucket -> key -> obj

	listCount   atomic.Int64
	headCount   atomic.Int64
	streamCount atomic.Int64
	putCount    atomic.Int64

	failPut    atomic.Bool
	failStream atomic.Bool
	failList   atomic.Bool
}

func newFakeDriver(id string) *fakeDriver {
	return &fakeDriver{
		id:   id,
		data: map[string]map[string]fakeObject{},
	}
}

// seed puts an object into the fake driver bypassing any failure
// injection — used by tests to populate the primary's source data.
func (d *fakeDriver) seed(bucket, key string, body []byte, lastModified time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.data[bucket]; !ok {
		d.data[bucket] = map[string]fakeObject{}
	}
	d.data[bucket][key] = fakeObject{
		body:         body,
		contentType:  "application/octet-stream",
		etag:         fmt.Sprintf("etag-%s-%d", key, len(body)),
		lastModified: lastModified,
	}
}

// has returns true when the fake driver has an object at (bucket, key).
func (d *fakeDriver) has(bucket, key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.data[bucket][key]
	return ok
}

func (d *fakeDriver) Capabilities(_ context.Context) (Capabilities, error) {
	return Capabilities{Driver: d.id, ServerSideCopy: false}, nil
}

func (d *fakeDriver) ListObjects(_ context.Context, bucket, _ string, _ int) (ObjectPage, error) {
	d.listCount.Add(1)
	if d.failList.Load() {
		return ObjectPage{}, errors.New("fake list failure")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []ObjectInfo
	for key, obj := range d.data[bucket] {
		out = append(out, ObjectInfo{
			Key:          key,
			Size:         int64(len(obj.body)),
			ETag:         obj.etag,
			LastModified: obj.lastModified,
		})
	}
	return ObjectPage{Objects: out, IsTruncated: false}, nil
}

func (d *fakeDriver) StatObject(_ context.Context, bucket, key string) (ObjectInfo, error) {
	d.headCount.Add(1)
	d.mu.Lock()
	defer d.mu.Unlock()
	obj, ok := d.data[bucket][key]
	if !ok {
		return ObjectInfo{}, errors.New("not found")
	}
	return ObjectInfo{
		Key:          key,
		Size:         int64(len(obj.body)),
		ETag:         obj.etag,
		LastModified: obj.lastModified,
	}, nil
}

func (d *fakeDriver) StreamObject(_ context.Context, bucket, key string) (StreamResult, error) {
	d.streamCount.Add(1)
	if d.failStream.Load() {
		return StreamResult{}, errors.New("fake stream failure")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	obj, ok := d.data[bucket][key]
	if !ok {
		return StreamResult{}, errors.New("not found")
	}
	return StreamResult{
		Body:          io.NopCloser(bytes.NewReader(obj.body)),
		ContentType:   obj.contentType,
		ContentLength: int64(len(obj.body)),
		ETag:          obj.etag,
		LastModified:  obj.lastModified,
	}, nil
}

func (d *fakeDriver) PutObjectStream(_ context.Context, bucket, key string, reader io.Reader, contentType string, size int64) error {
	d.putCount.Add(1)
	if d.failPut.Load() {
		return errors.New("fake put failure")
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.data[bucket]; !ok {
		d.data[bucket] = map[string]fakeObject{}
	}
	d.data[bucket][key] = fakeObject{
		body:         body,
		contentType:  contentType,
		etag:         fmt.Sprintf("etag-%s-%d", key, len(body)),
		lastModified: time.Now().UTC(),
	}
	_ = size
	return nil
}

func (d *fakeDriver) ServerSideCopy(_ context.Context, _, _, _, _ string) error {
	return errors.New("ServerSideCopy not implemented in fakeDriver")
}

// mapResolver maps regionID -> ReplicationClient. The owner field is
// ignored (tests use a single owner). Goroutine-safe.
type mapResolver struct {
	mu      sync.Mutex
	drivers map[string]ReplicationClient
}

func newMapResolver() *mapResolver {
	return &mapResolver{drivers: map[string]ReplicationClient{}}
}

func (m *mapResolver) set(regionID string, d ReplicationClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.drivers[regionID] = d
}

func (m *mapResolver) Resolve(_ context.Context, _, regionID string) (ReplicationClient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.drivers[regionID]
	if !ok {
		return nil, fmt.Errorf("mapResolver: no driver for region %q", regionID)
	}
	return d, nil
}

// recordingAudit captures every Log call so tests can assert audit
// emission. Mutex-guarded; safe under engine concurrency.
type recordingAudit struct {
	mu     sync.Mutex
	events []audit.Event
}

func newRecordingAudit() *recordingAudit { return &recordingAudit{} }

func (r *recordingAudit) Log(e audit.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recordingAudit) Query(_, _ time.Time, _ audit.QueryFilter) ([]audit.Event, error) {
	return nil, nil
}
func (r *recordingAudit) QueryWithTotal(_, _ time.Time, _ audit.QueryFilter) ([]audit.Event, int, error) {
	return nil, 0, nil
}

func (r *recordingAudit) snapshot() []audit.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]audit.Event, len(r.events))
	copy(out, r.events)
	return out
}

// countByAction returns the number of audit events with the given action.
func (r *recordingAudit) countByAction(action string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int
	for _, e := range r.events {
		if e.Action == action {
			n++
		}
	}
	return n
}

// newTestEngine wires up an engine with a memory store + fakes. Returns
// the engine, the store, the resolver, and the audit recorder so tests
// can drive them as needed.
func newTestEngine(t *testing.T) (*Engine, FederatedBuckets, *mapResolver, *recordingAudit) {
	t.Helper()
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	res := newMapResolver()
	rec := newRecordingAudit()
	e := NewEngine(st, res, rec, nil)
	// Very short tick so tests don't wait 10s when they want to assert
	// a multi-tick behaviour. Individual tests can override.
	e.SetTickInterval(20 * time.Millisecond)
	e.SetWorkers(2)
	return e, st, res, rec
}

// waitFor polls until check returns true or the deadline expires.
// Test helper for asserting engine eventually-converges semantics.
func waitFor(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitFor: condition never satisfied within %v", timeout)
}

// TestEngine_PerFederationGoroutine: Start with N federations launches
// exactly N goroutine slots in the engine's tracking map.
func TestEngine_PerFederationGoroutine(t *testing.T) {
	e, st, res, _ := newTestEngine(t)
	ctx := context.Background()

	primary := newFakeDriver("primary")
	res.set("region-primary", primary)
	res.set("region-r1", newFakeDriver("r1"))
	res.set("region-r2", newFakeDriver("r2"))

	for i := 0; i < 3; i++ {
		_, err := st.Create(ctx, FederatedBucket{
			OwnerUserID: "matthew",
			Name:        fmt.Sprintf("fed-%d", i),
			Primary:     ReplicaTarget{RegionID: "region-primary", Bucket: fmt.Sprintf("p-%d", i)},
			Replicas:    []ReplicaTarget{{RegionID: "region-r1", Bucket: fmt.Sprintf("r-%d", i)}},
			Policy:      DefaultPolicy(),
		})
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	e.Start(ctx)
	defer e.Stop()

	if got := e.LoopCount(); got != 3 {
		t.Fatalf("expected 3 federation loops, got %d", got)
	}
}

// TestEngine_TickTriggersReplication: a federation with two primary
// objects replicates them to the replica on the first tick. After
// success the replica has the same objects + size + counts.
func TestEngine_TickTriggersReplication(t *testing.T) {
	e, st, res, rec := newTestEngine(t)
	ctx := context.Background()

	primary := newFakeDriver("primary")
	replica := newFakeDriver("replica")
	res.set("region-primary", primary)
	res.set("region-replica", replica)

	primary.seed("p-bucket", "alpha.txt", []byte("alpha body"), time.Now().UTC())
	primary.seed("p-bucket", "beta.txt", []byte("beta body!"), time.Now().UTC())

	fb, err := st.Create(ctx, FederatedBucket{
		OwnerUserID: "matthew",
		Name:        "fed",
		Primary:     ReplicaTarget{RegionID: "region-primary", Bucket: "p-bucket"},
		Replicas:    []ReplicaTarget{{RegionID: "region-replica", Bucket: "r-bucket"}},
		Policy:      DefaultPolicy(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	e.Start(ctx)
	defer e.Stop()

	waitFor(t, 2*time.Second, func() bool {
		return replica.has("r-bucket", "alpha.txt") && replica.has("r-bucket", "beta.txt")
	})

	if replicated := rec.countByAction("federation:replicate_object"); replicated < 2 {
		t.Fatalf("expected at least 2 federation:replicate_object audit events, got %d", replicated)
	}

	// Replica health should be in-sync with zero lag.
	got, err := st.Get(ctx, fb.ID)
	if err != nil {
		t.Fatalf("Get post-tick: %v", err)
	}
	if len(got.Replicas) != 1 {
		t.Fatalf("expected 1 replica, got %d", len(got.Replicas))
	}
	r := got.Replicas[0]
	if r.Health != HealthInSync {
		t.Fatalf("expected health=in-sync, got %q", r.Health)
	}
	if r.LagObjects != 0 || r.LagBytes != 0 {
		t.Fatalf("expected zero lag after sync, got objects=%d bytes=%d", r.LagObjects, r.LagBytes)
	}
}

// TestEngine_HealthCalculation: table-driven check that ComputeHealth
// maps (lastSync, now, lagAlertSec, failureCount) -> health correctly.
func TestEngine_HealthCalculation(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		lastSync     time.Time
		lagAlertSec  int
		failureCount int
		want         string
	}{
		{
			name:        "zero lastSync renders in-sync",
			lastSync:    time.Time{},
			lagAlertSec: 300,
			want:        HealthInSync,
		},
		{
			name:        "within threshold",
			lastSync:    now.Add(-60 * time.Second),
			lagAlertSec: 300,
			want:        HealthInSync,
		},
		{
			name:        "exactly threshold",
			lastSync:    now.Add(-300 * time.Second),
			lagAlertSec: 300,
			want:        HealthInSync,
		},
		{
			name:        "slightly over threshold",
			lastSync:    now.Add(-301 * time.Second),
			lagAlertSec: 300,
			want:        HealthLagging,
		},
		{
			name:        "over 10x threshold -> stale",
			lastSync:    now.Add(-3001 * time.Second),
			lagAlertSec: 300,
			want:        HealthStale,
		},
		{
			name:         "broken regardless of lag",
			lastSync:     now.Add(-1 * time.Second),
			lagAlertSec:  300,
			failureCount: BrokenFailureThreshold,
			want:         HealthBroken,
		},
		{
			name:         "two failures still lagging not broken",
			lastSync:     now.Add(-301 * time.Second),
			lagAlertSec:  300,
			failureCount: BrokenFailureThreshold - 1,
			want:         HealthLagging,
		},
		{
			name:        "zero lagAlertSec is always in-sync",
			lastSync:    now.Add(-1 * time.Hour),
			lagAlertSec: 0,
			want:        HealthInSync,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeHealth(tc.lastSync, now, tc.lagAlertSec, tc.failureCount)
			if got != tc.want {
				t.Fatalf("ComputeHealth(%v, %v, %d, %d) = %q, want %q",
					tc.lastSync, now, tc.lagAlertSec, tc.failureCount, got, tc.want)
			}
		})
	}
}

// TestEngine_ReplicateErrorIncrementsFailCount: a replica whose driver
// errors on PutObjectStream accumulates consecutive failures; on the
// third the health flips to "broken".
func TestEngine_ReplicateErrorIncrementsFailCount(t *testing.T) {
	e, st, res, _ := newTestEngine(t)
	ctx := context.Background()

	primary := newFakeDriver("primary")
	replica := newFakeDriver("replica")
	replica.failPut.Store(true) // every PUT fails
	res.set("region-primary", primary)
	res.set("region-replica", replica)

	primary.seed("p-bucket", "fail.txt", []byte("data"), time.Now().UTC())

	fb, err := st.Create(ctx, FederatedBucket{
		OwnerUserID: "matthew",
		Name:        "fed",
		Primary:     ReplicaTarget{RegionID: "region-primary", Bucket: "p-bucket"},
		Replicas:    []ReplicaTarget{{RegionID: "region-replica", Bucket: "r-bucket"}},
		Policy:      DefaultPolicy(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Drive ticks manually so we don't race the broken-after-3 boundary.
	rt := ReplicaTarget{RegionID: "region-replica", Bucket: "r-bucket"}
	for i := 1; i <= BrokenFailureThreshold; i++ {
		e.tickFederation(ctx, fb.ID)
		if got := e.FailureCount(fb.ID, rt); got != i {
			t.Fatalf("after tick %d expected failure count %d, got %d", i, i, got)
		}
	}

	got, err := st.Get(ctx, fb.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Replicas[0].Health != HealthBroken {
		t.Fatalf("expected health=broken after %d failures, got %q",
			BrokenFailureThreshold, got.Replicas[0].Health)
	}

	// A successful tick (PUT no longer fails) resets the failure count
	// and restores in-sync.
	replica.failPut.Store(false)
	e.tickFederation(ctx, fb.ID)
	if got := e.FailureCount(fb.ID, rt); got != 0 {
		t.Fatalf("expected failure count to reset on success, got %d", got)
	}
	got, _ = st.Get(ctx, fb.ID)
	if got.Replicas[0].Health != HealthInSync {
		t.Fatalf("expected health=in-sync after recovery, got %q", got.Replicas[0].Health)
	}
}

// TestEngine_TriggerNowImmediate: TriggerNow fires a tick without
// waiting for the next scheduled wake.
func TestEngine_TriggerNowImmediate(t *testing.T) {
	e, st, res, _ := newTestEngine(t)
	// Make the tick rare so the test fails if TriggerNow is a no-op.
	e.SetTickInterval(1 * time.Hour)

	ctx := context.Background()

	primary := newFakeDriver("primary")
	replica := newFakeDriver("replica")
	res.set("region-primary", primary)
	res.set("region-replica", replica)

	primary.seed("p-bucket", "trigger.txt", []byte("data"), time.Now().UTC())

	fb, err := st.Create(ctx, FederatedBucket{
		OwnerUserID: "matthew",
		Name:        "fed",
		Primary:     ReplicaTarget{RegionID: "region-primary", Bucket: "p-bucket"},
		Replicas:    []ReplicaTarget{{RegionID: "region-replica", Bucket: "r-bucket"}},
		Policy:      DefaultPolicy(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	e.Start(ctx)
	defer e.Stop()

	// The initial boot tick will catch this. Add another object after
	// the engine has settled, then TriggerNow + assert it landed
	// without waiting an hour.
	waitFor(t, 2*time.Second, func() bool {
		return replica.has("r-bucket", "trigger.txt")
	})

	primary.seed("p-bucket", "after.txt", []byte("after"), time.Now().UTC().Add(time.Hour))

	if err := e.TriggerNow(fb.ID); err != nil {
		t.Fatalf("TriggerNow: %v", err)
	}

	waitFor(t, 2*time.Second, func() bool {
		return replica.has("r-bucket", "after.txt")
	})
}

// TestEngine_StopWaitsForInflight: Stop blocks until in-flight
// replicates finish. We stall a put via a gate channel and assert Stop
// doesn't return before the goroutine releases.
func TestEngine_StopWaitsForInflight(t *testing.T) {
	e, st, res, _ := newTestEngine(t)
	ctx := context.Background()

	primary := newFakeDriver("primary")
	replica := &slowDriver{
		fakeDriver: newFakeDriver("replica"),
		gate:       make(chan struct{}),
	}
	res.set("region-primary", primary)
	res.set("region-replica", replica)

	primary.seed("p-bucket", "slow.txt", []byte("slow body"), time.Now().UTC())

	_, err := st.Create(ctx, FederatedBucket{
		OwnerUserID: "matthew",
		Name:        "fed",
		Primary:     ReplicaTarget{RegionID: "region-primary", Bucket: "p-bucket"},
		Replicas:    []ReplicaTarget{{RegionID: "region-replica", Bucket: "r-bucket"}},
		Policy:      DefaultPolicy(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	e.Start(ctx)

	// Wait for the slow put to begin.
	waitFor(t, 1*time.Second, func() bool {
		return replica.putStarted.Load()
	})

	stopped := make(chan struct{})
	go func() {
		e.Stop()
		close(stopped)
	}()

	// Stop should NOT return while the put is blocked.
	select {
	case <-stopped:
		t.Fatalf("Stop returned before in-flight put released")
	case <-time.After(150 * time.Millisecond):
	}

	// Release the gate; Stop should now return promptly.
	close(replica.gate)
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatalf("Stop did not return after in-flight put released")
	}
}

// slowDriver is a fakeDriver that blocks PutObjectStream on a gate
// channel — used by TestEngine_StopWaitsForInflight.
type slowDriver struct {
	*fakeDriver
	gate       chan struct{}
	putStarted atomic.Bool
}

func (s *slowDriver) PutObjectStream(ctx context.Context, bucket, key string, reader io.Reader, contentType string, size int64) error {
	s.putStarted.Store(true)
	select {
	case <-s.gate:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.fakeDriver.PutObjectStream(ctx, bucket, key, reader, contentType, size)
}

// TestEngine_ScheduledModeSkippedByPollingLoop: a federation with
// SyncMode=scheduled should NOT trigger any replication in v1.6.0b.
func TestEngine_ScheduledModeSkippedByPollingLoop(t *testing.T) {
	e, st, res, rec := newTestEngine(t)
	ctx := context.Background()

	primary := newFakeDriver("primary")
	replica := newFakeDriver("replica")
	res.set("region-primary", primary)
	res.set("region-replica", replica)

	primary.seed("p-bucket", "noisy.txt", []byte("data"), time.Now().UTC())

	scheduledPolicy := DefaultPolicy()
	scheduledPolicy.SyncMode = SyncModeScheduled
	scheduledPolicy.Schedule = "@hourly"

	_, err := st.Create(ctx, FederatedBucket{
		OwnerUserID: "matthew",
		Name:        "scheduled",
		Primary:     ReplicaTarget{RegionID: "region-primary", Bucket: "p-bucket"},
		Replicas:    []ReplicaTarget{{RegionID: "region-replica", Bucket: "r-bucket"}},
		Policy:      scheduledPolicy,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	e.Start(ctx)
	defer e.Stop()

	// Give the engine more than one tick interval of wall-clock time
	// to NOT replicate. 100ms covers many of the 20ms test ticks.
	time.Sleep(100 * time.Millisecond)

	if replica.has("r-bucket", "noisy.txt") {
		t.Fatalf("scheduled mode federation should not be replicated by the polling loop")
	}
	if got := rec.countByAction("federation:replicate_object"); got != 0 {
		t.Fatalf("expected zero replicate_object audit events for scheduled mode, got %d", got)
	}
}

// TestEngine_LastSyncFiltersOlderObjects: objects modified BEFORE the
// replica's LastSync are not HEAD-checked or replicated again. Lets
// the steady-state engine skip the dominant cost on a quiescent bucket.
func TestEngine_LastSyncFiltersOlderObjects(t *testing.T) {
	e, st, res, _ := newTestEngine(t)
	ctx := context.Background()

	primary := newFakeDriver("primary")
	replica := newFakeDriver("replica")
	res.set("region-primary", primary)
	res.set("region-replica", replica)

	old := time.Now().UTC().Add(-1 * time.Hour)
	primary.seed("p-bucket", "old.txt", []byte("old"), old)

	fb, err := st.Create(ctx, FederatedBucket{
		OwnerUserID: "matthew",
		Name:        "fed",
		Primary:     ReplicaTarget{RegionID: "region-primary", Bucket: "p-bucket"},
		Replicas: []ReplicaTarget{{
			RegionID: "region-replica",
			Bucket:   "r-bucket",
			LastSync: time.Now().UTC(), // already past everything
		}},
		Policy: DefaultPolicy(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Pre-populate the LastSync via UpdateReplicaHealth so the store's
	// view matches our intent.
	if err := st.UpdateReplicaHealth(ctx, fb.ID, "region-replica", "r-bucket", ReplicaTarget{
		LastSync: time.Now().UTC(),
		Health:   HealthInSync,
	}); err != nil {
		t.Fatalf("UpdateReplicaHealth: %v", err)
	}

	headsBefore := replica.headCount.Load()
	e.tickFederation(ctx, fb.ID)
	headsAfter := replica.headCount.Load()

	if headsAfter != headsBefore {
		t.Fatalf("expected zero HEAD calls on quiescent bucket, got %d new heads",
			headsAfter-headsBefore)
	}
	if replica.has("r-bucket", "old.txt") {
		t.Fatalf("old object should not be replicated when LastSync covers it")
	}
}

// TestEngine_EnsureLoop: calling EnsureLoop after a federation is
// created spawns a loop for it without restarting the engine. Calling
// it twice is a no-op.
func TestEngine_EnsureLoop(t *testing.T) {
	e, st, res, _ := newTestEngine(t)
	ctx := context.Background()

	res.set("region-primary", newFakeDriver("primary"))
	res.set("region-replica", newFakeDriver("replica"))

	e.Start(ctx)
	defer e.Stop()

	if got := e.LoopCount(); got != 0 {
		t.Fatalf("fresh engine should have 0 loops, got %d", got)
	}

	fb, err := st.Create(ctx, FederatedBucket{
		OwnerUserID: "matthew",
		Name:        "fed",
		Primary:     ReplicaTarget{RegionID: "region-primary", Bucket: "p-bucket"},
		Replicas:    []ReplicaTarget{{RegionID: "region-replica", Bucket: "r-bucket"}},
		Policy:      DefaultPolicy(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	e.EnsureLoop(ctx, fb.ID)
	if got := e.LoopCount(); got != 1 {
		t.Fatalf("after EnsureLoop expected 1 loop, got %d", got)
	}

	// Calling again is a no-op (no duplicate goroutine).
	e.EnsureLoop(ctx, fb.ID)
	if got := e.LoopCount(); got != 1 {
		t.Fatalf("EnsureLoop should be idempotent, got %d after second call", got)
	}

	e.RemoveLoop(fb.ID)
	if got := e.LoopCount(); got != 0 {
		t.Fatalf("after RemoveLoop expected 0 loops, got %d", got)
	}
}

// TestEngine_StopIsIdempotent: a second Stop call is a no-op.
func TestEngine_StopIsIdempotent(t *testing.T) {
	e, _, _, _ := newTestEngine(t)
	e.Start(context.Background())
	e.Stop()
	e.Stop() // must not panic / block
}

// TestEngine_TriggerNowOnUnknownFederation: an ad-hoc trigger for a
// federation the engine doesn't know about errors cleanly rather than
// panicking.
func TestEngine_TriggerNowOnUnknownFederation(t *testing.T) {
	e, _, _, _ := newTestEngine(t)
	e.Start(context.Background())
	defer e.Stop()

	err := e.TriggerNow("does-not-exist")
	if err == nil {
		t.Fatalf("expected error for TriggerNow on unknown federation, got nil")
	}
	if !strings.Contains(err.Error(), "no running engine loop") {
		t.Fatalf("expected 'no running engine loop' error, got %v", err)
	}
}

// gatedDriver is a fakeDriver that fails ListObjects whenever the
// failList gate is held. Used by the watchdog tests to simulate a
// primary that's "unreachable" — the watchdog probes via ListObjects
// so failing that call is the test's only required injection point.
//
// Wraps the underlying fakeDriver so seed / has / per-call counters
// keep working; only the ListObjects path is interposed.
type gatedDriver struct {
	*fakeDriver
}

func newGatedDriver(id string) *gatedDriver {
	return &gatedDriver{fakeDriver: newFakeDriver(id)}
}

// TestWatchdog_NotSpawnedWhenPolicyDisabled: a federation with
// Policy.AutoFailover=false should never produce a watchdog goroutine.
func TestWatchdog_NotSpawnedWhenPolicyDisabled(t *testing.T) {
	e, st, res, _ := newTestEngine(t)
	ctx := context.Background()

	res.set("region-primary", newFakeDriver("primary"))
	res.set("region-replica", newFakeDriver("replica"))

	policy := DefaultPolicy()
	policy.AutoFailover = false // explicit — make the test's intent obvious
	_, err := st.Create(ctx, FederatedBucket{
		OwnerUserID: "matthew",
		Name:        "fed",
		Primary:     ReplicaTarget{RegionID: "region-primary", Bucket: "p-bucket"},
		Replicas:    []ReplicaTarget{{RegionID: "region-replica", Bucket: "r-bucket"}},
		Policy:      policy,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	e.Start(ctx)
	defer e.Stop()

	if got := e.WatchdogCount(); got != 0 {
		t.Fatalf("expected 0 watchdogs when policy disabled, got %d", got)
	}
}

// TestWatchdog_FailingPrimaryTriggersFailover: a federation with
// AutoFailover=true whose primary errors on probe should auto-promote
// the (sole) replica after AutoFailoverSec / watchdogInterval
// consecutive failures.
func TestWatchdog_FailingPrimaryTriggersFailover(t *testing.T) {
	e, st, res, rec := newTestEngine(t)
	// Sub-second watchdog interval so the test finishes quickly. With
	// watchdogInterval=20ms and AutoFailoverSec=1, the watchdog needs
	// 1/1=1 (clamped to 1) failure -> failover after the first probe.
	// We bump AutoFailoverSec to 1s = 50 probes at 20ms to make the
	// "counts up over time" semantics visible.
	e.SetWatchdogInterval(20 * time.Millisecond)
	ctx := context.Background()

	primary := newGatedDriver("primary")
	replica := newFakeDriver("replica")
	res.set("region-primary", primary)
	res.set("region-replica", replica)

	// Primary is broken from the start.
	primary.failList.Store(true)
	primary.failStream.Store(true)

	policy := DefaultPolicy()
	policy.AutoFailover = true
	policy.AutoFailoverSec = 1 // 1s of failures -> 50 probes at 20ms

	fb, err := st.Create(ctx, FederatedBucket{
		OwnerUserID: "matthew",
		Name:        "fed",
		Primary:     ReplicaTarget{RegionID: "region-primary", Bucket: "p-bucket"},
		Replicas:    []ReplicaTarget{{RegionID: "region-replica", Bucket: "r-bucket"}},
		Policy:      policy,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	e.Start(ctx)
	defer e.Stop()

	if got := e.WatchdogCount(); got != 1 {
		t.Fatalf("expected 1 watchdog at boot, got %d", got)
	}

	// Wait for the failover to materialise: store's primary should now
	// point at region-replica/r-bucket. 5s deadline covers 1s of probes
	// + scheduling slack.
	waitFor(t, 5*time.Second, func() bool {
		got, err := st.Get(ctx, fb.ID)
		if err != nil {
			return false
		}
		return got.Primary.RegionID == "region-replica" && got.Primary.Bucket == "r-bucket"
	})

	got, err := st.Get(ctx, fb.ID)
	if err != nil {
		t.Fatalf("Get post-failover: %v", err)
	}
	if got.Primary.RegionID != "region-replica" || got.Primary.Bucket != "r-bucket" {
		t.Fatalf("primary should have failed over to region-replica/r-bucket, got %s/%s",
			got.Primary.RegionID, got.Primary.Bucket)
	}
	if len(got.Replicas) != 1 {
		t.Fatalf("expected 1 replica after swap, got %d", len(got.Replicas))
	}
	if got.Replicas[0].RegionID != "region-primary" || got.Replicas[0].Bucket != "p-bucket" {
		t.Fatalf("demoted primary should be a replica, got %+v", got.Replicas[0])
	}

	// Audit event check happens in TestWatchdog_AuditEventEmitted below,
	// but assert at least one auto_failover record was emitted here so
	// a partial failure shows up at the obvious test rather than the
	// dedicated audit test.
	if rec.countByAction("federation:auto_failover") == 0 {
		t.Fatalf("expected at least one federation:auto_failover audit event")
	}
}

// TestWatchdog_HealthyPrimaryDoesNothing: a federation with
// AutoFailover=true whose primary is healthy should never auto-failover,
// and the consecutive-failures counter should stay reset.
func TestWatchdog_HealthyPrimaryDoesNothing(t *testing.T) {
	e, st, res, rec := newTestEngine(t)
	e.SetWatchdogInterval(10 * time.Millisecond)
	ctx := context.Background()

	primary := newFakeDriver("primary")
	replica := newFakeDriver("replica")
	res.set("region-primary", primary)
	res.set("region-replica", replica)

	policy := DefaultPolicy()
	policy.AutoFailover = true
	policy.AutoFailoverSec = 60 // never reach via the test wall-clock window

	fb, err := st.Create(ctx, FederatedBucket{
		OwnerUserID: "matthew",
		Name:        "fed",
		Primary:     ReplicaTarget{RegionID: "region-primary", Bucket: "p-bucket"},
		Replicas:    []ReplicaTarget{{RegionID: "region-replica", Bucket: "r-bucket"}},
		Policy:      policy,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	e.Start(ctx)
	defer e.Stop()

	// Let the watchdog probe ~20+ times. Healthy probes should reset
	// the counter on every loop, so no auto-failover.
	time.Sleep(250 * time.Millisecond)

	got, err := st.Get(ctx, fb.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Primary.RegionID != "region-primary" || got.Primary.Bucket != "p-bucket" {
		t.Fatalf("primary should remain unchanged on healthy probe, got %s/%s",
			got.Primary.RegionID, got.Primary.Bucket)
	}
	if rec.countByAction("federation:auto_failover") != 0 {
		t.Fatalf("expected zero auto_failover audit events on healthy primary, got %d",
			rec.countByAction("federation:auto_failover"))
	}
	if rec.countByAction("federation:auto_failover_skipped") != 0 {
		t.Fatalf("expected zero auto_failover_skipped events on healthy primary, got %d",
			rec.countByAction("federation:auto_failover_skipped"))
	}
}

// TestWatchdog_NoFailoverWhenAllReplicasBroken: primary fails AND every
// replica is marked Broken -> watchdog emits federation:auto_failover_skipped
// and leaves the primary in place.
func TestWatchdog_NoFailoverWhenAllReplicasBroken(t *testing.T) {
	e, st, res, rec := newTestEngine(t)
	e.SetWatchdogInterval(20 * time.Millisecond)
	ctx := context.Background()

	primary := newGatedDriver("primary")
	replica := newFakeDriver("replica")
	res.set("region-primary", primary)
	res.set("region-replica", replica)

	primary.failList.Store(true)

	policy := DefaultPolicy()
	policy.AutoFailover = true
	policy.AutoFailoverSec = 1

	fb, err := st.Create(ctx, FederatedBucket{
		OwnerUserID: "matthew",
		Name:        "fed",
		Primary:     ReplicaTarget{RegionID: "region-primary", Bucket: "p-bucket"},
		Replicas: []ReplicaTarget{{
			RegionID: "region-replica",
			Bucket:   "r-bucket",
			Health:   HealthBroken,
		}},
		Policy: policy,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Persist the broken health via UpdateReplicaHealth so the store
	// view matches the test's intent — Create doesn't trust caller-
	// supplied health fields on the engine path but does on the data path.
	if err := st.UpdateReplicaHealth(ctx, fb.ID, "region-replica", "r-bucket", ReplicaTarget{
		Health: HealthBroken,
	}); err != nil {
		t.Fatalf("UpdateReplicaHealth: %v", err)
	}

	e.Start(ctx)
	defer e.Stop()

	// Wait for at least one auto_failover_skipped audit event.
	waitFor(t, 5*time.Second, func() bool {
		return rec.countByAction("federation:auto_failover_skipped") >= 1
	})

	got, err := st.Get(ctx, fb.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Primary.RegionID != "region-primary" || got.Primary.Bucket != "p-bucket" {
		t.Fatalf("primary should remain unchanged when no healthy replica, got %s/%s",
			got.Primary.RegionID, got.Primary.Bucket)
	}
	if rec.countByAction("federation:auto_failover") != 0 {
		t.Fatalf("expected zero auto_failover success events, got %d",
			rec.countByAction("federation:auto_failover"))
	}
}

// TestWatchdog_PicksHealthiestReplica: with multiple non-broken replicas,
// the watchdog should pick the lowest-lag one for promotion. Verified
// directly via pickHealthiestReplica so we don't have to drive the full
// engine for a pure-function check.
func TestWatchdog_PicksHealthiestReplica(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		replicas []ReplicaTarget
		wantR    string
		wantB    string
		wantOK   bool
	}{
		{
			name: "lowest lag wins",
			replicas: []ReplicaTarget{
				{RegionID: "r-a", Bucket: "ba", LastSync: now.Add(-10 * time.Minute)},
				{RegionID: "r-b", Bucket: "bb", LastSync: now.Add(-1 * time.Minute)},
				{RegionID: "r-c", Bucket: "bc", LastSync: now.Add(-5 * time.Minute)},
			},
			wantR:  "r-b",
			wantB:  "bb",
			wantOK: true,
		},
		{
			name: "broken excluded",
			replicas: []ReplicaTarget{
				{RegionID: "r-a", Bucket: "ba", LastSync: now.Add(-30 * time.Second), Health: HealthBroken},
				{RegionID: "r-b", Bucket: "bb", LastSync: now.Add(-10 * time.Minute)},
			},
			wantR:  "r-b",
			wantB:  "bb",
			wantOK: true,
		},
		{
			name: "tie broken by region asc",
			replicas: []ReplicaTarget{
				{RegionID: "r-z", Bucket: "bb", LastSync: now.Add(-1 * time.Minute)},
				{RegionID: "r-a", Bucket: "ba", LastSync: now.Add(-1 * time.Minute)},
			},
			wantR:  "r-a",
			wantB:  "ba",
			wantOK: true,
		},
		{
			name: "tie broken by bucket asc when region equal",
			replicas: []ReplicaTarget{
				{RegionID: "r-a", Bucket: "bz", LastSync: now.Add(-1 * time.Minute)},
				{RegionID: "r-a", Bucket: "ba", LastSync: now.Add(-1 * time.Minute)},
			},
			wantR:  "r-a",
			wantB:  "ba",
			wantOK: true,
		},
		{
			name: "seen wins over never-seen",
			replicas: []ReplicaTarget{
				{RegionID: "r-a", Bucket: "ba"}, // zero LastSync
				{RegionID: "r-b", Bucket: "bb", LastSync: now.Add(-1 * time.Hour)},
			},
			wantR:  "r-b",
			wantB:  "bb",
			wantOK: true,
		},
		{
			name: "all broken returns no candidate",
			replicas: []ReplicaTarget{
				{RegionID: "r-a", Bucket: "ba", Health: HealthBroken},
				{RegionID: "r-b", Bucket: "bb", Health: HealthBroken},
			},
			wantOK: false,
		},
		{
			name:     "empty replicas",
			replicas: nil,
			wantOK:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fb := FederatedBucket{Replicas: tc.replicas}
			got, ok := pickHealthiestReplica(fb, now)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if got.RegionID != tc.wantR || got.Bucket != tc.wantB {
				t.Fatalf("got %s/%s, want %s/%s",
					got.RegionID, got.Bucket, tc.wantR, tc.wantB)
			}
		})
	}
}

// TestWatchdog_PolicyToggleSpawnsAndStops: flipping Policy.AutoFailover
// via Update + EnsureLoop should spawn the watchdog when toggled on
// and stop it when toggled off.
func TestWatchdog_PolicyToggleSpawnsAndStops(t *testing.T) {
	e, st, res, _ := newTestEngine(t)
	e.SetWatchdogInterval(20 * time.Millisecond)
	ctx := context.Background()

	res.set("region-primary", newFakeDriver("primary"))
	res.set("region-replica", newFakeDriver("replica"))

	policy := DefaultPolicy()
	policy.AutoFailover = false

	fb, err := st.Create(ctx, FederatedBucket{
		OwnerUserID: "matthew",
		Name:        "fed",
		Primary:     ReplicaTarget{RegionID: "region-primary", Bucket: "p-bucket"},
		Replicas:    []ReplicaTarget{{RegionID: "region-replica", Bucket: "r-bucket"}},
		Policy:      policy,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	e.Start(ctx)
	defer e.Stop()

	if e.HasWatchdog(fb.ID) {
		t.Fatalf("watchdog should not be running with policy disabled")
	}

	// Flip policy to true + run EnsureLoop -> watchdog spawns.
	enabledPolicy := policy
	enabledPolicy.AutoFailover = true
	enabledPolicy.AutoFailoverSec = 60
	if _, err := st.Update(ctx, fb.ID, FederatedBucket{
		Name:     fb.Name,
		Primary:  fb.Primary,
		Replicas: fb.Replicas,
		Policy:   enabledPolicy,
	}); err != nil {
		t.Fatalf("Update on: %v", err)
	}
	e.EnsureLoop(ctx, fb.ID)
	if !e.HasWatchdog(fb.ID) {
		t.Fatalf("watchdog should be running after policy flip to true")
	}

	// Flip policy back to false + run EnsureLoop -> watchdog stops.
	disabledPolicy := enabledPolicy
	disabledPolicy.AutoFailover = false
	if _, err := st.Update(ctx, fb.ID, FederatedBucket{
		Name:     fb.Name,
		Primary:  fb.Primary,
		Replicas: fb.Replicas,
		Policy:   disabledPolicy,
	}); err != nil {
		t.Fatalf("Update off: %v", err)
	}
	e.EnsureLoop(ctx, fb.ID)
	if e.HasWatchdog(fb.ID) {
		t.Fatalf("watchdog should stop after policy flip to false")
	}
}

// TestWatchdog_AuditEventEmitted: a successful auto-failover writes a
// federation:auto_failover audit event with the old + new primary +
// reason in the detail.
func TestWatchdog_AuditEventEmitted(t *testing.T) {
	e, st, res, rec := newTestEngine(t)
	e.SetWatchdogInterval(20 * time.Millisecond)
	ctx := context.Background()

	primary := newGatedDriver("primary")
	replica := newFakeDriver("replica")
	res.set("region-primary", primary)
	res.set("region-replica", replica)

	primary.failList.Store(true)

	policy := DefaultPolicy()
	policy.AutoFailover = true
	policy.AutoFailoverSec = 1

	fb, err := st.Create(ctx, FederatedBucket{
		OwnerUserID: "matthew",
		Name:        "fed",
		Primary:     ReplicaTarget{RegionID: "region-primary", Bucket: "p-bucket"},
		Replicas:    []ReplicaTarget{{RegionID: "region-replica", Bucket: "r-bucket"}},
		Policy:      policy,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	e.Start(ctx)
	defer e.Stop()

	waitFor(t, 5*time.Second, func() bool {
		return rec.countByAction("federation:auto_failover") >= 1
	})

	var found bool
	for _, ev := range rec.snapshot() {
		if ev.Action != "federation:auto_failover" {
			continue
		}
		if ev.Resource != "federation:"+fb.ID {
			t.Fatalf("audit resource mismatch: got %q, want federation:%s", ev.Resource, fb.ID)
		}
		if ev.Result != audit.ResultSuccess {
			t.Fatalf("audit result should be success, got %q", ev.Result)
		}
		if ev.Actor != "matthew" {
			t.Fatalf("audit actor mismatch: got %q, want matthew", ev.Actor)
		}
		// Detail should contain old + new primary + reason.
		for _, want := range []string{
			"old_primary=region-primary:p-bucket",
			"new_primary=region-replica:r-bucket",
			"reason=primary unreachable for 1s",
		} {
			if !strings.Contains(ev.Detail, want) {
				t.Errorf("audit detail %q missing %q", ev.Detail, want)
			}
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("federation:auto_failover audit event never landed")
	}
}

// TestWatchdog_RemoveLoopStopsWatchdog: RemoveLoop on a federation with
// an active watchdog should also tear down the watchdog goroutine.
func TestWatchdog_RemoveLoopStopsWatchdog(t *testing.T) {
	e, st, res, _ := newTestEngine(t)
	e.SetWatchdogInterval(20 * time.Millisecond)
	ctx := context.Background()

	res.set("region-primary", newFakeDriver("primary"))
	res.set("region-replica", newFakeDriver("replica"))

	policy := DefaultPolicy()
	policy.AutoFailover = true
	policy.AutoFailoverSec = 60

	fb, err := st.Create(ctx, FederatedBucket{
		OwnerUserID: "matthew",
		Name:        "fed",
		Primary:     ReplicaTarget{RegionID: "region-primary", Bucket: "p-bucket"},
		Replicas:    []ReplicaTarget{{RegionID: "region-replica", Bucket: "r-bucket"}},
		Policy:      policy,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	e.Start(ctx)
	defer e.Stop()

	if !e.HasWatchdog(fb.ID) {
		t.Fatalf("watchdog should be running at boot when policy enabled")
	}

	e.RemoveLoop(fb.ID)
	if e.HasWatchdog(fb.ID) {
		t.Fatalf("RemoveLoop should stop the watchdog goroutine")
	}
	if got := e.LoopCount(); got != 0 {
		t.Fatalf("RemoveLoop should also stop the replication loop, got %d loops", got)
	}
}

// TestEngine_AuditOnReplicateFailure: a failing PUT still emits a
// federation:replicate_object audit event with result=failure.
func TestEngine_AuditOnReplicateFailure(t *testing.T) {
	e, st, res, rec := newTestEngine(t)
	ctx := context.Background()

	primary := newFakeDriver("primary")
	replica := newFakeDriver("replica")
	replica.failPut.Store(true)
	res.set("region-primary", primary)
	res.set("region-replica", replica)

	primary.seed("p-bucket", "fail.txt", []byte("data"), time.Now().UTC())

	fb, err := st.Create(ctx, FederatedBucket{
		OwnerUserID: "matthew",
		Name:        "fed",
		Primary:     ReplicaTarget{RegionID: "region-primary", Bucket: "p-bucket"},
		Replicas:    []ReplicaTarget{{RegionID: "region-replica", Bucket: "r-bucket"}},
		Policy:      DefaultPolicy(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	e.tickFederation(ctx, fb.ID)

	var failureEvents int
	for _, ev := range rec.snapshot() {
		if ev.Action == "federation:replicate_object" && ev.Result == audit.ResultFailure {
			failureEvents++
		}
	}
	if failureEvents == 0 {
		t.Fatalf("expected at least one failure audit event, got %d", failureEvents)
	}
}
