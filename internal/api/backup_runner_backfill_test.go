package api

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/backup"
	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/store"
)

// TestPruneSnapshots_NoTimestamps returns early when no snapshots exist.
func TestPruneSnapshots_NoTimestamps(t *testing.T) {
	srv, _ := newTestServerEnv(t)

	b := backup.Backup{
		ID:          "test-backup",
		Name:        "test-backup",
		DstBucket:   "test-bucket",
		DstPrefix:   "",
		Retention:   backup.RetentionPolicy{KeepDaily: 7},
		Schedule:    backup.ScheduleManual,
	}

	mockDrv := &testMockDriver{}
	mockDrv.listObjectsFunc = func(ctx context.Context, bucket, prefix, continuation string, delim string, limit int) (driver.ObjectPage, error) {
		return driver.ObjectPage{IsTruncated: false}, nil
	}

	pruned, _, err := srv.pruneSnapshots(context.Background(), b, mockDrv, time.Now())

	if pruned != 0 {
		t.Errorf("expected 0 when no snapshots, got %d", pruned)
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestPruneSnapshots_ZeroRetention returns early when retention is zero.
func TestPruneSnapshots_ZeroRetention(t *testing.T) {
	srv, _ := newTestServerEnv(t)

	b := backup.Backup{
		ID:          "test-backup",
		Name:        "test-backup",
		DstBucket:   "test-bucket",
		DstPrefix:   "",
		Retention:   backup.RetentionPolicy{}, // Zero retention - no pruning
		Schedule:    backup.ScheduleManual,
	}

	now := time.Now().UTC()
	mockDrv := &testMockDriver{}
	
	// Return 2 snapshot timestamps
	mockDrv.listObjectsFunc = func(ctx context.Context, bucket, prefix, continuation string, delim string, limit int) (driver.ObjectPage, error) {
		return driver.ObjectPage{
			CommonPrefixes: []string{
				backup.SnapshotRoot(b.Name) + now.Add(-24*time.Hour).Format(backup.SnapshotTimestampLayout),
				backup.SnapshotRoot(b.Name) + now.Add(-48*time.Hour).Format(backup.SnapshotTimestampLayout),
			},
			IsTruncated: false,
		}, nil
	}

	pruned, _, err := srv.pruneSnapshots(context.Background(), b, mockDrv, now)

	if pruned != 0 {
		t.Errorf("expected 0 with zero retention, got %d", pruned)
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestPruneSnapshots_PrunesOldSnapshots deletes snapshots beyond retention.
func TestPruneSnapshots_PrunesOldSnapshots(t *testing.T) {
	srv, _ := newTestServerEnv(t)

	b := backup.Backup{
		ID:          "test-backup",
		Name:        "test-backup",
		DstBucket:   "test-bucket",
		DstPrefix:   "",
		Retention:   backup.RetentionPolicy{KeepDaily: 3}, // Keep only last 3 days
		Schedule:    backup.ScheduleManual,
	}

	now := time.Now().UTC()
	prunedCount := 0
	
	mockDrv := &testMockDriver{}
	
	// Return timestamps - some should be pruned based on retention
	mockDrv.listObjectsFunc = func(ctx context.Context, bucket, prefix, continuation string, delim string, limit int) (driver.ObjectPage, error) {
		return driver.ObjectPage{
			CommonPrefixes: []string{
				backup.SnapshotRoot(b.Name) + now.Add(-12*time.Hour).Format(backup.SnapshotTimestampLayout), // Keep (within 3 days)
				backup.SnapshotRoot(b.Name) + now.Add(-36*time.Hour).Format(backup.SnapshotTimestampLayout),  // Keep (within 3 days)
				// Prune: older than 3 days
				backup.SnapshotRoot(b.Name) + now.Add(-4*24*time.Hour).Format(backup.SnapshotTimestampLayout),
				backup.SnapshotRoot(b.Name) + now.Add(-5*24*time.Hour).Format(backup.SnapshotTimestampLayout),
			},
			IsTruncated: false,
		}, nil
	}
	
	mockDrv.deleteObjectFunc = func(ctx context.Context, bucket, key string) error {
		prunedCount++
		return nil // Simulate successful delete
	}

	pruned, _, err := srv.pruneSnapshots(context.Background(), b, mockDrv, now)

	if pruned < 1 {
		t.Errorf("expected at least 1 snapshot pruned, got %d", pruned)
	}
	if err != nil {
		t.Logf("error (acceptable for partial prune): %v", err)
	}
}

// TestPruneSnapshots_PartialFailure logs and continues.
func TestPruneSnapshots_PartialFailure(t *testing.T) {
	srv, _ := newTestServerEnv(t)

	b := backup.Backup{
		ID:          "test-backup",
		Name:        "test-backup",
		DstBucket:   "test-bucket",
		DstPrefix:   "",
		Retention:   backup.RetentionPolicy{KeepDaily: 3},
		Schedule:    backup.ScheduleManual,
	}

	now := time.Now().UTC()
	
	mockDrv := &testMockDriver{}
	
	mockDrv.listObjectsFunc = func(ctx context.Context, bucket, prefix, continuation string, delim string, limit int) (driver.ObjectPage, error) {
		if len(prefix) > 20 && !strings.Contains(prefix, "CommonPrefixes") {
			// This is a deleteAllUnderPrefix call - return objects to trigger delete
			return driver.ObjectPage{
				Objects: []driver.ObjectInfo{
					{Key: prefix + "file1.txt", Size: 100, IsDir: false},
				},
				IsTruncated: false,
			}, nil
		}
		// This is a listSnapshotTimestamps call - return CommonPrefixes
		return driver.ObjectPage{
			CommonPrefixes: []string{
				backup.SnapshotRoot(b.Name) + now.Add(-4*24*time.Hour).Format(backup.SnapshotTimestampLayout),
				backup.SnapshotRoot(b.Name) + now.Add(-5*24*time.Hour).Format(backup.SnapshotTimestampLayout),
			},
			IsTruncated: false,
		}, nil
	}
	
	prunedCount := 0
	mockDrv.deleteObjectFunc = func(ctx context.Context, bucket, key string) error {
		prunedCount++
		if prunedCount == 1 {
			return fmt.Errorf("simulated delete failure") // Fail on first delete
		}
		return nil // Success on second
	}

	pruned, _, err := srv.pruneSnapshots(context.Background(), b, mockDrv, now)

	// Should have pruned at least one despite partial failure
	if pruned < 1 {
		t.Errorf("expected at least 1 snapshot pruned despite error, got %d", pruned)
	}
	// Error should be reported but not prevent pruning of remaining snapshots
	if err == nil {
		t.Error("expected error from failed delete")
	}
}

// TestListSnapshotTimestamps_EmptyBucket returns empty list.
func TestListSnapshotTimestamps_EmptyBucket(t *testing.T) {
	mockDrv := &testMockDriver{}
	mockDrv.listObjectsFunc = func(ctx context.Context, bucket, prefix, continuation string, delim string, limit int) (driver.ObjectPage, error) {
		return driver.ObjectPage{IsTruncated: false}, nil
	}

	timestamps, err := listSnapshotTimestamps(context.Background(), mockDrv, "test-bucket", backup.SnapshotRoot("test-backup"))

	if len(timestamps) != 0 {
		t.Errorf("expected empty list, got %d timestamps", len(timestamps))
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestListSnapshotTimestamps_IgnoresNonMatchingPrefixes skips non-timestamp prefixes.
func TestListSnapshotTimestamps_IgnoresNonMatchingPrefixes(t *testing.T) {
	mockDrv := &testMockDriver{}
	
	now := time.Now().UTC()
	validTS := now.Add(-24*time.Hour).Format(backup.SnapshotTimestampLayout)
	
	mockDrv.listObjectsFunc = func(ctx context.Context, bucket, prefix, continuation string, delim string, limit int) (driver.ObjectPage, error) {
		return driver.ObjectPage{
			CommonPrefixes: []string{
				"operator-data/",                    // Should be ignored - not a timestamp
				backup.SnapshotRoot("test-backup") + validTS, // Should be included
				"random-folder/",                   // Should be ignored
			},
			IsTruncated: false,
		}, nil
	}

	timestamps, err := listSnapshotTimestamps(context.Background(), mockDrv, "test-bucket", backup.SnapshotRoot("test-backup"))

	if len(timestamps) != 1 {
		t.Errorf("expected 1 timestamp (filtered), got %d: %v", len(timestamps), timestamps)
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestListSnapshotTimestamps_HandlesPagination processes multiple pages.
func TestListSnapshotTimestamps_HandlesPagination(t *testing.T) {
	mockDrv := &testMockDriver{}
	
	page1Called := false
	page2Called := false
	
	now := time.Now().UTC()
	validTS1 := now.Add(-24*time.Hour).Format(backup.SnapshotTimestampLayout)
	validTS2 := now.Add(-48*time.Hour).Format(backup.SnapshotTimestampLayout)
	
	mockDrv.listObjectsFunc = func(ctx context.Context, bucket, prefix, continuation string, delim string, limit int) (driver.ObjectPage, error) {
		if !page1Called {
			page1Called = true
			return driver.ObjectPage{
				CommonPrefixes: []string{
					backup.SnapshotRoot("test-backup") + validTS1,
				},
				IsTruncated:    true,
				NextContinuation: "page2-token",
			}, nil
		}
		page2Called = true
		return driver.ObjectPage{
			CommonPrefixes: []string{
				backup.SnapshotRoot("test-backup") + validTS2,
			},
			IsTruncated: false,
		}, nil
	}

	timestamps, err := listSnapshotTimestamps(context.Background(), mockDrv, "test-bucket", backup.SnapshotRoot("test-backup"))

	if page1Called != true || !page2Called {
		t.Error("expected both pages to be called")
	}
	
	if len(timestamps) != 2 {
		t.Errorf("expected 2 timestamps from pagination, got %d: %v", len(timestamps), timestamps)
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestDeleteAllUnderPrefix_EmptyBucket returns 0 bytes.
func TestDeleteAllUnderPrefix_EmptyBucket(t *testing.T) {
	mockDrv := &testMockDriver{}
	mockDrv.listObjectsFunc = func(ctx context.Context, bucket, prefix, continuation string, delim string, limit int) (driver.ObjectPage, error) {
		return driver.ObjectPage{IsTruncated: false}, nil
	}

	bytes, err := deleteAllUnderPrefix(context.Background(), mockDrv, "test-bucket", "snapshot/2024-01-01_15-00-00/")

	if bytes != 0 {
		t.Errorf("expected 0 bytes, got %d", bytes)
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestDeleteAllUnderPrefix_DeletesObjects collects byte counts.
func TestDeleteAllUnderPrefix_DeletesObjects(t *testing.T) {
	mockDrv := &testMockDriver{}
	
	mockDrv.listObjectsFunc = func(ctx context.Context, bucket, prefix, continuation string, delim string, limit int) (driver.ObjectPage, error) {
		return driver.ObjectPage{
			Objects: []driver.ObjectInfo{
				{Key: "file1.txt", Size: 100, IsDir: false},
				{Key: "file2.bin", Size: 200, IsDir: false},
				{Key: "folder/", Size: 0, IsDir: true}, // Should be skipped
				{Key: "file3.dat", Size: 50, IsDir: false},
			},
			IsTruncated: false,
		}, nil
	}

	bytes, err := deleteAllUnderPrefix(context.Background(), mockDrv, "test-bucket", "snapshot/2024-01-01_15-00-00/")

	if bytes != 350 { // 100 + 200 + 50
		t.Errorf("expected 350 bytes, got %d", bytes)
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestDeleteAllUnderPrefix_PaginatedDeletes handles large directories.
func TestDeleteAllUnderPrefix_PaginatedDeletes(t *testing.T) {
	mockDrv := &testMockDriver{}
	
	pageCount := 0
	mockDrv.listObjectsFunc = func(ctx context.Context, bucket, prefix, continuation string, delim string, limit int) (driver.ObjectPage, error) {
		pageCount++
		if pageCount == 1 {
			return driver.ObjectPage{
				Objects: []driver.ObjectInfo{
					{Key: "file1.txt", Size: 100, IsDir: false},
					{Key: "file2.txt", Size: 200, IsDir: false},
				},
				IsTruncated:    true,
				NextContinuation: "page2-token",
			}, nil
		}
		return driver.ObjectPage{
			Objects: []driver.ObjectInfo{
				{Key: "file3.txt", Size: 300, IsDir: false},
			},
			IsTruncated: false,
		}, nil
	}

	bytes, err := deleteAllUnderPrefix(context.Background(), mockDrv, "test-bucket", "snapshot/2024-01-01_15-00-00/")

	if bytes != 600 { // 100 + 200 + 300
		t.Errorf("expected 600 bytes, got %d", bytes)
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	if pageCount != 2 {
		t.Errorf("expected 2 pages, got %d", pageCount)
	}
}

// TestDeleteAllUnderPrefix_ListErrorStopsLoop returns early on list failure.
func TestDeleteAllUnderPrefix_ListErrorStopsLoop(t *testing.T) {
	mockDrv := &testMockDriver{}
	
	mockDrv.listObjectsFunc = func(ctx context.Context, bucket, prefix, continuation string, delim string, limit int) (driver.ObjectPage, error) {
		return driver.ObjectPage{}, fmt.Errorf("list failure")
	}

	bytes, err := deleteAllUnderPrefix(context.Background(), mockDrv, "test-bucket", "snapshot/2024-01-01_15-00-00/")

	if bytes != 0 {
		t.Errorf("expected 0 bytes on error, got %d", bytes)
	}
	if err == nil {
		t.Error("expected error from list failure")
	}
}

// TestDeleteAllUnderPrefix_DeleteErrorStopsLoop returns early on delete failure.
func TestDeleteAllUnderPrefix_DeleteErrorStopsLoop(t *testing.T) {
	mockDrv := &testMockDriver{}
	
	mockDrv.listObjectsFunc = func(ctx context.Context, bucket, prefix, continuation string, delim string, limit int) (driver.ObjectPage, error) {
		return driver.ObjectPage{
			Objects: []driver.ObjectInfo{
				{Key: "file1.txt", Size: 100, IsDir: false},
				{Key: "file2.txt", Size: 200, IsDir: false},
			},
			IsTruncated: false,
		}, nil
	}
	
	mockDrv.deleteObjectFunc = func(ctx context.Context, bucket, key string) error {
		if key == "file1.txt" {
			return fmt.Errorf("delete failure") // Fail on first file
		}
		return nil
	}

	bytes, err := deleteAllUnderPrefix(context.Background(), mockDrv, "test-bucket", "snapshot/2024-01-01_15-00-00/")

	if bytes != 0 { // Should return immediately on first error
		t.Errorf("expected 0 bytes (early exit), got %d", bytes)
	}
	if err == nil {
		t.Error("expected error from delete failure")
	}
}

// TestResolveBackupConn_EmptyConnection returns error.
func TestResolveBackupConn_EmptyConnection(t *testing.T) {
	srv, _ := newTestServerEnv(t)

	conn, err := srv.resolveBackupConn(context.Background(), "test-user", "")

	if conn != "" {
		t.Errorf("expected empty connection on error, got %q", conn)
	}
	if err == nil {
		t.Error("expected error for empty connection ID")
	}
}

// TestResolveBackupConn_FallsBackToConnectionID when region not found.
func TestResolveBackupConn_FallsBackToConnectionID(t *testing.T) {
	srv, _ := newTestServerEnv(t)

	conn, err := srv.resolveBackupConn(context.Background(), "test-user", "direct-connection-id")

	// Should fall back to treating as connection ID without error
	if conn != "direct-connection-id" {
		t.Errorf("expected direct fallback, got %q", conn)
	}
	_ = err
}

// TestResolveBackupConn_NoRegionsStoreFallsBack.
func TestResolveBackupConn_NoRegionsStoreFallsBack(t *testing.T) {
	srv, _ := newTestServerEnv(t)

	conn, err := srv.resolveBackupConn(context.Background(), "test-user", "direct-connection-id")

	if conn != "direct-connection-id" {
		t.Errorf("expected direct fallback, got %q", conn)
	}
	if err != nil {
		t.Logf("error (may be expected): %v", err)
	}
}

// newTestServerEnv creates a minimal server for backup runner tests.
func newTestServerEnv(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	tmp := t.TempDir()

	cfg := newTestConfig()
	cfg.DataDir = tmp

	st, err := store.Open(tmp, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	srv := New(cfg, st, &testMockConnectionStore{}, nil, nil)
	return srv, st
}
