package providerauth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Terminal result states for an owned device flow (MADR 0074 P20 step 9).
// They are additive: an older negotiated client keeps reading the boolean OK
// field and never sees these.
const (
	ResultCompleted          = "completed"
	ResultCancelled          = "cancelled"
	ResultExpired            = "expired"
	ResultFailed             = "failed"
	ResultConflict           = "conflict"
	ResultReadyToActivate    = "ready_to_activate"
	ResultRecoveryRequired   = "recovery_required"
	ResultUnsupportedBackend = "unsupported_backend"
)

// Retryable reports whether a client may sensibly start the same flow again.
// ready_to_activate is deliberately excluded: it is not terminal and needs no
// retry, only patience.
func Retryable(state string) bool {
	switch state {
	case ResultCancelled, ResultExpired, ResultConflict, ResultFailed:
		return true
	}
	return false
}

// OwnedHandle is the subset of provider.DeviceAuthHandle this package needs.
// It is redeclared rather than imported because provider depends on this
// package, and an owned flow's mechanics belong here.
type OwnedHandle interface {
	Wait(ctx context.Context) error
	Cancel()
}

// updateSource is optionally implemented by a handle publishing non-terminal
// state changes.
type updateSource interface {
	Updates() <-chan string
}

// TerminalResult is the bounded record retained for a device that was not
// connected when its flow ended (MADR 0074 P20 step 7).
//
// It stores no child output, device code, credential metadata, or error detail
// beyond a classified state: a result waiting in memory for a reconnect is
// exactly the wrong place for any of that.
type TerminalResult struct {
	FlowID     string
	ProviderID string
	UpstreamID string
	State      string
	Retryable  bool
	// UserCode is always empty. It exists so a test can assert the code is not
	// retained; nothing ever sets it.
	UserCode string
}

// Reservation is admission plus ownership of one device flow.
//
// Reserving is separated from starting the provider because the old server
// started the child first and decided admission afterwards, which orphaned the
// process whenever admission failed (MADR 0074 F3). A reservation that never
// attaches owns no child and releases its slot; one that does attach owns the
// handle until terminal cleanup completes.
type Reservation struct {
	id         string
	deviceID   string
	providerID string
	upstreamID string
	reg        *Registry

	expiresAt time.Time

	mu       sync.Mutex
	handle   OwnedHandle
	flow     DeviceFlowInfo
	detached bool
	// timer is the resume window's: detach replaces it and reattach clears
	// it. The code's deadline must not live here, or any disconnect and
	// resume would lose it (MADR 0074 D33).
	timer *time.Timer
	// deadline ends the flow when the provider's code expires, or, once the
	// flow is ready_to_activate, when ActivationGrace runs out. Only Attach,
	// armGrace and finish touch it.
	deadline *time.Timer
	// expired is set by the deadline before it cancels, so the terminal state
	// reads expired and not cancelled.
	expired bool
	// updatesClosed is set, under mu, when finish closes updates, so forward
	// never sends on a closed channel.
	updatesClosed bool

	updates chan string

	done       chan struct{}
	finishOnce sync.Once
	cancelOnce sync.Once
	result     error
	state      string
}

// ID is the flow id clients use.
func (r *Reservation) ID() string { return r.id }

// DeviceID is the owning device.
func (r *Reservation) DeviceID() string { return r.deviceID }

// Flow is the display payload, valid after Attach.
func (r *Reservation) Flow() DeviceFlowInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.flow
}

// ExpiresIn is the whole seconds left, floored at zero. After Attach it is the
// code's own lifetime, capped at the reservation's DefaultTTL (MADR 0074 D33).
func (r *Reservation) ExpiresIn() int {
	r.mu.Lock()
	exp := r.expiresAt
	r.mu.Unlock()
	d := time.Until(exp)
	if d <= 0 {
		return 0
	}
	return int(d.Seconds())
}

// Done closes when the flow has reached a terminal result and its cleanup has
// completed.
func (r *Reservation) Done() <-chan struct{} { return r.done }

// Updates forwards the handle's non-terminal states. It closes with the flow.
func (r *Reservation) Updates() <-chan string { return r.updates }

// Result is the single terminal error, valid once Done is closed.
func (r *Reservation) Result() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.result
}

// State is the classified terminal state, valid once Done is closed.
func (r *Reservation) State() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

// Attach binds exactly one handle and starts the owner goroutine.
//
// A second attach fails and cancels the handle it was given, so a caller that
// races cannot leak a started child (MADR 0074 P20 step 1).
func (r *Reservation) Attach(h OwnedHandle, flow DeviceFlowInfo) error {
	if h == nil {
		return fmt.Errorf("provider auth: attach needs a handle")
	}
	r.mu.Lock()
	if r.handle != nil {
		r.mu.Unlock()
		h.Cancel()
		return fmt.Errorf("provider auth: flow already has a handle")
	}
	r.handle = h
	r.flow = flow
	// The deadline is the code's lifetime from now, never past the
	// reservation's own cap.
	d := r.expiresAt.Sub(r.reg.now())
	if flow.ExpiresIn > 0 {
		if life := time.Duration(flow.ExpiresIn) * time.Second; life < d {
			d = life
			r.expiresAt = r.expiresAt.Add(life - r.expiresAt.Sub(r.reg.now()))
		}
	}
	r.deadline = time.AfterFunc(d, r.expire)
	r.mu.Unlock()

	go r.own(h)
	return nil
}

// expire is the deadline's callback: the code ran out, or the activation
// grace did. It marks the flow expired and cancels it through the handle, so
// the child dies and the transaction aborts on the one cleanup path.
func (r *Reservation) expire() {
	r.mu.Lock()
	r.expired = true
	r.mu.Unlock()
	r.Cancel()
}

// armGrace replaces the code's deadline once the flow is ready_to_activate.
// The exchange succeeded and the candidate is validated, so the code's expiry
// no longer applies; ActivationGrace bounds the wait for an idle provider
// instead (MADR 0074 D33).
func (r *Reservation) armGrace() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deadline == nil {
		return
	}
	r.deadline.Stop()
	r.deadline = time.AfterFunc(ActivationGrace, r.expire)
}

// own is the reservation's single owner goroutine. It forwards updates, calls
// Wait exactly once, converts a panic into a failed terminal result, and stays
// responsible until cleanup completes (MADR 0074 P20 step 2).
func (r *Reservation) own(h OwnedHandle) {
	if src, ok := h.(updateSource); ok {
		go r.forward(src.Updates())
	}

	var err error
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				// A panicking provider handle must not take the daemon with
				// it, and must not strand this flow's admission slot.
				err = fmt.Errorf("provider auth: device flow panicked: %v", rec)
			}
		}()
		err = h.Wait(context.Background())
	}()

	r.finish(err)
}

func (r *Reservation) forward(in <-chan string) {
	for s := range in {
		if s == ResultReadyToActivate {
			r.armGrace()
		}
		// The send and finish's close share r.mu: finish can run while the
		// handle's last update is still in flight, and a send on a closed
		// channel panics. The send never blocks, so holding the lock is safe.
		r.mu.Lock()
		if r.updatesClosed {
			r.mu.Unlock()
			return
		}
		select {
		case r.updates <- s:
		default:
		}
		r.mu.Unlock()
	}
}

// finish records the single terminal result and releases admission.
func (r *Reservation) finish(err error) {
	r.finishOnce.Do(func() {
		r.mu.Lock()
		r.result = err
		r.state = classify(err)
		if r.expired && r.state == ResultCancelled {
			// The deadline cancelled it; say so. A flow that completed or
			// failed on its own in the same instant keeps that outcome.
			r.state = ResultExpired
		}
		if r.timer != nil {
			r.timer.Stop()
			r.timer = nil
		}
		if r.deadline != nil {
			r.deadline.Stop()
			r.deadline = nil
		}
		detached := r.detached
		r.updatesClosed = true
		close(r.updates)
		res := TerminalResult{
			FlowID:     r.id,
			ProviderID: r.providerID,
			UpstreamID: r.upstreamID,
			State:      r.state,
			Retryable:  Retryable(r.state),
		}
		r.mu.Unlock()

		r.reg.remove(r.id)
		if detached {
			// Nobody is connected to receive it; retain the bounded record.
			r.reg.retain(r.deviceID, res)
		}
		close(r.done)
	})
}

// Cancel signals the handle. It does not release admission: the slot stays held
// until the handle's terminal cleanup actually completes, so a cancelled flow
// cannot be replaced by a new one while its child is still dying.
func (r *Reservation) Cancel() {
	r.cancelOnce.Do(func() {
		r.mu.Lock()
		h := r.handle
		r.mu.Unlock()
		if h == nil {
			// Never attached: nothing was started, so release immediately.
			r.finish(ErrFlowCancelled)
			return
		}
		h.Cancel()
	})
}

func classify(err error) string {
	switch {
	case err == nil:
		return ResultCompleted
	case isErr(err, ErrFlowCancelled), isErr(err, context.Canceled):
		return ResultCancelled
	case isErr(err, ErrActivationExpired), isErr(err, context.DeadlineExceeded):
		return ResultExpired
	case isErr(err, ErrConflict):
		return ResultConflict
	case isErr(err, ErrUnsupportedBackend):
		return ResultUnsupportedBackend
	case isErr(err, ErrRecoveryRequired):
		return ResultRecoveryRequired
	default:
		return ResultFailed
	}
}

func isErr(err, target error) bool { return errors.Is(err, target) }

// detach marks the flow disconnected and arms its resume window. Expiry
// cancels; a reattach before then disarms.
func (r *Reservation) detach(window time.Duration) {
	r.mu.Lock()
	if r.detached {
		r.mu.Unlock()
		return
	}
	r.detached = true
	if r.timer != nil {
		r.timer.Stop()
	}
	r.timer = time.AfterFunc(window, r.Cancel)
	r.mu.Unlock()
}

// reattach cancels the resume window after the owning device reconnects.
func (r *Reservation) reattach() {
	r.mu.Lock()
	r.detached = false
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
	r.mu.Unlock()
}
