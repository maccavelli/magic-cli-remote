package ws

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// resumeStore holds the per-device resume tokens (MADR 0068 D4). Memory
// only, by design: a daemon restart invalidates every token, and the epoch
// path (P3) covers that case with a full resync. One token per device,
// rotated on every v2 auth — an old connection's token dies the moment a
// newer login of the same device succeeds, matching D3's one-live-socket
// rule.
type resumeStore struct {
	mu       sync.Mutex
	byDevice map[string]resumeEntry
	window   time.Duration
	// now is a test seam.
	now func() time.Time
}

type resumeEntry struct {
	token   string
	expires time.Time
}

func newResumeStore(window time.Duration) *resumeStore {
	if window <= 0 {
		window = 120 * time.Second // MADR 0068 Q1 default
	}
	return &resumeStore{
		byDevice: make(map[string]resumeEntry),
		window:   window,
		now:      time.Now,
	}
}

// issue mints and stores a fresh token for the device, replacing any prior
// one. requested optionally narrows the validity window (never widens —
// the server default is the ceiling, MADR 0068 Q1). Returns the token and
// the granted window.
func (r *resumeStore) issue(deviceID string, requested time.Duration) (string, time.Duration) {
	window := r.window
	if requested > 0 && requested < window {
		window = requested
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// No token beats a predictable token.
		return "", window
	}
	token := hex.EncodeToString(b[:])
	r.mu.Lock()
	r.byDevice[deviceID] = resumeEntry{token: token, expires: r.now().Add(window)}
	r.mu.Unlock()
	return token, window
}

// validate reports whether token is the device's current, unexpired resume
// token. It does not consume — the caller's subsequent issue() rotates it.
func (r *resumeStore) validate(deviceID, token string) bool {
	if token == "" {
		return false
	}
	r.mu.Lock()
	e, ok := r.byDevice[deviceID]
	r.mu.Unlock()
	return ok && e.token == token && r.now().Before(e.expires)
}
