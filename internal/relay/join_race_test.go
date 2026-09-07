package relay

import (
	"errors"
	"testing"

	"github.com/coder/websocket"
)

// 0115 P2 (F1): the join-timeout / claimTunnel race. Every interleaving of
// {phone leaves, tunnel claims/publishes} must release the phone slot exactly
// once and leave no goroutine waiting on a done that will never close. These
// are direct hub-call tests: no sockets, every step deterministic.

func raceHub(t *testing.T) *hub {
	t.Helper()
	cred, err := ParseAllowFlag("h1:0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	h := newHub([]HostCredential{cred}, DefaultLimits(), false, nil)
	var control *websocket.Conn
	if err := h.register("h1", control, func() {}); err != nil {
		t.Fatalf("register: %v", err)
	}
	return h
}

func doneClosed(p *pendingJoin) bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// assertNoDivergence runs the slot sweep twice with no live splices and
// requires zero corrections: after the F1 fix the counters are exact, so the
// sweep must have nothing to heal (0115 P2 step 8).
func assertNoDivergence(t *testing.T, h *hub) {
	t.Helper()
	if n := h.reconcilePhones(map[string]int{}); n != 0 {
		t.Fatalf("first sweep corrected %d hosts; counters should be exact", n)
	}
	if n := h.reconcilePhones(map[string]int{}); n != 0 {
		t.Fatalf("second sweep corrected %d hosts; counters should be exact", n)
	}
}

// Phone leaves after the host claimed but before publish. publishTunnel must
// observe phoneGone: release the slot, wake the tunnel handler, return false.
func TestPhoneGoneAfterClaimBeforePublish(t *testing.T) {
	h := raceHub(t)
	// A second join holds one slot so exactly-once release is observable:
	// a double release would drop the count to 0, a lost release leaves 2.
	if _, err := h.beginJoin("h1", nil); err != nil {
		t.Fatal(err)
	}
	p, err := h.beginJoin("h1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.claimTunnel(p.sessionID, "h1", p.tunnelToken, ""); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if orphan := h.phoneGone(p); orphan != nil {
		t.Fatal("no tunnel was published; phoneGone must not return one")
	}
	if h.publishTunnel(p, &websocket.Conn{}) {
		t.Fatal("publish after phoneGone must report the phone gone")
	}
	if got := h.phoneCount("h1"); got != 1 {
		t.Fatalf("phones=%d, want 1 (exactly-once release)", got)
	}
	if !doneClosed(p) {
		t.Fatal("done must be closed so handleTunnel is not parked")
	}
	assertNoDivergence(t, h)
}

// Phone leaves after publish already succeeded. phoneGone must hand the
// published tunnel back for closing and release the slot itself.
func TestPhoneGoneAfterPublish(t *testing.T) {
	h := raceHub(t)
	p, err := h.beginJoin("h1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.claimTunnel(p.sessionID, "h1", p.tunnelToken, ""); err != nil {
		t.Fatalf("claim: %v", err)
	}
	tun := &websocket.Conn{}
	if !h.publishTunnel(p, tun) {
		t.Fatal("publish should succeed while the phone still waits")
	}
	orphan := h.phoneGone(p)
	if orphan != tun {
		t.Fatalf("phoneGone returned %p, want the published tunnel %p", orphan, tun)
	}
	if got := h.phoneCount("h1"); got != 0 {
		t.Fatalf("phones=%d, want 0", got)
	}
	if !doneClosed(p) {
		t.Fatal("done must be closed so handleTunnel is not parked")
	}
	assertNoDivergence(t, h)
}

// Phone leaves while the join is still pending: identical to the old
// cancelJoin path, and a later claim must fail as unknown_session.
func TestPhoneGonePendingIsCancel(t *testing.T) {
	h := raceHub(t)
	p, err := h.beginJoin("h1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if orphan := h.phoneGone(p); orphan != nil {
		t.Fatal("nothing was published")
	}
	if _, ok := <-p.ready; ok {
		t.Fatal("ready must be closed for a cancelled pending join")
	}
	if got := h.phoneCount("h1"); got != 0 {
		t.Fatalf("phones=%d, want 0", got)
	}
	if _, err := h.claimTunnel(p.sessionID, "h1", p.tunnelToken, ""); !errors.Is(err, errUnknownSession) {
		t.Fatalf("claim after cancel: err=%v, want unknown_session", err)
	}
	assertNoDivergence(t, h)
}

// Phone leaves after claim; the host's tunnel_ok write then fails and the
// tunnel handler abandons. The one reserved slot is released exactly once.
func TestAbandonAfterPhoneGone(t *testing.T) {
	h := raceHub(t)
	if _, err := h.beginJoin("h1", nil); err != nil { // slot canary, as above
		t.Fatal(err)
	}
	p, err := h.beginJoin("h1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.claimTunnel(p.sessionID, "h1", p.tunnelToken, ""); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if orphan := h.phoneGone(p); orphan != nil {
		t.Fatal("nothing was published")
	}
	h.abandonTunnel(p)
	if got := h.phoneCount("h1"); got != 1 {
		t.Fatalf("phones=%d, want 1 (exactly-once release)", got)
	}
	if !doneClosed(p) {
		t.Fatal("done must be closed after abandon")
	}
	assertNoDivergence(t, h)
}

// Phone's tunnel_ok write fails first (abandonTunnel closes ready); then the
// phone times out and calls phoneGone. The canary join must still hold one
// slot — a receive from the closed ready channel must not release again
// (0142 F18).
func TestPhoneGoneAfterAbandon(t *testing.T) {
	h := raceHub(t)
	if _, err := h.beginJoin("h1", nil); err != nil {
		t.Fatal(err)
	}
	p, err := h.beginJoin("h1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.claimTunnel(p.sessionID, "h1", p.tunnelToken, ""); err != nil {
		t.Fatalf("claim: %v", err)
	}
	h.abandonTunnel(p)
	if orphan := h.phoneGone(p); orphan != nil {
		t.Fatalf("phoneGone after abandon returned %v, want nil", orphan)
	}
	if got := h.phoneCount("h1"); got != 1 {
		t.Fatalf("phones=%d, want 1 (exactly-once release)", got)
	}
	if !doneClosed(p) {
		t.Fatal("done must be closed after abandon")
	}
	assertNoDivergence(t, h)
}
