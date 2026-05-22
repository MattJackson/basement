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
package backup

import (
	"time"
)

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
