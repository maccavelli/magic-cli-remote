package picker

import (
	"context"
	"sync"
	"time"
)

// DefaultCatalogTTL is how long a fetched catalog stays fresh. Model catalogs
// change when a user edits host-side credentials or the engine updates its
// model list — minutes-scale events — while a picker open costs a multi-MB
// engine fetch, so a short TTL removes almost all of that traffic without
// making a real change wait.
const DefaultCatalogTTL = 5 * time.Minute

type cacheEntry struct {
	cat     Catalog
	expires time.Time
	gen     int
}

// call is one in-flight fetch. Waiters block on done and then read cat/err,
// so a failed fetch fails everyone waiting on it instead of turning N waiters
// into N sequential retries against an engine that just said no.
type call struct {
	done chan struct{}
	cat  Catalog
	err  error
}

// Cache memoizes catalogs by key with a TTL and an engine generation.
//
// It also single-flights: two picker opens racing on the same key issue one
// fetch, not two. That matters more than the TTL for the pathological case —
// tapping the model row twice used to mean two 4 MB engine fetches.
//
// The zero value is not usable; call [NewCache]. A nil *Cache is usable and
// simply never caches, so a provider constructed without one still works.
type Cache[K comparable] struct {
	ttl time.Duration

	mu       sync.Mutex
	entries  map[K]*cacheEntry
	inflight map[K]*call
	// gen is the current engine generation. Bumping it invalidates every
	// entry and disowns any fetch already in flight.
	gen int
}

// NewCache creates a cache with the given TTL (<= 0 uses [DefaultCatalogTTL]).
func NewCache[K comparable](ttl time.Duration) *Cache[K] {
	if ttl <= 0 {
		ttl = DefaultCatalogTTL
	}
	return &Cache[K]{
		ttl:      ttl,
		entries:  make(map[K]*cacheEntry),
		inflight: make(map[K]*call),
	}
}

// Invalidate drops every cached entry. Call it when the engine restarts: a new
// engine may have a different catalog, and a stale one would name models the
// new process will refuse.
func (c *Cache[K]) Invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.gen++
	c.entries = make(map[K]*cacheEntry)
	c.mu.Unlock()
}

// Get returns the cached catalog for key, calling fetch when absent or stale.
// A fetch error is returned to every waiter and is not cached — a transient
// engine hiccup must not blank the picker for the whole TTL.
func (c *Cache[K]) Get(ctx context.Context, key K, fetch func(context.Context) (Catalog, error)) (Catalog, error) {
	if c == nil {
		return fetch(ctx)
	}
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && e.gen == c.gen && time.Now().Before(e.expires) {
		cat := e.cat
		c.mu.Unlock()
		return cat, nil
	}
	if cl, busy := c.inflight[key]; busy {
		c.mu.Unlock()
		select {
		case <-cl.done:
			return cl.cat, cl.err
		case <-ctx.Done():
			// This caller gave up; the leader keeps going for the others.
			return Catalog{}, ctx.Err()
		}
	}
	cl := &call{done: make(chan struct{})}
	c.inflight[key] = cl
	gen := c.gen
	c.mu.Unlock()

	cl.cat, cl.err = fetch(ctx)

	c.mu.Lock()
	if c.inflight[key] == cl {
		delete(c.inflight, key)
	}
	// Only store when the generation still matches: an engine restart during
	// the fetch means this result describes a process that is already gone.
	if cl.err == nil && gen == c.gen {
		c.entries[key] = &cacheEntry{cat: cl.cat, expires: time.Now().Add(c.ttl), gen: gen}
	}
	c.mu.Unlock()
	close(cl.done)
	return cl.cat, cl.err
}
