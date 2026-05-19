package driver

import (
	"fmt"
	"sort"
	"sync"
)

// Config is opaque from the registry's perspective — each driver knows what
// to pull from it. For T2.03, treated as map[string]string to avoid circular
// imports with internal/config.
type Config = map[string]string

var (
	mu        sync.RWMutex
	factories = make(map[string]Factory)
)

// Factory creates a new driver instance.
type Factory func(cfg Config) (Driver, error)

// Builder is the per-driver-type constructor; takes a connection's flat config
// map and returns a Driver. Used by Registry.BuildFor to build instances from
// Connection records.
type Builder func(cfg Config) (Driver, error)

// Register adds a driver factory. Panics if name is empty or already registered
// (called from init() — programmer error).
func Register(name string, f Factory) {
	mu.Lock()
	defer mu.Unlock()

	if name == "" {
		panic("driver: register requires non-empty name")
	}

	if _, exists := factories[name]; exists {
		panic(fmt.Sprintf("driver: duplicate registration for %q", name))
	}

	factories[name] = f
}

// Open instantiates a registered driver by name. Returns error if not found.
func Open(name string, cfg Config) (Driver, error) {
	mu.RLock()
	defer mu.RUnlock()

	f, exists := factories[name]
	if !exists {
		return nil, fmt.Errorf("driver: unknown driver %q", name)
	}

	return f(cfg)
}

// Registered returns the sorted list of registered driver names.
func Registered() []string {
	mu.RLock()
	defer mu.RUnlock()

	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
