package federation

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

// ErrNotFound is returned by Get / Update / Delete / UpdateReplicaHealth
// when no FederatedBucket with the requested ID exists.
var ErrNotFound = errors.New("federated bucket not found")

// ErrDuplicateName is returned by Create when the same owner already has
// a federation with the requested Name. Federation names are user-scoped
// — two different OwnerUserIDs can each have a federation called "photos"
// without colliding. The check uses the raw Name as-stored; whitespace
// trimming is the API handler's responsibility (v1.6.0c).
var ErrDuplicateName = errors.New("duplicate federated bucket name for owner")

// FederatedBuckets is the persistence interface for FederatedBucket
// records. Same shape as internal/backup.Backups — a single JSON file
// ({dataDir}/federated_buckets.json), atomic write, RWMutex-guarded.
//
// UpdateReplicaHealth is the engine hot-path callback (v1.6.0b). It
// mutates JUST the LastSync / Health / LagBytes / LagObjects fields on
// one ReplicaTarget entry inside the parent FederatedBucket — every
// other field on the parent record is preserved. This matters because
// the engine fires one health update per replicated object; if it
// rewrote the whole record on every event the JSON file would churn.
type FederatedBuckets interface {
	Create(ctx context.Context, fb FederatedBucket) (FederatedBucket, error)
	Get(ctx context.Context, id string) (FederatedBucket, error)
	Update(ctx context.Context, id string, patch FederatedBucket) (FederatedBucket, error)
	Delete(ctx context.Context, id string) error
	ListForUser(ctx context.Context, userID string) ([]FederatedBucket, error)
	// All returns every FederatedBucket across every owner. Used by the
	// v1.6.0b replication engine's boot-time "register every federation"
	// loop — ListForUser would force one call per user and the engine
	// has no notion of user identity at boot. Order is unspecified.
	All(ctx context.Context) ([]FederatedBucket, error)
	UpdateReplicaHealth(ctx context.Context, fbID, regionID, bucket string, health ReplicaTarget) error
	// FindByTarget returns the FederatedBucket owned by userID that
	// references the given (regionID, bucket) pair as EITHER the primary
	// or one of the replicas. Returns ErrNotFound when no federation
	// matches. Used by the v1.6.0e bucket-browser reverse-lookup endpoint
	// so the FE can render a "this bucket is federated" badge without
	// the caller having to list + filter client-side.
	//
	// First cut iterates ListForUser-equivalent rows and filters in
	// memory; O(N) where N = the user's federations. Acceptable for
	// sub-100 federations per user. Add a secondary index later if the
	// caller count climbs.
	FindByTarget(ctx context.Context, userID, regionID, bucket string) (FederatedBucket, error)
}

// fileStore implements FederatedBuckets against
// {dataDir}/federated_buckets.json. The in-memory map is the source of
// truth between writes; load happens once at Open. mu guards both the
// map and on-disk file — the outer atomic rename is cheap so we hold
// the lock across the whole write.
type fileStore struct {
	mu   stdsync.RWMutex
	path string
	rows map[string]FederatedBucket
}

// Open opens or initialises the federated-buckets JSON file at
// {dataDir}/federated_buckets.json. A missing file is treated as an
// empty store; only a read failure of an existing file returns an
// error.
func Open(dataDir string) (FederatedBuckets, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}
	fs := &fileStore{
		path: filepath.Join(dataDir, "federated_buckets.json"),
		rows: map[string]FederatedBucket{},
	}
	data, err := os.ReadFile(fs.path)
	if err != nil {
		if os.IsNotExist(err) {
			return fs, nil
		}
		return nil, fmt.Errorf("reading federated_buckets.json: %w", err)
	}
	if len(data) == 0 {
		return fs, nil
	}
	var rows []FederatedBucket
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("parsing federated_buckets.json: %w", err)
	}
	for _, fb := range rows {
		fs.rows[fb.ID] = fb
	}
	return fs, nil
}

// writeLocked persists the current in-memory state to disk via the
// tmp+rename atomic-write idiom. Caller must hold fs.mu.
func (fs *fileStore) writeLocked() error {
	rows := make([]FederatedBucket, 0, len(fs.rows))
	for _, fb := range fs.rows {
		rows = append(rows, fb)
	}
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling federated buckets: %w", err)
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
// FederatedBucket. The caller's ID / CreatedAt / UpdatedAt fields are
// overwritten so a stale client can't pin them.
//
// Uniqueness: (OwnerUserID, Name) must not already exist. Same name
// under a different owner is allowed — federation names are user-scoped.
func (fs *fileStore) Create(_ context.Context, fb FederatedBucket) (FederatedBucket, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Per-user name uniqueness: a single owner can't have two
	// federations with the same logical name. The compare uses Name
	// as-stored; the v1.6.0c API handler is responsible for any
	// whitespace/case normalisation before reaching the store.
	for _, existing := range fs.rows {
		if existing.OwnerUserID == fb.OwnerUserID && existing.Name == fb.Name {
			return FederatedBucket{}, ErrDuplicateName
		}
	}

	fb.ID = uuid.New().String()
	now := time.Now().UTC()
	fb.CreatedAt = now
	fb.UpdatedAt = now
	fs.rows[fb.ID] = fb
	if err := fs.writeLocked(); err != nil {
		delete(fs.rows, fb.ID)
		return FederatedBucket{}, err
	}
	return fb, nil
}

func (fs *fileStore) Get(_ context.Context, id string) (FederatedBucket, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	fb, ok := fs.rows[id]
	if !ok {
		return FederatedBucket{}, ErrNotFound
	}
	return fb, nil
}

// Update applies the mutable fields of patch (Name, Primary, Replicas,
// Policy) over the stored record. Identity fields (ID, OwnerUserID,
// CreatedAt) are NEVER taken from the patch — those belong to the
// server, not the client.
//
// Note: Update is the slow path (operator changes policy or adds a
// replica). The engine never goes through here — see
// UpdateReplicaHealth for the per-object health callback.
func (fs *fileStore) Update(_ context.Context, id string, patch FederatedBucket) (FederatedBucket, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	cur, ok := fs.rows[id]
	if !ok {
		return FederatedBucket{}, ErrNotFound
	}
	cur.Name = patch.Name
	cur.Primary = patch.Primary
	cur.Replicas = patch.Replicas
	cur.Policy = patch.Policy
	cur.UpdatedAt = time.Now().UTC()
	fs.rows[id] = cur
	if err := fs.writeLocked(); err != nil {
		return FederatedBucket{}, err
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

func (fs *fileStore) ListForUser(_ context.Context, userID string) ([]FederatedBucket, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	out := make([]FederatedBucket, 0)
	for _, fb := range fs.rows {
		if fb.OwnerUserID == userID {
			out = append(out, fb)
		}
	}
	return out, nil
}

// All returns a snapshot of every FederatedBucket in the store. The
// replication engine calls this once at boot to spin up a worker per
// federation; subsequent CRUD goes through the engine's TriggerNow /
// Reload path (v1.6.0c). Order is unspecified — the engine treats the
// slice as a set.
func (fs *fileStore) All(_ context.Context) ([]FederatedBucket, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	out := make([]FederatedBucket, 0, len(fs.rows))
	for _, fb := range fs.rows {
		out = append(out, fb)
	}
	return out, nil
}

// FindByTarget walks the owner's federations looking for one whose
// primary or any replica matches (regionID, bucket). Returns
// ErrNotFound when no row matches — the caller (the v1.6.0e
// /by-target endpoint) translates that into 204 No Content. The
// (regionID, bucket) compare is exact; the API handler trims
// whitespace before reaching us.
func (fs *fileStore) FindByTarget(_ context.Context, userID, regionID, bucket string) (FederatedBucket, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	for _, fb := range fs.rows {
		if fb.OwnerUserID != userID {
			continue
		}
		if fb.Primary.RegionID == regionID && fb.Primary.Bucket == bucket {
			return fb, nil
		}
		for _, rep := range fb.Replicas {
			if rep.RegionID == regionID && rep.Bucket == bucket {
				return fb, nil
			}
		}
	}
	return FederatedBucket{}, ErrNotFound
}

// UpdateReplicaHealth mutates the LastSync / Health / LagBytes /
// LagObjects fields on EXACTLY ONE ReplicaTarget inside the parent
// FederatedBucket. The replica is keyed by (regionID, bucket) — the
// engine knows which replica it just replicated to.
//
// The (RegionID, Bucket) of the target are preserved as-stored; only
// the four health/lag fields are copied from the supplied `health`
// argument. Other replicas on the same FederatedBucket are untouched.
// The parent's UpdatedAt timestamp is bumped because the persisted
// record has changed.
//
// Returns ErrNotFound when either the FederatedBucket or the named
// replica is missing — the engine should drop the event in either case
// (the federation may have just been deleted or reconfigured).
func (fs *fileStore) UpdateReplicaHealth(_ context.Context, fbID, regionID, bucket string, health ReplicaTarget) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	cur, ok := fs.rows[fbID]
	if !ok {
		return ErrNotFound
	}

	for i := range cur.Replicas {
		r := &cur.Replicas[i]
		if r.RegionID != regionID || r.Bucket != bucket {
			continue
		}
		r.LastSync = health.LastSync
		r.Health = health.Health
		r.LagBytes = health.LagBytes
		r.LagObjects = health.LagObjects
		cur.UpdatedAt = time.Now().UTC()
		fs.rows[fbID] = cur
		return fs.writeLocked()
	}
	return ErrNotFound
}
