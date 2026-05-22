package webhook

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

// ErrNotFound is returned by Get / Update / Delete when no Webhook
// with the requested ID exists. Mirrors the (backup|federation).ErrNotFound
// shape so the handler layer can errors.Is across subsystems uniformly.
var ErrNotFound = errors.New("webhook not found")

// ErrDuplicateName is returned by Create when the same owner already
// has a webhook with the requested Name. Names are user-scoped — two
// different owners can each have a webhook called "ci" without
// colliding. Matches the federation store's per-user uniqueness rule.
var ErrDuplicateName = errors.New("duplicate webhook name for owner")

// Store is the persistence interface for Webhook records. Same shape
// as backup.Backups / federation.FederatedBuckets — a single JSON file
// ({dataDir}/webhooks.json), atomic write, RWMutex-guarded.
//
// RecordDelivery is the engine's hot-path callback: it mutates JUST
// LastDelivery + FailureCount + (possibly) Enabled on one row without
// rewriting any other field. Without that, every delivery would have
// to read-modify-write the whole record back through Update.
type Store interface {
	Create(ctx context.Context, w Webhook) (Webhook, error)
	Get(ctx context.Context, id string) (Webhook, error)
	Update(ctx context.Context, id string, patch Webhook) (Webhook, error)
	Delete(ctx context.Context, id string) error
	ListForUser(ctx context.Context, userID string) ([]Webhook, error)
	// All returns every Webhook across every owner. Used by the engine
	// at boot to seed per-webhook worker goroutines without iterating
	// users.
	All(ctx context.Context) ([]Webhook, error)
	// ListForBucket returns the webhooks that, given a single envelope's
	// (regionID, bucket) coordinates, are CANDIDATES for delivery — i.e.
	// their BucketFilter (if any) does not exclude this target. The
	// engine still calls Webhook.Matches per envelope to apply the
	// remaining filters (events, prefix, enabled), but pre-filtering
	// here keeps the per-event scan bounded on busy deployments.
	ListForBucket(ctx context.Context, regionID, bucket string) ([]Webhook, error)
	// RecordDelivery writes the result of one delivery attempt: updates
	// LastDelivery, increments or resets FailureCount, and disables the
	// webhook when the consecutive-failure threshold is hit. Returns the
	// post-update Webhook (so the engine can detect the auto-disable
	// transition without a re-Get).
	RecordDelivery(ctx context.Context, id string, result DeliveryResult) (Webhook, error)
}

// fileStore implements Store against {dataDir}/webhooks.json. The
// in-memory map is the source of truth between writes; load happens
// once at Open. mu guards both the map and on-disk file — the outer
// atomic rename is cheap so we hold the lock across the whole write.
type fileStore struct {
	mu   stdsync.RWMutex
	path string
	rows map[string]Webhook
}

// Open opens or initialises the webhooks JSON file at
// {dataDir}/webhooks.json. A missing file is treated as an empty
// store; only a read failure of an existing file returns an error.
func Open(dataDir string) (Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}
	fs := &fileStore{
		path: filepath.Join(dataDir, "webhooks.json"),
		rows: map[string]Webhook{},
	}
	data, err := os.ReadFile(fs.path)
	if err != nil {
		if os.IsNotExist(err) {
			return fs, nil
		}
		return nil, fmt.Errorf("reading webhooks.json: %w", err)
	}
	if len(data) == 0 {
		return fs, nil
	}
	var rows []Webhook
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("parsing webhooks.json: %w", err)
	}
	for _, w := range rows {
		fs.rows[w.ID] = w
	}
	return fs, nil
}

// writeLocked persists the current in-memory state via tmp+rename.
// Caller must hold fs.mu.
func (fs *fileStore) writeLocked() error {
	rows := make([]Webhook, 0, len(fs.rows))
	for _, w := range fs.rows {
		rows = append(rows, w)
	}
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling webhooks: %w", err)
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
// Webhook. The caller's ID / CreatedAt / UpdatedAt / LastDelivery /
// FailureCount fields are overwritten — those belong to the server.
//
// Uniqueness: (OwnerUserID, Name) must not already exist. Defensive
// copy of the Events slice so the caller can't mutate the in-memory
// row through a retained reference.
func (fs *fileStore) Create(_ context.Context, w Webhook) (Webhook, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for _, existing := range fs.rows {
		if existing.OwnerUserID == w.OwnerUserID && existing.Name == w.Name {
			return Webhook{}, ErrDuplicateName
		}
	}

	w.ID = uuid.New().String()
	now := time.Now().UTC()
	w.CreatedAt = now
	w.UpdatedAt = now
	w.LastDelivery = nil
	w.FailureCount = 0
	stored := w
	if len(w.Events) > 0 {
		stored.Events = make([]EventType, len(w.Events))
		copy(stored.Events, w.Events)
	}
	if w.BucketFilter != nil {
		bf := *w.BucketFilter
		stored.BucketFilter = &bf
	}
	fs.rows[stored.ID] = stored
	if err := fs.writeLocked(); err != nil {
		delete(fs.rows, stored.ID)
		return Webhook{}, err
	}
	return cloneWebhook(stored), nil
}

func (fs *fileStore) Get(_ context.Context, id string) (Webhook, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	w, ok := fs.rows[id]
	if !ok {
		return Webhook{}, ErrNotFound
	}
	return cloneWebhook(w), nil
}

// Update applies the mutable fields of patch (Name, TargetURL, Events,
// BucketFilter, PrefixFilter, Secret, Enabled) over the stored record.
// Identity / history fields (ID, OwnerUserID, CreatedAt, LastDelivery,
// FailureCount) are NEVER taken from the patch.
//
// A Secret of "" on the patch is interpreted as "keep the existing
// secret" — the API layer relies on this so an Update body that omits
// secret (the common case, since List/Get redact it) does not blank
// out the row.
func (fs *fileStore) Update(_ context.Context, id string, patch Webhook) (Webhook, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	cur, ok := fs.rows[id]
	if !ok {
		return Webhook{}, ErrNotFound
	}
	// Per-owner Name uniqueness must also hold on rename. The compare
	// uses the patch's Name verbatim; the API handler is responsible
	// for whitespace normalisation before reaching the store.
	if patch.Name != cur.Name {
		for otherID, other := range fs.rows {
			if otherID == id {
				continue
			}
			if other.OwnerUserID == cur.OwnerUserID && other.Name == patch.Name {
				return Webhook{}, ErrDuplicateName
			}
		}
	}
	cur.Name = patch.Name
	cur.TargetURL = patch.TargetURL
	if len(patch.Events) > 0 {
		cur.Events = make([]EventType, len(patch.Events))
		copy(cur.Events, patch.Events)
	} else {
		cur.Events = nil
	}
	if patch.BucketFilter != nil {
		bf := *patch.BucketFilter
		cur.BucketFilter = &bf
	} else {
		cur.BucketFilter = nil
	}
	cur.PrefixFilter = patch.PrefixFilter
	if patch.Secret != "" {
		cur.Secret = patch.Secret
	}
	cur.Enabled = patch.Enabled
	cur.UpdatedAt = time.Now().UTC()
	fs.rows[id] = cur
	if err := fs.writeLocked(); err != nil {
		return Webhook{}, err
	}
	return cloneWebhook(cur), nil
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

func (fs *fileStore) ListForUser(_ context.Context, userID string) ([]Webhook, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	out := make([]Webhook, 0)
	for _, w := range fs.rows {
		if w.OwnerUserID == userID {
			out = append(out, cloneWebhook(w))
		}
	}
	return out, nil
}

func (fs *fileStore) All(_ context.Context) ([]Webhook, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	out := make([]Webhook, 0, len(fs.rows))
	for _, w := range fs.rows {
		out = append(out, cloneWebhook(w))
	}
	return out, nil
}

// ListForBucket pre-filters by BucketFilter. A webhook with no filter
// matches every (regionID, bucket); one with RegionID set narrows to
// that region; with Bucket set additionally narrows to the exact
// bucket. The engine applies the remaining (events, prefix, enabled)
// filters per envelope.
func (fs *fileStore) ListForBucket(_ context.Context, regionID, bucket string) ([]Webhook, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	out := make([]Webhook, 0)
	for _, w := range fs.rows {
		if w.BucketFilter != nil {
			if w.BucketFilter.RegionID != "" && w.BucketFilter.RegionID != regionID {
				continue
			}
			if w.BucketFilter.Bucket != "" && w.BucketFilter.Bucket != bucket {
				continue
			}
		}
		out = append(out, cloneWebhook(w))
	}
	return out, nil
}

// RecordDelivery writes the engine's per-attempt result back to the
// store. Success resets FailureCount; failure increments it and, on
// crossing AutoDisableThreshold, flips Enabled=false. The post-update
// Webhook is returned so the caller can detect the auto-disable
// transition (was enabled, now disabled) and emit the audit event.
func (fs *fileStore) RecordDelivery(_ context.Context, id string, result DeliveryResult) (Webhook, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	cur, ok := fs.rows[id]
	if !ok {
		return Webhook{}, ErrNotFound
	}
	r := result
	cur.LastDelivery = &r
	if result.Success {
		cur.FailureCount = 0
	} else {
		cur.FailureCount++
		if cur.FailureCount >= AutoDisableThreshold {
			cur.Enabled = false
		}
	}
	cur.UpdatedAt = time.Now().UTC()
	fs.rows[id] = cur
	if err := fs.writeLocked(); err != nil {
		return Webhook{}, err
	}
	return cloneWebhook(cur), nil
}

// cloneWebhook returns a deep copy whose Events slice + BucketFilter
// + LastDelivery pointer have their own backing memory. Every read
// path returns this clone so a concurrent caller can mutate its result
// without racing the in-memory map.
func cloneWebhook(w Webhook) Webhook {
	out := w
	if len(w.Events) > 0 {
		out.Events = make([]EventType, len(w.Events))
		copy(out.Events, w.Events)
	} else {
		out.Events = nil
	}
	if w.BucketFilter != nil {
		bf := *w.BucketFilter
		out.BucketFilter = &bf
	}
	if w.LastDelivery != nil {
		d := *w.LastDelivery
		out.LastDelivery = &d
	}
	return out
}
