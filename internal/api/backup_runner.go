// Package api: backup -> sync-engine runner bridge (v1.5.0a/b).
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
// v1.5.0b layers two behaviours on the v1.5.0a foundation:
//
//   - Snapshot mode: when Backup.Mode == BackupModeSnapshot, step 2
//     above writes into {DstBucket}/{slug(Name)}/{ts}/ instead of
//     the operator-configured DstPrefix.
//   - Retention prune: after a successful snapshot run, list the
//     existing snapshots under {DstBucket}/{slug(Name)}/, compute
//     keep/prune via backup.PlanPrune, and recursively delete the
//     pruned timestamps. Best-effort: a prune failure logs but
//     never overturns the run's Success flag — the snapshot itself
//     landed cleanly, so the operator's data is safe.
//
// We deliberately do NOT save the SyncJob into syncStore — the
// /files/syncs UI is for the user's ad-hoc copies. Backup runs live
// in backups.json (history) and never appear in the syncs list.
package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mattjackson/basement/internal/backup"
	"github.com/mattjackson/basement/internal/driver"
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

	// Resolve mode + destination layout. Snapshot mode owns the
	// destination prefix end-to-end (the operator-configured
	// DstPrefix is ignored on purpose so the retention scan can
	// list every snapshot under one root).
	mode := b.ResolveMode()
	dstPrefix := b.DstPrefix
	snapshotPrefix := ""
	if mode == backup.BackupModeSnapshot {
		snapshotPrefix = backup.SnapshotPrefix(b.Name, started)
		// SnapshotPrefix already ends with "/"; the sync engine's
		// plan.go handles that just fine — it normalises by
		// trimming the trailing "/" when re-applying.
		dstPrefix = strings.TrimSuffix(snapshotPrefix, "/")
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
		DstPrefix:       dstPrefix,
		CreatedAt:       started,
		State:           "queued",
	}
	result.JobID = job.ID
	result.SnapshotPrefix = snapshotPrefix

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

	// Snapshot mode: run the retention prune pass once the copy has
	// landed. Failures here log + add to Errors but do NOT flip
	// Success — the snapshot itself is safe; only the cleanup
	// stumbled. The next successful run will retry the prune.
	if mode == backup.BackupModeSnapshot {
		pruned, reclaimed, perr := s.pruneSnapshots(ctx, b, dstDrv, started)
		result.SnapshotsPruned = pruned
		result.BytesReclaimed = reclaimed
		if perr != nil {
			result.Errors = appendBounded(result.Errors, fmt.Sprintf("retention prune: %v", perr))
		}
	}

	return result
}

// pruneSnapshots enumerates the existing snapshot timestamps under
// {DstBucket}/{slug(Name)}/, computes keep/prune via PlanPrune, and
// recursively deletes each pruned snapshot's contents.
//
// `now` is the run's started-at: every snapshot present (including
// the one we just wrote) is younger than or equal to this point, so
// PlanPrune's clock-skew guard never trips on the run itself.
//
// Returns (snapshotsPruned, bytesReclaimed, error). A partial failure
// is reported via the error but the counters still reflect what got
// removed before the failure.
func (s *Server) pruneSnapshots(ctx context.Context, b backup.Backup, dstDrv driver.Driver, now time.Time) (int64, int64, error) {
	root := backup.SnapshotRoot(b.Name)
	timestamps, err := listSnapshotTimestamps(ctx, dstDrv, b.DstBucket, root)
	if err != nil {
		return 0, 0, fmt.Errorf("list snapshots: %w", err)
	}
	if len(timestamps) == 0 {
		return 0, 0, nil
	}
	_, pruneList := backup.PlanPrune(timestamps, b.ResolveRetention(), now)
	if len(pruneList) == 0 {
		return 0, 0, nil
	}

	var totalPruned int64
	var totalBytes int64
	var firstErr error
	for _, ts := range pruneList {
		prefix := backup.SnapshotPrefix(b.Name, ts)
		bytes, derr := deleteAllUnderPrefix(ctx, dstDrv, b.DstBucket, prefix)
		totalBytes += bytes
		if derr != nil {
			// Log + continue: a single failed deletion shouldn't
			// halt the rest of the pass. The next run retries.
			s.logger.Warn("backup: prune snapshot failed",
				"backupId", b.ID, "prefix", prefix, "error", derr)
			if firstErr == nil {
				firstErr = derr
			}
			continue
		}
		totalPruned++
	}
	return totalPruned, totalBytes, firstErr
}

// listSnapshotTimestamps walks {bucket}/{root} with delimiter="/" and
// returns the timestamps of every CommonPrefix that matches the
// snapshot pattern. Non-matching prefixes are ignored so operator-
// owned data dropped alongside the managed snapshots never appears
// in the prune candidate set.
func listSnapshotTimestamps(ctx context.Context, drv driver.Driver, bucket, root string) ([]time.Time, error) {
	var out []time.Time
	var continuation string
	for {
		page, err := drv.ListObjects(ctx, bucket, root, continuation, "/", 1000)
		if err != nil {
			return nil, err
		}
		for _, cp := range page.CommonPrefixes {
			ts, ok := backup.ParseSnapshotTimestamp(cp)
			if !ok {
				continue
			}
			out = append(out, ts)
		}
		if !page.IsTruncated || page.NextContinuation == "" {
			break
		}
		continuation = page.NextContinuation
	}
	return out, nil
}

// deleteAllUnderPrefix recursively removes every object under
// {bucket}/{prefix} (flat list, delete one at a time). Returns the
// sum of object Sizes for the bytesReclaimed metric. A per-object
// delete error stops the loop and surfaces — the caller logs and
// moves on to the next snapshot prefix.
//
// We use delimiter="" so a deep snapshot tree gets enumerated in a
// single linear pass. S3 multi-object delete would be faster but
// the Driver interface doesn't expose it uniformly across our four
// backends; the per-object DeleteObject is the lowest common
// denominator and survives mixed-driver setups.
func deleteAllUnderPrefix(ctx context.Context, drv driver.Driver, bucket, prefix string) (int64, error) {
	var bytes int64
	var continuation string
	for {
		page, err := drv.ListObjects(ctx, bucket, prefix, continuation, "", 1000)
		if err != nil {
			return bytes, fmt.Errorf("list under %q: %w", prefix, err)
		}
		for _, obj := range page.Objects {
			if obj.IsDir {
				continue
			}
			if derr := drv.DeleteObject(ctx, bucket, obj.Key); derr != nil {
				return bytes, fmt.Errorf("delete %q: %w", obj.Key, derr)
			}
			bytes += obj.Size
		}
		if !page.IsTruncated || page.NextContinuation == "" {
			break
		}
		continuation = page.NextContinuation
	}
	return bytes, nil
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
