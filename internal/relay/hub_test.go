package relay

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// completeTunnel runs the claim+publish pair that handleTunnel performs, for
// tests that assert on the combined outcome. Production splits the two so the
// tunnel_ok handshake completes before handlePhone can start splicing.
func (h *hub) completeTunnel(sessionID, hostID, token, secret string, tunnel *websocket.Conn) (*pendingJoin, error) {
	p, err := h.claimTunnel(sessionID, hostID, token, secret)
	if err != nil {
		return nil, err
	}
	if !h.publishTunnel(p, tunnel) {
		return nil, fmt.Errorf("already_claimed")
	}
	return p, nil
}

func TestResolvedLimitsPartial(t *testing.T) {
	l := ResolvedLimits(Limits{MaxPhonesPerHost: 2})
	if l.MaxPhonesPerHost != 2 {
		t.Fatalf("phones=%d", l.MaxPhonesPerHost)
	}
	d := DefaultLimits()
	if l.MaxHosts != d.MaxHosts {
		t.Fatalf("hosts=%d want default %d", l.MaxHosts, d.MaxHosts)
	}
	if l.MaxMessageBytes != d.MaxMessageBytes {
		t.Fatal(l.MaxMessageBytes)
	}
}

func TestNewUsesResolvedLimits(t *testing.T) {
	cred, err := ParseAllowFlag("h1:sixteen-chars-min-1")
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Config{
		Allow:  []HostCredential{cred},
		Limits: Limits{MaxPhonesPerHost: 3}, // only phones set
	}, nil)
	if srv.cfg.Limits.MaxPhonesPerHost != 3 {
		t.Fatalf("phones overridden: %d", srv.cfg.Limits.MaxPhonesPerHost)
	}
	if srv.cfg.Limits.MaxHosts != DefaultLimits().MaxHosts {
		t.Fatalf("hosts not defaulted: %d", srv.cfg.Limits.MaxHosts)
	}
}

func TestHubCancelJoinReleasesPhone(t *testing.T) {
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	h := newHub([]HostCredential{cred}, DefaultLimits(), false, nil)
	// Fake control conn is never used for read/write in these unit tests.
	var control *websocket.Conn
	if err := h.register("h1", control, func() {}); err != nil {
		t.Fatal(err)
	}
	p, err := h.beginJoin("h1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.phoneCount("h1") != 1 {
		t.Fatalf("phones=%d", h.phoneCount("h1"))
	}
	h.cancelJoin(p.sessionID)
	if h.phoneCount("h1") != 0 {
		t.Fatalf("after cancel phones=%d", h.phoneCount("h1"))
	}
	if h.pendingCount() != 0 {
		t.Fatal("pending not cleared")
	}
}

func TestHubUnregisterReleasesPendingPhones(t *testing.T) {
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	h := newHub([]HostCredential{cred}, DefaultLimits(), false, nil)
	var control *websocket.Conn
	if err := h.register("h1", control, func() {}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.beginJoin("h1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.beginJoin("h1", nil); err != nil {
		t.Fatal(err)
	}
	if h.phoneCount("h1") != 2 {
		t.Fatalf("phones=%d", h.phoneCount("h1"))
	}
	h.unregister("h1", control)
	// Host gone — phoneCount returns 0.
	if h.phoneCount("h1") != 0 {
		t.Fatalf("phones=%d", h.phoneCount("h1"))
	}
	if h.pendingCount() != 0 {
		t.Fatal("pending not cleared")
	}
	// Re-register must start at 0, not stuck at limit (R1).
	if err := h.register("h1", control, func() {}); err != nil {
		t.Fatal(err)
	}
	if h.phoneCount("h1") != 0 {
		t.Fatalf("re-register phones=%d want 0", h.phoneCount("h1"))
	}
	// Should be able to fill MaxPhonesPerHost again.
	for i := 0; i < DefaultLimits().MaxPhonesPerHost; i++ {
		if _, err := h.beginJoin("h1", nil); err != nil {
			t.Fatalf("join %d: %v", i, err)
		}
	}
	if _, err := h.beginJoin("h1", nil); err == nil {
		t.Fatal("expected limit")
	}
}

func TestHubAlreadyClaimedReleasesPhone(t *testing.T) {
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	h := newHub([]HostCredential{cred}, DefaultLimits(), false, nil)
	var control *websocket.Conn
	if err := h.register("h1", control, func() {}); err != nil {
		t.Fatal(err)
	}
	p, err := h.beginJoin("h1", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate phone timeout: cancelJoin closes ready.
	h.cancelJoin(p.sessionID)
	if h.phoneCount("h1") != 0 {
		t.Fatalf("after cancel %d", h.phoneCount("h1"))
	}
	// Tunnel arrives late — pending gone → unknown_session, not a leak.
	_, err = h.completeTunnel(p.sessionID, "h1", p.tunnelToken, "", nil)
	if err == nil {
		t.Fatal("expected unknown_session")
	}
	if h.phoneCount("h1") != 0 {
		t.Fatalf("phones=%d", h.phoneCount("h1"))
	}
}

func TestHubTunnelTokenAuth(t *testing.T) {
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	h := newHub([]HostCredential{cred}, DefaultLimits(), false, nil)
	var control *websocket.Conn
	_ = h.register("h1", control, func() {})
	p, err := h.beginJoin("h1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.tunnelToken == "" {
		t.Fatal("expected tunnel token")
	}
	// Wrong token rejected.
	if _, err := h.completeTunnel(p.sessionID, "h1", "bad-token", "", nil); err == nil {
		t.Fatal("expected unauthorized")
	}
	// Token works (R12).
	var tun *websocket.Conn
	if _, err := h.completeTunnel(p.sessionID, "h1", p.tunnelToken, "", tun); err != nil {
		t.Fatal(err)
	}
	h.endPhone("h1")

	// Legacy secret rejected by default (MADR 0017 D13).
	p2, err := h.beginJoin("h1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.completeTunnel(p2.sessionID, "h1", "", "sixteen-chars-min-1", tun); err == nil {
		t.Fatal("expected unauthorized when legacy secret disabled")
	}
	h.cancelJoin(p2.sessionID)

	// Legacy secret works when opted in.
	hLegacy := newHub([]HostCredential{cred}, DefaultLimits(), true, nil)
	_ = hLegacy.register("h1", control, func() {})
	p3, err := hLegacy.beginJoin("h1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hLegacy.completeTunnel(p3.sessionID, "h1", "", "sixteen-chars-min-1", tun); err != nil {
		t.Fatalf("legacy secret with flag: %v", err)
	}
	hLegacy.endPhone("h1")
}

func TestHubPhonesSurviveHostUnregister(t *testing.T) {
	// MADR 0017 D10: active claim keeps durable phone count after control drop.
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	lim := DefaultLimits()
	lim.MaxPhonesPerHost = 1
	h := newHub([]HostCredential{cred}, lim, false, nil)
	var c1, c2 *websocket.Conn
	_ = h.register("h1", c1, func() {})
	p, err := h.beginJoin("h1", nil)
	if err != nil {
		t.Fatal(err)
	}
	var tun *websocket.Conn
	if _, err := h.completeTunnel(p.sessionID, "h1", p.tunnelToken, "", tun); err != nil {
		t.Fatal(err)
	}
	if h.phoneCount("h1") != 1 {
		t.Fatalf("phones=%d", h.phoneCount("h1"))
	}
	// Host control drops; active splice still holds the slot.
	h.unregister("h1", c1)
	if h.phoneCount("h1") != 1 {
		t.Fatalf("after unregister phones=%d want 1", h.phoneCount("h1"))
	}
	// Re-register cannot accept another join until endPhone.
	_ = h.register("h1", c2, func() {})
	if _, err := h.beginJoin("h1", nil); err == nil {
		t.Fatal("expected limit while active splice holds phone slot")
	}
	h.endPhone("h1")
	if h.phoneCount("h1") != 0 {
		t.Fatalf("after endPhone phones=%d", h.phoneCount("h1"))
	}
	if _, err := h.beginJoin("h1", nil); err != nil {
		t.Fatalf("join after release: %v", err)
	}
}

func TestResolvedLimitsClampsCeilings(t *testing.T) {
	l := ResolvedLimits(Limits{MaxMessageBytes: MaxLimitMessageBytes * 2, MaxHosts: MaxLimitHosts + 10})
	if l.MaxMessageBytes != MaxLimitMessageBytes {
		t.Fatalf("message bytes %d", l.MaxMessageBytes)
	}
	if l.MaxHosts != MaxLimitHosts {
		t.Fatalf("hosts %d", l.MaxHosts)
	}
}

func TestValidateHostAndSessionID(t *testing.T) {
	if err := validateHostID("ok-host_1"); err != nil {
		t.Fatal(err)
	}
	if err := validateHostID(strings.Repeat("a", 200)); err == nil {
		t.Fatal("expected too long")
	}
	if err := validateHostID("bad id"); err == nil {
		t.Fatal("expected invalid char")
	}
	if err := validateSessionID("550e8400-e29b-41d4-a716-446655440000"); err != nil {
		t.Fatal(err)
	}
	if err := validateSessionID(strings.Repeat("x", MaxSessionIDLen+1)); err == nil {
		t.Fatal("expected session too long")
	}
}

func TestHubExpireStalePending(t *testing.T) {
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	h := newHub([]HostCredential{cred}, DefaultLimits(), false, nil)
	var control *websocket.Conn
	_ = h.register("h1", control, func() {})
	p, _ := h.beginJoin("h1", nil)
	// Force old created time.
	h.mu.Lock()
	p.created = time.Now().Add(-time.Hour)
	h.mu.Unlock()
	if n := h.expireStalePending(time.Minute); n != 1 {
		t.Fatalf("expired %d", n)
	}
	if h.pendingCount() != 0 || h.phoneCount("h1") != 0 {
		t.Fatal("orphan not cleaned")
	}
}

func TestHubAlreadyClaimedWhenReadyFull(t *testing.T) {
	// ready is buffered(1); if we somehow double-complete, second should release.
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	h := newHub([]HostCredential{cred}, DefaultLimits(), false, nil)
	var control *websocket.Conn
	_ = h.register("h1", control, func() {})
	p, _ := h.beginJoin("h1", nil)
	// First complete succeeds (puts tunnel on ready).
	var tun *websocket.Conn
	p2, err := h.completeTunnel(p.sessionID, "h1", p.tunnelToken, "", tun)
	if err != nil || p2 == nil {
		t.Fatalf("first complete: %v", err)
	}
	if h.phoneCount("h1") != 1 {
		t.Fatalf("phones after claim %d", h.phoneCount("h1"))
	}
	// Second complete: pending already deleted.
	_, err = h.completeTunnel(p.sessionID, "h1", p.tunnelToken, "", tun)
	if err == nil {
		t.Fatal("expected unknown_session")
	}
	// Slot still held until endPhone (splice path).
	if h.phoneCount("h1") != 1 {
		t.Fatalf("phones=%d", h.phoneCount("h1"))
	}
	h.endPhone("h1")
	if h.phoneCount("h1") != 0 {
		t.Fatal(h.phoneCount("h1"))
	}
}

func TestHubReRegisterPreservesPhoneCount(t *testing.T) {
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	h := newHub([]HostCredential{cred}, DefaultLimits(), false, nil)
	var c1, c2 *websocket.Conn
	_ = h.register("h1", c1, func() {})
	_, _ = h.beginJoin("h1", nil)
	if h.phoneCount("h1") != 1 {
		t.Fatal(h.phoneCount("h1"))
	}
	// Re-register (new control) keeps durable phones=1 (D10).
	_ = h.register("h1", c2, func() {})
	if h.phoneCount("h1") != 1 {
		t.Fatalf("re-register reset phones to %d", h.phoneCount("h1"))
	}
}

// Once completeTunnel hands a pendingJoin to the tunnel handler, handlePhone
// and handleTunnel both own it and both have paths that end the join. A bare
// close(done) in each panicked the serving goroutine with "close of closed
// channel" whenever they raced — reachable in production by a host that drops
// its connection right after claiming the tunnel.
func TestPendingJoinCloseDoneIsIdempotent(t *testing.T) {
	p := &pendingJoin{done: make(chan struct{})}

	p.closeDone()
	p.closeDone() // second close must not panic
	p.closeDone()

	select {
	case <-p.done:
	default:
		t.Fatal("done should be closed")
	}
}

// The real failure was concurrent, so assert it under contention too: every
// goroutine calls closeDone at once and exactly one close must happen.
func TestPendingJoinCloseDoneConcurrent(t *testing.T) {
	for range 200 {
		p := &pendingJoin{done: make(chan struct{})}
		start := make(chan struct{})
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				<-start
				p.closeDone()
			})
		}
		close(start) // release them together to maximise overlap
		wg.Wait()

		select {
		case <-p.done:
		default:
			t.Fatal("done should be closed")
		}
	}
}

// claimTunnel must NOT hand the connection to the waiting phone handler: that
// handler splices immediately, so a tunnel published before tunnel_ok is
// written receives the phone's first frame in place of the handshake envelope
// (the host then logs "expected text frame, got MessageBinary" and drops the
// tunnel). Publishing is publishTunnel's job, after the write.
func TestHubClaimTunnelDoesNotPublish(t *testing.T) {
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	h := newHub([]HostCredential{cred}, DefaultLimits(), false, nil)
	_ = h.register("h1", nil, func() {})
	p, err := h.beginJoin("h1", nil)
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := h.claimTunnel(p.sessionID, "h1", p.tunnelToken, "")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	select {
	case c := <-claimed.ready:
		t.Fatalf("claimTunnel published the tunnel (%v); handlePhone could splice "+
			"before tunnel_ok is written", c)
	default:
	}

	// The claim is exclusive even though nothing was published yet.
	if _, err := h.claimTunnel(p.sessionID, "h1", p.tunnelToken, ""); err == nil {
		t.Fatal("second claim should fail with unknown_session")
	}

	if !h.publishTunnel(claimed, nil) {
		t.Fatal("publishTunnel should hand off to the waiting phone")
	}
	select {
	case <-claimed.ready:
	default:
		t.Fatal("phone handler never received the tunnel")
	}
}

// A failed tunnel_ok write leaves the join claimed but unpublished. The phone
// is already out of h.pending, so nothing else can wake it: abandonTunnel must
// release the slot and close ready rather than stranding it until join timeout.
func TestHubAbandonTunnelReleasesWaitingPhone(t *testing.T) {
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	h := newHub([]HostCredential{cred}, DefaultLimits(), false, nil)
	_ = h.register("h1", nil, func() {})
	p, err := h.beginJoin("h1", nil)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := h.claimTunnel(p.sessionID, "h1", p.tunnelToken, "")
	if err != nil {
		t.Fatal(err)
	}
	if h.phoneCount("h1") != 1 {
		t.Fatalf("phones after claim=%d", h.phoneCount("h1"))
	}

	h.abandonTunnel(claimed)

	select {
	case c, ok := <-claimed.ready:
		if ok {
			t.Fatalf("expected ready closed, got %v", c)
		}
	default:
		t.Fatal("ready still open — the phone would block until join timeout")
	}
	if h.phoneCount("h1") != 0 {
		t.Fatalf("slot leaked: phones=%d", h.phoneCount("h1"))
	}
}
