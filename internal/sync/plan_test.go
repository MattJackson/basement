package sync

import (
	"testing"
	"time"
)

// TestStorePersistence verifies basic store functionality
func TestStorePersistence(t *testing.T) {
	dataDir := t.TempDir()
	store := NewFileStore(dataDir)

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

	if persisted.ID != job.ID {
		t.Errorf("Expected ID '%s', got '%s'", job.ID, persisted.ID)
	}
}
