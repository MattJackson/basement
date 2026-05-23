// Package skin: Registry is the boot-time bag of registered Skin
// implementations. main.go registers basement-default once at boot;
// the API layer reads via All() for /api/v1/skins. v1.13.0b will add
// user-uploaded skins via a loader that walks {dataDir}/skins/.
//
// Mirrors the shape of internal/gateway.Registry and
// internal/driver.Registry — basement's three axes of pluggability
// follow the same contract so an operator (and a future contributor)
// reads one pattern, not three.

package skin

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ErrDuplicateSkin is returned by Register when a Skin with the same
// Name() is already registered. main.go fails loud on this — a
// duplicate registration is always a programming error.
var ErrDuplicateSkin = errors.New("skin: duplicate name")

// Registry holds the live set of Skin implementations. Safe for
// concurrent Get / All calls; Register is typically called once at
// boot but is mutex-protected anyway for safety in tests that register
// from multiple goroutines.
type Registry struct {
	mu    sync.RWMutex
	skins map[string]Skin
}

// New returns an empty Registry. main.go calls this once at boot and
// proceeds to Register basement-default; v1.13.0b will iterate the
// {dataDir}/skins/ directory after that.
func New() *Registry {
	return &Registry{
		skins: make(map[string]Skin),
	}
}

// Register adds a Skin to the registry. Returns ErrDuplicateSkin if a
// Skin with the same Name is already registered, or an error when the
// Name is empty.
//
// We accept Skin by value rather than by pointer: skins are immutable
// data once registered, and forcing a copy at the registration
// boundary stops callers from later mutating the value behind the
// registry's back.
func (r *Registry) Register(s Skin) error {
	if s.Name == "" {
		return fmt.Errorf("skin: Register: empty Name")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.skins[s.Name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateSkin, s.Name)
	}
	r.skins[s.Name] = s
	return nil
}

// Get returns the Skin with the given name, or (zero, false) if none
// is registered.
//
// Callers that need to render a deploy's "active" skin should
// fall back to BuiltInDefaultName when Get returns false — an
// org_capabilities ActiveSkin can refer to a skin that's been
// uninstalled.
func (r *Registry) Get(name string) (Skin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skins[name]
	return s, ok
}

// All returns every registered Skin, sorted alphabetically by Name.
// The deterministic order matters for the skin selector — the FE
// renders a list and a stable order keeps the UI from shuffling
// between requests.
func (r *Registry) All() []Skin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Skin, 0, len(r.skins))
	for _, s := range r.skins {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}
