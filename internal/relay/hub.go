package relay

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
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
	limits Limits
	// allowLegacyTunnelSecret permits secret-based tunnel claims (D13 default false).
	allowLegacyTunnelSecret bool
	log                     *slog.Logger
}

type hostSlot struct {
	hostID  string
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
		return fmt.Errorf("unknown_host")
	}
	if len(h.hosts) >= h.limits.MaxHosts {
		if _, online := h.hosts[hostID]; !online {
			return fmt.Errorf("limit")
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
		hostID:  hostID,
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
		return fmt.Errorf("host_offline")
	}
	slot.writeMu.Lock()
	defer slot.writeMu.Unlock()
	return writeEnv(ctx, slot.control, env)
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
		return nil, fmt.Errorf("host_offline")
	}
	if h.phones[hostID] >= h.limits.MaxPhonesPerHost {
		return nil, fmt.Errorf("limit")
	}
	if len(h.pending) >= h.limits.MaxConcurrentJoin {
		return nil, fmt.Errorf("limit")
	}
	token, err := newTunnelToken()
	if err != nil {
		return nil, fmt.Errorf("internal")
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

// completeTunnel claims a pending join. Prefer short-lived token (R12); legacy
// registration secret only when AllowLegacyTunnelSecret (MADR 0017 D13).
func (h *hub) completeTunnel(sessionID, hostID, token, secret string, tunnel *websocket.Conn) (*pendingJoin, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.pending[sessionID]
	if !ok {
		return nil, fmt.Errorf("unknown_session")
	}
	if p.hostID != hostID {
		return nil, fmt.Errorf("unauthorized")
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
		return nil, fmt.Errorf("unauthorized")
	}
	delete(h.pending, sessionID)
	select {
	case p.ready <- tunnel:
		return p, nil
	default:
		// Phone already left (ready closed or full); release slot (MADR 0016 R2).
		h.releasePhoneLocked(hostID)
		return nil, fmt.Errorf("already_claimed")
	}
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
