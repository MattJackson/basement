package driver

import (
	"context"
	"fmt"
	"sync"

	"github.com/mattjackson/basement/internal/store"
)

// Registry is a runtime cache for building and reusing driver instances from
// connection records. It caches built instances by connection ID so repeated
// calls to For() are cheap. Callers should signal Invalidate(connID) after
// Update/Delete operations to evict stale entries.
type Registry struct {
	conns store.Connections
	mu    sync.RWMutex
	cache map[string]Driver // keyed by connection id
}

// NewRegistry creates a new driver registry backed by the given connections store.
func NewRegistry(conns store.Connections) *Registry {
	return &Registry{
		conns: conns,
		cache: make(map[string]Driver),
	}
}

// For returns a driver instance for the given connection ID. It looks up the
// connection from the backing store, builds a new driver if not cached, and
// returns the cached or newly built instance. Errors include not found and
// unknown driver (if no builder is registered for that driver type).
func (r *Registry) For(ctx context.Context, connID string) (Driver, error) {
	// Fast path: check cache with read lock
	r.mu.RLock()
	cached, ok := r.cache[connID]
	r.mu.RUnlock()

	if ok {
		return cached, nil
	}

	// Slow path: miss, need to load and build
	conn, err := r.conns.Get(ctx, connID)
	if err != nil {
		return nil, fmt.Errorf("getting connection %s: %w", connID, err)
	}

	drv, err := BuildFor(conn)
	if err != nil {
		return nil, fmt.Errorf("building driver for connection %s: %w", connID, err)
	}

	// Store in cache with write lock
	r.mu.Lock()
	r.cache[connID] = drv
	r.mu.Unlock()

	return drv, nil
}

// BuildFor builds a driver instance from a Connection record. It looks up the
// builder registered for the connection's Driver and calls it with the
// connection's Config map. Returns ErrUnknownDriver if no builder is registered.
func BuildFor(conn store.Connection) (Driver, error) {
	// Reuse existing Open which uses factories map
	return Open(conn.Driver, conn.Config)
}

// Invalidate removes a cached driver instance for the given connection ID.
// Call this after Update/Delete to ensure stale instances are not reused.
func (r *Registry) Invalidate(connID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.cache, connID)
}
