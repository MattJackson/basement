package driver

import (
	"context"
	"fmt"
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

