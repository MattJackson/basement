package backup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	stdsync "sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Runner is the contract the Scheduler uses to actually copy
// objects. The production implementation builds a sync.SyncJob and
// invokes sync.Engine.Run; tests substitute a stub that returns a
// deterministic BackupResult so the scheduler can be exercised
// without spinning up real drivers.
//
// Returning a BackupResult (rather than mutating one) keeps the
// Runner pure — the Scheduler is the only writer of result state.
type Runner interface {
	Run(ctx context.Context, b Backup) BackupResult
}

// RunnerFunc adapts a function to the Runner interface.
type RunnerFunc func(ctx context.Context, b Backup) BackupResult

// Run implements Runner.
func (f RunnerFunc) Run(ctx context.Context, b Backup) BackupResult {
	return f(ctx, b)
}

// Scheduler is a thin wrapper around robfig/cron/v3 that maps
// Backup.ID -> cron.EntryID. Add/Remove/Reschedule keep the two
// in sync. Job execution always happens in a goroutine (cron.New
// has no chain wrapper) and we install a panic recover so a
// single misbehaving Backup can't kill the cron loop.
type Scheduler struct {
	mu       stdsync.Mutex
	cron     *cron.Cron
	parser   cron.Parser
	store    Backups
	runner   Runner
	logger   *slog.Logger
	entries  map[string]cron.EntryID
	started  bool
}

// NewScheduler wires up an unstarted Scheduler. Call LoadAll then
// Start to begin firing jobs; both are split so the API server can
// wire Add/Remove for ad-hoc CRUD even before Start, and so a test
// can drive ticks deterministically by calling Trigger directly.
func NewScheduler(store Backups, runner Runner, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	// 5-field standard cron + @hourly / @daily / @weekly /
	// @monthly / @yearly descriptors. We don't expose seconds
	// because the wizard never produces a sub-minute cadence
	// and accepting them would let an operator paste in a
	// schedule that flatlines the server.
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	return &Scheduler{
		// Both Scheduler.Add (for pre-validation) and the inner
		// cron loop use the same parser, so the two never disagree
		// on what a "valid" schedule looks like.
		cron:    cron.New(cron.WithParser(parser)),
		parser:  parser,
		store:   store,
		runner:  runner,
		logger:  logger,
		entries: map[string]cron.EntryID{},
	}
}

// LoadAll registers every persisted Backup that has a non-manual
// schedule + Disabled=false. Used by main.go at startup. Errors
// from individual entries are logged but never abort the load —
// one bad cron expression shouldn't keep the operator's other
// backups from running.
func (s *Scheduler) LoadAll(ctx context.Context) error {
	all, err := s.store.All(ctx)
	if err != nil {
		return fmt.Errorf("loading backups: %w", err)
	}
	for _, b := range all {
		if !s.isSchedulable(b) {
			continue
		}
		if err := s.Add(b); err != nil {
			s.logger.Warn("backup scheduler: failed to register backup",
				"backupId", b.ID, "schedule", b.Schedule, "error", err)
		}
	}
	return nil
}

// Start begins the cron loop. Idempotent — calling Start twice is a
// no-op rather than an error so the test harness doesn't have to
// special-case re-use.
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return
	}
	s.cron.Start()
	s.started = true
}

// Stop halts the cron loop and waits for any in-flight goroutines
// to complete. Returns the context returned by cron.Stop so callers
// can wait on shutdown if they want — main.go currently doesn't,
// since the process is exiting anyway.
func (s *Scheduler) Stop() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	s.started = false
	return s.cron.Stop()
}

// Add registers a Backup's cron entry. If the Backup already has
// an entry we Remove the old one first so a Reschedule via this
// path is safe. Returns an error only on invalid cron expression.
//
// Skips registration for manual / disabled backups (callers should
// pre-filter via isSchedulable, but we double-check here so an
// out-of-band caller can't accidentally schedule a manual job).
func (s *Scheduler) Add(b Backup) error {
	if !s.isSchedulable(b) {
		return nil
	}
	if _, err := s.parser.Parse(b.Schedule); err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", b.Schedule, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.entries[b.ID]; ok {
		s.cron.Remove(existing)
		delete(s.entries, b.ID)
	}
	id, err := s.cron.AddFunc(b.Schedule, func() { s.fire(b.ID) })
	if err != nil {
		return fmt.Errorf("registering cron entry: %w", err)
	}
	s.entries[b.ID] = id
	return nil
}

// Remove drops the cron entry for a Backup. Safe to call on a
// Backup that was never scheduled (no-op).
func (s *Scheduler) Remove(backupID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.entries[backupID]; ok {
		s.cron.Remove(id)
		delete(s.entries, backupID)
	}
}

// Reschedule is a convenience that re-resolves the latest stored
// state for a Backup and rebuilds its cron entry from scratch.
// Used by the API handlers after Update.
func (s *Scheduler) Reschedule(ctx context.Context, backupID string) error {
	b, err := s.store.Get(ctx, backupID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.Remove(backupID)
			return nil
		}
		return err
	}
	s.Remove(backupID)
	return s.Add(b)
}

// Trigger runs a Backup right now, bypassing the schedule. Used by
// the /run endpoint and by tests. Synchronous so the test can
// inspect the recorded result; the API handler wraps it in a
// goroutine to keep the HTTP response under the 15s WriteTimeout.
func (s *Scheduler) Trigger(ctx context.Context, backupID string) error {
	return s.runAndRecord(ctx, backupID)
}

// fire is the cron-callback entry point. We swallow the cron's
// own context (there isn't one) and use context.Background, since
// these jobs may legitimately outlive a single tick.
//
// Panics inside the runner are caught and logged so one bad
// Backup can't take down the scheduler loop.
func (s *Scheduler) fire(backupID string) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("backup scheduler: panic in fire",
				"backupId", backupID, "panic", r)
		}
	}()
	if err := s.runAndRecord(context.Background(), backupID); err != nil {
		s.logger.Error("backup scheduler: run failed",
			"backupId", backupID, "error", err)
	}
}

// runAndRecord loads the latest Backup state, invokes the runner,
// and writes the result back to the store. Disabled / missing
// backups are no-ops so a stale cron tick after Delete doesn't
// resurrect data.
func (s *Scheduler) runAndRecord(ctx context.Context, backupID string) error {
	b, err := s.store.Get(ctx, backupID)
	if err != nil {
		return err
	}
	if b.Disabled {
		return nil
	}
	if s.runner == nil {
		return errors.New("scheduler: no runner configured")
	}
	started := time.Now().UTC()
	result := s.runner.Run(ctx, b)
	// Belt-and-braces: a runner may forget to fill these; ensure
	// the persisted result has at least an envelope so the UI never
	// renders a half-empty card.
	if result.StartedAt.IsZero() {
		result.StartedAt = started
	}
	if result.CompletedAt.IsZero() {
		result.CompletedAt = time.Now().UTC()
	}
	return s.store.RecordResult(ctx, b.ID, result)
}

// isSchedulable returns true when a Backup should be registered as a
// recurring cron entry. Manual + disabled backups stay out of cron
// but can still be invoked through Trigger.
func (s *Scheduler) isSchedulable(b Backup) bool {
	if b.Disabled {
		return false
	}
	if b.Schedule == "" || b.Schedule == ScheduleManual {
		return false
	}
	return true
}

// EntryCount returns how many cron entries the scheduler is
// currently tracking. Exposed for tests so they can assert that
// Add / Remove / Disabled handling actually wired things up.
func (s *Scheduler) EntryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}
