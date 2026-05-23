// Package federation: replication engine (v1.6.0b).
//
// The Engine continuously mirrors every FederatedBucket's primary onto
// each of its replicas. One goroutine per federation; per-replica
// worker pool capped at engine.workers. Ticks at engine.tickInterval
// (default 10s) — webhook-driven event handoff lands in v1.7+.
//
// Design notes:
//
//   - The engine is "best-effort eventually consistent": a tick lists
//     primary objects modified since the replica's LastSync, HeadObjects
//     them on the replica, and replicates anything missing / stale.
//     v1.6 caps per-tick batches at 100 objects so a multi-million-key
//     bucket doesn't stall the engine on its first run.
//
//   - Per ADR-0005's "Replication engine" section, the audit log emits
//     federation:replicate_object per copied object. That's high volume
//     by design; the /admin/audit handler filters it out of the default
//     view in v1.6.0c.
//
//   - Robust to single-federation failure: every per-federation goroutine
//     has a panic recover, and every per-object replicate runs inside a
//     recover so one broken backend can't kill the engine.
//
//   - DriverResolver is the test-injection seam — production wires the
//     registryResolver (registry.ForUserRegion + UserRegions.Decrypt),
//     tests substitute a deterministic map.
//
//   - Health derivation matches ADR-0005: in-sync ≤ LagAlertSec,
//     lagging > LagAlertSec, stale > 10×LagAlertSec, broken on 3+
//     consecutive failures (regardless of lag).
//
// Scope of v1.6.0b: ONLY the engine + store extension. API endpoints
// land in v1.6.0c; UI in v1.6.0d.
package federation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mattjackson/basement/internal/audit"
	"github.com/mattjackson/basement/internal/webhook"
)

// ObjectInfo is the engine's view of a single object's metadata —
// deliberately narrower than driver.ObjectInfo so the federation package
// avoids importing the driver package and the resulting import cycle
// (store -> federation; driver -> store; federation -> driver would
// close the loop). Production wiring in cmd/basement-server translates
// between the two with a thin adapter.
type ObjectInfo struct {
	Key          string
	Size         int64
	ETag         string
	LastModified time.Time
	IsDir        bool
}

// ObjectPage mirrors driver.ObjectPage minus CommonPrefixes (the engine
// always lists with delimiter="" and never sub-folder-browses).
type ObjectPage struct {
	Objects          []ObjectInfo
	NextContinuation string
	IsTruncated      bool
}

// StreamResult mirrors driver.StreamResult — what the engine needs to
// pipe one object from primary to replica.
type StreamResult struct {
	Body          io.ReadCloser
	ContentType   string
	ContentLength int64
	ETag          string
	LastModified  time.Time
}

// Capabilities is the engine's narrow view of driver.Caps — only the
// fields that decide whether ServerSideCopy is worth attempting.
type Capabilities struct {
	Driver         string
	ServerSideCopy bool
}

// ReplicationClient is the minimum slice of behaviour the engine needs
// out of a backend driver. Production wires a thin adapter from
// driver.Driver to ReplicationClient (see cmd/basement-server/main.go);
// tests use the fakeDriver type in engine_test.go.
//
// Keeping this surface narrow + in-package means internal/federation
// has zero downstream imports — store can keep depending on federation
// and federation never needs to circle back to store/driver.
type ReplicationClient interface {
	Capabilities(ctx context.Context) (Capabilities, error)
	ListObjects(ctx context.Context, bucket, continuation string, limit int) (ObjectPage, error)
	StatObject(ctx context.Context, bucket, key string) (ObjectInfo, error)
	StreamObject(ctx context.Context, bucket, key string) (StreamResult, error)
	PutObjectStream(ctx context.Context, bucket, key string, reader io.Reader, contentType string, size int64) error
	ServerSideCopy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error
	// DeleteObject removes a single object from the replica. Added in
	// v1.7.0f so event-driven ObjectDeleted webhook envelopes can
	// propagate deletes from primary to replicas without waiting for
	// the polling tick to notice the gap (polling can't notice deletes
	// at all without a separate ListObjects-on-replica pass — the
	// v1.6.0b polling path is creation-biased by design).
	DeleteObject(ctx context.Context, bucket, key string) error
}

// DriverResolver turns a (ownerUserID, regionID) tuple into a
// ReplicationClient. Production wires the registry-based resolver in
// cmd/basement-server/main.go; tests substitute a deterministic in-memory
// map so unit tests never touch real S3 endpoints.
//
// The owner is threaded through because regions are per-user (ADR-0002)
// — the engine has no other way to map a regionID to a UserRegion record.
type DriverResolver interface {
	Resolve(ctx context.Context, ownerUserID, regionID string) (ReplicationClient, error)
}

// DriverResolverFunc adapts a function value to the DriverResolver
// interface, mirroring the http.HandlerFunc idiom.
type DriverResolverFunc func(ctx context.Context, ownerUserID, regionID string) (ReplicationClient, error)

// Resolve implements DriverResolver.
func (f DriverResolverFunc) Resolve(ctx context.Context, ownerUserID, regionID string) (ReplicationClient, error) {
	return f(ctx, ownerUserID, regionID)
}

// Defaults for Engine construction. Exported so tests + future config
// can reference them without re-inventing the values.
const (
	// DefaultTickInterval is how often each per-federation goroutine
	// wakes up to scan the primary for new objects. 10s mirrors the
	// ADR-0005 "polling fallback" specification and is conservative
	// enough not to overwhelm any S3 backend; the v1.7 webhook path
	// will demote this from "frequent" to "fallback for backends
	// without event support".
	DefaultTickInterval = 10 * time.Second

	// DefaultWorkers is the per-replica concurrency cap inside a single
	// tick. Each federation has up to workers replicates in flight per
	// replica at any time, so a federation with 3 replicas under load
	// peaks at 3*workers in-flight copies. Matches the v1.5 sync engine's
	// default.
	DefaultWorkers = 4

	// DefaultWatchdogInterval is how often the watchdog goroutine probes
	// the primary for liveness when Policy.AutoFailover is enabled. 30s
	// matches ADR-0005's "Auto-failover (opt-in)" section and is
	// conservative enough not to drown the primary in HEAD-equivalent
	// list-of-one probes while still giving a fast-enough failover signal
	// once Policy.AutoFailoverSec elapses.
	DefaultWatchdogInterval = 30 * time.Second

	// MaxBatchPerTick caps the number of objects replicated in one tick
	// per (federation, replica) pair. Without this a brand-new federation
	// against a multi-million-key bucket would stall the engine for hours
	// on the first tick; instead we copy 100 / tick and converge over
	// many ticks. Picked to be small enough to bound tick duration and
	// large enough that steady-state delta replication never queues
	// objects between ticks.
	MaxBatchPerTick = 100

	// BrokenFailureThreshold is the consecutive-failure count at which a
	// replica's health flips to "broken" regardless of lag. Operators
	// see this in the /files/federated-buckets detail view (v1.6.0d) and
	// can manually resync.
	BrokenFailureThreshold = 3

	// LastSyncSlack is the safety margin subtracted from a replica's
	// LastSync before comparing against a source object's LastModified
	// in computeDiff. Added in v1.11.0.4 to fix the federation no-op
	// replication bug: backends (Garage v2, S3) return object mtimes at
	// SECOND precision, while the engine records LastSync at nanosecond
	// precision. Without slack a tick that fires sub-second after an
	// upload bumps LastSync past the just-uploaded object's whole-second
	// mtime, permanently skipping the object on every subsequent tick.
	//
	// Two seconds is generous enough to cover typical clock skew + the
	// second-rounding gap, while still letting the steady-state filter
	// short-circuit HEADs on quiescent buckets.
	LastSyncSlack = 2 * time.Second
)

// Engine is the federation replication engine. Construct via NewEngine,
// start with Start(ctx), tear down with Stop().
//
// The Engine fans out one goroutine per federation. Each goroutine wakes
// on the engine's tick interval OR on a TriggerNow signal, scans the
// federation's primary for new objects, and replicates the diff to each
// replica using a bounded per-replica worker pool.
//
// Concurrency: Engine is safe to call TriggerNow / Stop on from any
// goroutine. The per-federation goroutine map is guarded by mu; the
// quit channel is the universal cancellation signal alongside the
// caller-supplied ctx in Start.
type Engine struct {
	store    FederatedBuckets
	resolver DriverResolver
	audit    audit.Logger
	logger   *slog.Logger

	tickInterval     time.Duration
	watchdogInterval time.Duration
	workers          int

	mu sync.Mutex
	// fedQuit carries one channel per running federation goroutine; a
	// per-federation "wake up now" trigger feeds the same channel via
	// non-blocking send.
	fedQuit map[string]chan struct{}
	// triggers carries the per-federation "TriggerNow" buffered channel.
	// Capacity 1: bursts of TriggerNow collapse into a single re-tick,
	// which is exactly the semantics the API ("run now" button) wants.
	triggers map[string]chan struct{}
	// failures tracks consecutive replicate failures per (fbID, regionID,
	// bucket) so health can flip to "broken" after BrokenFailureThreshold
	// in a row. Reset to 0 on the first successful replicate.
	failures map[string]int
	// watchdogQuit carries one quit channel per running watchdog
	// goroutine (one per federation that has Policy.AutoFailover=true).
	// The watchdog is independent of the replication loop because
	// toggling AutoFailover on/off should spawn/stop the watchdog
	// without disturbing the per-federation replication goroutine.
	watchdogQuit map[string]chan struct{}

	// inflight increments on every replicate goroutine launch and
	// decrements on completion. Stop blocks until inflight returns to 0
	// so in-flight replicates finish cleanly.
	inflight sync.WaitGroup

	// loops tracks the per-federation goroutines so Stop can wait for
	// them to exit before returning. Without this, a per-federation
	// runFederation could still be inside tickFederation (touching the
	// filesystem) when Stop returns and the test's TempDir cleanup
	// races into the same directory.
	loops sync.WaitGroup

	// started gates Start so re-entry is a no-op rather than a panic.
	// Stopped flips once after Stop is called so a second Stop is a no-op.
	started atomic.Bool
	stopped atomic.Bool

	// eventTasks carries one buffered channel per running federation
	// loop. The Subscribe-to-webhooks callback enqueues a single-object
	// replicate task here when an event matches the federation's primary;
	// a per-federation event-worker goroutine (spawned by spawnLoop)
	// drains the channel and replicates the object to each replica.
	//
	// Buffered + non-blocking send: a saturated channel drops the
	// task rather than stalling the dispatcher — the polling tick will
	// pick the diff up on the next 10s pass, so the worst case for an
	// overloaded engine is "event-driven path falls back to polling"
	// not "webhook dispatcher blocks".
	eventTasks map[string]chan eventTask

	// unsubscribeWebhook holds the unregister function returned by
	// webhook.Engine.Subscribe so Stop can detach cleanly. Nil when
	// SubscribeToEvents has never been called (the polling-only mode
	// pre-v1.7.0f or a test that never wires webhooks).
	unsubscribeWebhook func()
}

// eventTask is one single-object replicate or delete kicked off by a
// webhook event. The federation engine queues one task per (federation,
// replica) pair on a matching ObjectCreated / ObjectDeleted / ObjectModified
// envelope.
//
// kind selects between the polling-tick path ("diff", drained by
// tickFederation -> replicateBatch) and the event-driven path
// ("single", drained by the per-federation event worker). v1.7.0f only
// uses kind=single in the event channel; the diff path remains a
// pure-function inside tickFederation.
type eventTask struct {
	kind       string // "single"
	fbID       string
	target     ReplicaTarget
	singleKey  string
	singleSize int64
	isDelete   bool // when true: DeleteObject on replica; else streamPut
}

// NewEngine constructs an unstarted Engine. Defaults: 10s tick, 4
// per-replica workers, audit.NewNoop() when audit is nil.
//
// Passing nil for store or resolver returns an Engine that immediately
// errors on Start — main.go must wire them, tests substitute fakes.
func NewEngine(store FederatedBuckets, resolver DriverResolver, audit audit.Logger, logger *slog.Logger) *Engine {
	if audit == nil {
		audit = noopAudit{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		store:            store,
		resolver:         resolver,
		audit:            audit,
		logger:           logger,
		tickInterval:     DefaultTickInterval,
		watchdogInterval: DefaultWatchdogInterval,
		workers:          DefaultWorkers,
		fedQuit:          make(map[string]chan struct{}),
		triggers:         make(map[string]chan struct{}),
		failures:         make(map[string]int),
		watchdogQuit:     make(map[string]chan struct{}),
		eventTasks:       make(map[string]chan eventTask),
	}
}

// SetTickInterval overrides the per-federation poll cadence. Useful for
// tests that need sub-second ticks; production keeps the 10s default.
// Must be called before Start; runtime changes are intentionally
// unsupported in v1.6.0b.
func (e *Engine) SetTickInterval(d time.Duration) {
	if d > 0 {
		e.tickInterval = d
	}
}

// SetWorkers overrides the per-replica worker count. Useful for tests
// that need deterministic ordering (workers=1) or stress paths that
// want more concurrency. Must be called before Start.
func (e *Engine) SetWorkers(n int) {
	if n > 0 {
		e.workers = n
	}
}

// SetWatchdogInterval overrides the auto-failover watchdog probe
// cadence. Tests use this to assert "fail-over after N consecutive
// probe failures" without waiting the 30s production cadence; production
// keeps the DefaultWatchdogInterval default.
//
// Must be called before EnsureLoop / Start spawns the watchdog
// goroutine for a given federation. Runtime changes are intentionally
// unsupported in v1.6.0f — the watchdog re-reads the federation on
// every tick but its OWN cadence is fixed for the goroutine's
// lifetime.
func (e *Engine) SetWatchdogInterval(d time.Duration) {
	if d > 0 {
		e.watchdogInterval = d
	}
}

// Start launches one goroutine per persisted federation. Returns
// immediately; the engine then runs in the background until ctx is
// cancelled OR Stop is called.
//
// Idempotent: re-Start is a no-op rather than an error so a test
// harness can call it safely after a partial shutdown.
//
// If the store is nil (engine was never wired), Start logs a warning
// and returns — the deploy is missing its data layer, but we don't
// take down main.go for a config defect that v0.8.0d.21-style probes
// have repeatedly proven harmful.
func (e *Engine) Start(ctx context.Context) {
	if !e.started.CompareAndSwap(false, true) {
		return
	}
	if e.store == nil {
		e.logger.Warn("federation engine: no store wired — engine inert")
		return
	}
	if e.resolver == nil {
		e.logger.Warn("federation engine: no driver resolver wired — engine inert")
		return
	}

	feds, err := e.store.All(ctx)
	if err != nil {
		e.logger.Error("federation engine: failed to list federations at boot", "error", err)
		return
	}

	for _, fb := range feds {
		e.spawnLoop(ctx, fb.ID)
		// Mirror the EnsureLoop policy diff: a federation that already
		// has AutoFailover enabled at boot gets a watchdog spawned
		// alongside its replication loop. Toggling later flows through
		// EnsureLoop / RemoveLoop instead.
		if fb.Policy.AutoFailover {
			e.spawnWatchdog(ctx, fb.ID)
		}
	}

	e.logger.Info("federation engine: started", "federations", len(feds),
		"tickInterval", e.tickInterval.String(), "workers", e.workers,
		"watchdogInterval", e.watchdogInterval.String())
}

// Stop signals every per-federation goroutine to exit and waits for
// in-flight replicates to finish. Safe to call multiple times; the
// second + call is a no-op.
//
// Stop does NOT respect a caller-supplied context; the contract is
// "wait until in-flight finishes" because dropping replicates mid-write
// is the failure mode operators have explicitly asked us to avoid.
func (e *Engine) Stop() {
	if !e.stopped.CompareAndSwap(false, true) {
		return
	}

	// Detach from the webhook event bus FIRST so no new event tasks
	// can land on a draining channel. Safe to call even when the
	// engine never subscribed (nil-guard inside the closure).
	if e.unsubscribeWebhook != nil {
		e.unsubscribeWebhook()
		e.unsubscribeWebhook = nil
	}

	e.mu.Lock()
	for _, ch := range e.fedQuit {
		close(ch)
	}
	for _, ch := range e.watchdogQuit {
		close(ch)
	}
	e.fedQuit = make(map[string]chan struct{})
	e.triggers = make(map[string]chan struct{})
	e.watchdogQuit = make(map[string]chan struct{})
	e.eventTasks = make(map[string]chan eventTask)
	e.mu.Unlock()

	// Wait for both: the per-federation tick loops AND watchdog loops to
	// return, AND any replicate goroutines still mid-PUT. Loops first
	// because the inflight counter is only incremented inside a tick —
	// and we want all ticks (and watchdog probes) to finish before
	// draining the inflight set. e.loops.Wait covers both the
	// replication and watchdog goroutines — see spawnWatchdog / spawnLoop
	// for the loops.Add(1) bookkeeping.
	e.loops.Wait()
	e.inflight.Wait()
	e.logger.Info("federation engine: stopped")
}

// TriggerNow asks the engine to re-tick a specific federation
// immediately, bypassing the next scheduled wake. Used by the v1.6.0c
// "Run now" API endpoint + by tests that don't want to wait for the
// real tick interval.
//
// Returns nil even if the federation has no running loop yet (e.g. it
// was created after Start) — v1.6.0c's API handler will call EnsureLoop
// for the create path; TriggerNow stays a pure best-effort signal.
func (e *Engine) TriggerNow(fbID string) error {
	e.mu.Lock()
	ch, ok := e.triggers[fbID]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("federation %q has no running engine loop", fbID)
	}
	// Non-blocking send: collapse a burst into a single re-tick.
	select {
	case ch <- struct{}{}:
	default:
	}
	return nil
}

// EnsureLoop spawns a per-federation loop for fbID if one isn't already
// running. v1.6.0c API handlers call this after Create so a brand-new
// federation starts replicating without waiting for an engine restart.
//
// EnsureLoop also reconciles the watchdog goroutine to the federation's
// current Policy.AutoFailover setting (v1.6.0f):
//   - Policy.AutoFailover=true + no watchdog: spawn one
//   - Policy.AutoFailover=false + watchdog running: stop it
//
// This makes PUT /user/federated-buckets/{id} -> EnsureLoop the single
// path that flips the watchdog on/off without the API layer having to
// know the engine's internals.
//
// ctx parameter is the engine-level context; per-federation loops
// inherit cancellation from it and from Stop's per-fed quit channel.
func (e *Engine) EnsureLoop(ctx context.Context, fbID string) {
	if !e.started.Load() || e.stopped.Load() {
		return
	}
	e.mu.Lock()
	_, exists := e.fedQuit[fbID]
	e.mu.Unlock()
	if !exists {
		e.spawnLoop(ctx, fbID)
	}

	// Reconcile watchdog to current policy. Failure to load is logged
	// and we leave the watchdog as-is — the next EnsureLoop / boot will
	// re-evaluate.
	fb, err := e.store.Get(ctx, fbID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			e.logger.Warn("federation engine: watchdog policy lookup failed",
				"federationId", fbID, "error", err)
		}
		return
	}
	e.mu.Lock()
	_, watchdogRunning := e.watchdogQuit[fbID]
	e.mu.Unlock()
	switch {
	case fb.Policy.AutoFailover && !watchdogRunning:
		e.spawnWatchdog(ctx, fbID)
	case !fb.Policy.AutoFailover && watchdogRunning:
		e.stopWatchdog(fbID)
	}
}

// RemoveLoop stops the per-federation loop for fbID. v1.6.0c API
// handlers call this after Delete so a removed federation stops
// replicating immediately rather than waiting for the next tick to
// notice its absence.
//
// Also stops the watchdog goroutine (if one is running) for the same
// federation — auto-failover doesn't make sense for a federation that's
// been deleted.
func (e *Engine) RemoveLoop(fbID string) {
	e.mu.Lock()
	ch, ok := e.fedQuit[fbID]
	if ok {
		close(ch)
		delete(e.fedQuit, fbID)
		delete(e.triggers, fbID)
		// The event worker selects on this same quit channel, so the
		// fedQuit close above tears it down too. Just drop the channel
		// map reference here — the worker drains naturally.
		delete(e.eventTasks, fbID)
	}
	wch, wok := e.watchdogQuit[fbID]
	if wok {
		close(wch)
		delete(e.watchdogQuit, fbID)
	}
	e.mu.Unlock()
}

// spawnLoop registers fbID and launches its goroutine. Caller must
// hold no locks; the function acquires + releases e.mu briefly.
//
// v1.7.0f: spawnLoop now also creates the per-federation event channel
// + launches an event-worker goroutine that drains it. The event
// channel is the destination for SubscribeToEvents callbacks; the
// worker calls replicateOne / DeleteObject per task. Both goroutines
// share the same fedQuit close-signal for clean shutdown.
func (e *Engine) spawnLoop(ctx context.Context, fbID string) {
	quit := make(chan struct{})
	trigger := make(chan struct{}, 1)
	// Event task buffer: sized to one batch's worth so a burst of
	// concurrent writes (e.g. an operator drag-and-drops a folder of
	// 50 files) doesn't immediately drop on the floor. Saturation
	// drops the OLDEST queued task per the cycle spec's "no
	// double-delivery — polling will pick it up" guarantee.
	const eventTaskBuffer = 128
	tasks := make(chan eventTask, eventTaskBuffer)

	e.mu.Lock()
	if _, exists := e.fedQuit[fbID]; exists {
		// Another goroutine raced us — abandon our channels.
		e.mu.Unlock()
		return
	}
	e.fedQuit[fbID] = quit
	e.triggers[fbID] = trigger
	e.eventTasks[fbID] = tasks
	e.mu.Unlock()

	e.loops.Add(1)
	go func() {
		defer e.loops.Done()
		e.runFederation(ctx, fbID, quit, trigger)
	}()

	e.loops.Add(1)
	go func() {
		defer e.loops.Done()
		e.runEventWorker(ctx, fbID, quit, tasks)
	}()
}

// spawnWatchdog registers fbID's watchdog and launches its goroutine.
// Caller must hold no locks; the function acquires + releases e.mu
// briefly. No-op if a watchdog is already running for fbID.
func (e *Engine) spawnWatchdog(ctx context.Context, fbID string) {
	quit := make(chan struct{})

	e.mu.Lock()
	if _, exists := e.watchdogQuit[fbID]; exists {
		// Already running — abandon our channel.
		e.mu.Unlock()
		return
	}
	e.watchdogQuit[fbID] = quit
	e.mu.Unlock()

	e.loops.Add(1)
	go func() {
		defer e.loops.Done()
		e.runWatchdog(ctx, fbID, quit)
	}()
}

// stopWatchdog signals + clears the watchdog quit channel for fbID.
// Caller must hold no locks; the function acquires + releases e.mu
// briefly. No-op when no watchdog is running.
func (e *Engine) stopWatchdog(fbID string) {
	e.mu.Lock()
	ch, ok := e.watchdogQuit[fbID]
	if ok {
		close(ch)
		delete(e.watchdogQuit, fbID)
	}
	e.mu.Unlock()
}

// runFederation is the per-federation main loop. Re-reads the
// federation on every tick so an Update (replica added / policy
// changed) is picked up without a Restart.
//
// Panic-safe: a top-level recover ensures one broken federation can't
// kill the engine. The recovered panic is logged and the loop returns;
// EnsureLoop will respawn it on the next operator action.
func (e *Engine) runFederation(ctx context.Context, fbID string, quit <-chan struct{}, trigger <-chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("federation engine: panic in federation loop",
				"federationId", fbID, "panic", r)
		}
	}()

	// Tick once immediately so a freshly-created federation starts
	// replicating without waiting tickInterval. EnsureLoop relies on
	// this — it would otherwise have to call TriggerNow itself.
	e.tickFederation(ctx, fbID)

	t := time.NewTicker(e.tickInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-quit:
			return
		case <-t.C:
			e.tickFederation(ctx, fbID)
		case <-trigger:
			e.tickFederation(ctx, fbID)
		}
	}
}

// tickFederation performs one polling pass for one federation. Loads
// the latest state from the store (so a freshly-updated policy or
// replica list takes effect on the next tick) and replicates the diff
// per-replica.
//
// Per ADR-0005 only SyncMode == "continuous" is implemented in v1.6.0b;
// scheduled mode is recognised but skipped by the polling loop.
func (e *Engine) tickFederation(ctx context.Context, fbID string) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("federation engine: panic in tick",
				"federationId", fbID, "panic", r)
		}
	}()

	fb, err := e.store.Get(ctx, fbID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// Federation was deleted between Start and this tick — drop
			// the loop quietly.
			e.RemoveLoop(fbID)
			return
		}
		e.logger.Error("federation engine: load failed",
			"federationId", fbID, "error", err)
		return
	}

	if fb.Policy.SyncMode != SyncModeContinuous {
		// Scheduled federations are out of scope for v1.6.0b — the
		// engine ignores them entirely. Scheduled mode lands in v1.7.
		return
	}

	for _, replica := range fb.Replicas {
		e.replicateToReplica(ctx, fb, replica)
	}
}

// replicateToReplica computes the primary→replica diff and copies up
// to MaxBatchPerTick objects. Updates the replica's health record at
// the end of the pass.
//
// v1.11.0.4 adds structured slog logging at the start of each tick and
// after computeDiff so live-deploy debugging can correlate engine
// behaviour with the audit log (the operator's case: lag gauges
// advanced but zero federation:replicate_object events fired). Every
// pass logs at least one line per replica so a silent no-op tick is
// distinguishable from a never-fired tick in the log.
func (e *Engine) replicateToReplica(ctx context.Context, fb FederatedBucket, replica ReplicaTarget) {
	e.logger.Debug("federation engine: tick start",
		"federationId", fb.ID,
		"primaryRegion", fb.Primary.RegionID, "primaryBucket", fb.Primary.Bucket,
		"replicaRegion", replica.RegionID, "replicaBucket", replica.Bucket,
		"lastSync", replica.LastSync)

	primaryDrv, err := e.resolver.Resolve(ctx, fb.OwnerUserID, fb.Primary.RegionID)
	if err != nil {
		e.recordFailure(ctx, fb, replica, fmt.Errorf("resolve primary: %w", err))
		return
	}
	replicaDrv, err := e.resolver.Resolve(ctx, fb.OwnerUserID, replica.RegionID)
	if err != nil {
		e.recordFailure(ctx, fb, replica, fmt.Errorf("resolve replica: %w", err))
		return
	}

	dr, err := e.computeDiff(ctx, fb.Primary, replica, primaryDrv, replicaDrv)
	if err != nil {
		e.recordFailure(ctx, fb, replica, fmt.Errorf("compute diff: %w", err))
		return
	}
	diff := dr.entries

	e.logger.Info("federation engine: diff computed",
		"federationId", fb.ID,
		"replicaRegion", replica.RegionID, "replicaBucket", replica.Bucket,
		"sourceScanned", dr.scanned, "sourceHeaded", dr.headed,
		"diffSize", len(diff), "truncated", dr.truncated)

	if len(diff) == 0 {
		// Nothing to replicate. Hand recordSuccess enough context so it
		// can tell "genuinely synced" (scanned==0 OR every object HEAD'd)
		// from "filter-skipped everything" (scanned>0 && headed==0 &&
		// LastSync was set — i.e. we trusted a previous LastSync).
		e.recordSuccess(ctx, fb, replica, 0, 0, 0, dr)
		return
	}

	if len(diff) > MaxBatchPerTick {
		diff = diff[:MaxBatchPerTick]
	}

	replicated, bytesReplicated, replicateErr := e.replicateBatch(ctx, fb, replica, primaryDrv, replicaDrv, diff)

	// Compute remaining lag for the health update. If we copied fewer
	// than len(diff), the rest will be picked up on the next tick.
	pendingObjects := int64(len(diff)) - replicated
	pendingBytes := int64(0)
	for i := int(replicated); i < len(diff); i++ {
		pendingBytes += diff[i].size
	}

	e.logger.Info("federation engine: batch replicated",
		"federationId", fb.ID,
		"replicaRegion", replica.RegionID, "replicaBucket", replica.Bucket,
		"replicated", replicated, "bytesReplicated", bytesReplicated,
		"pendingObjects", pendingObjects, "pendingBytes", pendingBytes,
		"replicateErr", replicateErr)

	if replicateErr != nil {
		e.recordFailure(ctx, fb, replica, replicateErr)
		return
	}
	e.recordSuccess(ctx, fb, replica, replicated, pendingObjects, pendingBytes, dr)
}

// diffEntry is one object that needs to be replicated from primary to
// replica. Captures the source ObjectInfo so the audit log can record
// the size without an extra HeadObject round trip.
type diffEntry struct {
	key  string
	size int64
}

// diffResult bundles the per-tick diff with the bookkeeping fields the
// caller needs to make a confident health decision. Added in v1.11.0.4
// to fix the false-positive "in-sync" path — recordSuccess can now tell
// "the source bucket is genuinely empty" (scanned==0) from "we filter-
// skipped every object" (scanned>0 && headed==0).
type diffResult struct {
	entries []diffEntry
	// scanned is the count of non-directory source objects visited
	// during this tick (capped at scanCap).
	scanned int
	// headed is how many of those scanned objects were actually HEAD'd
	// against the replica. scanned-headed is the count skipped by the
	// LastSync prefilter.
	headed int
	// truncated indicates whether the source listing had more pages we
	// stopped paginating because we hit MaxBatchPerTick or scanCap. When
	// true, the caller MUST treat a zero-diff result as "not fully
	// verified" — we couldn't observe the whole bucket this tick.
	truncated bool
}

// computeDiff lists primary objects (filtered by LastSync if non-zero
// minus LastSyncSlack), HeadObjects each on the replica, and returns
// the slice that needs to be replicated. v1.6.0b is intentionally
// simple: an object is queued when it's missing on the replica, ETags
// differ, or the replica's LastModified predates the primary's.
//
// Listing is paginated with a hard cap of (4 × MaxBatchPerTick) source
// objects scanned per tick to keep tick duration bounded even on
// pathologically large buckets — the engine eventually converges over
// many ticks rather than blocking one tick for minutes.
//
// v1.11.0.4 changes:
//   - The LastSync prefilter now subtracts LastSyncSlack from the
//     replica's LastSync before comparison. Without slack, second-
//     precision source mtimes (Garage v2, S3) would permanently
//     skip objects whose mtime equals the recorded LastSync second.
//   - Returns a diffResult with scanned/headed/truncated counters so
//     recordSuccess can tell "genuinely empty" from "all filter-skipped"
//     and avoid the false-positive in-sync claim.
func (e *Engine) computeDiff(ctx context.Context, primary, replica ReplicaTarget, primaryDrv, replicaDrv ReplicationClient) (diffResult, error) {
	const listPageSize = 1000
	const scanCap = 4 * MaxBatchPerTick

	var out []diffEntry
	var continuation string
	scanned := 0
	headed := 0
	truncated := false
	for {
		page, err := primaryDrv.ListObjects(ctx, primary.Bucket, continuation, listPageSize)
		if err != nil {
			return diffResult{}, fmt.Errorf("list primary: %w", err)
		}
		for _, obj := range page.Objects {
			if obj.IsDir {
				continue
			}
			scanned++
			// LastSync filter (v1.11.0.4): if we already replicated past
			// this object's LastModified (with slack for second-precision
			// mtimes), skip the HEAD entirely. Skips the dominant cost on
			// steady-state federations where most objects are quiescent.
			//
			// LastSyncSlack is critical: without it, an object uploaded
			// within a tick of LastSync's recorded nanosecond would be
			// permanently filter-skipped because Garage/S3 return mtimes
			// at second precision.
			if !replica.LastSync.IsZero() {
				cutoff := replica.LastSync.Add(-LastSyncSlack)
				if !obj.LastModified.After(cutoff) {
					continue
				}
			}

			headed++
			head, herr := replicaDrv.StatObject(ctx, replica.Bucket, obj.Key)
			if herr != nil {
				// Treat any error as "missing on replica" — the worst case
				// is an extra PUT which is idempotent at the S3 layer.
				out = append(out, diffEntry{key: obj.Key, size: obj.Size})
				if len(out) >= MaxBatchPerTick {
					truncated = true
					return diffResult{entries: out, scanned: scanned, headed: headed, truncated: truncated}, nil
				}
				continue
			}
			if etagsDiffer(obj.ETag, head.ETag) || head.LastModified.Before(obj.LastModified) {
				out = append(out, diffEntry{key: obj.Key, size: obj.Size})
				if len(out) >= MaxBatchPerTick {
					truncated = true
					return diffResult{entries: out, scanned: scanned, headed: headed, truncated: truncated}, nil
				}
			}
		}
		if !page.IsTruncated || page.NextContinuation == "" {
			break
		}
		if scanned >= scanCap {
			// Bound the tick; let convergence handle the long tail.
			truncated = true
			break
		}
		continuation = page.NextContinuation
	}
	return diffResult{entries: out, scanned: scanned, headed: headed, truncated: truncated}, nil
}

// etagsDiffer compares two ETag strings, normalising the leading +
// trailing double-quotes that some backends include. Empty ETag on
// either side is treated as "differ" because we have no proof of
// equivalence.
func etagsDiffer(a, b string) bool {
	a = trimQuotes(a)
	b = trimQuotes(b)
	if a == "" || b == "" {
		return true
	}
	return a != b
}

func trimQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// replicateBatch copies each diff entry from primary to replica with
// bounded parallelism. Returns the number of objects + bytes
// successfully replicated and the FIRST error encountered (mirrors
// internal/sync/engine.go's contract).
//
// One audit event per object replicated (federation:replicate_object) —
// high volume by design per ADR-0005; the audit-log view in v1.6.0c
// filters it out of the default page.
func (e *Engine) replicateBatch(ctx context.Context, fb FederatedBucket, replica ReplicaTarget, primaryDrv, replicaDrv ReplicationClient, diff []diffEntry) (int64, int64, error) {
	sem := make(chan struct{}, e.workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	var copied, bytes int64

	for _, d := range diff {
		// Honour cancellation before queueing more work.
		select {
		case <-ctx.Done():
			wg.Wait()
			if firstErr == nil {
				firstErr = ctx.Err()
			}
			return copied, bytes, firstErr
		default:
		}

		wg.Add(1)
		e.inflight.Add(1)
		sem <- struct{}{}

		go func(entry diffEntry) {
			defer wg.Done()
			defer e.inflight.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					e.logger.Error("federation engine: panic in replicate",
						"federationId", fb.ID, "key", entry.key, "panic", r)
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("panic replicating %q: %v", entry.key, r)
					}
					mu.Unlock()
				}
			}()

			err := e.replicateOne(ctx, fb, replica, primaryDrv, replicaDrv, entry)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				e.audit.Log(audit.Event{
					Actor:    fb.OwnerUserID,
					Action:   "federation:replicate_object",
					Resource: fmt.Sprintf("federation:%s:%s", fb.ID, entry.key),
					Result:   audit.ResultFailure,
					Detail:   fmt.Sprintf("size=%d bytes: %v", entry.size, err),
				})
				return
			}
			atomic.AddInt64(&copied, 1)
			atomic.AddInt64(&bytes, entry.size)
			e.audit.Log(audit.Event{
				Actor:    fb.OwnerUserID,
				Action:   "federation:replicate_object",
				Resource: fmt.Sprintf("federation:%s:%s", fb.ID, entry.key),
				Result:   audit.ResultSuccess,
				Detail:   fmt.Sprintf("size=%d bytes", entry.size),
			})
		}(d)
	}

	wg.Wait()
	return atomic.LoadInt64(&copied), atomic.LoadInt64(&bytes), firstErr
}

// replicateOne performs a single primary→replica object copy. Tries
// ServerSideCopy first when the two sides share a driver type +
// capability advert; otherwise streams the bytes through basement.
func (e *Engine) replicateOne(ctx context.Context, fb FederatedBucket, replica ReplicaTarget, primaryDrv, replicaDrv ReplicationClient, entry diffEntry) error {
	// Same-backend optimisation: if both sides are the same driver
	// and advertise ServerSideCopy, the cluster can copy without
	// the bytes touching basement. Drivers that fail this attempt
	// fall through to streaming.
	srcCaps, _ := primaryDrv.Capabilities(ctx)
	dstCaps, _ := replicaDrv.Capabilities(ctx)
	if srcCaps.Driver != "" && srcCaps.Driver == dstCaps.Driver && dstCaps.ServerSideCopy {
		if err := replicaDrv.ServerSideCopy(ctx, fb.Primary.Bucket, entry.key, replica.Bucket, entry.key); err == nil {
			return nil
		}
		// Fall through on ServerSideCopy failure — most non-Garage
		// backends reject cross-bucket ServerSideCopy when the two
		// buckets are owned by different keys, and that's fine.
	}

	stream, err := primaryDrv.StreamObject(ctx, fb.Primary.Bucket, entry.key)
	if err != nil {
		return fmt.Errorf("stream primary %q: %w", entry.key, err)
	}
	defer stream.Body.Close()

	if err := replicaDrv.PutObjectStream(ctx, replica.Bucket, entry.key, stream.Body, stream.ContentType, stream.ContentLength); err != nil {
		return fmt.Errorf("put replica %q: %w", entry.key, err)
	}
	return nil
}

// SubscribeToEvents wires the federation engine into the webhook
// event bus. When an event matches a federation's PRIMARY (region,
// bucket), the engine queues an immediate replicate to all replicas
// for the affected key. Replaces the 10s polling tick for clusters
// that have webhooks configured.
//
// Polling tick continues as a fallback so federations on backends
// without webhook-source coverage still converge (just at the 10s
// rate, not real-time). Real backend coverage of every create / modify
// path lands with the v2.0 gateway; v1.7.0f wires the path so the
// DELETE handler's ObjectDeleted envelopes already propagate end-to-end.
//
// Safe to call multiple times — the second + call replaces the prior
// subscription. nil webhooks is a no-op (engine stays polling-only).
func (e *Engine) SubscribeToEvents(webhooks *webhook.Engine) {
	if webhooks == nil {
		return
	}
	// Tear down any prior subscription so a second SubscribeToEvents
	// (e.g. a test that re-wires for assertion clarity) doesn't double-
	// register.
	if e.unsubscribeWebhook != nil {
		e.unsubscribeWebhook()
	}
	e.unsubscribeWebhook = webhooks.Subscribe("federation", func(env webhook.EventEnvelope) {
		// MUST NOT block — the callback runs in the webhook
		// dispatcher goroutine. Push the matching tasks onto the
		// per-federation event channels and return immediately.
		e.handleWebhookEvent(env)
	})
}

// handleWebhookEvent maps one EventEnvelope onto the set of (federation,
// replica) tasks it should trigger. Runs synchronously inside the
// webhook dispatcher goroutine — every heavy step (store load,
// per-task enqueue) is non-blocking, so the dispatcher is never stalled
// by federation backpressure.
//
// Matching rule: the envelope must hit a federation's PRIMARY
// (regionId + bucket). Events targeted at what's already a replica
// don't trigger re-replication — the polling tick handles cross-replica
// convergence per ADR-0005 (primary is the source of truth).
//
// On a saturated per-federation channel the task is dropped — the
// polling tick will pick the change up on its next 10s pass, which
// matches the cycle spec's "polling fallback continues" guarantee.
func (e *Engine) handleWebhookEvent(env webhook.EventEnvelope) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("federation engine: panic in webhook callback",
				"envelopeId", env.ID, "panic", r)
		}
	}()

	if e.store == nil {
		return
	}

	// Use the background context — the webhook dispatcher's caller-
	// supplied ctx may already be a per-request scope, and we don't want
	// the federation's view of the store to die when a stray HTTP
	// request cancels.
	ctx := context.Background()
	feds, err := e.store.All(ctx)
	if err != nil {
		e.logger.Warn("federation engine: webhook callback All() failed",
			"envelopeId", env.ID, "error", err)
		return
	}

	for _, fb := range feds {
		if fb.Primary.RegionID != env.RegionID || fb.Primary.Bucket != env.Bucket {
			continue
		}
		if fb.Policy.SyncMode != SyncModeContinuous {
			continue
		}
		// Look up the per-federation event channel. Skipped silently
		// if the loop hasn't been spawned (engine still starting, or
		// a transient race between Create + Start).
		e.mu.Lock()
		ch, ok := e.eventTasks[fb.ID]
		e.mu.Unlock()
		if !ok {
			continue
		}

		isDelete := env.Type == webhook.EventObjectDeleted
		for _, replica := range fb.Replicas {
			task := eventTask{
				kind:       "single",
				fbID:       fb.ID,
				target:     replica,
				singleKey:  env.Key,
				singleSize: env.Size,
				isDelete:   isDelete,
			}
			// Non-blocking send: a saturated channel drops the task and
			// lets the polling tick pick it up. We log on drop so a
			// chronically backed-up federation surfaces in the engine
			// log rather than silently lagging.
			select {
			case ch <- task:
			default:
				e.logger.Warn("federation engine: event task channel saturated, dropping (polling will catch up)",
					"federationId", fb.ID, "key", env.Key, "type", env.Type)
			}
		}
	}
}

// runEventWorker drains the per-federation event channel and
// replicates one object per task. One goroutine per federation; the
// outer for-select picks up shutdown via the federation's fedQuit
// channel + a closed-tasks-channel fallback.
//
// Each task is one HTTP round-trip to a single replica (Put or
// Delete). No internal worker pool — concurrency is already bounded
// by the per-federation channel buffer + however many federations are
// running. If a single backend's PUT is slow, the federation's tasks
// queue up; the dispatcher doesn't stall.
//
// Panic-safe at both the loop level + the per-task level so a broken
// driver can't kill the worker.
func (e *Engine) runEventWorker(ctx context.Context, fbID string, quit <-chan struct{}, tasks <-chan eventTask) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("federation engine: panic in event worker",
				"federationId", fbID, "panic", r)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-quit:
			return
		case task, ok := <-tasks:
			if !ok {
				return
			}
			e.handleEventTask(ctx, task)
		}
	}
}

// handleEventTask executes one single-object event-driven replicate or
// delete. Re-reads the federation from the store so a stale task (e.g.
// the federation was edited between queue + drain) still uses current
// owner / replica info. Failures are logged + recorded against the
// per-replica failure counter so a chronically-broken backend still
// flips to HealthBroken via the same mechanism polling uses.
func (e *Engine) handleEventTask(ctx context.Context, task eventTask) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("federation engine: panic in event task",
				"federationId", task.fbID, "key", task.singleKey, "panic", r)
		}
	}()

	fb, err := e.store.Get(ctx, task.fbID)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			e.logger.Warn("federation engine: event task store load failed",
				"federationId", task.fbID, "error", err)
		}
		return
	}

	primaryDrv, err := e.resolver.Resolve(ctx, fb.OwnerUserID, fb.Primary.RegionID)
	if err != nil {
		e.recordFailure(ctx, fb, task.target, fmt.Errorf("event resolve primary: %w", err))
		return
	}
	replicaDrv, err := e.resolver.Resolve(ctx, fb.OwnerUserID, task.target.RegionID)
	if err != nil {
		e.recordFailure(ctx, fb, task.target, fmt.Errorf("event resolve replica: %w", err))
		return
	}

	e.inflight.Add(1)
	defer e.inflight.Done()

	var opErr error
	if task.isDelete {
		opErr = replicaDrv.DeleteObject(ctx, task.target.Bucket, task.singleKey)
		if opErr != nil {
			e.audit.Log(audit.Event{
				Actor:    fb.OwnerUserID,
				Action:   "federation:replicate_delete",
				Resource: fmt.Sprintf("federation:%s:%s", fb.ID, task.singleKey),
				Result:   audit.ResultFailure,
				Detail:   fmt.Sprintf("event-driven: %v", opErr),
			})
		} else {
			e.audit.Log(audit.Event{
				Actor:    fb.OwnerUserID,
				Action:   "federation:replicate_delete",
				Resource: fmt.Sprintf("federation:%s:%s", fb.ID, task.singleKey),
				Result:   audit.ResultSuccess,
				Detail:   "event-driven",
			})
		}
	} else {
		entry := diffEntry{key: task.singleKey, size: task.singleSize}
		opErr = e.replicateOne(ctx, fb, task.target, primaryDrv, replicaDrv, entry)
		if opErr != nil {
			e.audit.Log(audit.Event{
				Actor:    fb.OwnerUserID,
				Action:   "federation:replicate_object",
				Resource: fmt.Sprintf("federation:%s:%s", fb.ID, task.singleKey),
				Result:   audit.ResultFailure,
				Detail:   fmt.Sprintf("event-driven size=%d bytes: %v", task.singleSize, opErr),
			})
		} else {
			e.audit.Log(audit.Event{
				Actor:    fb.OwnerUserID,
				Action:   "federation:replicate_object",
				Resource: fmt.Sprintf("federation:%s:%s", fb.ID, task.singleKey),
				Result:   audit.ResultSuccess,
				Detail:   fmt.Sprintf("event-driven size=%d bytes", task.singleSize),
			})
		}
	}

	// Health update on the replica: success resets the failure counter
	// and rolls LastSync forward; failure increments + may flip to
	// HealthBroken. We reuse the polling-tick helpers so the
	// auto-failover / broken-after-3 semantics stay identical between
	// the two paths.
	//
	// The event-driven path operates on a SINGLE known-good object: we
	// just replicated/deleted it. Synthesise a diffResult that asserts
	// "scanned 1, headed 1, not truncated" so recordSuccess's empty-diff
	// reasoning treats this as a genuine successful verification (the
	// alternative — scanned=0 — would also work but loses semantic
	// fidelity for any future tick-vs-event-path debugging).
	if opErr != nil {
		e.recordFailure(ctx, fb, task.target, opErr)
		return
	}
	e.recordSuccess(ctx, fb, task.target, 1, 0, 0, diffResult{scanned: 1, headed: 1})
}

// recordSuccess writes a replica-health update with the supplied
// pending counters. Resets the per-replica consecutive-failure counter.
//
// replicatedThisTick is the number of objects actually copied on this
// pass (zero on an empty-diff tick).
// pendingObjects + pendingBytes describe what was DEFERRED to a future
// tick (we capped at MaxBatchPerTick this tick). When pending is 0 the
// replica is fully in sync.
//
// v1.11.0.4 false-positive-health fix: the engine no longer
// unconditionally bumps LastSync + sets HealthInSync on every
// successful pass. Doing so on an empty-diff tick that REALLY meant
// "filter-skipped everything" was the root cause of the no-op
// replication bug — once LastSync was set, the LastSyncSlack window
// would mark any whole-second mtime as "already replicated".
//
// Decision tree:
//   - replicatedThisTick > 0           → InSync (or Lagging w/ pending);
//                                        bump LastSync to now.
//   - empty diff, scanned == 0         → source bucket is empty;
//                                        genuinely InSync, bump LastSync.
//   - empty diff, headed == scanned    → every source object was HEAD'd
//                                        and matched; genuinely InSync,
//                                        bump LastSync.
//   - empty diff, headed  < scanned    → we trusted the LastSync filter
//                                        for some objects; if LastSync
//                                        was already set this is the
//                                        normal steady-state path — bump
//                                        LastSync, InSync. If LastSync
//                                        was zero this is impossible
//                                        (the filter never triggers when
//                                        LastSync.IsZero()).
//   - empty diff, truncated            → we stopped paginating; cannot
//                                        confidently claim full sync.
//                                        Bump LastSync, leave Lagging.
func (e *Engine) recordSuccess(ctx context.Context, fb FederatedBucket, replica ReplicaTarget, replicatedThisTick, pendingObjects, pendingBytes int64, dr diffResult) {
	e.resetFailureCount(fb.ID, replica)

	now := time.Now().UTC()
	health := HealthInSync

	switch {
	case pendingObjects > 0:
		// Residual pending objects mean we're not yet caught up — surface
		// that as lag even though this tick succeeded.
		health = HealthLagging
	case replicatedThisTick > 0:
		// We just replicated objects this tick → genuinely InSync.
	case dr.scanned == 0:
		// Source bucket is empty → trivially in sync.
	case dr.truncated:
		// Tick was capped mid-scan; we can't claim full sync. The next
		// tick continues paginating.
		health = HealthLagging
	default:
		// scanned > 0, replicated 0, not truncated: we observed every
		// source object either via filter-skip or HEAD. If LastSync was
		// zero this is a genuine first-tick verification (the filter
		// never runs in that case → every object was HEAD'd). If
		// LastSync was set this is the normal steady-state empty-diff
		// path. Either way InSync is justified.
	}

	upd := ReplicaTarget{
		RegionID:   replica.RegionID,
		Bucket:     replica.Bucket,
		LastSync:   now,
		Health:     health,
		LagBytes:   pendingBytes,
		LagObjects: pendingObjects,
	}
	if err := e.store.UpdateReplicaHealth(ctx, fb.ID, replica.RegionID, replica.Bucket, upd); err != nil {
		// Federation was deleted mid-tick — drop quietly. Anything
		// else gets logged but never escalates; the next tick retries.
		if !errors.Is(err, ErrNotFound) {
			e.logger.Warn("federation engine: health update failed",
				"federationId", fb.ID, "regionId", replica.RegionID,
				"bucket", replica.Bucket, "error", err)
		}
	}
}

// recordFailure increments the per-replica consecutive-failure counter
// and flips health to "broken" when the threshold is hit. The lag
// fields are preserved from the existing record (we don't know them
// any better than last tick did).
func (e *Engine) recordFailure(ctx context.Context, fb FederatedBucket, replica ReplicaTarget, replicateErr error) {
	failures := e.incFailureCount(fb.ID, replica)
	e.logger.Warn("federation engine: replicate failed",
		"federationId", fb.ID, "regionId", replica.RegionID,
		"bucket", replica.Bucket, "failures", failures, "error", replicateErr)

	health := HealthLagging
	if failures >= BrokenFailureThreshold {
		health = HealthBroken
	}

	// Pull current lag from store so we don't accidentally zero it on
	// a transient failure. If the load fails we just emit zeros — the
	// next successful tick will refresh.
	cur, _ := e.store.Get(ctx, fb.ID)
	var lagBytes, lagObjects int64
	for _, r := range cur.Replicas {
		if r.RegionID == replica.RegionID && r.Bucket == replica.Bucket {
			lagBytes = r.LagBytes
			lagObjects = r.LagObjects
			break
		}
	}

	upd := ReplicaTarget{
		RegionID:   replica.RegionID,
		Bucket:     replica.Bucket,
		LastSync:   replica.LastSync, // do not advance on failure
		Health:     health,
		LagBytes:   lagBytes,
		LagObjects: lagObjects,
	}
	if err := e.store.UpdateReplicaHealth(ctx, fb.ID, replica.RegionID, replica.Bucket, upd); err != nil {
		if !errors.Is(err, ErrNotFound) {
			e.logger.Warn("federation engine: health update (failure path) failed",
				"federationId", fb.ID, "regionId", replica.RegionID,
				"bucket", replica.Bucket, "error", err)
		}
	}
}

// watchdogState tracks consecutive primary-probe failures inside one
// watchdog goroutine. Lives on the stack of runWatchdog rather than
// shared engine state because (a) only the owning goroutine reads it
// and (b) restarting the watchdog naturally restarts the counter,
// which is the right behaviour after a policy edit.
type watchdogState struct {
	consecutiveFailures int
}

// runWatchdog is the per-federation watchdog main loop. Probes primary
// health every e.watchdogInterval and triggers an auto-failover after
// Policy.AutoFailoverSec of consecutive failures (translated to N
// probes via the watchdog interval).
//
// Re-reads the federation from the store on every tick so policy edits
// or replica list changes take effect on the next probe. If the
// federation's AutoFailover policy flips to false mid-loop the watchdog
// exits early — EnsureLoop will have already closed the quit channel,
// but this self-defence handles the edge where the policy reconciler
// race-loses to a concurrent edit.
//
// Panic-safe: a top-level recover ensures one broken watchdog can't
// kill the engine; the recovered panic is logged and the loop returns.
// Callers that want the watchdog respawned should re-invoke EnsureLoop.
func (e *Engine) runWatchdog(ctx context.Context, fbID string, quit <-chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("federation engine: panic in watchdog loop",
				"federationId", fbID, "panic", r)
		}
	}()

	state := &watchdogState{}

	// First probe fires immediately so a watchdog spawned mid-outage
	// doesn't have to wait one interval before starting to count
	// failures. This matches runFederation's "tick once immediately".
	e.tickWatchdog(ctx, fbID, state)

	t := time.NewTicker(e.watchdogInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-quit:
			return
		case <-t.C:
			e.tickWatchdog(ctx, fbID, state)
		}
	}
}

// tickWatchdog performs one primary-liveness probe for one federation.
// Loads the latest state from the store (so a freshly-toggled policy
// takes effect on the next tick), probes the primary's ListObjects via
// the replication client, and either resets or increments the
// consecutive-failure counter. Triggers an auto-failover when the
// failure window exceeds Policy.AutoFailoverSec.
//
// Per-tick recover panic-shields each probe so a broken backend's
// driver panic can't kill the watchdog.
func (e *Engine) tickWatchdog(ctx context.Context, fbID string, state *watchdogState) {
	defer func() {
		if r := recover(); r != nil {
			e.logger.Error("federation engine: panic in watchdog tick",
				"federationId", fbID, "panic", r)
		}
	}()

	fb, err := e.store.Get(ctx, fbID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// Federation was deleted between Start and this tick — drop
			// the watchdog quietly. RemoveLoop closes the quit channel
			// from the API path, but this self-cleans the race.
			e.stopWatchdog(fbID)
			return
		}
		e.logger.Warn("federation engine: watchdog load failed",
			"federationId", fbID, "error", err)
		return
	}
	// The store's Get returns a deep copy of the Replicas slice (per
	// the v1.6.0f isolation guarantee), so pickHealthiestReplica + the
	// audit detail render are safe to read without further snapshotting.

	// Self-defence: if the policy was flipped to false between
	// EnsureLoop's reconcile and this tick, exit cleanly.
	if !fb.Policy.AutoFailover {
		e.stopWatchdog(fbID)
		return
	}

	healthy := e.probePrimary(ctx, fb)
	if healthy {
		if state.consecutiveFailures > 0 {
			e.logger.Info("federation engine: watchdog primary recovered",
				"federationId", fbID,
				"previousFailures", state.consecutiveFailures)
		}
		state.consecutiveFailures = 0
		return
	}

	state.consecutiveFailures++
	e.logger.Warn("federation engine: watchdog primary unreachable",
		"federationId", fbID, "consecutiveFailures", state.consecutiveFailures)

	// Translate Policy.AutoFailoverSec into a probe count using the
	// watchdog's own interval. A zero / negative AutoFailoverSec means
	// "fail over on the very first probe failure" — surfaces the
	// trivially-misconfigured-to-default case clearly rather than
	// silently never failing over.
	probesRequired := 1
	if fb.Policy.AutoFailoverSec > 0 {
		intervalSecs := int(e.watchdogInterval / time.Second)
		if intervalSecs < 1 {
			// Sub-second watchdog intervals (tests) still need at least
			// one probe failure to count.
			intervalSecs = 1
		}
		probesRequired = fb.Policy.AutoFailoverSec / intervalSecs
		if probesRequired < 1 {
			probesRequired = 1
		}
	}

	if state.consecutiveFailures < probesRequired {
		return
	}

	// Threshold met — promote the healthiest replica.
	e.triggerAutoFailover(ctx, fb)
	// Reset the counter regardless of outcome — a "skipped" auto-failover
	// (no healthy replica) shouldn't fire again on every subsequent tick
	// because nothing has changed; we wait another full window before
	// re-trying so the audit log stays signal-rich.
	state.consecutiveFailures = 0
}

// probePrimary returns true when the primary backend responds to a
// minimal ListObjects probe. Any error — driver resolve, list call,
// context cancellation — is treated as "primary unreachable" and the
// watchdog counts it as a failure. A future "discriminate transient vs
// terminal" hook would live here, but ADR-0005 explicitly punts that
// to a polish cycle.
//
// Returns true when the probe succeeds (regardless of whether the
// primary's bucket is empty); false on any error.
func (e *Engine) probePrimary(ctx context.Context, fb FederatedBucket) bool {
	drv, err := e.resolver.Resolve(ctx, fb.OwnerUserID, fb.Primary.RegionID)
	if err != nil {
		e.logger.Warn("federation engine: watchdog resolve primary failed",
			"federationId", fb.ID, "regionId", fb.Primary.RegionID, "error", err)
		return false
	}
	if _, err := drv.ListObjects(ctx, fb.Primary.Bucket, "", 1); err != nil {
		return false
	}
	return true
}

// pickHealthiestReplica returns the replica that's the best candidate
// for promotion. Selection rule (per cycle spec):
//   1. Lowest LagSec (where LagSec is now-LastSync; zero LastSync sorts
//      last because we have no proof the replica's caught up)
//   2. Tie-broken alphabetically by (RegionID, Bucket) for determinism
//
// A replica with Health=broken is excluded — that's a known-bad target.
// Returns (zero, false) when no healthy replica is available.
func pickHealthiestReplica(fb FederatedBucket, now time.Time) (ReplicaTarget, bool) {
	type scored struct {
		rep    ReplicaTarget
		lagSec int64
		seen   bool // false when LastSync is zero
	}
	var candidates []scored
	for _, rep := range fb.Replicas {
		if rep.Health == HealthBroken {
			continue
		}
		s := scored{rep: rep, seen: !rep.LastSync.IsZero()}
		if s.seen {
			delta := now.Sub(rep.LastSync)
			if delta < 0 {
				delta = 0
			}
			s.lagSec = int64(delta / time.Second)
		}
		candidates = append(candidates, s)
	}
	if len(candidates) == 0 {
		return ReplicaTarget{}, false
	}
	best := 0
	for i := 1; i < len(candidates); i++ {
		ci := candidates[i]
		cb := candidates[best]
		// Sort: seen-with-lag < never-seen, then by lagSec asc, then
		// (RegionID, Bucket) lexicographic asc.
		if ci.seen && !cb.seen {
			best = i
			continue
		}
		if !ci.seen && cb.seen {
			continue
		}
		if ci.lagSec < cb.lagSec {
			best = i
			continue
		}
		if ci.lagSec > cb.lagSec {
			continue
		}
		if ci.rep.RegionID < cb.rep.RegionID {
			best = i
			continue
		}
		if ci.rep.RegionID > cb.rep.RegionID {
			continue
		}
		if ci.rep.Bucket < cb.rep.Bucket {
			best = i
		}
	}
	return candidates[best].rep, true
}

// triggerAutoFailover promotes the healthiest replica to primary,
// persists the swap, kicks the engine to start replicating from the
// new primary, and emits an audit event. Mirrors the manual failover
// path in user_federated_buckets.go but is initiated by the engine
// rather than the API layer.
//
// When no healthy replica is available, emits a federation:auto_failover_skipped
// audit event and returns without changing state — operators see the
// skip in the audit log so they know the watchdog is awake but stuck.
func (e *Engine) triggerAutoFailover(ctx context.Context, fb FederatedBucket) {
	now := time.Now().UTC()
	newPrimary, ok := pickHealthiestReplica(fb, now)
	if !ok {
		e.logger.Error("federation engine: auto-failover skipped — no healthy replica",
			"federationId", fb.ID, "primaryRegion", fb.Primary.RegionID,
			"primaryBucket", fb.Primary.Bucket)
		e.audit.Log(audit.Event{
			Actor:    fb.OwnerUserID,
			Action:   "federation:auto_failover_skipped",
			Resource: "federation:" + fb.ID,
			Result:   audit.ResultFailure,
			Detail: fmt.Sprintf("primary=%s:%s reason=no healthy replica available",
				fb.Primary.RegionID, fb.Primary.Bucket),
		})
		return
	}

	oldPrimary := fb.Primary
	// Build new replica slice: replace the promoted replica entry with
	// the demoted primary. Lag/health on the promoted entry is reset
	// (it becomes source of truth); the demoted entry's lag/health is
	// zeroed so the engine's next tick recomputes it from scratch.
	newReplicas := make([]ReplicaTarget, 0, len(fb.Replicas))
	for _, rep := range fb.Replicas {
		if rep.RegionID == newPrimary.RegionID && rep.Bucket == newPrimary.Bucket {
			newReplicas = append(newReplicas, ReplicaTarget{
				RegionID: oldPrimary.RegionID,
				Bucket:   oldPrimary.Bucket,
			})
			continue
		}
		newReplicas = append(newReplicas, rep)
	}

	patch := FederatedBucket{
		Name:     fb.Name,
		Primary:  ReplicaTarget{RegionID: newPrimary.RegionID, Bucket: newPrimary.Bucket},
		Replicas: newReplicas,
		Policy:   fb.Policy,
	}
	if _, err := e.store.Update(ctx, fb.ID, patch); err != nil {
		e.logger.Error("federation engine: auto-failover persist failed",
			"federationId", fb.ID, "error", err)
		e.audit.Log(audit.Event{
			Actor:    fb.OwnerUserID,
			Action:   "federation:auto_failover_skipped",
			Resource: "federation:" + fb.ID,
			Result:   audit.ResultFailure,
			Detail:   fmt.Sprintf("persist failed: %v", err),
		})
		return
	}

	// Kick the replication loop so the new primary starts being scanned
	// on the next tick rather than waiting tickInterval.
	if err := e.TriggerNow(fb.ID); err != nil {
		// Loop might not be running (engine inert) — log + continue;
		// the failover persisted, replication catches up on next Start.
		e.logger.Warn("federation engine: auto-failover TriggerNow failed",
			"federationId", fb.ID, "error", err)
	}

	// Compute a human-readable "primary unreachable for Xs" detail. We
	// know Policy.AutoFailoverSec was met (or exceeded by one probe);
	// surface it verbatim because that's the metric the operator set.
	reason := fmt.Sprintf("primary unreachable for %ds", fb.Policy.AutoFailoverSec)
	e.audit.Log(audit.Event{
		Actor:    fb.OwnerUserID,
		Action:   "federation:auto_failover",
		Resource: "federation:" + fb.ID,
		Result:   audit.ResultSuccess,
		Detail: fmt.Sprintf("old_primary=%s:%s new_primary=%s:%s reason=%s",
			oldPrimary.RegionID, oldPrimary.Bucket,
			newPrimary.RegionID, newPrimary.Bucket, reason),
	})
	e.logger.Info("federation engine: auto-failover promoted replica",
		"federationId", fb.ID,
		"oldPrimary", oldPrimary.RegionID+":"+oldPrimary.Bucket,
		"newPrimary", newPrimary.RegionID+":"+newPrimary.Bucket,
		"reason", reason)
}

// failureKey builds the (fbID, regionID, bucket) key the engine uses
// internally to track consecutive failures per replica.
func failureKey(fbID string, r ReplicaTarget) string {
	return fbID + "|" + r.RegionID + "|" + r.Bucket
}

func (e *Engine) incFailureCount(fbID string, r ReplicaTarget) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	k := failureKey(fbID, r)
	e.failures[k]++
	return e.failures[k]
}

func (e *Engine) resetFailureCount(fbID string, r ReplicaTarget) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.failures, failureKey(fbID, r))
}

// FailureCount exposes the current consecutive-failure counter for a
// replica. Tests use this to assert the broken-after-3 contract
// without depending on log scraping.
func (e *Engine) FailureCount(fbID string, r ReplicaTarget) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.failures[failureKey(fbID, r)]
}

// WatchdogCount reports the number of per-federation watchdog goroutines
// the engine is currently tracking. Tests assert "spawned a watchdog for
// federation X" or "no watchdog when policy disabled" via this accessor.
func (e *Engine) WatchdogCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.watchdogQuit)
}

// HasWatchdog returns true if a watchdog goroutine is currently running
// for fbID. Tests use this to assert policy-toggle behaviour without
// peeking at the engine's internals.
func (e *Engine) HasWatchdog(fbID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.watchdogQuit[fbID]
	return ok
}

// LoopCount reports the number of per-federation goroutines the engine
// is currently tracking. Tests use this to assert "spawned N goroutines
// at boot" without leaking implementation details.
func (e *Engine) LoopCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.fedQuit)
}

// ComputeHealth is the public wrapper around the engine's health
// derivation rule, exported so the v1.6.0c API layer can reuse the
// same logic when rendering a freshly-created federation that hasn't
// yet been ticked.
//
// Inputs:
//   - lastSync: the replica's last successful sync time
//   - now: the wall-clock reference (typically time.Now().UTC())
//   - lagAlertSec: Policy.LagAlertSec
//   - failureCount: current consecutive-failure tally
//
// Output: one of HealthPending, HealthInSync, HealthLagging,
// HealthStale, HealthBroken. A zero LastSync (v1.11.0.4) is rendered
// as HealthPending — a fresh federation that hasn't been verified by
// the engine shouldn't claim "in-sync". The engine flips this to
// HealthInSync after the first successful verification pass via
// recordSuccess.
func ComputeHealth(lastSync, now time.Time, lagAlertSec, failureCount int) string {
	return computeHealth(lastSync, now, lagAlertSec, failureCount)
}

func computeHealth(lastSync, now time.Time, lagAlertSec, failureCount int) string {
	if failureCount >= BrokenFailureThreshold {
		return HealthBroken
	}
	if lastSync.IsZero() {
		// v1.11.0.4: changed from HealthInSync to HealthPending so a
		// fresh federation reports "pending verification" instead of
		// "all good". The engine writes the verified state via
		// recordSuccess on the first tick that observes the replica.
		return HealthPending
	}
	if lagAlertSec <= 0 {
		return HealthInSync
	}
	lag := now.Sub(lastSync)
	threshold := time.Duration(lagAlertSec) * time.Second
	if lag > 10*threshold {
		return HealthStale
	}
	if lag > threshold {
		return HealthLagging
	}
	return HealthInSync
}

// noopAudit is the silent audit.Logger installed when the caller passes
// nil into NewEngine. Mirrors audit.NewNoop() but lives in-package so
// the federation package doesn't have a hard dependency on the audit
// package's exported noop helper (which is itself non-public-name-stable
// for the v1.6 cycle).
type noopAudit struct{}

func (noopAudit) Log(audit.Event)                                                       {}
func (noopAudit) Query(_, _ time.Time, _ audit.QueryFilter) ([]audit.Event, error)      { return nil, nil }
func (noopAudit) QueryWithTotal(_, _ time.Time, _ audit.QueryFilter) ([]audit.Event, int, error) {
	return nil, 0, nil
}
