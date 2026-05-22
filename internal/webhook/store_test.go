package webhook

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// TestStoreRoundTrip exercises Create + Get + Update + ListForUser +
// Delete and confirms reopening replays the disk state.
func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ctx := context.Background()

	created, err := store.Create(ctx, Webhook{
		OwnerUserID: "matthew",
		Name:        "ci-build",
		TargetURL:   "https://ci.example.com/hook",
		Events:      []EventType{EventObjectCreated, EventObjectDeleted},
		Secret:      "shared-secret-1234567890abcdef",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected generated ID, got empty")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("expected timestamps to be filled, got zero")
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "ci-build" {
		t.Fatalf("expected name=ci-build, got %q", got.Name)
	}
	if len(got.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got.Events))
	}

	// Update: rename + flip enabled + omit secret -> secret preserved.
	updated, err := store.Update(ctx, created.ID, Webhook{
		Name:      "ci-build-renamed",
		TargetURL: "https://ci2.example.com/hook",
		Events:    []EventType{EventObjectDeleted},
		Enabled:   false,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "ci-build-renamed" {
		t.Fatalf("expected rename, got %q", updated.Name)
	}
	if updated.Enabled {
		t.Fatalf("expected Enabled=false after patch")
	}
	if updated.Secret != created.Secret {
		t.Fatalf("expected secret preserved when patch.Secret==\"\", got %q", updated.Secret)
	}
	if len(updated.Events) != 1 || updated.Events[0] != EventObjectDeleted {
		t.Fatalf("expected single event, got %+v", updated.Events)
	}

	// ListForUser scopes correctly.
	mine, _ := store.ListForUser(ctx, "matthew")
	if len(mine) != 1 {
		t.Fatalf("expected 1 webhook for matthew, got %d", len(mine))
	}
	theirs, _ := store.ListForUser(ctx, "someone-else")
	if len(theirs) != 0 {
		t.Fatalf("expected 0 webhooks for other user, got %d", len(theirs))
	}

	// Reopen replays on-disk state.
	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	again, err := reopened.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("reopen Get: %v", err)
	}
	if again.Name != "ci-build-renamed" {
		t.Fatalf("expected reopened name 'ci-build-renamed', got %q", again.Name)
	}

	// Delete + verify 404.
	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

// TestStoreDuplicateName: same user + name collides; different user +
// same name is allowed (mirrors federation per-user uniqueness).
func TestStoreDuplicateName(t *testing.T) {
	store, _ := Open(t.TempDir())
	ctx := context.Background()
	if _, err := store.Create(ctx, Webhook{OwnerUserID: "matthew", Name: "h", Enabled: true}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := store.Create(ctx, Webhook{OwnerUserID: "matthew", Name: "h", Enabled: true}); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("expected ErrDuplicateName on same user+name, got %v", err)
	}
	if _, err := store.Create(ctx, Webhook{OwnerUserID: "alice", Name: "h", Enabled: true}); err != nil {
		t.Fatalf("cross-user same-name Create should succeed, got %v", err)
	}
}

// TestStoreUpdateRenameCollides: renaming to another row's name fails
// with ErrDuplicateName.
func TestStoreUpdateRenameCollides(t *testing.T) {
	store, _ := Open(t.TempDir())
	ctx := context.Background()
	a, _ := store.Create(ctx, Webhook{OwnerUserID: "matthew", Name: "alpha", Enabled: true})
	_, _ = store.Create(ctx, Webhook{OwnerUserID: "matthew", Name: "bravo", Enabled: true})
	_, err := store.Update(ctx, a.ID, Webhook{Name: "bravo"})
	if !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("expected ErrDuplicateName on rename collision, got %v", err)
	}
}

// TestStoreListForBucket: a webhook with a (region, bucket) filter is
// excluded from a non-matching pre-filter; firehose (no filter)
// matches everything.
func TestStoreListForBucket(t *testing.T) {
	store, _ := Open(t.TempDir())
	ctx := context.Background()
	_, _ = store.Create(ctx, Webhook{
		OwnerUserID: "matthew",
		Name:        "scoped",
		Enabled:     true,
		BucketFilter: &BucketFilter{RegionID: "region-a", Bucket: "lsi"},
	})
	_, _ = store.Create(ctx, Webhook{
		OwnerUserID: "matthew",
		Name:        "firehose",
		Enabled:     true,
	})

	// Hit on the scoped target — both webhooks pass the pre-filter.
	hits, _ := store.ListForBucket(ctx, "region-a", "lsi")
	if len(hits) != 2 {
		t.Fatalf("expected 2 candidates on matching bucket, got %d", len(hits))
	}

	// Different bucket — only the firehose passes.
	hits, _ = store.ListForBucket(ctx, "region-a", "other")
	if len(hits) != 1 {
		t.Fatalf("expected 1 candidate on non-matching bucket, got %d", len(hits))
	}
	if hits[0].Name != "firehose" {
		t.Fatalf("expected firehose to pass, got %q", hits[0].Name)
	}
}

// TestStoreRecordDeliveryAutoDisable: AutoDisableThreshold consecutive
// failures flips Enabled=false; a subsequent success would reset the
// counter (verified by RecordDelivery with Success=true).
func TestStoreRecordDeliveryAutoDisable(t *testing.T) {
	store, _ := Open(t.TempDir())
	ctx := context.Background()
	w, _ := store.Create(ctx, Webhook{OwnerUserID: "matthew", Name: "x", Enabled: true})

	for i := 0; i < AutoDisableThreshold-1; i++ {
		got, err := store.RecordDelivery(ctx, w.ID, DeliveryResult{Success: false})
		if err != nil {
			t.Fatalf("RecordDelivery #%d: %v", i, err)
		}
		if !got.Enabled {
			t.Fatalf("expected still enabled after %d failures, got disabled", i+1)
		}
	}
	// One more failure crosses the threshold.
	got, err := store.RecordDelivery(ctx, w.ID, DeliveryResult{Success: false})
	if err != nil {
		t.Fatalf("RecordDelivery (threshold): %v", err)
	}
	if got.Enabled {
		t.Fatalf("expected disabled after %d consecutive failures, still enabled", AutoDisableThreshold)
	}
	if got.FailureCount != AutoDisableThreshold {
		t.Fatalf("expected FailureCount=%d, got %d", AutoDisableThreshold, got.FailureCount)
	}

	// Success resets the counter even though the row is now disabled.
	got, _ = store.RecordDelivery(ctx, w.ID, DeliveryResult{Success: true})
	if got.FailureCount != 0 {
		t.Fatalf("expected FailureCount=0 after success, got %d", got.FailureCount)
	}
}

// TestStoreMissingFile confirms an empty data dir yields an empty
// store rather than an error.
func TestStoreMissingFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fresh")
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open on missing dir: %v", err)
	}
	all, _ := store.All(context.Background())
	if len(all) != 0 {
		t.Fatalf("expected empty store, got %d rows", len(all))
	}
}

// TestStoreCreateDefensiveCopy: mutating the caller-supplied Events
// slice after Create must not change the stored row.
func TestStoreCreateDefensiveCopy(t *testing.T) {
	store, _ := Open(t.TempDir())
	ctx := context.Background()
	events := []EventType{EventObjectCreated}
	created, _ := store.Create(ctx, Webhook{
		OwnerUserID: "matthew",
		Name:        "defcopy",
		Enabled:     true,
		Events:      events,
	})
	// Mutate after Create.
	events[0] = EventObjectDeleted
	got, _ := store.Get(ctx, created.ID)
	if got.Events[0] != EventObjectCreated {
		t.Fatalf("expected stored Events[0]=created, got %q (mutation leaked)", got.Events[0])
	}
}
