package ws

import (
	"crypto/rand"
	"crypto/subtle"
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
	// readRand is a test seam (default crypto/rand.Read).
	readRand func([]byte) (int, error)
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
		readRand: rand.Read,
	}
}

// issue mints and stores a fresh token for the device, replacing any prior
// one. requested optionally narrows the validity window (never widens —
// the server default is the ceiling, MADR 0068 Q1).
//
// ok is false when the token could not be minted (RNG failure): the caller
// must omit resume_token and caps.resume rather than send an empty string
// (0070 F3). Auth itself still succeeds — resume failure is not an auth
// failure (0068).
func (r *resumeStore) issue(deviceID string, requested time.Duration) (token string, window time.Duration, ok bool) {
	window = r.window
	if requested > 0 && requested < window {
		window = requested
	}
	read := r.readRand
	if read == nil {
		read = rand.Read
	}
	var b [16]byte
	if _, err := read(b[:]); err != nil {
		// No token beats a predictable token; do not store an entry.
		return "", window, false
	}
	token = hex.EncodeToString(b[:])
	r.mu.Lock()
	r.purgeExpiredLocked()
	r.byDevice[deviceID] = resumeEntry{token: token, expires: r.now().Add(window)}
	r.mu.Unlock()
	return token, window, true
}

// validate reports whether token is the device's current, unexpired resume
// token. It does not consume — the caller's subsequent issue() rotates it.
func (r *resumeStore) validate(deviceID, token string) bool {
	if token == "" {
		return false
	}
	r.mu.Lock()
	r.purgeExpiredLocked()
	e, ok := r.byDevice[deviceID]
	r.mu.Unlock()
	// Constant-time, matching every other secret comparison in the tree
	// (auth/store.go, relay/hub.go). Not reachable pre-auth — resume
	// validation runs only after the 256-bit device token authenticated, and
	// a miss costs a full reconcile rather than access — but the house rule
	// should not have one exception (MADR 0095 F11).
	return ok &&
		subtle.ConstantTimeCompare([]byte(e.token), []byte(token)) == 1 &&
		r.now().Before(e.expires)
}

// purgeExpiredLocked drops expired resume entries (0071 F4). Caller holds r.mu.
func (r *resumeStore) purgeExpiredLocked() {
	now := r.now()
	for id, e := range r.byDevice {
		if !now.Before(e.expires) {
			delete(r.byDevice, id)
		}
	}
}
