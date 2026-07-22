package provider

import (
	"fmt"
	"sync"
)

// Registry maps provider IDs to implementations.
type Registry struct {
	mu   sync.RWMutex
	byID map[ID]Provider
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byID: make(map[ID]Provider)}
}

// Register adds a provider. Overwrites same ID.
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[p.ID()] = p
}

// Get returns a provider by ID.
func (r *Registry) Get(id ID) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byID[id]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", id)
	}
	return p, nil
}

// All returns the registered providers (for lifecycle hooks like shutdown).
func (r *Registry) All() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Provider, 0, len(r.byID))
	for _, p := range r.byID {
		out = append(out, p)
	}
	return out
}

// Info is a public listing of a provider.
type Info struct {
	ID    ID   `json:"id"`
	Ready bool `json:"ready"`
}

// List returns registered providers. Ready probes (PATH lookups — filesystem
// I/O) run on a snapshot, outside the lock.
func (r *Registry) List() []Info {
	r.mu.RLock()
	provs := make([]Provider, 0, len(r.byID))
	for _, p := range r.byID {
		provs = append(provs, p)
	}
	r.mu.RUnlock()

	out := make([]Info, 0, len(provs))
	for _, p := range provs {
		out = append(out, Info{ID: p.ID(), Ready: p.Ready()})
	}
	return out
}
