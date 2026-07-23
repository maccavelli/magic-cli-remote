package relay

import (
	"testing"

	"github.com/coder/websocket"
)

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
	h := newHub([]HostCredential{cred}, DefaultLimits(), nil)
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
	h := newHub([]HostCredential{cred}, DefaultLimits(), nil)
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
	h := newHub([]HostCredential{cred}, DefaultLimits(), nil)
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
	_, err = h.completeTunnel(p.sessionID, "h1", "sixteen-chars-min-1", nil)
	if err == nil {
		t.Fatal("expected unknown_session")
	}
	if h.phoneCount("h1") != 0 {
		t.Fatalf("phones=%d", h.phoneCount("h1"))
	}
}

func TestHubAlreadyClaimedWhenReadyFull(t *testing.T) {
	// ready is buffered(1); if we somehow double-complete, second should release.
	cred, _ := ParseAllowFlag("h1:sixteen-chars-min-1")
	h := newHub([]HostCredential{cred}, DefaultLimits(), nil)
	var control *websocket.Conn
	_ = h.register("h1", control, func() {})
	p, _ := h.beginJoin("h1", nil)
	// First complete succeeds (puts tunnel on ready).
	var tun *websocket.Conn
	p2, err := h.completeTunnel(p.sessionID, "h1", "sixteen-chars-min-1", tun)
	if err != nil || p2 == nil {
		t.Fatalf("first complete: %v", err)
	}
	if h.phoneCount("h1") != 1 {
		t.Fatalf("phones after claim %d", h.phoneCount("h1"))
	}
	// Second complete: pending already deleted.
	_, err = h.completeTunnel(p.sessionID, "h1", "sixteen-chars-min-1", tun)
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
	h := newHub([]HostCredential{cred}, DefaultLimits(), nil)
	var c1, c2 *websocket.Conn
	_ = h.register("h1", c1, func() {})
	_, _ = h.beginJoin("h1", nil)
	if h.phoneCount("h1") != 1 {
		t.Fatal(h.phoneCount("h1"))
	}
	// Re-register (new control) keeps phones=1.
	_ = h.register("h1", c2, func() {})
	if h.phoneCount("h1") != 1 {
		t.Fatalf("re-register reset phones to %d", h.phoneCount("h1"))
	}
}
