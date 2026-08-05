package relay

import (
	"testing"
)

// MADR 0068 P2.4 — the Q5/Q6 spike, answered from the join plane itself.
//
// Q5 (can the relay replace a device's older join?): **No.** JoinPayload
// carries only host_id (protocol.go:41-43); the phone's client certificate
// terminates at the daemon *inside* the tunnel, so no device identity ever
// exists relay-side. Per-device replacement at the relay would require
// leaking device identity into the join plane — contrary to the relay's
// zero-knowledge posture. The zombie window is therefore bounded by the
// daemon's read deadline (which tears the splice down end-to-end, A1 Go
// finding 11) plus P6's slot sweep, not by replacement.
//
// Q6 (does a second join for the same host_id coexist with a lingering
// first?): **Yes** — asserted below. Slots are independent up to
// MaxPhonesPerHost; a new join never conflicts with, waits on, or evicts an
// older one.
func TestSecondJoinCoexistsWithLingeringFirst(t *testing.T) {
	cred, err := ParseAllowFlag("host-1:sixteen-chars-min-secret")
	if err != nil {
		t.Fatal(err)
	}
	h := newHub([]HostCredential{cred}, DefaultLimits(), false, nil)
	if err := h.register("host-1", nil, func() {}); err != nil {
		t.Fatal(err)
	}

	first, err := h.beginJoin("host-1", nil)
	if err != nil {
		t.Fatalf("first join: %v", err)
	}
	// The first join is never completed nor cancelled — the shape of a
	// phone that suspended mid-join with its splice accounting held.
	second, err := h.beginJoin("host-1", nil)
	if err != nil {
		t.Fatalf("second join while first lingers: %v", err)
	}
	if first.sessionID == second.sessionID {
		t.Fatal("joins must be independent sessions")
	}
	if got := h.phoneCount("host-1"); got != 2 {
		t.Fatalf("phones = %d, want 2 (both slots held)", got)
	}

	// And the lingering slot is only released by an explicit end — the
	// durability P6's sweep exists for.
	h.endPhone("host-1")
	if got := h.phoneCount("host-1"); got != 1 {
		t.Fatalf("phones after one endPhone = %d, want 1", got)
	}
}
