package federation

import (
	"context"
	"errors"
	"testing"
	"time"
)

// newFed is a small builder so each test doesn't repeat the noisy
// struct literal. Caller can override Name / OwnerUserID / Replicas.
func newFed(owner, name string, replicas ...ReplicaTarget) FederatedBucket {
	return FederatedBucket{
		OwnerUserID: owner,
		Name:        name,
		Primary:     ReplicaTarget{RegionID: "region-primary", Bucket: "photos"},
		Replicas:    replicas,
		Policy:      DefaultPolicy(),
	}
}

// TestFederatedBuckets_CreateGet covers the basic round trip: Create
// assigns a UUID + timestamps; Get returns the same record.
func TestFederatedBuckets_CreateGet(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	in := newFed("matthew", "lsi",
		ReplicaTarget{RegionID: "region-b2", Bucket: "lsi-b2"},
	)
	created, err := store.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected generated ID, got empty")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("expected CreatedAt/UpdatedAt to be filled, got zero")
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "lsi" || got.OwnerUserID != "matthew" {
		t.Fatalf("Get returned wrong identity: %+v", got)
	}
	if len(got.Replicas) != 1 || got.Replicas[0].Bucket != "lsi-b2" {
		t.Fatalf("Get replicas mismatch: %+v", got.Replicas)
	}
	if got.Policy.SyncMode != SyncModeContinuous || got.Policy.LagAlertSec != 300 {
		t.Fatalf("Get policy lost defaults: %+v", got.Policy)
	}
}

// TestFederatedBuckets_UniquePerUserName: same OwnerUserID + Name
// collides; different owner with the same Name is allowed.
func TestFederatedBuckets_UniquePerUserName(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	if _, err := store.Create(ctx, newFed("matthew", "photos")); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := store.Create(ctx, newFed("matthew", "photos")); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("expected ErrDuplicateName on second Create for same user+name, got %v", err)
	}
	// Different owner with the same name is fine.
	if _, err := store.Create(ctx, newFed("alice", "photos")); err != nil {
		t.Fatalf("cross-user same-name Create should succeed, got %v", err)
	}
}

// TestFederatedBuckets_ListForUser: filters by OwnerUserID; other users'
// federations don't leak into the result.
func TestFederatedBuckets_ListForUser(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	if _, err := store.Create(ctx, newFed("matthew", "a")); err != nil {
		t.Fatalf("Create a: %v", err)
	}
	if _, err := store.Create(ctx, newFed("matthew", "b")); err != nil {
		t.Fatalf("Create b: %v", err)
	}
	if _, err := store.Create(ctx, newFed("alice", "a")); err != nil {
		t.Fatalf("Create alice: %v", err)
	}

	mine, err := store.ListForUser(ctx, "matthew")
	if err != nil {
		t.Fatalf("ListForUser matthew: %v", err)
	}
	if len(mine) != 2 {
		t.Fatalf("expected 2 federations for matthew, got %d", len(mine))
	}
	for _, fb := range mine {
		if fb.OwnerUserID != "matthew" {
			t.Fatalf("ListForUser leaked owner %q", fb.OwnerUserID)
		}
	}

	hers, err := store.ListForUser(ctx, "alice")
	if err != nil {
		t.Fatalf("ListForUser alice: %v", err)
	}
	if len(hers) != 1 || hers[0].Name != "a" {
		t.Fatalf("ListForUser alice mismatch: %+v", hers)
	}

	none, _ := store.ListForUser(ctx, "nobody")
	if len(none) != 0 {
		t.Fatalf("ListForUser unknown should be empty, got %d", len(none))
	}
}

// TestFederatedBuckets_All: returns every federation across every
// owner, used by the v1.6.0b replication engine at boot.
func TestFederatedBuckets_All(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	// Empty store: All returns an empty (non-nil) slice.
	all, err := store.All(ctx)
	if err != nil {
		t.Fatalf("All on empty store: %v", err)
	}
	if all == nil {
		t.Fatalf("All on empty store should return non-nil slice")
	}
	if len(all) != 0 {
		t.Fatalf("expected empty All on fresh store, got %d", len(all))
	}

	if _, err := store.Create(ctx, newFed("matthew", "a")); err != nil {
		t.Fatalf("Create matthew/a: %v", err)
	}
	if _, err := store.Create(ctx, newFed("matthew", "b")); err != nil {
		t.Fatalf("Create matthew/b: %v", err)
	}
	if _, err := store.Create(ctx, newFed("alice", "a")); err != nil {
		t.Fatalf("Create alice/a: %v", err)
	}

	all, err = store.All(ctx)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 federations from All, got %d", len(all))
	}

	// Both owners must appear.
	owners := map[string]int{}
	for _, fb := range all {
		owners[fb.OwnerUserID]++
	}
	if owners["matthew"] != 2 {
		t.Fatalf("expected 2 matthew federations in All, got %d", owners["matthew"])
	}
	if owners["alice"] != 1 {
		t.Fatalf("expected 1 alice federation in All, got %d", owners["alice"])
	}
}

// TestFederatedBuckets_Update: replicas can be added/removed and
// policy can change via a patch; identity fields (ID, OwnerUserID,
// CreatedAt) survive.
func TestFederatedBuckets_Update(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	created, err := store.Create(ctx, newFed("matthew", "lsi",
		ReplicaTarget{RegionID: "region-b2", Bucket: "lsi-b2"},
	))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	origCreatedAt := created.CreatedAt

	// Patch adds a second replica and tightens lag alert.
	patch := FederatedBucket{
		Name:    "lsi",
		Primary: ReplicaTarget{RegionID: "region-primary", Bucket: "photos"},
		Replicas: []ReplicaTarget{
			{RegionID: "region-b2", Bucket: "lsi-b2"},
			{RegionID: "region-wasabi", Bucket: "lsi-w"},
		},
		Policy: FederationPolicy{SyncMode: SyncModeContinuous, LagAlertSec: 60, WriteQuorum: 1},
	}
	// Sleep so UpdatedAt advances past CreatedAt on monotonic clocks.
	time.Sleep(time.Millisecond)
	updated, err := store.Update(ctx, created.ID, patch)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(updated.Replicas) != 2 {
		t.Fatalf("expected 2 replicas after Update, got %d", len(updated.Replicas))
	}
	if updated.Policy.LagAlertSec != 60 {
		t.Fatalf("expected LagAlertSec=60 after Update, got %d", updated.Policy.LagAlertSec)
	}
	if updated.OwnerUserID != "matthew" {
		t.Fatalf("Update changed OwnerUserID to %q", updated.OwnerUserID)
	}
	if !updated.CreatedAt.Equal(origCreatedAt) {
		t.Fatalf("Update changed CreatedAt: %v -> %v", origCreatedAt, updated.CreatedAt)
	}
	if !updated.UpdatedAt.After(origCreatedAt) {
		t.Fatalf("expected UpdatedAt to advance, got %v not after %v", updated.UpdatedAt, origCreatedAt)
	}

	// Patch removes a replica.
	patch.Replicas = []ReplicaTarget{{RegionID: "region-wasabi", Bucket: "lsi-w"}}
	shrunk, err := store.Update(ctx, created.ID, patch)
	if err != nil {
		t.Fatalf("Update shrink: %v", err)
	}
	if len(shrunk.Replicas) != 1 || shrunk.Replicas[0].RegionID != "region-wasabi" {
		t.Fatalf("Update did not shrink replicas correctly: %+v", shrunk.Replicas)
	}
}

// TestFederatedBuckets_Delete: Delete removes the row and Get returns
// ErrNotFound thereafter. Delete on a missing ID returns ErrNotFound.
func TestFederatedBuckets_Delete(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	created, err := store.Create(ctx, newFed("matthew", "gone"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after Delete, got %v", err)
	}
	if err := store.Delete(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on second Delete, got %v", err)
	}
}

// TestFederatedBuckets_UpdateReplicaHealth_Targeted: the engine
// callback mutates exactly one replica's health fields. Other replicas
// on the same FederatedBucket are untouched, and the primary is
// untouched.
func TestFederatedBuckets_UpdateReplicaHealth_Targeted(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	created, err := store.Create(ctx, newFed("matthew", "lsi",
		ReplicaTarget{RegionID: "region-b2", Bucket: "lsi-b2"},
		ReplicaTarget{RegionID: "region-wasabi", Bucket: "lsi-w"},
	))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	syncTime := time.Now().UTC().Truncate(time.Second)
	err = store.UpdateReplicaHealth(ctx, created.ID, "region-b2", "lsi-b2", ReplicaTarget{
		LastSync:   syncTime,
		Health:     HealthLagging,
		LagBytes:   1024,
		LagObjects: 3,
	})
	if err != nil {
		t.Fatalf("UpdateReplicaHealth: %v", err)
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after health update: %v", err)
	}

	// Matching replica should have new health, but RegionID + Bucket
	// preserved.
	var b2, wasabi ReplicaTarget
	for _, r := range got.Replicas {
		switch r.RegionID {
		case "region-b2":
			b2 = r
		case "region-wasabi":
			wasabi = r
		}
	}
	if b2.Bucket != "lsi-b2" {
		t.Fatalf("UpdateReplicaHealth clobbered Bucket on b2: %+v", b2)
	}
	if b2.Health != HealthLagging || b2.LagBytes != 1024 || b2.LagObjects != 3 {
		t.Fatalf("UpdateReplicaHealth did not apply to b2: %+v", b2)
	}
	if !b2.LastSync.Equal(syncTime) {
		t.Fatalf("UpdateReplicaHealth LastSync mismatch: got %v want %v", b2.LastSync, syncTime)
	}

	// Other replica untouched.
	if wasabi.Health != "" || wasabi.LagBytes != 0 || wasabi.LagObjects != 0 || !wasabi.LastSync.IsZero() {
		t.Fatalf("UpdateReplicaHealth leaked into unrelated replica: %+v", wasabi)
	}

	// Primary untouched.
	if got.Primary.Health != "" || got.Primary.LagBytes != 0 {
		t.Fatalf("UpdateReplicaHealth leaked into primary: %+v", got.Primary)
	}

	// Missing replica returns ErrNotFound.
	err = store.UpdateReplicaHealth(ctx, created.ID, "region-ghost", "nope", ReplicaTarget{Health: HealthBroken})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown replica, got %v", err)
	}
	// Missing FederatedBucket returns ErrNotFound.
	err = store.UpdateReplicaHealth(ctx, "no-such-fb", "region-b2", "lsi-b2", ReplicaTarget{Health: HealthBroken})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown fbID, got %v", err)
	}
}

// TestFederatedBuckets_Persists: close + reopen the store and confirm
// the record survives on disk.
func TestFederatedBuckets_Persists(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	created, err := store.Create(ctx, newFed("matthew", "persist",
		ReplicaTarget{RegionID: "region-b2", Bucket: "persist-b2"},
	))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Drop the first handle and reopen against the same dir.
	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := reopened.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("reopen Get: %v", err)
	}
	if got.Name != "persist" {
		t.Fatalf("expected reopened name 'persist', got %q", got.Name)
	}
	if len(got.Replicas) != 1 || got.Replicas[0].Bucket != "persist-b2" {
		t.Fatalf("reopened replicas mismatch: %+v", got.Replicas)
	}

	// And ListForUser still works against the reopened handle.
	mine, _ := reopened.ListForUser(ctx, "matthew")
	if len(mine) != 1 {
		t.Fatalf("reopened ListForUser size: want 1 got %d", len(mine))
	}
}

// TestFederatedBuckets_FindByTarget covers the v1.6.0e reverse-lookup
// path: given (regionID, bucket), find the federation that has it as
// the primary OR a replica, scoped to the owner. Misses return
// ErrNotFound; other users' federations are invisible.
func TestFederatedBuckets_FindByTarget(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	mine := FederatedBucket{
		OwnerUserID: "matthew",
		Name:        "lsi",
		Primary:     ReplicaTarget{RegionID: "region-garage", Bucket: "lsi-primary"},
		Replicas: []ReplicaTarget{
			{RegionID: "region-b2", Bucket: "lsi-b2"},
			{RegionID: "region-wasabi", Bucket: "lsi-wasabi"},
		},
		Policy: DefaultPolicy(),
	}
	created, err := store.Create(ctx, mine)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Another user's federation that references the same (region,
	// bucket) pair — must NOT leak across owners.
	other := FederatedBucket{
		OwnerUserID: "alice",
		Name:        "alice-fed",
		Primary:     ReplicaTarget{RegionID: "region-garage", Bucket: "lsi-primary"},
		Replicas:    []ReplicaTarget{{RegionID: "region-b2", Bucket: "alice-b2"}},
		Policy:      DefaultPolicy(),
	}
	if _, err := store.Create(ctx, other); err != nil {
		t.Fatalf("Create alice: %v", err)
	}

	// Primary match.
	got, err := store.FindByTarget(ctx, "matthew", "region-garage", "lsi-primary")
	if err != nil {
		t.Fatalf("FindByTarget primary: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("FindByTarget primary: wrong ID got=%q want=%q", got.ID, created.ID)
	}

	// Replica match.
	got, err = store.FindByTarget(ctx, "matthew", "region-wasabi", "lsi-wasabi")
	if err != nil {
		t.Fatalf("FindByTarget replica: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("FindByTarget replica: wrong ID got=%q want=%q", got.ID, created.ID)
	}

	// No match → ErrNotFound.
	if _, err := store.FindByTarget(ctx, "matthew", "region-garage", "no-such-bucket"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("FindByTarget no-match: want ErrNotFound got %v", err)
	}

	// Ownership scoping: matthew's federation invisible when alice queries.
	if _, err := store.FindByTarget(ctx, "alice", "region-wasabi", "lsi-wasabi"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("FindByTarget cross-owner: want ErrNotFound got %v", err)
	}

	// And the inverse — alice's own row is still findable.
	if _, err := store.FindByTarget(ctx, "alice", "region-b2", "alice-b2"); err != nil {
		t.Fatalf("FindByTarget alice's own: %v", err)
	}

	// Timestamps survive the round trip — FindByTarget returns the
	// stored record unchanged, so CreatedAt + UpdatedAt should match
	// what store.Create assigned.
	roundTrip, err := store.FindByTarget(ctx, "matthew", "region-garage", "lsi-primary")
	if err != nil {
		t.Fatalf("FindByTarget round-trip: %v", err)
	}
	if !roundTrip.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("FindByTarget CreatedAt drift: want %v got %v", created.CreatedAt, roundTrip.CreatedAt)
	}
	if time.Since(roundTrip.CreatedAt) > time.Minute {
		t.Fatalf("FindByTarget CreatedAt looks stale: %v", roundTrip.CreatedAt)
	}
}

// TestFederatedBuckets_DefaultPolicy: defaults match ADR-0005 — safe
// continuous mode, 5min lag alert, write quorum 1, auto-failover off.
func TestFederatedBuckets_DefaultPolicy(t *testing.T) {
	p := DefaultPolicy()
	if p.SyncMode != SyncModeContinuous {
		t.Fatalf("DefaultPolicy SyncMode: want %q got %q", SyncModeContinuous, p.SyncMode)
	}
	if p.LagAlertSec != 300 {
		t.Fatalf("DefaultPolicy LagAlertSec: want 300 got %d", p.LagAlertSec)
	}
	if p.WriteQuorum != 1 {
		t.Fatalf("DefaultPolicy WriteQuorum: want 1 got %d", p.WriteQuorum)
	}
	if p.AutoFailover {
		t.Fatalf("DefaultPolicy AutoFailover: want false got true")
	}
	if p.AutoFailoverSec != 0 {
		t.Fatalf("DefaultPolicy AutoFailoverSec: want 0 got %d", p.AutoFailoverSec)
	}
	if p.Schedule != "" {
		t.Fatalf("DefaultPolicy Schedule: want empty got %q", p.Schedule)
	}
}
