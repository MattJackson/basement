package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// loadJSON reads a JSON file, unmarshals it into T, and returns the result.
// If the file does not exist, returns zero value of T with nil error.
func loadJSON[T any](path string) (T, error) {
	var v T
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return v, nil
		}
		return v, fmt.Errorf("reading %s: %w", path, err)
	}

	if len(data) == 0 {
		return v, nil
	}

	if err := json.Unmarshal(data, &v); err != nil {
		return v, fmt.Errorf("unmarshaling %s: %w", path, err)
	}

	return v, nil
}

// saveJSON marshals v to JSON (indent=2), writes to <path>.tmp, fsyncs,
// then renames over <path> atomically.
func saveJSON(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating dir for %s: %w", path, err)
	}

	tmpPath := path + ".tmp"

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling: %w", err)
	}

	data = append(data, '\n')

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("writing tmp file: %w", err)
	}

	f, err := os.OpenFile(tmpPath, os.O_RDONLY|os.O_SYNC, 0644)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("opening tmp for fsync: %w", err)
	}

	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("fsyncing tmp file: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing tmp after fsync: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming tmp to final path: %w", err)
	}

	return nil
}
