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
	frame    []byte // captured response frame; may be empty if none written
	failed   bool
	finished bool
	at       time.Time
}

// idemAction tells dispatchAsync how to treat a begin() result.
type idemAction int

const (
	idemExecute idemAction = iota
	idemReplay
	idemWait
)

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

// begin reports what the caller should do for this (device, request) pair.
//
//	idemReplay — return frame (may be nil/empty if prior success wrote nothing
//	  capturable; still do not re-execute).
//	idemWait   — wait(ctx) until the in-flight op finishes, then use its frame.
//	idemExecute — this caller owns the work; register an in-progress entry.
func (l *idempotencyLedger) begin(deviceID, requestID string) (frame []byte, wait func(context.Context) []byte, action idemAction) {
	if l == nil || deviceID == "" || requestID == "" {
		return nil, nil, idemExecute
	}
	key := idemKey(deviceID, requestID)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.purgeLocked()
	if e, ok := l.entries[key]; ok {
		if e.finished {
			if e.failed {
				// Allow a fresh execute after a failed attempt.
				return nil, nil, idemExecute
			}
			// Copy so the caller cannot mutate the map entry.
			var out []byte
			if e.frame != nil {
				out = append([]byte(nil), e.frame...)
			}
			return out, nil, idemReplay
		}
		done := e.done
		return nil, func(ctx context.Context) []byte {
			select {
			case <-done:
				l.mu.Lock()
				defer l.mu.Unlock()
				if e2, ok := l.entries[key]; ok && e2.finished && !e2.failed && e2.frame != nil {
					return append([]byte(nil), e2.frame...)
				}
				return nil
			case <-ctx.Done():
				return nil
			}
		}, idemWait
	}
	l.entries[key] = &idemEntry{
		done: make(chan struct{}),
		at:   time.Now(),
	}
	return nil, nil, idemExecute
}

// complete marks the request finished and stores the response frame for replay.
// A nil frame still marks success (skip re-execute); prefer capturing the real
// writeJSON bytes so retries get the same envelope.
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
		// Prefer the first captured non-empty frame (writeJSON may complete
		// before dispatchAsync's success path runs with nil).
		if len(e.frame) == 0 && len(frame) > 0 {
			e.frame = append([]byte(nil), frame...)
		}
		return
	}
	if frame != nil {
		e.frame = append([]byte(nil), frame...)
	}
	e.finished = true
	e.at = time.Now()
	close(e.done)
	l.purgeLocked()
}

// capture stores a response frame for an in-progress request without requiring
// the handler to call complete. Used from writeJSON so the real bytes are kept.
func (l *idempotencyLedger) capture(deviceID, requestID string, frame []byte) {
	if l == nil || deviceID == "" || requestID == "" || len(frame) == 0 {
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
		if len(e.frame) == 0 {
			e.frame = append([]byte(nil), frame...)
		}
		return
	}
	e.frame = append([]byte(nil), frame...)
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
		// Already completed with a captured frame (e.g. error envelope); keep it.
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
