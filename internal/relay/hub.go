package relay

import (
	"context"
	"crypto/subtle"
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
	limits  Limits
	log     *slog.Logger
}

type hostSlot struct {
	hostID  string
	control *websocket.Conn
	writeMu sync.Mutex // serializes control-plane writes (dial)
	phones  int        // active spliced phones
	cancel  func()
}

type pendingJoin struct {
	sessionID string
	hostID    string
	phone     *websocket.Conn
	ready     chan *websocket.Conn // receives host tunnel conn
	done      chan struct{}        // closed when splice ends (unblocks tunnel handler)
	created   time.Time
}

func newHub(allow []HostCredential, limits Limits, log *slog.Logger) *hub {
	m := make(map[string][32]byte, len(allow))
	for _, c := range allow {
		m[c.HostID] = c.SecretHash
	}
	if log == nil {
		log = slog.Default()
	}
	return &hub{
		allow:   m,
		hosts:   make(map[string]*hostSlot),
		pending: make(map[string]*pendingJoin),
		limits:  limits,
		log:     log.With(slog.String("component", "relay.hub")),
	}
}

func (h *hub) checkSecret(hostID, secret string) bool {
	want, ok := h.allow[hostID]
	if !ok {
		return false
	}
	got := HashSecret(secret)
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
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
		// Replace stale registration (reconnect).
		if old.cancel != nil {
			old.cancel()
		}
		_ = old.control.Close(websocket.StatusGoingAway, "replaced")
	}
	h.hosts[hostID] = &hostSlot{hostID: hostID, control: control, cancel: cancel}
	h.log.Info("host registered", slog.String("host_id", hostID))
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
	delete(h.hosts, hostID)
	// Fail pending joins for this host.
	for id, p := range h.pending {
		if p.hostID == hostID {
			close(p.ready)
			delete(h.pending, id)
		}
	}
	h.log.Info("host unregistered", slog.String("host_id", hostID), slog.String("reason", "host_gone"))
}

func (h *hub) beginJoin(hostID string, phone *websocket.Conn) (*pendingJoin, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	slot, ok := h.hosts[hostID]
	if !ok {
		return nil, fmt.Errorf("host_offline")
	}
	if slot.phones >= h.limits.MaxPhonesPerHost {
		return nil, fmt.Errorf("limit")
	}
	if len(h.pending) >= h.limits.MaxConcurrentJoin {
		return nil, fmt.Errorf("limit")
	}
	sid := uuid.NewString()
	p := &pendingJoin{
		sessionID: sid,
		hostID:    hostID,
		phone:     phone,
		ready:     make(chan *websocket.Conn, 1),
		done:      make(chan struct{}),
		created:   time.Now(),
	}
	h.pending[sid] = p
	slot.phones++ // reserve capacity until splice ends
	return p, nil
}

func (h *hub) completeTunnel(sessionID, hostID, secret string, tunnel *websocket.Conn) (*pendingJoin, error) {
	if !h.checkSecret(hostID, secret) {
		return nil, fmt.Errorf("unauthorized")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.pending[sessionID]
	if !ok {
		return nil, fmt.Errorf("unknown_session")
	}
	if p.hostID != hostID {
		return nil, fmt.Errorf("unauthorized")
	}
	delete(h.pending, sessionID)
	select {
	case p.ready <- tunnel:
	default:
		return nil, fmt.Errorf("already_claimed")
	}
	return p, nil
}

func (h *hub) cancelJoin(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.pending[sessionID]
	if !ok {
		return
	}
	delete(h.pending, sessionID)
	if s, ok := h.hosts[p.hostID]; ok && s.phones > 0 {
		s.phones--
	}
	close(p.ready)
}

func (h *hub) endPhone(hostID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.hosts[hostID]; ok && s.phones > 0 {
		s.phones--
	}
}
