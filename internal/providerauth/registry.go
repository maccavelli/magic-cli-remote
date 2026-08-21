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
	// reservations are owned flows (MADR 0074 D27). They coexist with the
	// legacy flows map so providers that have not adopted the owned contract
	// keep working during rollout.
	reservations map[string]*Reservation
	// pending holds bounded terminal records for devices that disconnected
	// before their flow ended.
	pending map[string][]TerminalResult
	// now is swappable so expiry is testable without sleeping.
	now func() time.Time
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		flows:        make(map[string]*Flow),
		reservations: make(map[string]*Reservation),
		pending:      make(map[string][]TerminalResult),
		now:          time.Now,
	}
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
	return len(r.flows) + len(r.reservations)
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

// --- Owned reservations (MADR 0074 D27, P20 steps 1-7) ---

// Reserve claims admission for one device flow before any provider side effect
// exists. The returned reservation owns its slot until Cancel, or until an
// attached handle's terminal cleanup completes.
func (r *Registry) Reserve(_ context.Context, deviceID, providerID, upstreamID string) (*Reservation, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("provider auth: flow needs an owning device")
	}
	r.mu.Lock()
	r.sweepLocked()
	if len(r.flows)+len(r.reservations) >= MaxFlowsTotal {
		r.mu.Unlock()
		return nil, ErrTooManyFlows
	}
	perDevice := 0
	for _, e := range r.flows {
		if e.DeviceID == deviceID {
			perDevice++
		}
	}
	for _, e := range r.reservations {
		if e.deviceID == deviceID {
			perDevice++
		}
	}
	if perDevice >= MaxFlowsPerDevice {
		r.mu.Unlock()
		return nil, ErrTooManyFlows
	}

	res := &Reservation{
		id:         uuid.NewString(),
		deviceID:   deviceID,
		providerID: providerID,
		upstreamID: upstreamID,
		reg:        r,
		expiresAt:  r.now().Add(DefaultTTL),
		updates:    make(chan string, 1),
		done:       make(chan struct{}),
	}
	if r.reservations == nil {
		r.reservations = make(map[string]*Reservation)
	}
	r.reservations[res.id] = res
	r.mu.Unlock()
	return res, nil
}

// Reservation returns an owned flow if it belongs to deviceID.
func (r *Registry) Reservation(id, deviceID string) (*Reservation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	res, ok := r.reservations[id]
	if !ok || res.deviceID != deviceID {
		return nil, ErrFlowNotFound
	}
	return res, nil
}

func (r *Registry) remove(id string) {
	r.mu.Lock()
	delete(r.reservations, id)
	r.mu.Unlock()
}

// retain stores a bounded terminal record for a device that was not connected
// when its flow ended.
func (r *Registry) retain(deviceID string, res TerminalResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == nil {
		r.pending = make(map[string][]TerminalResult)
	}
	q := r.pending[deviceID]
	if len(q) >= MaxFlowsPerDevice {
		q = q[1:]
	}
	r.pending[deviceID] = append(q, res)
}

// PendingResults drains the terminal records retained for a device. Draining is
// once-only so a reconnect loop cannot replay an outcome forever.
func (r *Registry) PendingResults(deviceID string) []TerminalResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.pending[deviceID]
	delete(r.pending, deviceID)
	return out
}

// DetachDevice marks a device's owned flows as disconnected and arms the resume
// window. A transient disconnect must not kill a login the user is still
// completing on another screen (MADR 0074 D28).
func (r *Registry) DetachDevice(deviceID string, window time.Duration) int {
	r.mu.Lock()
	victims := make([]*Reservation, 0, 2)
	for _, res := range r.reservations {
		if res.deviceID == deviceID {
			victims = append(victims, res)
		}
	}
	r.mu.Unlock()

	for _, res := range victims {
		res.detach(window)
	}
	return len(victims)
}

// ResumeDevice reattaches a device's detached flows after a successful resume.
func (r *Registry) ResumeDevice(deviceID string) []*Reservation {
	r.mu.Lock()
	out := make([]*Reservation, 0, 2)
	for _, res := range r.reservations {
		if res.deviceID == deviceID {
			out = append(out, res)
		}
	}
	r.mu.Unlock()
	for _, res := range out {
		res.reattach()
	}
	return out
}

// CancelAll cancels every owned flow. Shutdown calls this before process exit
// destroys the in-memory ownership that is the only record of how to clean up
// (MADR 0074 F4).
func (r *Registry) CancelAll() {
	r.mu.Lock()
	victims := make([]*Reservation, 0, len(r.reservations))
	for _, res := range r.reservations {
		victims = append(victims, res)
	}
	legacy := make([]*Flow, 0, len(r.flows))
	for id, f := range r.flows {
		legacy = append(legacy, f)
		delete(r.flows, id)
	}
	r.mu.Unlock()

	for _, f := range legacy {
		f.finish()
	}
	for _, res := range victims {
		res.Cancel()
	}
}

// WaitAll blocks until every owned flow has completed its terminal cleanup, or
// the bound expires. On expiry it reports how much ownership is retained and
// preserves all state rather than forcing.
func (r *Registry) WaitAll(ctx context.Context, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		r.mu.Lock()
		remaining := len(r.reservations)
		r.mu.Unlock()
		if remaining == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("provider auth: %d device flow(s) still owned after %s", remaining, timeout)
		case <-time.After(20 * time.Millisecond):
		}
	}
}
