// Snapshot scheduler — background goroutine that fans out over
// every connection on top of the hour (plus jitter) and records
// one Snapshot per bucket. Runs forever until the parent context
// is cancelled.
//
// Lives in the metrics package (not main.go) so the iteration
// strategy is testable and reusable. main.go just kicks off the
// goroutine.

package metrics

import (
	"context"
	"log/slog"
	"math/rand"
	"time"

	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// driverProvider is the subset of *driver.Registry the scheduler
// needs. Defined as an interface so tests can substitute a fake.
type driverProvider interface {
	For(ctx context.Context, connID string) (driver.Driver, error)
}

// connectionLister is the subset of store.Connections the
// scheduler needs.
type connectionLister interface {
	List(ctx context.Context) ([]store.Connection, error)
}

// SchedulerConfig wires the scheduler's collaborators. All four
// fields are required; main.go passes the production registry +
// connection store + file recorder.
type SchedulerConfig struct {
	Conns    connectionLister
	Reg      driverProvider
	Recorder Recorder
	// PerClusterTimeout caps how long one cluster's fan-out may
	// take. Slow / dead clusters log + continue. Default 10s.
	PerClusterTimeout time.Duration
	// Interval between cycles. Production is 1 hour; tests pass
	// a short interval. Zero defaults to 1 hour.
	Interval time.Duration
	// MaxJitter caps the random delay added per cycle so a fleet
	// of basement pods doesn't all snapshot at the exact same
	// second. Default 90s; pass 0 to disable.
	MaxJitter time.Duration
}

// RunScheduler runs the hourly snapshot loop until ctx is done.
// Survives every per-cluster error (logs + continues) — the only
// way this returns is ctx.Err().
//
// First snapshot fires immediately on startup so an operator who
// just deployed isn't waiting an hour for the first data point.
func RunScheduler(ctx context.Context, cfg SchedulerConfig) {
	if cfg.PerClusterTimeout == 0 {
		cfg.PerClusterTimeout = 10 * time.Second
	}
	if cfg.Interval == 0 {
		cfg.Interval = time.Hour
	}
	if cfg.MaxJitter < 0 {
		cfg.MaxJitter = 0
	}

	// Immediate first cycle.
	runOneCycle(ctx, cfg)

	for {
		// Sleep until interval + jitter or ctx cancel.
		jitter := time.Duration(0)
		if cfg.MaxJitter > 0 {
			jitter = time.Duration(rand.Int63n(int64(cfg.MaxJitter)))
		}
		wait := cfg.Interval + jitter
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		runOneCycle(ctx, cfg)
	}
}

// runOneCycle iterates every connection and records one Snapshot
// per bucket. Per-connection errors log + continue; the cycle
// never short-circuits on one bad backend.
func runOneCycle(ctx context.Context, cfg SchedulerConfig) {
	if cfg.Conns == nil || cfg.Reg == nil || cfg.Recorder == nil {
		slog.Warn("metrics: scheduler not configured; skipping cycle")
		return
	}

	start := time.Now()
	conns, err := cfg.Conns.List(ctx)
	if err != nil {
		slog.Warn("metrics: listing connections failed", "error", err)
		return
	}

	totalBuckets := 0
	for _, conn := range conns {
		select {
		case <-ctx.Done():
			return
		default:
		}

		cctx, cancel := context.WithTimeout(ctx, cfg.PerClusterTimeout)
		drv, derr := cfg.Reg.For(cctx, conn.ID)
		if derr != nil {
			cancel()
			slog.Warn("metrics: building driver failed", "connection_id", conn.ID, "error", derr)
			continue
		}

		buckets, berr := drv.ListBuckets(cctx)
		if berr != nil {
			cancel()
			slog.Warn("metrics: listing buckets failed", "connection_id", conn.ID, "error", berr)
			continue
		}
		cancel()

		for _, b := range buckets {
			alias := ""
			if len(b.Aliases) > 0 {
				alias = b.Aliases[0]
			}
			snap := Snapshot{
				Time:         time.Now().UTC(),
				ConnectionID: conn.ID,
				BucketID:     b.ID,
				BucketAlias:  alias,
				Bytes:        b.Bytes,
				Objects:      b.Objects,
			}
			if err := cfg.Recorder.Snapshot(snap); err != nil {
				// FileRecorder already slog-warned; one bad
				// write doesn't stop the rest of the cycle.
				continue
			}
			totalBuckets++
		}
	}

	slog.Info("metrics: snapshot cycle complete",
		"connections", len(conns),
		"buckets", totalBuckets,
		"duration_ms", time.Since(start).Milliseconds(),
	)
}
