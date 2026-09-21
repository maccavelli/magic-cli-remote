package codex

import (
	"testing"
	"time"
)

// These are acceptance criterion A12 of PLAN 0163 and the regression tests for
// MADR 0163 F15: a capability disabled by a transient -32601/-32602 used to stay
// off for the engine's entire lifetime, because Disable had no counterpart.

func expiryState(t *testing.T, supported ...CapabilityID) (*capabilityState, *time.Time) {
	t.Helper()
	snapshot := CapabilitySnapshot{
		Supported: map[CapabilityID]bool{},
		Denied:    map[CapabilityID]CapabilityDenial{},
	}
	for _, id := range supported {
		snapshot.Supported[id] = true
	}
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	state := newCapabilityState(snapshot)
	state.now = func() time.Time { return now }
	return state, &now
}

func TestDeniedCapabilityComesBackAfterTheRetryWindow(t *testing.T) {
	state, now := expiryState(t, CapabilityThreadSettings)
	if !state.Supports(CapabilityThreadSettings) {
		t.Fatal("capability should start supported")
	}

	state.Disable(CapabilityThreadSettings, DenialMethodNotFound)
	if state.Supports(CapabilityThreadSettings) {
		t.Fatal("a fresh denial must hold")
	}

	// Just short of the window: still denied. A denial that expired immediately
	// would re-probe on every call, which is the opposite failure.
	*now = now.Add(capabilityRetryAfter - time.Second)
	if state.Supports(CapabilityThreadSettings) {
		t.Error("denial expired early")
	}

	*now = now.Add(2 * time.Second)
	if !state.Supports(CapabilityThreadSettings) {
		t.Fatal("denial did not expire after the retry window")
	}
	// And the snapshot agrees, so status surfaces stop reporting it as denied.
	snapshot := state.Snapshot()
	if !snapshot.Supports(CapabilityThreadSettings) {
		t.Error("snapshot still reports the capability unsupported")
	}
	if _, denied := snapshot.Denied[CapabilityThreadSettings]; denied {
		t.Error("snapshot still lists a denial reason for a re-enabled capability")
	}
}

func TestExpiryNeverInventsACapabilityTheEngineLacks(t *testing.T) {
	// Denied without ever having been supported — the engine simply does not
	// offer it (an experimental capability with experimentalApi refused, say).
	// Expiry must not hand it back, or the provider would advertise a method the
	// binary has no implementation for.
	state, now := expiryState(t)
	state.Disable(CapabilityCollaborationModes, DenialExperimentalRejected)

	*now = now.Add(100 * capabilityRetryAfter)
	if state.Supports(CapabilityCollaborationModes) {
		t.Fatal("expiry invented support for a capability that was never supported")
	}
}

func TestRedenialRestartsTheRetryWindow(t *testing.T) {
	state, now := expiryState(t, CapabilityThreadSettings)
	state.Disable(CapabilityThreadSettings, DenialMethodNotFound)

	*now = now.Add(capabilityRetryAfter + time.Second)
	if !state.Supports(CapabilityThreadSettings) {
		t.Fatal("expected the first expiry")
	}
	// The re-probe fails again, as it would for a method that is genuinely gone.
	state.Disable(CapabilityThreadSettings, DenialMethodNotFound)
	if state.Supports(CapabilityThreadSettings) {
		t.Fatal("the second denial must hold too")
	}
	*now = now.Add(capabilityRetryAfter - time.Second)
	if state.Supports(CapabilityThreadSettings) {
		t.Error("the window did not restart on re-denial, so a dead method is " +
			"re-probed far more often than once per window")
	}
}
