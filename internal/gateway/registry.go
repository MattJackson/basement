// Package gateway: Registry is the boot-time bag of Gateway
// implementations. main.go registers WebDAV + the four stubs once;
// the API server reads the registry to render /admin/gateways; the
// HTTP routes mount each gateway's HTTPHandler under its protocol-
// specific path.
//
// Mirrors the shape of internal/driver.Registry (the other axis of
// pluggability in basement). Both live separately because their
// lifecycles diverge: a driver is built per-connection at runtime; a
// gateway is built once at boot and never re-registered.

package gateway

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ErrDuplicateGateway is returned by Register when a Gateway with the
// same Name() is already registered. main.go fails loud on this — a
// duplicate registration is always a programming error.
var ErrDuplicateGateway = errors.New("gateway: duplicate Name()")

// Registry holds the live set of Gateway implementations. Safe for
// concurrent Get / All / Enabled calls; Register / StartAll / StopAll
// are typically called once at boot but are mutex-protected anyway
// for safety in tests that register from multiple goroutines.
type Registry struct {
	mu       sync.RWMutex
	gateways map[string]Gateway
}

// New returns an empty Registry. main.go calls this once at boot and
// proceeds to Register each gateway.
func New() *Registry {
	return &Registry{
		gateways: make(map[string]Gateway),
	}
}

// Register adds a Gateway to the registry. Returns ErrDuplicateGateway
// if a Gateway with the same Name() is already registered.
func (r *Registry) Register(g Gateway) error {
	if g == nil {
		return fmt.Errorf("gateway: Register(nil)")
	}
	name := g.Name()
	if name == "" {
		return fmt.Errorf("gateway: Register: empty Name()")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.gateways[name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateGateway, name)
	}
	r.gateways[name] = g
	return nil
}

// Get returns the Gateway with the given name, or (nil, false) if
// none is registered. Callers that need the gateway at request time
// (e.g. the /admin/gateways API) walk the All() list instead.
func (r *Registry) Get(name string) (Gateway, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.gateways[name]
	return g, ok
}

// All returns every registered Gateway, sorted alphabetically by
// Name(). The deterministic order matters for /admin/gateways — the
// FE renders cards in registry order and a stable order keeps the UI
// from shuffling between requests.
func (r *Registry) All() []Gateway {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Gateway, 0, len(r.gateways))
	for _, g := range r.gateways {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name() < out[j].Name()
	})
	return out
}

// Enabled returns the subset of registered gateways that the operator
// has flipped on in the org-capabilities Gateways nest. Only
// Implemented() gateways are considered — a stub gateway never counts
// as enabled regardless of what the config says.
//
// caps is the org-level Gateways nest from store.OrgCapabilities. The
// caller passes it through so this package doesn't take a dep on
// internal/store (preserving the "gateway package only knows about
// Backend" boundary).
func (r *Registry) Enabled(caps OrgCaps) []Gateway {
	all := r.All()
	out := make([]Gateway, 0, len(all))
	for _, g := range all {
		if !g.Implemented() {
			continue
		}
		if caps.IsEnabled(g.Name()) {
			out = append(out, g)
		}
	}
	return out
}

// StartAll calls Start on every registered Gateway. Returns the first
// error encountered (but continues starting the rest — operator might
// have one broken protocol but still wants the others up). main.go
// logs Warn for each failure so the operator sees them all.
func (r *Registry) StartAll(ctx context.Context) error {
	all := r.All()
	var firstErr error
	for _, g := range all {
		if !g.Implemented() {
			// Stub gateways have no-op Starts; call them anyway so
			// any tracing inside Start fires uniformly. The contract
			// says stub Start returns nil.
			_ = g.Start(ctx)
			continue
		}
		if err := g.Start(ctx); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("gateway %s: %w", g.Name(), err)
			}
		}
	}
	return firstErr
}

// StopAll calls Stop on every registered Gateway. Best-effort: errors
// are collected into a wrapped error so the operator sees every
// failed protocol on shutdown.
func (r *Registry) StopAll(ctx context.Context) error {
	all := r.All()
	var errs []error
	for _, g := range all {
		if err := g.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("gateway %s: %w", g.Name(), err))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// OrgCaps is the narrow view the registry needs of the org-level
// gateway capability map. Production wires a small adapter around
// store.OrgCapabilities; tests pass a static stub.
type OrgCaps interface {
	IsEnabled(gatewayName string) bool
}

// orgCapsFunc is a convenience adapter that lets a plain
// `func(name string) bool` satisfy OrgCaps. Useful in tests and in
// main.go where the bridge to store.OrgCapabilities is a one-liner.
type orgCapsFunc func(name string) bool

func (f orgCapsFunc) IsEnabled(name string) bool { return f(name) }

// OrgCapsFunc wraps a plain func into the OrgCaps interface.
func OrgCapsFunc(f func(name string) bool) OrgCaps { return orgCapsFunc(f) }
