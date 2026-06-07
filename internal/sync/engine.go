package sync

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mattjackson/basement/internal/driver"
)

// Engine handles execution of sync jobs with bounded parallelism.
type Engine struct {
	maxConcurrency int
	store          Store
}

// NewEngine creates a new sync engine.
func NewEngine(store Store, maxConcurrency int) *Engine {
	if maxConcurrency <= 0 {
		maxConcurrency = 4
	}
	return &Engine{
		maxConcurrency: maxConcurrency,
		store:          store,
	}
}

// runState is the live, in-memory control handle for a sync run currently
// executing in some goroutine. It is shared between the goroutine doing the
// work (which reads paused / ctx) and any Pause caller (which writes paused
// and calls cancel).
//
// Pause/Resume historically loaded a fresh *SyncJob from disk and mutated
// THAT pointer, which the running goroutine — holding a different *SyncJob —
// never observed. The registry fixes that by keying live runs on job ID so a
// Pause issued from a different Engine instance (the API builds a fresh
// Engine per HTTP request) still reaches the in-flight goroutine.
type runState struct {
	paused atomic.Bool
	cancel context.CancelFunc
}

// runRegistry tracks live sync runs across every Engine instance in the
// process. It is package-global on purpose: the HTTP layer constructs a fresh
// Engine per request, so a per-Engine map would never let a Pause handler
// reach the goroutine spawned by the create handler. Keyed by job ID.
var runRegistry = struct {
	mu     sync.Mutex
	active map[string]*runState
}{active: make(map[string]*runState)}

// registerRun installs a runState for jobID, replacing (and cancelling) any
// prior run for the same job. Returns the new runState.
func registerRun(jobID string, cancel context.CancelFunc) *runState {
	rs := &runState{cancel: cancel}
	runRegistry.mu.Lock()
	if prev, ok := runRegistry.active[jobID]; ok && prev.cancel != nil {
		// A stale run for this job is still registered; cancel it so two
		// goroutines don't fight over the same job.
		prev.cancel()
	}
	runRegistry.active[jobID] = rs
	runRegistry.mu.Unlock()
	return rs
}

// deregisterRun removes the runState for jobID, but only if it is still the
// one we registered (so a Resume that replaced us doesn't get clobbered).
func deregisterRun(jobID string, rs *runState) {
	runRegistry.mu.Lock()
	if cur, ok := runRegistry.active[jobID]; ok && cur == rs {
		delete(runRegistry.active, jobID)
	}
	runRegistry.mu.Unlock()
}

// lookupRun returns the live runState for jobID, if any.
func lookupRun(jobID string) (*runState, bool) {
	runRegistry.mu.Lock()
	rs, ok := runRegistry.active[jobID]
	runRegistry.mu.Unlock()
	return rs, ok
}

// Run executes a sync job with bounded parallelism.
//
// Run detaches from the caller's context: it derives its own cancellable run
// context (via context.WithoutCancel) so that an HTTP request context being
// cancelled when the handler returns cannot kill a background sync. The run
// context's cancel func is stored in the registry so Pause (and future
// shutdown logic) can stop the run.
func (e *Engine) Run(ctx context.Context, job *SyncJob, srcDriver driver.Driver, dstDriver driver.Driver) error {
	// Detach from the caller's lifecycle but keep its values (deadlines that
	// belong to a request are intentionally dropped — a background sync must
	// outlive the request that triggered it).
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	rs := registerRun(job.ID, cancel)
	defer cancel()
	defer deregisterRun(job.ID, rs)

	job.State = "running"
	now := time.Now()
	job.Progress.StartedAt = &now
	if err := e.store.Save(job); err != nil {
		// Persisting the initial state failed; surface but keep going so an
		// in-memory run can still complete (the store may recover).
		_ = err
	}

	// Plan the sync
	actions, err := Plan(runCtx, srcDriver, dstDriver, job.SrcConnectionID, job.SrcBucket, job.SrcPrefix, job.DstConnectionID, job.DstBucket, job.DstPrefix)
	if err != nil {
		job.State = "error"
		job.LastError = fmt.Sprintf("plan failed: %v", err)
		_ = e.store.Save(job)
		return fmt.Errorf("planning sync: %w", err)
	}

	job.Progress.ObjectsTotal = len(actions)
	// Populate BytesTotal so byte-progress is meaningful (was never set).
	var bytesTotal int64
	for i := range actions {
		if actions[i].ObjectInfo != nil {
			bytesTotal += actions[i].ObjectInfo.Size
		}
	}
	job.Progress.BytesTotal = bytesTotal
	_ = e.store.Save(job)

	// Process actions with bounded parallelism
	sem := make(chan struct{}, e.maxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for _, action := range actions {
		// Stop dispatching as soon as the run is paused/cancelled rather than
		// queueing every remaining object first.
		if rs.paused.Load() || runCtx.Err() != nil {
			break
		}

		// ctx-aware semaphore acquire so a cancelled run isn't wedged waiting
		// for a worker slot. The select's `break` only exits the select; the
		// runCtx.Err() check below exits the dispatch loop.
		select {
		case sem <- struct{}{}:
		case <-runCtx.Done():
		}
		if runCtx.Err() != nil {
			break
		}

		wg.Add(1)
		go func(a Action) {
			defer wg.Done()
			defer func() { <-sem }()
			// Recover so a single bad object (e.g. a nil body / driver panic)
			// can't crash the whole server process.
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("panic copying %s: %v", a.SrcKey, r)
						job.State = "error"
						job.LastError = fmt.Sprintf("panic copying %s: %v", a.SrcKey, r)
						_ = e.store.Save(job)
					}
					mu.Unlock()
					cancel() // stop the rest of the run
				}
			}()

			// All job.State / job.Progress access is guarded by mu; Save is
			// done under the lock so the store never marshals a job while
			// another worker is mutating it (was a data race). Use the
			// captured `a`, NOT the loop variable `action`.
			//
			// Stop is now driven by the SHARED runState.paused flag (set by
			// Pause through the registry) — not by re-reading job.State from a
			// disconnected disk copy.
			if rs.paused.Load() || runCtx.Err() != nil {
				return
			}
			mu.Lock()
			stop := job.State == "error"
			mu.Unlock()
			if stop {
				return
			}

			// "skip" actions are etag-matched at plan time — the destination
			// already holds an identical object, so do NOT re-stream it.
			// Count it toward progress (objects skipped) but transfer 0 bytes.
			if a.ActionType == "skip" {
				mu.Lock()
				job.Progress.ObjectsSkipped++
				_ = e.store.Save(job)
				mu.Unlock()
				return
			}

			err := e.copyObject(runCtx, srcDriver, dstDriver, a)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil && !rs.paused.Load() && job.State != "paused" {
					firstErr = err
					job.State = "error"
					job.LastError = err.Error()
					_ = e.store.Save(job)
					cancel() // abort in-flight + undispatched work on first error
				}
				return
			}
			job.Progress.ObjectsCopied++
			if a.ObjectInfo != nil {
				job.Progress.BytesCopied += a.ObjectInfo.Size
			}
			_ = e.store.Save(job)
		}(action)
	}

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if firstErr != nil {
		return firstErr
	}
	// A paused run must STAY paused; Pause already persisted "paused" to disk
	// and set the shared flag. Don't overwrite it to "done".
	if rs.paused.Load() {
		job.State = "paused"
		_ = e.store.Save(job)
		return nil
	}
	// Only a still-"running" job transitions to "done".
	if job.State == "running" {
		job.State = "done"
		finished := time.Now()
		job.Progress.FinishedAt = &finished
		_ = e.store.Save(job)
	}

	return nil
}

func (e *Engine) copyObject(ctx context.Context, srcDriver driver.Driver, dstDriver driver.Driver, action Action) error {
	// Try ServerSideCopy if same driver and capability available
	srcCaps, err := srcDriver.Capabilities(ctx)
	if err != nil {
		return fmt.Errorf("getting source capabilities: %w", err)
	}

	dstCaps, err := dstDriver.Capabilities(ctx)
	if err != nil {
		return fmt.Errorf("getting dest capabilities: %w", err)
	}

	if srcCaps.Driver == dstCaps.Driver && dstCaps.ServerSideCopy {
		err = dstDriver.ServerSideCopy(ctx, action.SrcBucket, action.SrcKey, action.DstBucket, action.DstKey)
		if err == nil {
			return nil
		}
		// Fall through to streaming if ServerSideCopy fails
	}

	// Stream object from source to destination
	srcResult, err := srcDriver.StreamObject(ctx, action.SrcBucket, action.SrcKey, "")
	if err != nil {
		return fmt.Errorf("streaming source object: %w", err)
	}
	defer srcResult.Body.Close()

	_, err = dstDriver.PutObjectStream(ctx, action.DstBucket, action.DstKey, srcResult.Body, srcResult.ContentType, srcResult.ContentLength)
	if err != nil {
		return fmt.Errorf("putting dest object: %w", err)
	}

	return nil
}

// Pause pauses a running sync job. It signals the LIVE run (if one is
// registered in this process) by setting the shared paused flag and
// cancelling its run context, then persists "paused" to the store so the
// state survives a reload. Pausing a job with no live run still persists the
// paused state.
func (e *Engine) Pause(ctx context.Context, jobID string) error {
	job, err := e.store.Load(jobID)
	if err != nil {
		return fmt.Errorf("loading job: %w", err)
	}

	if job.State != "running" && job.State != "queued" {
		return fmt.Errorf("job not in running state")
	}

	// Signal the live run first so workers stop ASAP, then persist. Setting
	// the flag before the live run's own terminal Save means its Run loop
	// observes paused == true and writes "paused" rather than "done".
	if rs, ok := lookupRun(jobID); ok {
		rs.paused.Store(true)
		if rs.cancel != nil {
			rs.cancel()
		}
	}

	job.State = "paused"
	if err := e.store.Save(job); err != nil {
		return fmt.Errorf("saving paused state: %w", err)
	}
	return nil
}

// Resume resumes a paused sync job. The run is detached from the caller's
// context inside Run (via context.WithoutCancel), so the caller may safely
// pass a request-scoped context that gets cancelled when the HTTP handler
// returns — the background run keeps going regardless.
func (e *Engine) Resume(ctx context.Context, jobID string, srcDriver driver.Driver, dstDriver driver.Driver) error {
	job, err := e.store.Load(jobID)
	if err != nil {
		return fmt.Errorf("loading job: %w", err)
	}

	if job.State != "paused" {
		return fmt.Errorf("job not in paused state")
	}

	job.State = "running"
	now := time.Now()
	if job.Progress.StartedAt == nil {
		job.Progress.StartedAt = &now
	}
	if err := e.store.Save(job); err != nil {
		return fmt.Errorf("saving resumed state: %w", err)
	}

	// Run derives its own background run context and registers the live run;
	// the caller's ctx does not gate the goroutine's lifetime.
	go e.Run(ctx, job, srcDriver, dstDriver)
	return nil
}
