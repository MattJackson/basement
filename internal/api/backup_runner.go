// Package api: backup -> sync-engine runner bridge (v1.5.0a).
//
// backup.Scheduler invokes a backup.Runner each time a Backup is due.
// The runner here:
//
//   1. Translates a Backup's SrcRegionID/DstRegionID to admin
//      Connection IDs via the same resolveRegionToConnection bridge
//      that user_syncs.go uses.
//   2. Builds a one-shot sync.SyncJob from the Backup's source +
//      destination + prefix fields. Schedule itself is irrelevant
//      here — the cron entry already decided it's time to run.
//   3. Calls sync.Engine.Run synchronously and translates the
//      progress + error into a backup.BackupResult.
//
// We deliberately do NOT save the SyncJob into syncStore — the
// /files/syncs UI is for the user's ad-hoc copies. Backup runs live
// in backups.json (history) and never appear in the syncs list.
package api

import (
	"context"
	"fmt"
	"time"

	"github.com/mattjackson/basement/internal/backup"
	"github.com/mattjackson/basement/internal/sync"
)

// newBackupRunner returns a backup.Runner closure bound to this
// Server. Pulled out as a method so server wiring (cmd/basement-server/main.go)
// can pass `s.NewBackupRunner()` straight into backup.NewScheduler.
func (s *Server) NewBackupRunner() backup.Runner {
	return backup.RunnerFunc(func(ctx context.Context, b backup.Backup) backup.BackupResult {
		return s.runBackupOnce(ctx, b)
	})
}

// runBackupOnce executes a single backup job and returns the
// outcome. Designed to NEVER panic — any error path produces a
// failure result that the scheduler can record.
func (s *Server) runBackupOnce(ctx context.Context, b backup.Backup) backup.BackupResult {
	started := time.Now().UTC()
	result := backup.BackupResult{
		StartedAt: started,
		Success:   false,
	}

	// Resolve src/dst region IDs to admin Connection IDs. If either
	// side is already a real Connection.ID, resolveRegionToConnection
	// returns ErrRegionNotFound — we then fall back to using the raw
	// value (which the sync engine path treats as a connection ID).
	srcConn, err := s.resolveBackupConn(ctx, b.OwnerUserID, b.SrcRegionID)
	if err != nil {
		result.CompletedAt = time.Now().UTC()
		result.Errors = []string{fmt.Sprintf("resolve source: %v", err)}
		return result
	}
	dstConn, err := s.resolveBackupConn(ctx, b.OwnerUserID, b.DstRegionID)
	if err != nil {
		result.CompletedAt = time.Now().UTC()
		result.Errors = []string{fmt.Sprintf("resolve destination: %v", err)}
		return result
	}

	if s.reg == nil {
		result.CompletedAt = time.Now().UTC()
		result.Errors = []string{"driver registry is not wired"}
		return result
	}

	srcDrv, err := s.reg.For(ctx, srcConn)
	if err != nil {
		result.CompletedAt = time.Now().UTC()
		result.Errors = []string{fmt.Sprintf("open source driver: %v", err)}
		return result
	}
	dstDrv, err := s.reg.For(ctx, dstConn)
	if err != nil {
		result.CompletedAt = time.Now().UTC()
		result.Errors = []string{fmt.Sprintf("open destination driver: %v", err)}
		return result
	}

	job := &sync.SyncJob{
		ID:              sync.GenerateID(),
		OwnerUserID:     b.OwnerUserID,
		Mode:            "pull",
		SrcConnectionID: srcConn,
		SrcBucket:       b.SrcBucket,
		SrcPrefix:       b.SrcPrefix,
		DstConnectionID: dstConn,
		DstBucket:       b.DstBucket,
		DstPrefix:       b.DstPrefix,
		CreatedAt:       started,
		State:           "queued",
	}
	result.JobID = job.ID

	// We use a transient in-memory sync.Store for the engine call —
	// backup runs intentionally don't pollute the user's /files/syncs
	// list. sync.NewEngine accepts any Store; this avoids needing a
	// new no-op constructor.
	engine := sync.NewEngine(noopSyncStore{}, 4)
	runErr := engine.Run(ctx, job, srcDrv, dstDrv)
	result.CompletedAt = time.Now().UTC()
	result.ObjectsCopied = int64(job.Progress.ObjectsCopied)
	result.BytesCopied = job.Progress.BytesCopied
	if runErr != nil {
		result.Errors = appendBounded(result.Errors, runErr.Error())
		return result
	}
	if job.LastError != "" {
		result.Errors = appendBounded(result.Errors, job.LastError)
		return result
	}
	result.Success = true
	return result
}

// resolveBackupConn handles the SrcRegionID/DstRegionID -> Connection.ID
// translation. If the value is already a Connection.ID, the resolver
// returns ErrRegionNotFound and we pass it through verbatim — keeping
// the bridge symmetric with maybeResolveRegionField in
// region_resolver.go.
func (s *Server) resolveBackupConn(ctx context.Context, userID, raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("empty connection identifier")
	}
	// Try the region path first.
	if s.regionsStore() != nil {
		region, err := s.regionsStore().Get(ctx, raw)
		if err == nil && region.UserID == userID {
			connID, rerr := s.resolveRegionToConnection(ctx, userID, raw)
			if rerr == nil {
				return connID, nil
			}
			return "", fmt.Errorf("region has no admin bridge (endpoint %q)", region.Endpoint)
		}
	}
	// Fall back to treating raw as a Connection.ID. We don't
	// verify here — the registry.For call later will surface a
	// clear error if it isn't a valid connection.
	return raw, nil
}

// noopSyncStore satisfies sync.Store but discards everything.
// Backup runs intentionally don't appear in /files/syncs, so we
// don't want their per-tick state churn either.
type noopSyncStore struct{}

func (noopSyncStore) Load(_ string) (*sync.SyncJob, error)         { return nil, fmt.Errorf("not used") }
func (noopSyncStore) Save(_ *sync.SyncJob) error                   { return nil }
func (noopSyncStore) List(_ string) ([]*sync.SyncJob, error)       { return nil, nil }
func (noopSyncStore) Delete(_ string) error                        { return nil }

// appendBounded keeps the errors slice under the same cap that
// backup.BackupResult ultimately enforces. Centralised so the runner
// doesn't grow an unbounded list when an engine streams 10k errors.
func appendBounded(existing []string, msg string) []string {
	const cap = 10
	if len(existing) >= cap {
		return existing
	}
	return append(existing, msg)
}
