package backup

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// TestStoreRoundTrip exercises Create + Get + Update + ListForUser +
// Delete. The on-disk JSON is reloaded via a second NewFileStore so
// we also verify the marshal/unmarshal cycle survives.
func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	ctx := context.Background()

	created, err := store.Create(ctx, Backup{
		OwnerUserID: "matthew",
		Name:        "lsi to cheshire weekly",
		SrcRegionID: "region-lsi",
		SrcBucket:   "photos",
		DstRegionID: "region-cheshire",
		DstBucket:   "photos-backup",
		Schedule:    ScheduleManual,
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
	if got.Name != created.Name {
		t.Fatalf("expected name %q, got %q", created.Name, got.Name)
	}

	// Update should preserve identity but rewrite mutable fields.
	patch := Backup{
		Name:        "renamed",
		SrcRegionID: "region-lsi",
		SrcBucket:   "photos",
		DstRegionID: "region-cheshire",
		DstBucket:   "photos-backup",
		Schedule:    "0 3 * * *",
		Disabled:    true,
	}
	updated, err := store.Update(ctx, created.ID, patch)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "renamed" {
		t.Fatalf("expected name 'renamed', got %q", updated.Name)
	}
	if updated.Schedule != "0 3 * * *" {
		t.Fatalf("expected new schedule, got %q", updated.Schedule)
	}
	if !updated.Disabled {
		t.Fatalf("expected Disabled=true after patch")
	}
	if updated.OwnerUserID != "matthew" {
		t.Fatalf("Update must not change OwnerUserID, got %q", updated.OwnerUserID)
	}
	if updated.CreatedAt != created.CreatedAt {
		t.Fatalf("Update must not change CreatedAt")
	}

	// List should return exactly one for matthew, zero for someone else.
	mine, err := store.ListForUser(ctx, "matthew")
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(mine) != 1 {
		t.Fatalf("expected 1 backup for matthew, got %d", len(mine))
	}
	theirs, _ := store.ListForUser(ctx, "someone-else")
	if len(theirs) != 0 {
		t.Fatalf("expected 0 backups for other user, got %d", len(theirs))
	}

	// Reopen the store to confirm on-disk persistence.
	reopened, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	again, err := reopened.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("reopen Get: %v", err)
	}
	if again.Name != "renamed" {
		t.Fatalf("expected reopened name 'renamed', got %q", again.Name)
	}

	// Delete + verify both Get + reopened Get 404.
	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

// TestStoreRecordResult verifies that recording a run prepends to
// history, trims to MaxHistory, and updates LastResult + LastRunAt.
func TestStoreRecordResult(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	ctx := context.Background()

	b, err := store.Create(ctx, Backup{
		OwnerUserID: "matthew",
		Name:        "history test",
		Schedule:    ScheduleManual,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Record MaxHistory+5 results; the oldest 5 should be dropped.
	for i := 0; i < MaxHistory+5; i++ {
		r := BackupResult{
			StartedAt:     time.Now().UTC().Add(time.Duration(i) * time.Minute),
			CompletedAt:   time.Now().UTC().Add(time.Duration(i)*time.Minute + 5*time.Second),
			ObjectsCopied: int64(i),
			Success:       true,
		}
		if err := store.RecordResult(ctx, b.ID, r); err != nil {
			t.Fatalf("RecordResult #%d: %v", i, err)
		}
	}

	got, _ := store.Get(ctx, b.ID)
	if len(got.History) != MaxHistory {
		t.Fatalf("expected history bounded to %d, got %d", MaxHistory, len(got.History))
	}
	// History is most-recent first, so [0] should have the highest
	// ObjectsCopied counter (which we used as the i index).
	if got.History[0].ObjectsCopied != int64(MaxHistory+5-1) {
		t.Fatalf("expected newest result first, got ObjectsCopied=%d", got.History[0].ObjectsCopied)
	}
	if got.LastResult == nil || got.LastResult.ObjectsCopied != int64(MaxHistory+5-1) {
		t.Fatalf("expected LastResult to mirror most recent")
	}
	if got.LastRunAt == nil {
		t.Fatalf("expected LastRunAt to be set")
	}
}

// TestStoreRecordResultErrorsTrim ensures the Errors slice on a
// single result is bounded.
func TestStoreRecordResultErrorsTrim(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	ctx := context.Background()
	b, _ := store.Create(ctx, Backup{OwnerUserID: "matthew", Schedule: ScheduleManual})

	errs := make([]string, maxErrorsPerResult+10)
	for i := range errs {
		errs[i] = "err"
	}
	if err := store.RecordResult(ctx, b.ID, BackupResult{Errors: errs}); err != nil {
		t.Fatalf("RecordResult: %v", err)
	}
	got, _ := store.Get(ctx, b.ID)
	if len(got.LastResult.Errors) != maxErrorsPerResult {
		t.Fatalf("expected Errors trimmed to %d, got %d", maxErrorsPerResult, len(got.LastResult.Errors))
	}
}

// TestStoreMissingFile confirms an empty data dir yields an empty
// store rather than an error.
func TestStoreMissingFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fresh")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore on missing dir: %v", err)
	}
	all, err := store.All(context.Background())
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected empty store, got %d rows", len(all))
	}
}
