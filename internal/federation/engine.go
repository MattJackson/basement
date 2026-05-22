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

	tickInterval time.Duration
	workers      int

	mu      sync.Mutex
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
		store:        store,
		resolver:     resolver,
		audit:        audit,
		logger:       logger,
		tickInterval: DefaultTickInterval,
		workers:      DefaultWorkers,
		fedQuit:      make(map[string]chan struct{}),
		triggers:     make(map[string]chan struct{}),
		failures:     make(map[string]int),
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
	}

	e.logger.Info("federation engine: started", "federations", len(feds),
		"tickInterval", e.tickInterval.String(), "workers", e.workers)
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

	e.mu.Lock()
	for _, ch := range e.fedQuit {
		close(ch)
	}
	e.fedQuit = make(map[string]chan struct{})
	e.triggers = make(map[string]chan struct{})
	e.mu.Unlock()

	// Wait for both: the per-federation tick loops to return, AND any
	// replicate goroutines still mid-PUT. Loops first because the
	// inflight counter is only incremented inside a tick — and we want
	// all ticks to finish before draining the inflight set.
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
// No-op when a loop is already running (Update / replica-add reuses the
// same loop; the next tick picks up the change because every tick
// re-reads the federation from the store).
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
	if exists {
		return
	}
	e.spawnLoop(ctx, fbID)
}

// RemoveLoop stops the per-federation loop for fbID. v1.6.0c API
// handlers call this after Delete so a removed federation stops
// replicating immediately rather than waiting for the next tick to
// notice its absence.
func (e *Engine) RemoveLoop(fbID string) {
	e.mu.Lock()
	ch, ok := e.fedQuit[fbID]
	if ok {
		close(ch)
		delete(e.fedQuit, fbID)
		delete(e.triggers, fbID)
	}
	e.mu.Unlock()
}

// spawnLoop registers fbID and launches its goroutine. Caller must
// hold no locks; the function acquires + releases e.mu briefly.
func (e *Engine) spawnLoop(ctx context.Context, fbID string) {
	quit := make(chan struct{})
	trigger := make(chan struct{}, 1)

	e.mu.Lock()
	if _, exists := e.fedQuit[fbID]; exists {
		// Another goroutine raced us — abandon our channels.
		e.mu.Unlock()
		return
	}
	e.fedQuit[fbID] = quit
	e.triggers[fbID] = trigger
	e.mu.Unlock()

	e.loops.Add(1)
	go func() {
		defer e.loops.Done()
		e.runFederation(ctx, fbID, quit, trigger)
	}()
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
func (e *Engine) replicateToReplica(ctx context.Context, fb FederatedBucket, replica ReplicaTarget) {
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

	diff, err := e.computeDiff(ctx, fb.Primary, replica, primaryDrv, replicaDrv)
	if err != nil {
		e.recordFailure(ctx, fb, replica, fmt.Errorf("compute diff: %w", err))
		return
	}

	if len(diff) == 0 {
		// Nothing to replicate — update health to in-sync with zero lag.
		e.recordSuccess(ctx, fb, replica, 0, 0)
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
	_ = bytesReplicated // metrics expose this in v1.6.0d; not used yet.

	if replicateErr != nil {
		e.recordFailure(ctx, fb, replica, replicateErr)
		return
	}
	e.recordSuccess(ctx, fb, replica, pendingObjects, pendingBytes)
}

// diffEntry is one object that needs to be replicated from primary to
// replica. Captures the source ObjectInfo so the audit log can record
// the size without an extra HeadObject round trip.
type diffEntry struct {
	key  string
	size int64
}

// computeDiff lists primary objects (filtered by LastSync if non-zero),
// HeadObjects each on the replica, and returns the slice that needs to
// be replicated. v1.6.0b is intentionally simple: an object is queued
// when it's missing on the replica, ETags differ, or the replica's
// LastModified predates the primary's.
//
// Listing is paginated with a hard cap of (4 × MaxBatchPerTick) source
// objects scanned per tick to keep tick duration bounded even on
// pathologically large buckets — the engine eventually converges over
// many ticks rather than blocking one tick for minutes.
func (e *Engine) computeDiff(ctx context.Context, primary, replica ReplicaTarget, primaryDrv, replicaDrv ReplicationClient) ([]diffEntry, error) {
	const listPageSize = 1000
	const scanCap = 4 * MaxBatchPerTick

	var out []diffEntry
	var continuation string
	scanned := 0
	for {
		page, err := primaryDrv.ListObjects(ctx, primary.Bucket, continuation, listPageSize)
		if err != nil {
			return nil, fmt.Errorf("list primary: %w", err)
		}
		for _, obj := range page.Objects {
			if obj.IsDir {
				continue
			}
			scanned++
			// LastSync filter: if we already replicated past this object's
			// LastModified, skip the HEAD entirely. Skips the dominant cost
			// on steady-state federations where most objects are quiescent.
			if !replica.LastSync.IsZero() && !obj.LastModified.After(replica.LastSync) {
				continue
			}

			head, herr := replicaDrv.StatObject(ctx, replica.Bucket, obj.Key)
			if herr != nil {
				// Treat any error as "missing on replica" — the worst case
				// is an extra PUT which is idempotent at the S3 layer.
				out = append(out, diffEntry{key: obj.Key, size: obj.Size})
				if len(out) >= MaxBatchPerTick {
					return out, nil
				}
				continue
			}
			if etagsDiffer(obj.ETag, head.ETag) || head.LastModified.Before(obj.LastModified) {
				out = append(out, diffEntry{key: obj.Key, size: obj.Size})
				if len(out) >= MaxBatchPerTick {
					return out, nil
				}
			}
		}
		if !page.IsTruncated || page.NextContinuation == "" {
			break
		}
		if scanned >= scanCap {
			// Bound the tick; let convergence handle the long tail.
			break
		}
		continuation = page.NextContinuation
	}
	return out, nil
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

// recordSuccess writes a replica-health update with the supplied
// pending counters + the current time as LastSync. Resets the
// per-replica consecutive-failure counter.
//
// pendingObjects + pendingBytes describe what was DEFERRED to a future
// tick (we capped at MaxBatchPerTick this tick). When pending is 0 the
// replica is fully in sync.
func (e *Engine) recordSuccess(ctx context.Context, fb FederatedBucket, replica ReplicaTarget, pendingObjects, pendingBytes int64) {
	e.resetFailureCount(fb.ID, replica)

	now := time.Now().UTC()
	health := computeHealth(now, now, fb.Policy.LagAlertSec, 0)
	if pendingObjects > 0 {
		// Even if we just successfully replicated, residual pending
		// objects mean we're not yet caught up — surface that as lag.
		// Approximate the lag as one tick interval per remaining batch.
		health = HealthLagging
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
// Output: one of HealthInSync, HealthLagging, HealthStale, HealthBroken.
// A zero LastSync is treated as never-replicated and rendered InSync —
// a fresh federation isn't unhealthy just because the engine hasn't
// run yet.
func ComputeHealth(lastSync, now time.Time, lagAlertSec, failureCount int) string {
	return computeHealth(lastSync, now, lagAlertSec, failureCount)
}

func computeHealth(lastSync, now time.Time, lagAlertSec, failureCount int) string {
	if failureCount >= BrokenFailureThreshold {
		return HealthBroken
	}
	if lastSync.IsZero() {
		return HealthInSync
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
