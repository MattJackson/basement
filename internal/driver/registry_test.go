package driver

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/mattjackson/basement/internal/store"
)

// mockConnStore is a minimal store.Connections implementation for testing.
type mockConnStore struct {
	conns map[string]store.Connection
	mu    sync.RWMutex
}

func newMockConnStore() *mockConnStore {
	return &mockConnStore{
		conns: make(map[string]store.Connection),
	}
}

func (m *mockConnStore) List(ctx context.Context) ([]store.Connection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]store.Connection, 0, len(m.conns))
	for _, c := range m.conns {
		result = append(result, c)
	}
	return result, nil
}

func (m *mockConnStore) Get(ctx context.Context, id string) (store.Connection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	c, ok := m.conns[id]
	if !ok {
		return store.Connection{}, fmt.Errorf("connection not found: %s", id)
	}
	return c, nil
}

func (m *mockConnStore) Create(ctx context.Context, c store.Connection) (store.Connection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if c.ID == "" {
		c.ID = "test-id" // simplify for tests
	}
	m.conns[c.ID] = c
	return c, nil
}

func (m *mockConnStore) Update(ctx context.Context, id string, patch store.Connection) (store.Connection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.conns[id]
	if !ok {
		return store.Connection{}, fmt.Errorf("connection not found: %s", id)
	}
	if patch.Label != "" {
		c.Label = patch.Label
	}
	m.conns[id] = c
	return c, nil
}

func (m *mockConnStore) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.conns, id)
	return nil
}

func (m *mockConnStore) Count(ctx context.Context) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.conns), nil
}

// SwapClusterSecret satisfies the v1.12.0b Connections interface
// addition. The driver registry tests don't exercise the CSK
// migration path, so this is a no-op that lets the type assertion
// pass through; the API-layer migration tests cover the real swap.
func (m *mockConnStore) SwapClusterSecret(ctx context.Context, cid string, oldEnc, newEnc []byte) error {
	return nil
}

// TestRegistryForCaches verifies that For() returns cached instances.
// We use a mock driver registered for testing purposes.
func TestRegistryForCaches(t *testing.T) {
	Register("test-mock", func(cfg Config) (Driver, error) {
		return &mockDriver{}, nil
	})

	mockStore := newMockConnStore()

	conn := store.Connection{
		ID:     "test-conn",
		Label:  "test-label",
		Driver: "test-mock",
		Config: map[string]string{
			"key": "val",
		},
		Owner: "org",
	}

	if _, err := mockStore.Create(context.Background(), conn); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	reg := NewRegistry(mockStore)
	ctx := context.Background()

	drv1, err := reg.For(ctx, "test-conn")
	if err != nil {
		t.Fatalf("For first call failed: %v", err)
	}

	drv2, err := reg.For(ctx, "test-conn")
	if err != nil {
		t.Fatalf("For second call failed: %v", err)
	}

	if drv1 != drv2 {
		t.Error("expected same driver instance from cache (same pointer)")
	}
}

// TestForUserGrantInjectsCredsAllKeyVariants is the regression guard for
// the credential-attribution bug (ADR-0001): ForUserGrant must override the
// connection's stored admin creds for EVERY driver, regardless of which
// config-key variant that driver reads for the S3 access-key ID. The
// aws_s3/minio drivers read cfg["access_key"], the garage drivers read
// cfg["access_key_id"], and legacy maps may carry cfg["s3_access_key"]. If
// any of these keeps the connection's admin key, the per-user grant silently
// signs as the cluster admin and backend audit logs mis-attribute the caller.
func TestForUserGrantInjectsCredsAllKeyVariants(t *testing.T) {
	const (
		adminAK = "GKADMIN00000000000000"
		adminSK = "adminsecret00000000000000000000000000000"
		userAK  = "GKUSER000000000000000"
		userSK  = "usersecret0000000000000000000000000000000"
	)

	var captured Config
	Register("test-usergrant-capture", func(cfg Config) (Driver, error) {
		// Snapshot the config the factory was handed.
		captured = make(Config, len(cfg))
		for k, v := range cfg {
			captured[k] = v
		}
		return &mockDriver{}, nil
	})

	mockStore := newMockConnStore()
	conn := store.Connection{
		ID:     "usergrant-conn",
		Driver: "test-usergrant-capture",
		Config: map[string]string{
			// Legacy admin creds still present on the stored record under
			// every variant — the user grant must override all of them.
			"access_key":    adminAK,
			"access_key_id": adminAK,
			"s3_access_key": adminAK,
			"secret_key":    adminSK,
			"s3_secret_key": adminSK,
		},
		Owner: "org",
	}
	if _, err := mockStore.Create(context.Background(), conn); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	reg := NewRegistry(mockStore)
	if _, err := reg.ForUserGrant(context.Background(), "usergrant-conn", userAK, userSK); err != nil {
		t.Fatalf("ForUserGrant: %v", err)
	}

	// Every access-key-ID variant any driver might read must carry the
	// user grant's key, not the connection's admin key.
	for _, k := range []string{"access_key", "access_key_id", "s3_access_key"} {
		if got := captured[k]; got != userAK {
			t.Errorf("cfg[%q] = %q, want user grant key %q (NOT admin key %q)", k, got, userAK, adminAK)
		}
	}
	// Both secret-key variants must carry the user grant's secret.
	for _, k := range []string{"secret_key", "s3_secret_key"} {
		if got := captured[k]; got != userSK {
			t.Errorf("cfg[%q] = %q, want user grant secret (NOT admin secret)", k, got)
		}
	}

	// Sanity: the stored connection record must remain unmutated — the
	// override clones the config map.
	stored, _ := mockStore.Get(context.Background(), "usergrant-conn")
	if stored.Config["access_key_id"] != adminAK {
		t.Errorf("stored connection config was mutated: access_key_id = %q, want %q", stored.Config["access_key_id"], adminAK)
	}
}

// TestRegistryInvalidateEvicts verifies that Invalidate() removes cached instances.
func TestRegistryInvalidateEvicts(t *testing.T) {
	Register("test-mock-2", func(cfg Config) (Driver, error) {
		mockDriverCounter++
		return &mockDriver{id: mockDriverCounter}, nil
	})

	mockStore := newMockConnStore()

	conn := store.Connection{
		ID:     "test-conn",
		Label:  "test-label",
		Driver: "test-mock-2",
		Config: map[string]string{
			"key": "val",
		},
		Owner: "org",
	}

	if _, err := mockStore.Create(context.Background(), conn); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	reg := NewRegistry(mockStore)
	ctx := context.Background()

	drv1, err := reg.For(ctx, "test-conn")
	if err != nil {
		t.Fatalf("For first call failed: %v", err)
	}

	reg.Invalidate("test-conn")

	drv2, err := reg.For(ctx, "test-conn")
	if err != nil {
		t.Fatalf("For after Invalidate failed: %v", err)
	}

	// Compare IDs - they should be different if cache was invalidated
	m1 := drv1.(*mockDriver)
	m2 := drv2.(*mockDriver)

	if m1.id == m2.id {
		t.Error("expected different driver instances after Invalidate (cache was evicted)")
	}
}

func TestRegistryForUnknownConnection(t *testing.T) {
	mockStore := newMockConnStore()
	reg := NewRegistry(mockStore)
	ctx := context.Background()

	_, err := reg.For(ctx, "non-existent-id")

	if err == nil {
		t.Error("expected error for unknown connection ID")
	}
}

func TestBuildForUnknownDriver(t *testing.T) {
	conn := store.Connection{
		ID:     "test-conn",
		Label:  "unknown-driver-conn",
		Driver: "non-existent-driver",
		Config: map[string]string{},
		Owner:  "org",
	}

	_, err := BuildFor(conn)

	if err == nil {
		t.Error("expected error for unknown driver")
	}
}

// TestRegistryForBuildError covers the BuildFor-error branch in For(): the
// connection exists but its factory returns an error.
func TestRegistryForBuildError(t *testing.T) {
	Register("test-build-fail", func(_ Config) (Driver, error) {
		return nil, fmt.Errorf("factory exploded")
	})

	mockStore := newMockConnStore()
	conn := store.Connection{
		ID:     "explodes",
		Label:  "explodes",
		Driver: "test-build-fail",
		Config: map[string]string{},
		Owner:  "org",
	}
	if _, err := mockStore.Create(context.Background(), conn); err != nil {
		t.Fatalf("Create: %v", err)
	}

	reg := NewRegistry(mockStore)
	_, err := reg.For(context.Background(), "explodes")
	if err == nil {
		t.Fatal("expected error from factory failure")
	}
	if !strings.Contains(err.Error(), "building driver") {
		t.Errorf("error msg=%q, want 'building driver' prefix", err.Error())
	}

	// Build-fail must not have cached anything; verify by checking that a
	// second call also fails (vs returning a stale ok-cache entry).
	_, err2 := reg.For(context.Background(), "explodes")
	if err2 == nil {
		t.Fatal("expected error on second call too — failed build should not be cached")
	}
}

// TestRegistryForUnknownDriverThroughFor covers the BuildFor unknown-driver
// path via Registry.For (not just via direct BuildFor).
func TestRegistryForUnknownDriverThroughFor(t *testing.T) {
	mockStore := newMockConnStore()
	conn := store.Connection{
		ID:     "unknown-drv",
		Label:  "x",
		Driver: "no-such-driver-name",
		Config: map[string]string{},
		Owner:  "org",
	}
	if _, err := mockStore.Create(context.Background(), conn); err != nil {
		t.Fatalf("Create: %v", err)
	}

	reg := NewRegistry(mockStore)
	_, err := reg.For(context.Background(), "unknown-drv")
	if err == nil {
		t.Fatal("expected error for unknown driver via For()")
	}
	if !strings.Contains(err.Error(), "building driver") {
		t.Errorf("error msg=%q, want 'building driver' prefix", err.Error())
	}
}

// TestRegistryConcurrentForAndInvalidate exercises the mutex pairing under
// -race: many goroutines hammer For() while a second set repeatedly
// Invalidate()s the same connection. No data race or panic permitted.
func TestRegistryConcurrentForAndInvalidate(t *testing.T) {
	Register("test-concurrent", func(_ Config) (Driver, error) {
		return &mockDriver{}, nil
	})

	mockStore := newMockConnStore()
	conn := store.Connection{
		ID:     "shared",
		Label:  "shared",
		Driver: "test-concurrent",
		Config: map[string]string{},
		Owner:  "org",
	}
	if _, err := mockStore.Create(context.Background(), conn); err != nil {
		t.Fatalf("Create: %v", err)
	}

	reg := NewRegistry(mockStore)
	ctx := context.Background()

	var wg sync.WaitGroup
	const readers = 20
	const invalidators = 5
	const iters = 200

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				if _, err := reg.For(ctx, "shared"); err != nil {
					t.Errorf("For failed mid-loop: %v", err)
					return
				}
			}
		}()
	}

	for i := 0; i < invalidators; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				reg.Invalidate("shared")
			}
		}()
	}

	wg.Wait()
}

// TestRegistryInvalidateUnknownConnIsNoop ensures Invalidate doesn't panic
// or error when called with an unknown ID.
func TestRegistryInvalidateUnknownConnIsNoop(t *testing.T) {
	mockStore := newMockConnStore()
	reg := NewRegistry(mockStore)

	// No assertion needed — just no panic.
	reg.Invalidate("never-was-here")
	reg.Invalidate("")
}
