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

// Info is a public listing of a provider.
type Info struct {
	ID    ID   `json:"id"`
	Ready bool `json:"ready"`
}

// List returns registered providers.
func (r *Registry) List() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Info, 0, len(r.byID))
	for id, p := range r.byID {
		out = append(out, Info{ID: id, Ready: p.Ready()})
	}
	return out
}
