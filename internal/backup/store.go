package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	stdsync "sync"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned by Store.Get / Update / Delete when no
// Backup with the requested ID exists.
var ErrNotFound = errors.New("backup not found")

// Backups is the persistence interface for Backup records. A single
// JSON file ({dataDir}/backups.json) holds every operator's
// configuration — the scheduler streams new results back through
// RecordResult, so write contention is bounded and atomic-rename is
// sufficient (no per-record sharding).
type Backups interface {
	Create(ctx context.Context, b Backup) (Backup, error)
	Get(ctx context.Context, id string) (Backup, error)
	Update(ctx context.Context, id string, patch Backup) (Backup, error)
	Delete(ctx context.Context, id string) error
	ListForUser(ctx context.Context, userID string) ([]Backup, error)
	// All is used by the Scheduler on startup to register cron
	// entries for every enabled backup. Distinct from ListForUser
	// (which is user-scoped + omits the scheduler's needs).
	All(ctx context.Context) ([]Backup, error)
	RecordResult(ctx context.Context, id string, r BackupResult) error
}

// fileStore implements Backups against {dataDir}/backups.json. The
// in-memory map is the source of truth between writes; load happens
// once at construction. mu guards both map and on-disk file — the
// outer atomic rename is cheap so we hold the lock across the whole
// write.
type fileStore struct {
	mu   stdsync.RWMutex
	path string
	rows map[string]Backup
}

// NewFileStore opens (or initialises) the backups JSON file. Returns
// an error only on read failure of an existing file; a missing file
// is treated as an empty store.
func NewFileStore(dataDir string) (Backups, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}
	fs := &fileStore{
		path: filepath.Join(dataDir, "backups.json"),
		rows: map[string]Backup{},
	}
	data, err := os.ReadFile(fs.path)
	if err != nil {
		if os.IsNotExist(err) {
			return fs, nil
		}
		return nil, fmt.Errorf("reading backups.json: %w", err)
	}
	if len(data) == 0 {
		return fs, nil
	}
	var rows []Backup
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("parsing backups.json: %w", err)
	}
	for _, b := range rows {
		fs.rows[b.ID] = b
	}
	return fs, nil
}

// writeLocked persists the current in-memory state to disk. Caller
// must hold fs.mu.
func (fs *fileStore) writeLocked() error {
	rows := make([]Backup, 0, len(fs.rows))
	for _, b := range fs.rows {
		rows = append(rows, b)
	}
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling backups: %w", err)
	}
	tmp := fs.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := os.Rename(tmp, fs.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

// Create assigns a fresh UUID + timestamps and persists the new
// Backup. The caller's ID / CreatedAt / UpdatedAt fields are
// overwritten so a stale client can't pin them.
func (fs *fileStore) Create(_ context.Context, b Backup) (Backup, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	b.ID = uuid.New().String()
	now := time.Now().UTC()
	b.CreatedAt = now
	b.UpdatedAt = now
	fs.rows[b.ID] = b
	if err := fs.writeLocked(); err != nil {
		delete(fs.rows, b.ID)
		return Backup{}, err
	}
	return b, nil
}

func (fs *fileStore) Get(_ context.Context, id string) (Backup, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	b, ok := fs.rows[id]
	if !ok {
		return Backup{}, ErrNotFound
	}
	return b, nil
}

// Update applies the mutable fields of patch (Name, schedule,
// src/dst, Disabled) over the stored record. Identity + history
// fields (ID, OwnerUserID, CreatedAt, History, LastRunAt,
// LastResult) are NEVER taken from the patch — those belong to the
// server, not the client.
func (fs *fileStore) Update(_ context.Context, id string, patch Backup) (Backup, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	cur, ok := fs.rows[id]
	if !ok {
		return Backup{}, ErrNotFound
	}
	cur.Name = patch.Name
	cur.SrcRegionID = patch.SrcRegionID
	cur.SrcBucket = patch.SrcBucket
	cur.SrcPrefix = patch.SrcPrefix
	cur.DstRegionID = patch.DstRegionID
	cur.DstBucket = patch.DstBucket
	cur.DstPrefix = patch.DstPrefix
	cur.Schedule = patch.Schedule
	cur.Disabled = patch.Disabled
	cur.UpdatedAt = time.Now().UTC()
	fs.rows[id] = cur
	if err := fs.writeLocked(); err != nil {
		return Backup{}, err
	}
	return cur, nil
}

func (fs *fileStore) Delete(_ context.Context, id string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if _, ok := fs.rows[id]; !ok {
		return ErrNotFound
	}
	delete(fs.rows, id)
	return fs.writeLocked()
}

func (fs *fileStore) ListForUser(_ context.Context, userID string) ([]Backup, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	out := make([]Backup, 0)
	for _, b := range fs.rows {
		if b.OwnerUserID == userID {
			out = append(out, b)
		}
	}
	return out, nil
}

func (fs *fileStore) All(_ context.Context) ([]Backup, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	out := make([]Backup, 0, len(fs.rows))
	for _, b := range fs.rows {
		out = append(out, b)
	}
	return out, nil
}

// RecordResult writes a fresh BackupResult into a Backup's
// LastResult + History (most-recent first, bounded to MaxHistory).
// Errors slice on the result is itself bounded — the scheduler
// should already have trimmed it but we trim again defensively.
// LastRunAt is set to StartedAt so the list view shows when the run
// began rather than completed.
func (fs *fileStore) RecordResult(_ context.Context, id string, r BackupResult) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	cur, ok := fs.rows[id]
	if !ok {
		return ErrNotFound
	}
	if len(r.Errors) > maxErrorsPerResult {
		r.Errors = r.Errors[:maxErrorsPerResult]
	}
	cur.LastResult = &r
	started := r.StartedAt
	cur.LastRunAt = &started
	hist := append([]BackupResult{r}, cur.History...)
	if len(hist) > MaxHistory {
		hist = hist[:MaxHistory]
	}
	cur.History = hist
	cur.UpdatedAt = time.Now().UTC()
	fs.rows[id] = cur
	return fs.writeLocked()
}
