package sync

import (
	"testing"
	"time"
)

// TestEngineConcurrency tests that engine can be created with custom concurrency settings
func TestEngineConcurrency(t *testing.T) {
	dataDir := t.TempDir()
	store := NewFileStore(dataDir)

	engine := NewEngine(store, 2)
	if engine == nil {
		t.Fatal("Expected non-nil engine")
	}

	defaultEngine := NewEngine(store, 0)
	if defaultEngine == nil {
		t.Fatal("Expected non-nil default engine")
	}

	job := &SyncJob{
		ID:              GenerateID(),
		OwnerUserID:     "test-user",
		Mode:            "pull",
		SrcConnectionID: "conn-1",
		SrcBucket:       "bucket-a",
		DstConnectionID: "conn-2",
		DstBucket:       "bucket-b",
		CreatedAt:       time.Now().UTC(),
		State:           "queued",
	}

	if err := store.Save(job); err != nil {
		t.Fatalf("store.Save() error = %v", err)
	}

	persisted, err := store.Load(job.ID)
	if err != nil {
		t.Fatalf("store.Load() error = %v", err)
	}

	if persisted.OwnerUserID != "test-user" {
		t.Errorf("Expected OwnerUserID 'test-user', got '%s'", persisted.OwnerUserID)
	}
}
