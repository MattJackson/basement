package sync

import (
	"context"
	"fmt"
	"sync"
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

// Run executes a sync job with bounded parallelism.
func (e *Engine) Run(ctx context.Context, job *SyncJob, srcDriver driver.Driver, dstDriver driver.Driver) error {
	job.State = "running"
	now := time.Now()
	job.Progress.StartedAt = &now
	e.store.Save(job)

	// Plan the sync
	actions, err := Plan(ctx, srcDriver, dstDriver, job.SrcConnectionID, job.SrcBucket, job.SrcPrefix, job.DstConnectionID, job.DstBucket, job.DstPrefix)
	if err != nil {
		job.State = "error"
		job.LastError = fmt.Sprintf("plan failed: %v", err)
		e.store.Save(job)
		return fmt.Errorf("planning sync: %w", err)
	}

	job.Progress.ObjectsTotal = len(actions)
	e.store.Save(job)

	// Process actions with bounded parallelism
	sem := make(chan struct{}, e.maxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for _, action := range actions {
		wg.Add(1)
		sem <- struct{}{}

		go func(a Action) {
			defer wg.Done()
			defer func() { <-sem }()

			if job.State == "paused" || job.State == "error" {
				return
			}

			err := e.copyObject(ctx, srcDriver, dstDriver, a)
			if err != nil {
				mu.Lock()
				if firstErr == nil && (job.State != "paused") {
					firstErr = err
					job.State = "error"
					job.LastError = err.Error()
					e.store.Save(job)
				}
				mu.Unlock()
				return
			}

			mu.Lock()
			job.Progress.ObjectsCopied++
			if action.ObjectInfo != nil {
				job.Progress.BytesCopied += action.ObjectInfo.Size
			}
			mu.Unlock()
			e.store.Save(job)
		}(action)
	}

	wg.Wait()

	if job.State == "running" || job.State == "paused" {
		if firstErr != nil {
			return firstErr
		}
		job.State = "done"
		finished := time.Now()
		job.Progress.FinishedAt = &finished
		e.store.Save(job)
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

// Pause pauses a running sync job.
func (e *Engine) Pause(ctx context.Context, jobID string) error {
	job, err := e.store.Load(jobID)
	if err != nil {
		return fmt.Errorf("loading job: %w", err)
	}

	if job.State != "running" && job.State != "queued" {
		return fmt.Errorf("job not in running state")
	}

	job.State = "paused"
	e.store.Save(job)
	return nil
}

// Resume resumes a paused sync job.
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
	e.store.Save(job)

	go e.Run(ctx, job, srcDriver, dstDriver)
	return nil
}
