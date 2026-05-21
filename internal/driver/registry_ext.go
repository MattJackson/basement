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
//
// User-region cache (ADR-0002, v1.1.0b): a third map keyed by
// "{endpoint}|{accessKeyID}" caches driver instances built from a raw
// (endpoint, accessKey, secret, region) tuple — no Connection record
// required. Used by the /api/v1/user/regions/* endpoints.
type Registry struct {
	conns store.Connections
	mu    sync.RWMutex
	cache map[string]Driver // keyed by connection id (cluster-tier creds)

	userMu    sync.RWMutex
	userCache map[string]Driver // keyed by connID + "|" + accessKeyID (user-tier creds)

	regionMu          sync.RWMutex
	regionCache       map[string]Driver // keyed by endpoint + "|" + accessKeyID
	regionDriverBuild RegionDriverBuilder
	regions           store.UserRegions
}

// RegionDriverBuilder constructs a Driver from a raw (endpoint, accessKey,
// secret, region) tuple — no Connection record required. The Registry
// holds one; production wiring leaves it nil and falls back to the
// registered "garage-v1" factory. Tests override it via
// SetRegionDriverBuilder to inject a mock driver without touching the
// global factory map.
type RegionDriverBuilder func(endpoint, accessKeyID, secretKey, region string) (Driver, error)

// NewRegistry creates a new driver registry backed by the given connections store.
func NewRegistry(conns store.Connections) *Registry {
	return &Registry{
		conns:       conns,
		cache:       make(map[string]Driver),
		userCache:   make(map[string]Driver),
		regionCache: make(map[string]Driver),
	}
}

// SetRegionDriverBuilder overrides the per-region driver constructor.
// Used by tests to inject a mock driver without registering a new factory
// in the global driver map. Pass nil to restore the default
// (Open("garage-v1", cfg)) behaviour.
func (r *Registry) SetRegionDriverBuilder(b RegionDriverBuilder) {
	r.regionMu.Lock()
	defer r.regionMu.Unlock()
	r.regionDriverBuild = b
	// Bust the cache — old entries were built with the previous builder.
	r.regionCache = make(map[string]Driver)
}

// SetUserRegionsStore attaches the per-user region keychain to the
// registry. main.go wires this once at boot, right after
// Store.WireUserRegions. ForUserRegion refuses to operate when this
// hasn't been set, returning ErrUnsupported — guards against a half-
// configured deploy that would otherwise sign user requests against
// regions that nothing else in the system can persist or look up.
func (r *Registry) SetUserRegionsStore(s store.UserRegions) {
	r.regionMu.Lock()
	defer r.regionMu.Unlock()
	r.regions = s
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

// ForUserRegion builds a driver that signs against an arbitrary S3
// endpoint with the supplied creds. No Connection record required —
// regions are user-owned and don't necessarily map to an admin-curated
// cluster (ADR-0002).
//
// Cache key: (endpoint, accessKeyID). Repeat calls from the same user
// for the same region share an underlying S3 client. Eviction on
// UserRegion delete is the caller's responsibility via
// InvalidateUserRegion; cycle staleness on rotation is acceptable
// because regions are user-managed and changes are rare.
//
// Driver type is forced to garage-v1 for now (matches v0.9.0e
// behaviour); when AWS/MinIO BYO support lands the caller will pass a
// driver type explicitly. The garage-v1 driver's S3 path only needs
// s3_endpoint + access_key_id + secret_key + region — admin_url and
// admin_token are not required for user-tier ops.
//
// Returns ErrUnsupported when SetUserRegionsStore hasn't been called
// (Store.UserRegions() was nil at wire-up) — defensive against a half-
// configured deploy. Callers map to 503.
func (r *Registry) ForUserRegion(_ context.Context, endpoint, accessKeyID, secretKey, region string) (Driver, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("ForUserRegion: empty endpoint")
	}
	if accessKeyID == "" {
		return nil, fmt.Errorf("ForUserRegion: empty accessKeyID")
	}
	if secretKey == "" {
		return nil, fmt.Errorf("ForUserRegion: empty secretKey")
	}

	cacheKey := endpoint + "|" + accessKeyID

	r.regionMu.RLock()
	regions := r.regions
	cached, ok := r.regionCache[cacheKey]
	builder := r.regionDriverBuild
	r.regionMu.RUnlock()
	if regions == nil {
		return nil, ErrUnsupported
	}
	if ok {
		return cached, nil
	}

	var drv Driver
	var err error
	if builder != nil {
		drv, err = builder(endpoint, accessKeyID, secretKey, region)
	} else {
		cfg := Config{
			"s3_endpoint":   endpoint,
			"access_key_id": accessKeyID,
			"secret_key":    secretKey,
			"region":        region,
			// Legacy aliases the garage_v1 S3 path may consult.
			"s3_access_key": accessKeyID,
			"s3_secret_key": secretKey,
		}
		drv, err = Open("garage-v1", cfg)
	}
	if err != nil {
		return nil, fmt.Errorf("building per-region driver for %s: %w", endpoint, err)
	}

	r.regionMu.Lock()
	r.regionCache[cacheKey] = drv
	r.regionMu.Unlock()

	return drv, nil
}

// InvalidateUserRegion evicts the cached per-region driver for the
// (endpoint, accessKeyID) pair. Callers invoke this after a UserRegion
// is deleted or its secret rotated so the next ForUserRegion rebuild
// picks up the fresh creds. Idempotent — a miss is a no-op.
func (r *Registry) InvalidateUserRegion(endpoint, accessKeyID string) {
	r.regionMu.Lock()
	delete(r.regionCache, endpoint+"|"+accessKeyID)
	r.regionMu.Unlock()
}
