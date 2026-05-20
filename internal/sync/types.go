package sync

import (
	"time"
)

// SyncJob represents a sync job configuration and state.
type SyncJob struct {
	ID              string       `json:"id"`
	OwnerUserID     string       `json:"owner_user_id"`
	Mode            string       `json:"mode"` // "pull" | "push" | "two-way"
	SrcConnectionID string       `json:"src_connection_id"`
	SrcBucket       string       `json:"src_bucket"`
	SrcPrefix       string       `json:"src_prefix,omitempty"`
	DstConnectionID string       `json:"dst_connection_id"`
	DstBucket       string       `json:"dst_bucket"`
	DstPrefix       string       `json:"dst_prefix,omitempty"`
	CreatedAt       time.Time    `json:"created_at"`
	State           string       `json:"state"` // "queued" | "running" | "paused" | "done" | "error"
	Progress        SyncProgress `json:"progress"`
	LastError       string       `json:"last_error,omitempty"`
}

// SyncProgress tracks the progress of a sync job.
type SyncProgress struct {
	ObjectsTotal  int       `json:"objects_total"`
	ObjectsCopied int       `json:"objects_copied"`
	BytesTotal    int64     `json:"bytes_total"`
	BytesCopied   int64     `json:"bytes_copied"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}
