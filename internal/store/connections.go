// Package store implements persistent storage for connection records.
//
// Connections are stored in connections.json under BASEMENT_DATA_DIR.
// Each connection represents a backend driver configuration (garage, garage-v1, aws-s3).
//
// Encryption at rest: NOT implemented in v0.2.0. Plain JSON is acceptable given
// that BASEMENT_DATA_DIR is operator-controlled with filesystem ACLs. This is a
// hardening pass required before the v0.5.0 release.
package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Supported drivers for validation.
const (
	DriverGarage    = "garage"
	DriverGarageV1  = "garage-v1"
	DriverAWSS3     = "aws-s3"
)

// SupportedDrivers is the set of drivers that can be used in connections.
var SupportedDrivers = map[string]bool{
	DriverGarage:  true,
	DriverGarageV1: true,
	DriverAWSS3:    true,
}

// Connection represents a backend connection configuration.
type Connection struct {
	ID        string                 `json:"id"`         // UUID
	Label     string                 `json:"label"`      // operator-set, mutable, unique case-insensitive
	Driver    string                 `json:"driver"`     // "garage" | "garage-v1" | "aws-s3"
	Config    map[string]string      `json:"config"`     // per-driver keys: adminUrl, adminToken, region, accessKey, secretKey, endpoint
	Color     string                 `json:"color,omitempty"` // hex; default "#C9874B" if empty
	Owner     string                 `json:"owner"`      // "org" always for v0.2.0
	CreatedAt time.Time              `json:"createdAt"`
}

// Connections interface defines the CRUD operations for connection records.
type Connections interface {
	List(ctx context.Context) ([]Connection, error)
	Get(ctx context.Context, id string) (Connection, error)
	Create(ctx context.Context, c Connection) (Connection, error) // assigns ID + createdAt
	Update(ctx context.Context, id string, patch Connection) (Connection, error)
	Delete(ctx context.Context, id string) error
	// Convenience for boot-time auto-seed:
	Count(ctx context.Context) (int, error)
}

// store implements Connections using JSON file persistence.
type store struct {
	dataDir     string
	connPath    string
	connsMu     sync.RWMutex
	connsCache  []Connection
}

// OpenConnections opens or creates the connections store at dataDir.
func OpenConnections(dataDir string) (Connections, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}

	s := &store{
		dataDir:    dataDir,
		connPath:   filepath.Join(dataDir, "connections.json"),
		connsCache: make([]Connection, 0),
	}

	if err := s.load(); err != nil {
		return nil, fmt.Errorf("loading existing connections: %w", err)
	}

	return s, nil
}

// load reads all connections from disk into the cache.
func (s *store) load() error {
	conns, err := loadJSON[[]Connection](s.connPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("loading connections: %w", err)
	}

	s.connsMu.Lock()
	defer s.connsMu.Unlock()

	if err == nil {
		s.connsCache = conns
	} else {
		s.connsCache = make([]Connection, 0)
	}

	return nil
}

// save writes the connections cache to disk atomically.
func (s *store) save() error {
	return saveJSON(s.connPath, s.connsCache)
}

// List returns all connections. Callers receive a deep copy.
func (s *store) List(ctx context.Context) ([]Connection, error) {
	s.connsMu.RLock()
	defer s.connsMu.RUnlock()

	result := make([]Connection, len(s.connsCache))
	copy(result, s.connsCache)
	return result, nil
}

// Get returns a single connection by ID. Returns error if not found.
func (s *store) Get(ctx context.Context, id string) (Connection, error) {
	s.connsMu.RLock()
	defer s.connsMu.RUnlock()

	for _, c := range s.connsCache {
		if c.ID == id {
			return c, nil
		}
	}

	return Connection{}, fmt.Errorf("connection not found: %s", id)
}

// Create adds a new connection. Assigns UUID and createdAt timestamp.
// Validates driver is supported and label is unique (case-insensitive).
func (s *store) Create(ctx context.Context, c Connection) (Connection, error) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()

	// Validate driver
	if !SupportedDrivers[c.Driver] {
		return Connection{}, fmt.Errorf("unsupported driver: %q", c.Driver)
	}

	// Validate label is non-empty and unique (case-insensitive trimmed)
	label := strings.TrimSpace(strings.ToLower(c.Label))
	if label == "" {
		return Connection{}, fmt.Errorf("label must be non-empty")
	}

	for _, existing := range s.connsCache {
		if strings.TrimSpace(strings.ToLower(existing.Label)) == label {
			return Connection{}, fmt.Errorf("duplicate label (case-insensitive): %q", c.Label)
		}
	}

	// Assign ID if not provided
	if c.ID == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return Connection{}, fmt.Errorf("generating UUID: %w", err)
		}
		c.ID = uuid.UUID(b).String()
	}

	// Assign createdAt if not provided
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}

	// Set default color if empty
	if c.Color == "" {
		c.Color = "#C9874B"
	}

	s.connsCache = append(s.connsCache, c)

	if err := s.save(); err != nil {
		return Connection{}, fmt.Errorf("persisting connection: %w", err)
	}

	return c, nil
}

// Update modifies an existing connection by ID. Returns error if not found.
// Only non-empty fields in the patch are applied (partial update).
func (s *store) Update(ctx context.Context, id string, patch Connection) (Connection, error) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()

	for i := range s.connsCache {
		if s.connsCache[i].ID == id {
			conn := &s.connsCache[i]

			// Apply patch fields if non-empty/non-nil
			if patch.Label != "" {
				label := strings.TrimSpace(strings.ToLower(patch.Label))
				if label == "" {
					return Connection{}, fmt.Errorf("label must be non-empty")
				}

				// Check uniqueness (excluding self)
				for _, existing := range s.connsCache {
					if existing.ID != id && strings.TrimSpace(strings.ToLower(existing.Label)) == label {
						return Connection{}, fmt.Errorf("duplicate label (case-insensitive): %q", patch.Label)
					}
				}

				conn.Label = patch.Label
			}

			if patch.Driver != "" && SupportedDrivers[patch.Driver] {
				conn.Driver = patch.Driver
			} else if patch.Driver != "" && !SupportedDrivers[patch.Driver] {
				return Connection{}, fmt.Errorf("unsupported driver: %q", patch.Driver)
			}

			if patch.Config != nil {
				conn.Config = patch.Config
			}

			if patch.Color != "" {
				conn.Color = patch.Color
			}

			if err := s.save(); err != nil {
				return Connection{}, fmt.Errorf("persisting update: %w", err)
			}

			return *conn, nil
		}
	}

	return Connection{}, fmt.Errorf("connection not found: %s", id)
}

// Delete removes a connection by ID. Returns error if not found.
func (s *store) Delete(ctx context.Context, id string) error {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()

	for i := range s.connsCache {
		if s.connsCache[i].ID == id {
			s.connsCache = append(s.connsCache[:i], s.connsCache[i+1:]...)
			return s.save()
		}
	}

	return fmt.Errorf("connection not found: %s", id)
}

// Count returns the number of connections.
func (s *store) Count(ctx context.Context) (int, error) {
	s.connsMu.RLock()
	defer s.connsMu.RUnlock()

	return len(s.connsCache), nil
}

// GenerateID creates a new UUID for connection IDs.
func GenerateID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating UUID: %w", err)
	}
	return uuid.UUID(b).String(), nil
}

// GenerateToken creates a random hex token for authentication.
func GenerateToken(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
