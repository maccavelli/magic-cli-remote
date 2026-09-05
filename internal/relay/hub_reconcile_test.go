package relay

import "testing"

// 0068 P6: the slot sweep self-corrects leaked phone reservations — but only
// after a divergence survives two consecutive sweeps, so legal in-flight
// windows are never "fixed" into a double release.

func newReconcileHub(t *testing.T) *hub {
	t.Helper()
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	return newHub([]HostCredential{cred}, Limits{
		MaxHosts:          4,
		MaxPhonesPerHost:  4,
		MaxConcurrentJoin: 4,
	}, false, nil)
}

func TestReconcileCorrectsLeakedSlotAfterTwoSweeps(t *testing.T) {
	h := newReconcileHub(t)
	// A leaked reservation: counted, but no splice and no pending join.
	h.mu.Lock()
	h.phones["h1"] = 3
	h.mu.Unlock()

	if n := h.reconcilePhones(map[string]int{"h1": 1}); n != 0 {
		t.Fatalf("first sweep corrected %d, want 0 (suspect only)", n)
	}
	if got := h.phoneCount("h1"); got != 3 {
		t.Fatalf("count changed on first sweep: %d", got)
	}
	if n := h.reconcilePhones(map[string]int{"h1": 1}); n != 1 {
		t.Fatalf("second sweep corrected %d, want 1", n)
	}
	if got := h.phoneCount("h1"); got != 1 {
		t.Fatalf("corrected count = %d, want 1 (the live splice)", got)
	}
}

func TestReconcileZeroTargetDeletesEntry(t *testing.T) {
	h := newReconcileHub(t)
	h.mu.Lock()
	h.phones["h1"] = 2
	h.mu.Unlock()
	_ = h.reconcilePhones(nil)
	_ = h.reconcilePhones(nil)
	h.mu.Lock()
	_, exists := h.phones["h1"]
	h.mu.Unlock()
	if exists {
		t.Fatal("fully-leaked host should be deleted from phones, not set to 0")
	}
}

func TestReconcileLeavesMovingCountersAlone(t *testing.T) {
	h := newReconcileHub(t)
	h.mu.Lock()
	h.phones["h1"] = 3
	h.mu.Unlock()

	_ = h.reconcilePhones(map[string]int{"h1": 1})
	// Between sweeps the numbers move (a splice ended): the divergence pair
	// changed, so the sweep must re-arm rather than correct.
	if n := h.reconcilePhones(map[string]int{}); n != 0 {
		t.Fatalf("moving divergence corrected %d, want 0", n)
	}
	if got := h.phoneCount("h1"); got != 3 {
		t.Fatalf("count changed while divergence was still moving: %d", got)
	}
	// Now it is stable at the new pair for two sweeps — corrected.
	if n := h.reconcilePhones(map[string]int{}); n != 1 {
		t.Fatalf("settled divergence corrected %d, want 1", n)
	}
}

func TestReconcileMatchingCountersUntouched(t *testing.T) {
	h := newReconcileHub(t)
	if err := h.register("h1", nil, nil); err != nil {
		t.Fatal(err)
	}
	p, err := h.beginJoin("h1", nil)
	if err != nil {
		t.Fatal(err)
	}
	// One pending reservation, zero splices: counters agree; nothing to do.
	for i := range 3 {
		if n := h.reconcilePhones(nil); n != 0 {
			t.Fatalf("sweep %d corrected %d on agreeing counters", i, n)
		}
	}
	if got := h.phoneCount("h1"); got != 1 {
		t.Fatalf("pending reservation lost: %d", got)
	}
	h.cancelJoin(p.sessionID)
}
