package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var auditMu sync.Mutex

// AuditEntry represents a single audit log entry.
type AuditEntry struct {
	Timestamp time.Time              `json:"ts"`
	UserID    string                 `json:"user_id"`
	Action    string                 `json:"action"`     // e.g. "bucket.create"
	Resource  string                 `json:"resource"`   // e.g. "bucket:photos"
	Details   map[string]any         `json:"details,omitempty"`
}

// AppendAudit appends an audit entry to the daily log file.
func (s *Store) AppendAudit(entry AuditEntry) error {
	auditMu.Lock()
	defer auditMu.Unlock()

	// Default the timestamp to server time when unset. (Callers are all
	// server-side handlers that pass the real event time via time.Now();
	// daily-file rotation is keyed on this event time. A zero value would
	// otherwise misfile the entry into the year-1 log.)
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	dir := filepath.Join(s.dataDir, "audit")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating audit dir: %w", err)
	}

	dateStr := entry.Timestamp.Format("2006-01-02")
	logPath := filepath.Join(dir, dateStr+".jsonl")

	// 0600: the audit trail (who-did-what) must not be world-readable.
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("opening audit log: %w", err)
	}
	defer func() { _ = f.Close() }()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling audit entry: %w", err)
	}

	data = append(data, '\n')

	writer := bufio.NewWriter(f)
	if _, err := writer.Write(data); err != nil {
		return fmt.Errorf("writing audit entry: %w", err)
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flushing audit log: %w", err)
	}

	if err := f.Sync(); err != nil {
		return fmt.Errorf("syncing audit log: %w", err)
	}

	return nil
}

// CleanupAudit removes audit log files older than the retention period.
func (s *Store) CleanupAudit() error {
	auditMu.Lock()
	defer auditMu.Unlock()

	dir := filepath.Join(s.dataDir, "audit")

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading audit dir: %w", err)
	}

	now := time.Now()
	cutoff := now.Add(-s.retention)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}

		dateStr := strings.TrimSuffix(name, ".jsonl")
		logDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue // skip files with invalid dates
		}

		if logDate.Before(cutoff) || logDate.Equal(cutoff) {
			logPath := filepath.Join(dir, name)
			if err := os.Remove(logPath); err != nil {
				return fmt.Errorf("removing old audit file %s: %w", name, err)
			}
		}
	}

	return nil
}


