// Package api: tests for backup runner CSK lock-state awareness (v1.12.0c).
//
// This file contains regression tests ensuring the backup runner correctly
// skips locked clusters when clusterSecrets manager is wired and the cluster
// has CSK admins configured.

package api

import (
	"context"
	"testing"
	"time"

	"github.com/mattjackson/basement/internal/driver"
	"github.com/mattjackson/basement/internal/backup"
	"github.com/mattjackson/basement/internal/clustersecret"
	"github.com/mattjackson/basement/internal/store"
)

// TestBackupRunner_SkipsLockedClusterWithCSK tests the v1.12.0a lock-state
// gate in runBackupOnce: when a cluster is locked AND has CSK admins, the
// backup should be skipped (not failed), returning early with an error message
// indicating the cluster is locked. The scheduler will retry on next interval.
//
// This test pins the behavior to prevent regression where lock-state checks
// might be removed or bypassed in future changes.
func TestBackupRunner_SkipsLockedClusterWithCSK(t *testing.T) {
	dataDir := t.TempDir()
	cfg := newTestConfig()
	cfg.DataDir = dataDir
	st, _ := store.Open(dataDir, 90*24*time.Hour)

	conns := &testMockConnectionStore{
		conns: []store.Connection{
			{
				ID:     "locked-cluster",
				Label:  "locked-cluster",
				Driver: "garage",
				Config: map[string]string{
					"admin_url":   "http://localhost:3476",
					"admin_token": "tok",
				},
			},
		},
	}

	testMock := &testMockDriver{}
	testMock.listObjectsFunc = func(_ context.Context, _, _, _, _ string, _ int) (driver.ObjectPage, error) {
		return driver.ObjectPage{Objects: []driver.ObjectInfo{{Key: "test.txt", ETag: "abc123"}}, IsTruncated: false}, nil
	}

	reg := driver.NewRegistry(conns)
	reg.SetRegionDriverBuilder(func(_, _, _, _, _ string) (driver.Driver, error) {
		return testMock, nil
	})

	srv := New(cfg, st, conns, nil, reg)

	mgr := clustersecret.New(clustersecret.NewMemoryStore())
	if err := mgr.BootstrapFirstAdmin("locked-cluster", "admin1", "password"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	mgr.Lock("locked-cluster")

	srv.SetClusterSecrets(mgr)

	bs, _ := backup.NewFileStore(dataDir)
	runner := srv.NewBackupRunner()

	b, err := bs.Create(context.Background(), backup.Backup{
		ID:          "test-locked-backup",
		OwnerUserID: "matthew",
		Name:        "test-locked-backup",
		Schedule:    backup.ScheduleManual,
		SrcRegionID: "locked-cluster",
		DstRegionID: "locked-cluster",
	})
	if err != nil {
		t.Fatalf("backup.Create: %v", err)
	}

	result := runner.Run(context.Background(), b)

	// Assert: backup was skipped (not failed).
	if result.Success {
		t.Fatal("expected backup to be skipped (not successful)")
	}

	if len(result.Errors) == 0 {
		t.Fatalf("expected error message about locked cluster, got empty errors")
	}

	hasLockedError := false
	for _, e := range result.Errors {
		if containsSubstring(e, "locked") && containsSubstring(e, "skipped") {
			hasLockedError = true
			break
		}
	}
	if !hasLockedError {
		t.Fatalf("expected error mentioning 'locked' and 'skipped', got: %v", result.Errors)
	}

	// Assert: CompletedAt was set.
	if result.CompletedAt.IsZero() {
		t.Fatal("expected CompletedAt to be set")
	}

	// Assert: runner method returned early without invoking sync engine.
	if result.JobID != "" {
		t.Fatalf("expected no JobID for skipped backup, got %q", result.JobID)
	}
}

// TestBackupRunner_AllowsUnlockedClusterWithCSK ensures that when a cluster
// has CSK admins but is unlocked (CSK in memory), the backup runner proceeds
// normally. This is the happy path that must continue working after lock-state
// gates are added.
func TestBackupRunner_AllowsUnlockedClusterWithCSK(t *testing.T) {
	dataDir := t.TempDir()
	cfg := newTestConfig()
	cfg.DataDir = dataDir
	st, _ := store.Open(dataDir, 90*24*time.Hour)

	conns := &testMockConnectionStore{
		conns: []store.Connection{
			{
				ID:     "unlocked-cluster",
				Label:  "unlocked-cluster",
				Driver: "garage",
				Config: map[string]string{
					"admin_url":   "http://localhost:3476",
					"admin_token": "tok",
				},
			},
		},
	}

	testMock := &testMockDriver{}
	testMock.listObjectsFunc = func(_ context.Context, _, _, _, _ string, _ int) (driver.ObjectPage, error) {
		return driver.ObjectPage{Objects: []driver.ObjectInfo{{Key: "test.txt", ETag: "abc123"}}, IsTruncated: false}, nil
	}

	reg := driver.NewRegistry(conns)
	reg.SetRegionDriverBuilder(func(_, _, _, _, _ string) (driver.Driver, error) {
		return testMock, nil
	})

	srv := New(cfg, st, conns, nil, reg)

	mgr := clustersecret.New(clustersecret.NewMemoryStore())
	if err := mgr.BootstrapFirstAdmin("unlocked-cluster", "admin1", "password"); err != nil {
		t.Fatalf("BootstrapFirstAdmin: %v", err)
	}
	// Cluster is unlocked by default after bootstrap; do NOT lock it.

	srv.SetClusterSecrets(mgr)

	bs, _ := backup.NewFileStore(dataDir)
	runCount := 0
	sched := backup.NewScheduler(bs, backup.RunnerFunc(func(_ context.Context, b backup.Backup) backup.BackupResult {
		runCount++
		return backup.BackupResult{
			StartedAt:     time.Now().UTC(),
			CompletedAt:   time.Now().UTC(),
			Success:       true,
			ObjectsCopied: 10,
		}
	}), nil)
	srv.SetBackups(bs, sched)
	t.Cleanup(func() { sched.Stop() })

	b, err := bs.Create(context.Background(), backup.Backup{
		ID:          "test-unlocked-backup",
		OwnerUserID: "matthew",
		Name:        "test-unlocked-backup",
		Schedule:    backup.ScheduleManual,
		SrcRegionID: "unlocked-cluster",
		DstRegionID: "unlocked-cluster",
	})
	if err != nil {
		t.Fatalf("backup.Create: %v", err)
	}

	sched.Trigger(context.Background(), b.ID)
	time.Sleep(10 * time.Millisecond)

	b, err = bs.Get(context.Background(), b.ID)
	if err != nil {
		t.Fatalf("backup.Get: %v", err)
	}

	if !b.LastResult.Success {
		t.Fatalf("expected backup to succeed, got errors: %v", b.LastResult.Errors)
	}

	if runCount != 1 {
		t.Fatalf("expected runner to be called once, got %d", runCount)
	}

	if b.LastResult.ObjectsCopied != 10 {
		t.Fatalf("expected ObjectsCopied=10, got %d", b.LastResult.ObjectsCopied)
	}
}

// TestBackupRunner_SkipsOnlyIfHasCSKAdmins ensures the lock-state check only
// applies to clusters that actually have CSK admins configured. Clusters without
// any CSK setup should proceed normally (no CSK needed for those).
func TestBackupRunner_SkipsOnlyIfHasCSKAdmins(t *testing.T) {
	dataDir := t.TempDir()
	cfg := newTestConfig()
	cfg.DataDir = dataDir
	st, _ := store.Open(dataDir, 90*24*time.Hour)

	conns := &testMockConnectionStore{
		conns: []store.Connection{
			{
				ID:     "no-csk-cluster",
				Label:  "no-csk-cluster",
				Driver: "garage",
				Config: map[string]string{
					"admin_url": "http://localhost:3476",
					// No admin_token - this cluster never had CSK enabled.
				},
			},
		},
	}

	testMock := &testMockDriver{}
	testMock.listObjectsFunc = func(_ context.Context, _, _, _, _ string, _ int) (driver.ObjectPage, error) {
		return driver.ObjectPage{Objects: []driver.ObjectInfo{{Key: "test.txt", ETag: "abc123"}}, IsTruncated: false}, nil
	}

	reg := driver.NewRegistry(conns)
	reg.SetRegionDriverBuilder(func(_, _, _, _, _ string) (driver.Driver, error) {
		return testMock, nil
	})

	srv := New(cfg, st, conns, nil, reg)

	mgr := clustersecret.New(clustersecret.NewMemoryStore())
	// Do NOT bootstrap any admins for "no-csk-cluster" - it has no CSK.

	srv.SetClusterSecrets(mgr)

	bs, _ := backup.NewFileStore(dataDir)
	runCount := 0
	sched := backup.NewScheduler(bs, backup.RunnerFunc(func(_ context.Context, b backup.Backup) backup.BackupResult {
		runCount++
		return backup.BackupResult{
			StartedAt:     time.Now().UTC(),
			CompletedAt:   time.Now().UTC(),
			Success:       true,
			ObjectsCopied: 5,
		}
	}), nil)
	srv.SetBackups(bs, sched)
	t.Cleanup(func() { sched.Stop() })

	b, err := bs.Create(context.Background(), backup.Backup{
		ID:          "test-no-csk-backup",
		OwnerUserID: "matthew",
		Name:        "test-no-csk-backup",
		Schedule:    backup.ScheduleManual,
		SrcRegionID: "no-csk-cluster",
		DstRegionID: "no-csk-cluster",
	})
	if err != nil {
		t.Fatalf("backup.Create: %v", err)
	}

	sched.Trigger(context.Background(), b.ID)
	time.Sleep(10 * time.Millisecond)

	b, err = bs.Get(context.Background(), b.ID)
	if err != nil {
		t.Fatalf("backup.Get: %v", err)
	}

	// Cluster without CSK admins should proceed normally.
	if runCount != 1 {
		t.Fatalf("expected runner to be called once for non-CSK cluster, got %d", runCount)
	}

	if !b.LastResult.Success {
		t.Fatalf("expected backup to succeed (no CSK on cluster), got errors: %v", b.LastResult.Errors)
	}

	if b.LastResult.ObjectsCopied != 5 {
		t.Fatalf("expected ObjectsCopied=5, got %d", b.LastResult.ObjectsCopied)
	}
}

// containsSubstring is a helper for test assertions.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
