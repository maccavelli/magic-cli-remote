package provider

import (
	"context"
	"errors"
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

// Info is a public listing of a provider. Auth is nil unless the listing was
// built by [Registry.ListWithAuth] and the provider implements [Auth].
type Info struct {
	ID    ID   `json:"id"`
	Ready bool `json:"ready"`
	Auth  *AuthState
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

// ListWithAuth is List plus each provider's credential state (MADR 0074 D3).
//
// Kept separate from List rather than folded into it for two reasons: List has
// no context, and an auth probe can do network I/O (kilo queries its engine),
// so it must be cancellable; and a client that never negotiated the
// provider_auth capability must not make the daemon do this work at all (D6).
//
// Probes run concurrently — serially, five providers at the 15s per-probe
// budget would be a 75s worst case on a listing the phone blocks on. A probe
// that fails contributes a degraded AuthError entry instead of failing the
// listing: a provider screen missing one chip beats a provider screen that
// never loads.
func (r *Registry) ListWithAuth(ctx context.Context) []Info {
	r.mu.RLock()
	provs := make([]Provider, 0, len(r.byID))
	for _, p := range r.byID {
		provs = append(provs, p)
	}
	r.mu.RUnlock()

	out := make([]Info, len(provs))
	var wg sync.WaitGroup
	for i, p := range provs {
		out[i] = Info{ID: p.ID(), Ready: p.Ready()}
		auth, ok := p.(Auth)
		if !ok {
			continue
		}
		wg.Add(1)
		go func(i int, auth Auth) {
			defer wg.Done()
			st, err := auth.AuthStatus(ctx)
			if err != nil {
				if errors.Is(err, ErrAuthUnsupported) {
					// Not a failure: this provider simply has nothing to say.
					return
				}
				out[i].Auth = &AuthState{Status: AuthError}
				return
			}
			out[i].Auth = &st
		}(i, auth)
	}
	wg.Wait()
	return out
}
