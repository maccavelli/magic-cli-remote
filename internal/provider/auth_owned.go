package provider

import (
	"context"

	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// ownedFlowHandle adapts a providerauth.OwnedFlow to DeviceAuthHandle.
//
// The two types are deliberately separate: providerauth owns process and
// transaction mechanics and must not depend on this package, while callers
// here should not learn a second flow vocabulary. This adapter is the only
// place the two meet (MADR 0074 D27).
type ownedFlowHandle struct {
	flow    *providerauth.OwnedFlow
	updates chan DeviceAuthState
}

// NewOwnedFlowHandle wraps an owned credential flow as a DeviceAuthHandle.
func NewOwnedFlowHandle(f *providerauth.OwnedFlow) DeviceAuthHandle {
	h := &ownedFlowHandle{flow: f, updates: make(chan DeviceAuthState, 1)}
	// One goroutine, owned by the handle, ending when the flow closes its
	// update channel. It never blocks the flow: publish is best-effort on both
	// sides, so a caller that ignores Updates cannot stall a login.
	go func() {
		defer close(h.updates)
		for s := range f.Updates() {
			select {
			case h.updates <- DeviceAuthState(s):
			default:
			}
		}
	}()
	return h
}

// Flow implements [DeviceAuthHandle].
func (h *ownedFlowHandle) Flow() DeviceFlow {
	f := h.flow.Flow()
	return DeviceFlow{
		VerificationURI: f.VerificationURI,
		UserCode:        f.UserCode,
		ExpiresIn:       f.ExpiresIn,
		Interval:        f.Interval,
	}
}

// Wait implements [DeviceAuthHandle].
func (h *ownedFlowHandle) Wait(ctx context.Context) error { return h.flow.Wait(ctx) }

// Cancel implements [DeviceAuthHandle].
func (h *ownedFlowHandle) Cancel() { h.flow.Cancel() }

// Updates implements [DeviceAuthUpdateSource].
func (h *ownedFlowHandle) Updates() <-chan DeviceAuthState { return h.updates }
