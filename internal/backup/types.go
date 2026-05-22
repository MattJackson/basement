// Package backup wraps the v0.8.x sync engine in a scheduled,
// recurring "backup" abstraction (v1.5.0a).
//
// A Backup is an operator-named source -> destination copy job with a
// cron schedule. The package layers two concerns on top of the existing
// internal/sync engine:
//
//   - Store: persistent CRUD for Backup records, single JSON file under
//     {dataDir}/backups.json, atomic write under RWMutex.
//   - Scheduler: wraps github.com/robfig/cron/v3, registers one cron
//     entry per enabled Backup, dispatches into the sync engine when
//     each entry fires.
//
// We deliberately do NOT duplicate Pull/Push semantics — the sync
// engine is the single source of truth for object copying. The
// Scheduler builds a *sync.SyncJob per run, calls Engine.Run, captures
// the result (objects/bytes/errors) into a BackupResult, then writes
// that snapshot back through the Store.
//
// v1.5.0b adds two orthogonal concerns:
//
//   - Mode: a Backup is either a Mirror (continuous overwrite of the
//     destination, the v1.5.0a behaviour) or a Snapshot (each run
//     writes to its own timestamped prefix, building a point-in-time
//     history at the destination).
//   - Retention: for Snapshot backups, a Grandfather-Father-Son rotation
//     policy that prunes old snapshots once they fall outside the
//     keep-window. The algorithm itself lives in retention.go and is a
//     pure function so it can be unit-tested without touching S3.
package backup

import (
	"time"
)

// BackupMode distinguishes the two write semantics a Backup can have.
//
// Mirror (default — v1.5.0a behaviour) writes directly into
// {DstBucket}/{DstPrefix}, overwriting the destination on each run.
// Useful when the operator only ever cares about "the latest copy".
//
// Snapshot writes into {DstBucket}/{slugify(Name)}/{YYYY-MM-DD_HH:MM:SS}/
// instead, building a point-in-time history. The runner then applies
// the configured RetentionPolicy to prune old snapshots.
type BackupMode string

const (
	// BackupModeMirror is the v1.5.0a behaviour and the default for
	// back-compat: each run overwrites the same destination prefix.
	BackupModeMirror BackupMode = "mirror"
	// BackupModeSnapshot writes each run into a fresh timestamped
	// prefix under {DstBucket}/{slug(Name)}/.
	BackupModeSnapshot BackupMode = "snapshot"
)

// RetentionPolicy describes how many snapshots of each cadence the
// runner should keep. Only meaningful when Backup.Mode is
// BackupModeSnapshot — the runner ignores it otherwise.
//
// Grandfather-Father-Son rotation: the KEEP set is the union of the
// last KeepDaily daily snapshots + the last KeepWeekly weekly snapshots
// + the last KeepMonthly monthly snapshots. Defaults are 7/4/12 (one
// week + one month + one year coverage with 23 stored snapshots).
type RetentionPolicy struct {
	KeepDaily   int `json:"keepDaily"`
	KeepWeekly  int `json:"keepWeekly"`
	KeepMonthly int `json:"keepMonthly"`
}

// DefaultRetention is the GFS rotation used when a Snapshot-mode
// Backup is saved without explicit retention values: 7 daily + 4
// weekly + 12 monthly snapshots, ~14 months of history with 23
// snapshots at steady state.
func DefaultRetention() RetentionPolicy {
	return RetentionPolicy{KeepDaily: 7, KeepWeekly: 4, KeepMonthly: 12}
}

// IsZero reports whether all keep-counts are zero — used by the
// store to detect "client sent no retention" and substitute defaults.
func (p RetentionPolicy) IsZero() bool {
	return p.KeepDaily == 0 && p.KeepWeekly == 0 && p.KeepMonthly == 0
}

// Backup is an operator-defined, scheduled bucket-to-bucket copy job.
//
// Field shapes mirror the v1.5 prompt; JSON tags use camelCase to
// match the rest of the v1.x user API surface. SrcRegionID /
// DstRegionID may carry EITHER a real Connection.ID or a UserRegion.ID
// owned by the caller — the API handler runs the same
// maybeResolveRegionField bridge as user_syncs.go before constructing
// the sync job, so we don't need a separate field per shape.
type Backup struct {
	ID          string        `json:"id"`
	OwnerUserID string        `json:"ownerUserId"`
	Name        string        `json:"name"`
	SrcRegionID string        `json:"srcRegionId"`
	SrcBucket   string        `json:"srcBucket"`
	SrcPrefix   string        `json:"srcPrefix,omitempty"`
	DstRegionID string        `json:"dstRegionId"`
	DstBucket   string        `json:"dstBucket"`
	DstPrefix   string        `json:"dstPrefix,omitempty"`
	// Schedule is a cron expression understood by robfig/cron/v3
	// (5-field "minute hour dom month dow"). The special string
	// "manual" disables the recurring schedule; the Backup can
	// still be Run on demand via the /run endpoint.
	Schedule   string        `json:"schedule"`
	LastRunAt  *time.Time    `json:"lastRunAt,omitempty"`
	LastResult *BackupResult `json:"lastResult,omitempty"`
	// History keeps the most recent run outcomes (most-recent
	// first, bounded to MaxHistory entries) so the detail page
	// can show "last 10 runs" without bolting on a separate file.
	History   []BackupResult `json:"history,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	Disabled  bool           `json:"disabled,omitempty"`
	// Mode selects "mirror" (continuous overwrite, the v1.5.0a
	// default) vs "snapshot" (timestamped-prefix history). Empty
	// string is treated as BackupModeMirror so records that pre-date
	// v1.5.0b keep their previous behaviour without explicit
	// migration. See ResolveMode.
	Mode BackupMode `json:"mode,omitempty"`
	// Retention is only meaningful when Mode == BackupModeSnapshot.
	// Stored as the operator wrote it; the runner substitutes
	// DefaultRetention() when all keep-counts are zero so a fresh
	// snapshot backup still prunes sensibly.
	Retention RetentionPolicy `json:"retention,omitempty"`
}

// ResolveMode returns BackupModeMirror when Mode is empty — the
// migration path for v1.5.0a records that never wrote the field.
// Centralised so every caller agrees on the default.
func (b Backup) ResolveMode() BackupMode {
	if b.Mode == BackupModeSnapshot {
		return BackupModeSnapshot
	}
	return BackupModeMirror
}

// ResolveRetention returns the policy the runner should apply: the
// operator's choice if any keep-count is non-zero, else
// DefaultRetention(). Only used in snapshot mode.
func (b Backup) ResolveRetention() RetentionPolicy {
	if b.Retention.IsZero() {
		return DefaultRetention()
	}
	return b.Retention
}

// BackupResult is the outcome of a single Backup run. Stored both
// as LastResult on the Backup (for the list view) and prepended onto
// History (for the detail view).
type BackupResult struct {
	StartedAt     time.Time `json:"startedAt"`
	CompletedAt   time.Time `json:"completedAt"`
	ObjectsCopied int64     `json:"objectsCopied"`
	BytesCopied   int64     `json:"bytesCopied"`
	// Errors collects up to maxErrorsPerResult error strings from
	// the underlying engine run. We don't try to reproduce the
	// engine's full progress shape — the scheduler is responsible
	// for telling the operator what happened, not for diagnosing
	// every action.
	Errors  []string `json:"errors,omitempty"`
	Success bool     `json:"success"`
	// JobID is the sync.SyncJob.ID that produced this result so an
	// operator chasing details can correlate against the sync log.
	// Empty for runs that failed before a SyncJob was created.
	JobID string `json:"jobId,omitempty"`
	// SnapshotPrefix is set for runs in BackupModeSnapshot: the
	// destination-relative path the run actually wrote to
	// (e.g. "lsi-to-cheshire/2026-05-21_03:00:00/"). Empty for
	// mirror-mode runs.
	SnapshotPrefix string `json:"snapshotPrefix,omitempty"`
	// SnapshotsPruned counts how many older snapshots the retention
	// pass deleted at the end of this run. Zero for mirror mode and
	// for snapshot runs where the keep-window covered every existing
	// snapshot.
	SnapshotsPruned int64 `json:"snapshotsPruned,omitempty"`
	// BytesReclaimed sums the sizes of every object the retention
	// pass removed. Best-effort: drivers that can't cheaply report
	// per-object size during list-and-delete may leave this at zero
	// even when SnapshotsPruned > 0.
	BytesReclaimed int64 `json:"bytesReclaimed,omitempty"`
}

// MaxHistory bounds the per-backup history slice so a long-lived
// daily schedule doesn't grow the JSON file without bound.
const MaxHistory = 10

// maxErrorsPerResult bounds the Errors slice on each BackupResult.
// Matches the v1.5 prompt's "up to 10 most recent" requirement.
const maxErrorsPerResult = 10

// Special schedule strings.
const (
	// ScheduleManual disables the cron entry — runs only via the
	// /run endpoint.
	ScheduleManual = "manual"
)
