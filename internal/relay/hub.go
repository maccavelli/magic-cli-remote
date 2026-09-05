package relay

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// hub tracks registered hosts and pending phone joins.
type hub struct {
	mu      sync.Mutex
	allow   map[string][32]byte // host_id → secret hash
	hosts   map[string]*hostSlot
	pending map[string]*pendingJoin // session_id → join
	// phones is durable reserved+active phone capacity per host_id (MADR 0017 D10).
	// Survives control disconnect / re-register so MaxPhonesPerHost cannot be
	// bypassed while splices are still running.
	phones map[string]int
	// suspect holds per-host divergences seen by the previous slot sweep
	// (0068 P6): a divergence must survive two consecutive sweeps before
	// it is corrected, so the in-flight windows where phones is legally
	// ahead of (splices + pending) — claim→publish, splice-end→endPhone —
	// can never be "fixed" into a double release.
	suspect map[string]slotDivergence
	limits  Limits
	// allowLegacyTunnelSecret permits secret-based tunnel claims (D13 default false).
	allowLegacyTunnelSecret bool
	log                     *slog.Logger
}

type hostSlot struct {
	control *websocket.Conn
	writeMu sync.Mutex // serializes control-plane writes (dial)
	cancel  func()
}

type pendingJoin struct {
	sessionID   string
	hostID      string
	tunnelToken string // single-use claim for /v1/tunnel (R12)
	phone       *websocket.Conn
	ready       chan *websocket.Conn // receives host tunnel conn
	done        chan struct{}        // closed when splice ends (unblocks tunnel handler)
	doneOnce    sync.Once            // guards done: two handlers race to close it
	created     time.Time
	// phoneGone marks that handlePhone abandoned this join after it was
	// claimed (0115 F1). Guarded by hub.mu, like pending-map membership.
	phoneGone bool
}

// closeDone releases everyone waiting on the join.
//
// Once completeTunnel hands the pendingJoin to the tunnel handler, TWO
// goroutines own it: handlePhone (which closes done when the splice ends, or
// when it cannot write join_ok) and handleTunnel (which closes it if writing
// tunnel_ok fails). Both can reach a close for the same join — a host that
// drops its connection immediately after claiming the tunnel is enough — and a
// bare close(done) then panics the serving goroutine with "close of closed
// channel". Idempotent by construction instead.
func (p *pendingJoin) closeDone() {
	p.doneOnce.Do(func() { close(p.done) })
}

func newHub(allow []HostCredential, limits Limits, allowLegacy bool, log *slog.Logger) *hub {
	m := make(map[string][32]byte, len(allow))
	for _, c := range allow {
		m[c.HostID] = c.SecretHash
	}
	if log == nil {
		log = slog.Default()
	}
	return &hub{
		allow:                   m,
		hosts:                   make(map[string]*hostSlot),
		pending:                 make(map[string]*pendingJoin),
		phones:                  make(map[string]int),
		suspect:                 make(map[string]slotDivergence),
		limits:                  limits,
		allowLegacyTunnelSecret: allowLegacy,
		log:                     log.With(slog.String("component", "relay.hub")),
	}
}

// checkSecret verifies host registration secret with constant-time work
// for unknown and known host_id (MADR 0017 D12).
func (h *hub) checkSecret(hostID, secret string) bool {
	got := HashSecret(secret)
	want, ok := h.allow[hostID]
	var ref [32]byte
	if ok {
		ref = want
	}
	// Always compare so unknown hosts do not skip SHA-256 + compare work.
	match := subtle.ConstantTimeCompare(ref[:], got[:]) == 1
	return ok && match
}

func (h *hub) register(hostID string, control *websocket.Conn, cancel func()) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.allow[hostID]; !ok {
		// Defense in depth: unreachable via handleHost (checkSecret gates).
		return errUnauthorized
	}
	if len(h.hosts) >= h.limits.MaxHosts {
		if _, online := h.hosts[hostID]; !online {
			return errLimit
		}
	}
	if old, ok := h.hosts[hostID]; ok {
		// Replace stale registration (reconnect). Cancel control; in-flight
		// splices keep running; durable phones map is unchanged (D10).
		if old.cancel != nil {
			old.cancel()
		}
		if old.control != nil {
			_ = old.control.Close(websocket.StatusGoingAway, "replaced")
		}
	}
	h.hosts[hostID] = &hostSlot{
		control: control,
		cancel:  cancel,
	}
	h.log.Info("host registered", slog.String("host_id", hostID), slog.Int("phones", h.phones[hostID]))
	return nil
}

// writeControl sends a join-plane envelope on the host control connection.
func (h *hub) writeControl(ctx context.Context, hostID string, env Envelope) error {
	h.mu.Lock()
	slot, ok := h.hosts[hostID]
	h.mu.Unlock()
	if !ok {
		return errHostOffline
	}
	slot.writeMu.Lock()
	defer slot.writeMu.Unlock()
	return WriteEnvelope(ctx, slot.control, env)
}

func (h *hub) unregister(hostID string, control *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	slot, ok := h.hosts[hostID]
	if !ok || slot.control != control {
		return
	}
	// Fail pending joins and release their reserved slots (MADR 0016 R1).
	// Active splices keep durable phone counts until endPhone (D10).
	for id, p := range h.pending {
		if p.hostID != hostID {
			continue
		}
		delete(h.pending, id)
		h.releasePhoneLocked(hostID)
		close(p.ready)
	}
	delete(h.hosts, hostID)
	h.log.Info("host unregistered", slog.String("host_id", hostID), slog.String("reason", "host_gone"),
		slog.Int("phones_remaining", h.phones[hostID]))
}

// closeAllHosts cancels and closes every registered host control (MADR 0017 D11).
func (h *hub) closeAllHosts(reason string) {
	h.mu.Lock()
	list := make([]*hostSlot, 0, len(h.hosts))
	for _, slot := range h.hosts {
		list = append(list, slot)
	}
	// Fail all pending joins.
	for id, p := range h.pending {
		delete(h.pending, id)
		h.releasePhoneLocked(p.hostID)
		close(p.ready)
	}
	h.hosts = make(map[string]*hostSlot)
	h.mu.Unlock()

	for _, slot := range list {
		if slot.cancel != nil {
			slot.cancel()
		}
		if slot.control != nil {
			_ = slot.control.Close(websocket.StatusGoingAway, reason)
		}
	}
}

func (h *hub) beginJoin(hostID string, phone *websocket.Conn) (*pendingJoin, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// R10: never distinguish "unknown host_id" from "offline" — both look
	// like host_offline so the allowlist cannot be enumerated via join errors.
	if _, ok := h.hosts[hostID]; !ok {
		return nil, errHostOffline
	}
	if h.phones[hostID] >= h.limits.MaxPhonesPerHost {
		return nil, errLimit
	}
	if len(h.pending) >= h.limits.MaxConcurrentJoin {
		return nil, errLimit
	}
	token, err := newTunnelToken()
	if err != nil {
		return nil, errInternal
	}
	sid := uuid.NewString()
	p := &pendingJoin{
		sessionID:   sid,
		hostID:      hostID,
		tunnelToken: token,
		phone:       phone,
		ready:       make(chan *websocket.Conn, 1),
		done:        make(chan struct{}),
		created:     time.Now(),
	}
	h.pending[sid] = p
	h.phones[hostID]++ // durable until endPhone / cancelJoin / failed completeTunnel
	return p, nil
}

// claimTunnel authenticates a tunnel dial and removes the join from pending so
// no second dial can claim it. Prefer short-lived token (R12); legacy
// registration secret only when AllowLegacyTunnelSecret (MADR 0017 D13).
//
// It deliberately does NOT hand the connection to the waiting phone handler.
// Publishing is a separate step (publishTunnel) that the caller must run only
// after the tunnel_ok handshake has been written: handlePhone starts splicing
// the instant it receives the tunnel, so publishing first let the phone's first
// frame reach the host while the host was still reading its text-framed
// tunnel_ok ("expected text frame, got MessageBinary"), and briefly put two
// goroutines on the same websocket writer.
func (h *hub) claimTunnel(sessionID, hostID, token, secret string) (*pendingJoin, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.pending[sessionID]
	if !ok {
		return nil, errUnknownSession
	}
	if p.hostID != hostID {
		return nil, errUnauthorized
	}
	authOK := false
	if token != "" && p.tunnelToken != "" {
		authOK = subtle.ConstantTimeCompare([]byte(token), []byte(p.tunnelToken)) == 1
	}
	if !authOK && secret != "" && h.allowLegacyTunnelSecret {
		// Legacy path: long-lived registration secret (opt-in only, D13).
		authOK = h.checkSecret(hostID, secret)
		if authOK {
			h.log.Info("tunnel claimed with legacy registration secret",
				slog.String("host_id", hostID),
				slog.String("session_id", sessionID))
		}
	}
	if !authOK {
		return nil, errUnauthorized
	}
	delete(h.pending, sessionID)
	return p, nil
}

// publishTunnel hands an authenticated tunnel to the waiting phone handler,
// which begins splicing immediately. Reports false when the phone already left
// (ready closed or full), having released its slot (MADR 0016 R2).
func (h *hub) publishTunnel(p *pendingJoin, tunnel *websocket.Conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p.phoneGone {
		// The phone abandoned the join after the claim (0115 F1): its slot is
		// still reserved, and no one is reading ready. Release here and wake
		// the tunnel handler.
		h.releasePhoneLocked(p.hostID)
		p.closeDone()
		return false
	}
	select {
	case p.ready <- tunnel:
		return true
	default:
		h.releasePhoneLocked(p.hostID)
		return false
	}
}

// abandonTunnel fails a join that was claimed but never published — the
// tunnel_ok write failed. The phone is still blocked on ready and is no longer
// in h.pending, so nothing else will ever wake it: release its slot and close
// ready here rather than making it wait out the full join timeout.
func (h *hub) abandonTunnel(p *pendingJoin) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Exactly-once release in both orders: when phoneGone ran first on a
	// claimed join it deliberately deferred the release to this path (or to
	// publishTunnel), so releasing here is correct whether or not the phone
	// is still waiting.
	h.releasePhoneLocked(p.hostID)
	close(p.ready)
	p.closeDone()
}

// expireStalePending cancels joins older than maxAge (R18 orphan GC).
// Returns how many were expired.
func (h *hub) expireStalePending(maxAge time.Duration) int {
	if maxAge <= 0 {
		return 0
	}
	h.mu.Lock()
	var stale []string
	cutoff := time.Now().Add(-maxAge)
	for id, p := range h.pending {
		if p.created.Before(cutoff) {
			stale = append(stale, id)
		}
	}
	h.mu.Unlock()
	for _, id := range stale {
		h.cancelJoin(id)
	}
	return len(stale)
}

func newTunnelToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// cancelJoin fails a join that is still in h.pending. Callers that hold a
// *pendingJoin which may already have been claimed must use phoneGone
// instead (0115 F1); this function silently no-ops on claimed joins.
func (h *hub) cancelJoin(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.pending[sessionID]
	if !ok {
		return
	}
	delete(h.pending, sessionID)
	h.releasePhoneLocked(p.hostID)
	close(p.ready)
}

// phoneGone is called by handlePhone when it abandons a join (timeout or
// request-context cancel). It resolves the race with claimTunnel /
// publishTunnel under one lock (0115 F1). The returned conn, when non-nil,
// is an already-published tunnel the caller must close (outside the lock).
func (h *hub) phoneGone(p *pendingJoin) (orphan *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.pending[p.sessionID]; ok {
		// Not yet claimed: identical to the old cancelJoin path.
		delete(h.pending, p.sessionID)
		h.releasePhoneLocked(p.hostID)
		close(p.ready)
		return nil
	}
	// Claimed. Either the tunnel is already in ready, or publishTunnel /
	// abandonTunnel has yet to run and will observe phoneGone.
	p.phoneGone = true
	select {
	case t, ok := <-p.ready:
		if !ok {
			return nil
		}
		h.releasePhoneLocked(p.hostID)
		p.closeDone()
		return t
	default:
		return nil
	}
}

func (h *hub) endPhone(hostID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.releasePhoneLocked(hostID)
}

// releasePhoneLocked decrements durable phone reservation. Caller holds h.mu.
func (h *hub) releasePhoneLocked(hostID string) {
	n := h.phones[hostID]
	if n <= 0 {
		return
	}
	n--
	if n == 0 {
		delete(h.phones, hostID)
		return
	}
	h.phones[hostID] = n
}

// slotDivergence is one host's counter disagreement as seen by a sweep.
type slotDivergence struct{ have, want int }

// reconcilePhones compares the durable phones counters against ground truth
// (live splices per host + pending reservations) and self-corrects
// divergence that persists across two consecutive sweeps (0068 P6, A1 Go
// unknown 8). The counters are maintained by paired begin/release calls, so
// any lasting disagreement is a leaked or lost release — logged loudly,
// because the sweep masks the symptom (a host wedged at MaxPhonesPerHost)
// but the pairing bug it hides still wants fixing. Returns how many hosts
// were corrected.
func (h *hub) reconcilePhones(liveSplices map[string]int) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	pendingBy := make(map[string]int, len(h.pending))
	for _, p := range h.pending {
		pendingBy[p.hostID]++
	}
	hosts := make(map[string]struct{}, len(h.phones)+len(liveSplices))
	for id := range h.phones {
		hosts[id] = struct{}{}
	}
	for id := range liveSplices {
		hosts[id] = struct{}{}
	}
	for id := range pendingBy {
		hosts[id] = struct{}{}
	}
	corrected := 0
	next := make(map[string]slotDivergence)
	for id := range hosts {
		want := liveSplices[id] + pendingBy[id]
		have := h.phones[id]
		if have == want {
			continue
		}
		d := slotDivergence{have: have, want: want}
		if prev, seen := h.suspect[id]; !seen || prev != d {
			// First sighting (or the numbers moved — work is in flight):
			// remember and give it one more sweep interval to settle.
			next[id] = d
			continue
		}
		if want == 0 {
			delete(h.phones, id)
		} else {
			h.phones[id] = want
		}
		corrected++
		h.log.Warn("phone slot divergence corrected",
			slog.String("host_id", id),
			slog.Int("have", have),
			slog.Int("want", want))
	}
	h.suspect = next
	return corrected
}

// phoneCount returns reserved+active phones for a host (tests).
func (h *hub) phoneCount(hostID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.phones[hostID]
}

// pendingCount returns pending joins (tests).
func (h *hub) pendingCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.pending)
}
