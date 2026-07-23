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
	limits  Limits
	log     *slog.Logger
}

type hostSlot struct {
	hostID  string
	control *websocket.Conn
	writeMu sync.Mutex // serializes control-plane writes (dial)
	phones  int        // reserved + active phone slots (beginJoin → endPhone)
	cancel  func()
}

type pendingJoin struct {
	sessionID   string
	hostID      string
	tunnelToken string // single-use claim for /v1/tunnel (R12)
	phone       *websocket.Conn
	ready       chan *websocket.Conn // receives host tunnel conn
	done        chan struct{}        // closed when splice ends (unblocks tunnel handler)
	created     time.Time
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
		// Replace stale registration (reconnect). Cancel control; in-flight
		// splices keep running and will endPhone against this host id.
		if old.cancel != nil {
			old.cancel()
		}
		if old.control != nil {
			_ = old.control.Close(websocket.StatusGoingAway, "replaced")
		}
		// Preserve phone count across re-register so active/pending slots
		// remain accurate (MADR 0016 capacity).
		h.hosts[hostID] = &hostSlot{
			hostID:  hostID,
			control: control,
			cancel:  cancel,
			phones:  old.phones,
		}
	} else {
		h.hosts[hostID] = &hostSlot{hostID: hostID, control: control, cancel: cancel}
	}
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
	// Fail pending joins and release their reserved slots (MADR 0016 R1).
	for id, p := range h.pending {
		if p.hostID != hostID {
			continue
		}
		delete(h.pending, id)
		if slot.phones > 0 {
			slot.phones--
		}
		close(p.ready)
	}
	delete(h.hosts, hostID)
	h.log.Info("host unregistered", slog.String("host_id", hostID), slog.String("reason", "host_gone"))
}

func (h *hub) beginJoin(hostID string, phone *websocket.Conn) (*pendingJoin, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// R10: never distinguish "unknown host_id" from "offline" — both look
	// like host_offline so the allowlist cannot be enumerated via join errors.
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
	slot.phones++ // reserve until endPhone / cancelJoin / failed completeTunnel
	return p, nil
}

// completeTunnel claims a pending join. Prefer short-lived token (R12); legacy
// registration secret is still accepted for one release of old host clients.
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
	if !authOK && secret != "" {
		// Legacy path: long-lived registration secret (H1 constant-time).
		authOK = h.checkSecret(hostID, secret)
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

// releasePhoneLocked decrements the host phone reservation. Caller holds h.mu.
func (h *hub) releasePhoneLocked(hostID string) {
	if s, ok := h.hosts[hostID]; ok && s.phones > 0 {
		s.phones--
	}
}

// phoneCount returns reserved+active phones for a host (tests).
func (h *hub) phoneCount(hostID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.hosts[hostID]; ok {
		return s.phones
	}
	return 0
}

// pendingCount returns pending joins (tests).
func (h *hub) pendingCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.pending)
}
