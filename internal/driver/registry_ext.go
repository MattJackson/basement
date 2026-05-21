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
//
// User-grant cache (ADR-0001, v0.9.0f): a separate map keyed by
// "{connID}|{accessKeyID}" caches driver instances built with per-user
// S3 credentials so backend audit logs attribute requests to the
// caller's key rather than the cluster's shared key.
type Registry struct {
	conns store.Connections
	mu    sync.RWMutex
	cache map[string]Driver // keyed by connection id (cluster-tier creds)

	userMu    sync.RWMutex
	userCache map[string]Driver // keyed by connID + "|" + accessKeyID (user-tier creds)
}

// NewRegistry creates a new driver registry backed by the given connections store.
func NewRegistry(conns store.Connections) *Registry {
	return &Registry{
		conns:     conns,
		cache:     make(map[string]Driver),
		userCache: make(map[string]Driver),
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
//
// Also evicts every per-user driver instance keyed off this connection,
// so a re-uploaded admin token or rotated S3 endpoint propagates to
// every grant-backed driver on the next request.
func (r *Registry) Invalidate(connID string) {
	r.mu.Lock()
	delete(r.cache, connID)
	r.mu.Unlock()

	r.userMu.Lock()
	for k := range r.userCache {
		if len(k) > len(connID) && k[:len(connID)] == connID && k[len(connID)] == '|' {
			delete(r.userCache, k)
		}
	}
	r.userMu.Unlock()
}

// ForUserGrant returns a driver instance whose S3 credentials are the
// BucketGrant's key (not the cluster's shared key). Per ADR-0001's
// "What the runtime does differently" section: every user-facing S3
// op is signed with the user's identity so backend audit logs see
// "matthew at 14:32" instead of "all activity by GK…".
//
// Caches instances per (connectionID, accessKeyID) so repeat requests
// from the same user share the same underlying S3 client. The cache
// is invalidated alongside the connection-level cache when Invalidate
// is called.
//
// connID identifies the basement Connection record (its admin_url
// + s3_endpoint are read from there); accessKeyID + secretKey override
// the Connection's stored S3 creds. Driver type is whatever the
// Connection record says — Garage v1 today, growing as other backends
// add user-tier grant support.
func (r *Registry) ForUserGrant(ctx context.Context, connID, accessKeyID, secretKey string) (Driver, error) {
	if connID == "" {
		return nil, fmt.Errorf("ForUserGrant: empty connID")
	}
	if accessKeyID == "" {
		return nil, fmt.Errorf("ForUserGrant: empty accessKeyID")
	}
	if secretKey == "" {
		return nil, fmt.Errorf("ForUserGrant: empty secretKey")
	}

	cacheKey := connID + "|" + accessKeyID

	r.userMu.RLock()
	cached, ok := r.userCache[cacheKey]
	r.userMu.RUnlock()
	if ok {
		return cached, nil
	}

	conn, err := r.conns.Get(ctx, connID)
	if err != nil {
		return nil, fmt.Errorf("getting connection %s: %w", connID, err)
	}

	// Clone the Connection's config so the override doesn't mutate the
	// stored record. Per ADR-0001's Edit Cluster refactor (v0.9.0d) the
	// Connection no longer carries access_key_id / secret_key in its
	// canonical fields, but legacy records may still have them — we
	// override here regardless so the user's key wins unambiguously.
	cfg := make(Config, len(conn.Config)+2)
	for k, v := range conn.Config {
		cfg[k] = v
	}
	cfg["access_key_id"] = accessKeyID
	cfg["secret_key"] = secretKey
	// Legacy alias the garage_v1 driver may still consult on some
	// branches — copy through so an older config map keeps working.
	cfg["s3_access_key"] = accessKeyID
	cfg["s3_secret_key"] = secretKey

	drv, err := Open(conn.Driver, cfg)
	if err != nil {
		return nil, fmt.Errorf("building per-user driver for connection %s: %w", connID, err)
	}

	r.userMu.Lock()
	r.userCache[cacheKey] = drv
	r.userMu.Unlock()

	return drv, nil
}
