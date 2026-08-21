package providerauth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Flow limits. Both are low on purpose: a device flow is a deliberate,
// attended action, so more than a couple in flight per device means something
// is looping rather than a user signing in.
const (
	MaxFlowsPerDevice = 2
	MaxFlowsTotal     = 8
	// DefaultTTL bounds a flow when the provider states no expiry. Codex's own
	// device codes expire in 15 minutes; nothing should outlive that.
	DefaultTTL = 15 * time.Minute
)

var (
	// ErrFlowNotFound covers both an unknown id and one belonging to another
	// device — the caller must not be able to tell those apart, or flow ids
	// become an enumeration oracle.
	ErrFlowNotFound = errors.New("provider auth: no such flow")
	// ErrTooManyFlows means the per-device or global cap is reached.
	ErrTooManyFlows = errors.New("provider auth: too many flows in progress")
	// ErrFlowCancelled is the terminal result of a flow that mcremote killed
	// rather than one the provider finished. Every Wait observer sees it once
	// Kill has initiated termination (MADR 0074 D27).
	ErrFlowCancelled = errors.New("provider auth: device sign-in cancelled")
)

// Flow is one in-progress device authorization.
type Flow struct {
	ID              string
	ProviderID      string
	UpstreamID      string
	DeviceID        string
	VerificationURI string
	UserCode        string
	ExpiresAt       time.Time
	Interval        time.Duration

	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// ExpiresIn is the whole seconds left, floored at zero.
func (f *Flow) ExpiresIn() int {
	d := time.Until(f.ExpiresAt)
	if d <= 0 {
		return 0
	}
	return int(d.Seconds())
}

// Registry tracks live flows so they can be cancelled, expired, and scoped to
// the device that started them.
type Registry struct {
	mu    sync.Mutex
	flows map[string]*Flow
	// now is swappable so expiry is testable without sleeping.
	now func() time.Time
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{flows: make(map[string]*Flow), now: time.Now}
}

// Add registers a started flow and returns it with an id assigned.
//
// The context returned is cancelled when the flow is cancelled or expires; the
// caller's polling loop must respect it, or a cancelled flow would keep
// hitting the upstream until its code died on its own.
func (r *Registry) Add(parent context.Context, f *Flow) (*Flow, context.Context, error) {
	if f.DeviceID == "" {
		return nil, nil, fmt.Errorf("provider auth: flow needs an owning device")
	}
	r.mu.Lock()
	r.sweepLocked()
	if len(r.flows) >= MaxFlowsTotal {
		r.mu.Unlock()
		return nil, nil, ErrTooManyFlows
	}
	perDevice := 0
	for _, e := range r.flows {
		if e.DeviceID == f.DeviceID {
			perDevice++
		}
	}
	if perDevice >= MaxFlowsPerDevice {
		r.mu.Unlock()
		return nil, nil, ErrTooManyFlows
	}

	if f.ExpiresAt.IsZero() {
		f.ExpiresAt = r.now().Add(DefaultTTL)
	} else if max := r.now().Add(DefaultTTL); f.ExpiresAt.After(max) {
		// Never trust a provider-supplied expiry beyond our own ceiling.
		f.ExpiresAt = max
	}
	ctx, cancel := context.WithDeadline(parent, f.ExpiresAt)
	f.ID = uuid.NewString()
	f.cancel = cancel
	f.done = make(chan struct{})
	r.flows[f.ID] = f
	r.mu.Unlock()
	return f, ctx, nil
}

// Get returns a flow if it belongs to deviceID and has not finished.
func (r *Registry) Get(id, deviceID string) (*Flow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.flows[id]
	if !ok || f.DeviceID != deviceID {
		return nil, ErrFlowNotFound
	}
	return f, nil
}

// Cancel ends a flow started by deviceID. Cancelling twice is not an error —
// a phone that retries a cancel after a reconnect should not see a failure.
func (r *Registry) Cancel(id, deviceID string) error {
	r.mu.Lock()
	f, ok := r.flows[id]
	if !ok || f.DeviceID != deviceID {
		r.mu.Unlock()
		return ErrFlowNotFound
	}
	delete(r.flows, id)
	r.mu.Unlock()
	f.finish()
	return nil
}

// Finish removes a completed flow. Called by the driver once its wait returns.
func (r *Registry) Finish(id string) {
	r.mu.Lock()
	f, ok := r.flows[id]
	delete(r.flows, id)
	r.mu.Unlock()
	if ok {
		f.finish()
	}
}

// CancelDevice ends every flow owned by a device — used when it disconnects,
// so an abandoned flow does not keep polling on behalf of nobody.
func (r *Registry) CancelDevice(deviceID string) int {
	r.mu.Lock()
	victims := make([]*Flow, 0, 2)
	for id, f := range r.flows {
		if f.DeviceID == deviceID {
			victims = append(victims, f)
			delete(r.flows, id)
		}
	}
	r.mu.Unlock()
	for _, f := range victims {
		f.finish()
	}
	return len(victims)
}

// Len reports the number of live flows (test and diagnostics aid).
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sweepLocked()
	return len(r.flows)
}

// sweepLocked drops expired flows. Caller holds r.mu.
func (r *Registry) sweepLocked() {
	now := r.now()
	for id, f := range r.flows {
		if now.After(f.ExpiresAt) {
			delete(r.flows, id)
			f.finish()
		}
	}
}

// finish cancels the flow's context exactly once.
func (f *Flow) finish() {
	f.once.Do(func() {
		if f.cancel != nil {
			f.cancel()
		}
		if f.done != nil {
			close(f.done)
		}
	})
}

// Done is closed when the flow ends, however it ends.
func (f *Flow) Done() <-chan struct{} { return f.done }
