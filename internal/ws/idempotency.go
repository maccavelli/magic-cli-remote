package ws

import (
	"context"
	"sync"
	"time"
)

// idempotencyLedger stores in-flight and completed mutating request results
// keyed by (deviceID, requestID) so a client retry after timeout/reconnect
// does not re-execute the mutation (MADR 0056 H-2b).
type idempotencyLedger struct {
	mu      sync.Mutex
	entries map[string]*idemEntry
	// maxEntries bounds memory; oldest completed entries are dropped first.
	maxEntries int
	ttl        time.Duration
}

type idemEntry struct {
	done     chan struct{}
	frame    []byte // optional captured response; nil means "completed, no frame"
	failed   bool
	finished bool
	at       time.Time
}

func newIdempotencyLedger() *idempotencyLedger {
	return &idempotencyLedger{
		entries:    make(map[string]*idemEntry),
		maxEntries: 256,
		ttl:        10 * time.Minute,
	}
}

func idemKey(deviceID, requestID string) string {
	return deviceID + "\x00" + requestID
}

// begin returns (cachedFrame, nil) if a completed result exists,
// (nil, waitFn) if in progress, or (nil, nil) if this caller should execute.
func (l *idempotencyLedger) begin(deviceID, requestID string) (cached []byte, wait func(context.Context) []byte) {
	if l == nil || deviceID == "" || requestID == "" {
		return nil, nil
	}
	key := idemKey(deviceID, requestID)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.purgeLocked()
	if e, ok := l.entries[key]; ok {
		if e.finished {
			if e.failed {
				return nil, nil // allow retry after failure
			}
			return e.frame, nil
		}
		// In progress: wait for completion.
		done := e.done
		return nil, func(ctx context.Context) []byte {
			select {
			case <-done:
				l.mu.Lock()
				defer l.mu.Unlock()
				if e2, ok := l.entries[key]; ok && e2.finished && !e2.failed {
					return e2.frame
				}
				return nil
			case <-ctx.Done():
				return nil
			}
		}
	}
	l.entries[key] = &idemEntry{
		done: make(chan struct{}),
		at:   time.Now(),
	}
	return nil, nil
}

func (l *idempotencyLedger) complete(deviceID, requestID string, frame []byte) {
	if l == nil || deviceID == "" || requestID == "" {
		return
	}
	key := idemKey(deviceID, requestID)
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		e = &idemEntry{done: make(chan struct{})}
		l.entries[key] = e
	}
	if e.finished {
		return
	}
	e.frame = frame
	e.finished = true
	e.at = time.Now()
	close(e.done)
	l.purgeLocked()
}

func (l *idempotencyLedger) fail(deviceID, requestID string) {
	if l == nil || deviceID == "" || requestID == "" {
		return
	}
	key := idemKey(deviceID, requestID)
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[key]
	if !ok {
		return
	}
	if e.finished {
		return
	}
	e.failed = true
	e.finished = true
	e.at = time.Now()
	close(e.done)
	// Drop failed keys so a retry can re-execute.
	delete(l.entries, key)
}

func (l *idempotencyLedger) purgeLocked() {
	now := time.Now()
	for k, e := range l.entries {
		if e.finished && now.Sub(e.at) > l.ttl {
			delete(l.entries, k)
		}
	}
	for len(l.entries) > l.maxEntries {
		// Drop an arbitrary finished entry.
		for k, e := range l.entries {
			if e.finished {
				delete(l.entries, k)
				break
			}
			// If nothing finished, stop to avoid deleting in-flight work.
			return
		}
	}
}
