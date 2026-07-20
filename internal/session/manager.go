// Package session manages live agent sessions and event fan-out.
package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// StatusDisconnected is the session status reported once the backing provider
// process is gone. Sessions in this state are no longer live.
const StatusDisconnected = "disconnected"

// historyBufferCap bounds the per-session replay ring buffer. It matches the
// spirit of the mobile client's kMaxTranscriptItems: keep enough to rebuild a
// transcript on reconnect while bounding daemon memory. Oldest events drop.
const historyBufferCap = 500

// ErrNotLive is returned when a mutating op targets a missing or dead session.
var ErrNotLive = errors.New("session not found or not live")

// Meta is public session metadata.
type Meta struct {
	ID             string      `json:"id"`
	Provider       provider.ID `json:"provider"`
	Name           string      `json:"name"`
	CWD            string      `json:"cwd,omitempty"`
	AgentSessionID string      `json:"agent_session_id,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
	Status         string      `json:"status"`
	Live           bool        `json:"live"`
}

type entry struct {
	meta   Meta
	sess   provider.Session
	cancel context.CancelFunc
	// dead is set briefly while auto-close runs after a disconnected status.
	// Entries are removed from m.sessions on close; dead is not a long-lived
	// tombstone (R2=A).
	dead bool
	// history is a bounded ring buffer of every event this session emitted,
	// oldest first, for session.history replay. Capped at historyBufferCap.
	history []event.Event
}

// EventHandler is called for every session event (e.g. WS broadcast).
type EventHandler func(ev event.Event)

// Manager tracks sessions created via providers.
type Manager struct {
	reg     *provider.Registry
	store   *Store
	log     *slog.Logger
	onEvent EventHandler

	// createMu serializes Create so close-and-replace cannot race another Create
	// for the same local session id (R3=B).
	createMu sync.Mutex

	mu       sync.RWMutex
	sessions map[string]*entry
}

// NewManager creates a session manager. store may be nil (no persistence).
func NewManager(reg *provider.Registry, store *Store, log *slog.Logger, onEvent EventHandler) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		reg:      reg,
		store:    store,
		log:      log.With(slog.String("component", "session")),
		onEvent:  onEvent,
		sessions: make(map[string]*entry),
	}
}

// Create starts a new session with the given provider.
//
// If opts.LocalSessionID already names a live session, that session is fully
// closed first and replaced (R3=B). The returned Meta is live only after the
// new provider Start succeeds.
func (m *Manager) Create(ctx context.Context, providerID provider.ID, opts provider.StartOptions) (Meta, error) {
	p, err := m.reg.Get(providerID)
	if err != nil {
		return Meta{}, err
	}
	if opts.LocalSessionID == "" {
		opts.LocalSessionID = uuid.NewString()
	}

	m.createMu.Lock()
	defer m.createMu.Unlock()

	// Close-and-replace: never map-overwrite without closing the prior process.
	if err := m.close(ctx, opts.LocalSessionID, false); err != nil && !errors.Is(err, ErrNotLive) {
		// close returns ErrNotLive-shaped not-found; tolerate missing.
		if !isSessionMissing(err) {
			return Meta{}, err
		}
	}

	sess, err := p.Start(ctx, opts)
	if err != nil {
		return Meta{}, err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	meta := Meta{
		ID:             sess.ID(),
		Provider:       providerID,
		Name:           opts.Name,
		CWD:            opts.CWD,
		AgentSessionID: sess.AgentSessionID(),
		CreatedAt:      time.Now().UTC(),
		Status:         "idle",
		Live:           true,
	}

	m.mu.Lock()
	// Defensive: if another path inserted the same id, close it out-of-band.
	if prev, ok := m.sessions[sess.ID()]; ok {
		delete(m.sessions, sess.ID())
		m.mu.Unlock()
		prev.cancel()
		_ = prev.sess.Close(ctx)
		m.mu.Lock()
	}
	m.sessions[sess.ID()] = &entry{meta: meta, sess: sess, cancel: cancel}
	m.mu.Unlock()

	go m.pump(runCtx, sess)
	m.persist(meta)

	m.log.Info("session created",
		slog.String("session_id", meta.ID),
		slog.String("provider", string(providerID)),
		slog.String("agent_session_id", meta.AgentSessionID),
	)
	return meta, nil
}

func (m *Manager) pump(ctx context.Context, sess provider.Session) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sess.Events():
			if !ok {
				// Channel closed without a disconnected status: still auto-close
				// so the entry does not linger as a commandable zombie (R2=A).
				m.autoClose(sess.ID(), "events closed")
				return
			}
			autoClose := false
			m.mu.Lock()
			if e, ok := m.sessions[sess.ID()]; ok {
				if ev.Type == event.TypeSessionStatus && ev.Status != "" {
					e.meta.Status = ev.Status
					if ev.AgentSessionID != "" {
						e.meta.AgentSessionID = ev.AgentSessionID
					}
					if ev.Status == StatusDisconnected {
						e.dead = true
						e.meta.Live = false
						autoClose = true
					}
					m.persistLocked(e.meta)
				}
				// Buffer every event for history replay; drop the oldest once
				// the ring is full so memory stays bounded.
				e.history = append(e.history, ev)
				if len(e.history) > historyBufferCap {
					e.history = e.history[len(e.history)-historyBufferCap:]
				}
			}
			m.mu.Unlock()
			if m.onEvent != nil {
				m.onEvent(ev)
			}
			if autoClose {
				// Full teardown outside the lock; entry removed, process closed.
				m.autoClose(sess.ID(), "provider disconnected")
				return
			}
		}
	}
}

// autoClose tears down a session after provider death. Safe to call when the
// entry is already gone (e.g. concurrent Close).
func (m *Manager) autoClose(id, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := m.close(ctx, id, false); err != nil && !isSessionMissing(err) {
		m.log.Warn("auto-close session failed",
			slog.String("session_id", id),
			slog.String("reason", reason),
			slog.String("err", err.Error()),
		)
		return
	}
	m.log.Info("session auto-closed",
		slog.String("session_id", id),
		slog.String("reason", reason),
	)
}

// Get returns session metadata if currently tracked (live).
func (m *Manager) Get(id string) (Meta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.sessions[id]
	if !ok || e.dead {
		return Meta{}, fmt.Errorf("%w: %q", ErrNotLive, id)
	}
	return e.meta, nil
}

// History returns a copy of the buffered event replay for a session, oldest
// first. An unknown or never-active session returns an empty (non-nil) slice,
// not an error. A closed session is dropped from m.sessions, so its buffer is
// gone and History returns empty — replay is a best-effort live-session aid.
func (m *Manager) History(id string) []event.Event {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.sessions[id]
	if !ok || e.dead || len(e.history) == 0 {
		return []event.Event{}
	}
	out := make([]event.Event, len(e.history))
	copy(out, e.history)
	return out
}

// List returns live sessions, merged with persisted records not currently live.
func (m *Manager) List() []Meta {
	m.mu.RLock()
	live := make(map[string]Meta, len(m.sessions))
	out := make([]Meta, 0, len(m.sessions))
	for _, e := range m.sessions {
		if e.dead {
			continue
		}
		meta := e.meta
		meta.Live = true
		live[meta.ID] = meta
		out = append(out, meta)
	}
	m.mu.RUnlock()

	if m.store == nil {
		return out
	}
	recs, err := m.store.List()
	if err != nil {
		return out
	}
	for _, rec := range recs {
		if _, ok := live[rec.ID]; ok {
			continue
		}
		out = append(out, Meta{
			ID:             rec.ID,
			Provider:       rec.Provider,
			Name:           rec.Name,
			CWD:            rec.CWD,
			AgentSessionID: rec.AgentSessionID,
			CreatedAt:      rec.CreatedAt,
			Status:         rec.Status,
			Live:           false,
		})
	}
	return out
}

// liveEntry returns the entry for a live session. Caller must not retain the
// pointer across unlock without holding m.mu; we copy the session interface only.
func (m *Manager) liveSession(id string) (provider.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.sessions[id]
	if !ok || e.dead {
		return nil, fmt.Errorf("%w: %q", ErrNotLive, id)
	}
	return e.sess, nil
}

// Prompt sends a text prompt to a live session.
func (m *Manager) Prompt(ctx context.Context, id, text string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	return sess.Prompt(ctx, []provider.Content{{Type: "text", Text: text}})
}

// Cancel cancels the in-flight turn on a session.
func (m *Manager) Cancel(ctx context.Context, id string) error {
	sess, err := m.liveSession(id)
	if err != nil {
		return err
	}
	return sess.Cancel(ctx)
}

// RespondPermission forwards a permission decision to the session.
func (m *Manager) RespondPermission(ctx context.Context, sessionID, permissionID, optionID string, cancelled bool) error {
	sess, err := m.liveSession(sessionID)
	if err != nil {
		return err
	}
	ps, ok := sess.(provider.PermissionSession)
	if !ok {
		return fmt.Errorf("session %q does not support remote permissions", sessionID)
	}
	return ps.RespondPermission(ctx, permissionID, optionID, cancelled)
}

// Close closes and removes a live session; persistence is updated to disconnected
// unless purge is true (hard delete from disk).
func (m *Manager) Close(ctx context.Context, id string) error {
	return m.close(ctx, id, false)
}

// Delete closes a live session if any and removes disk record.
func (m *Manager) Delete(ctx context.Context, id string) error {
	return m.close(ctx, id, true)
}

func (m *Manager) close(ctx context.Context, id string, purge bool) error {
	m.mu.Lock()
	e, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	if ok {
		e.cancel()
		_ = e.sess.Close(ctx)
		meta := e.meta
		meta.Status = StatusDisconnected
		meta.Live = false
		if purge {
			if m.store != nil {
				_ = m.store.Delete(id)
			}
		} else {
			m.persist(meta)
		}
		m.log.Info("session closed", slog.String("session_id", id))
		return nil
	}

	if purge && m.store != nil {
		return m.store.Delete(id)
	}
	return fmt.Errorf("%w: %q", ErrNotLive, id)
}

func isSessionMissing(err error) bool {
	return err != nil && errors.Is(err, ErrNotLive)
}

// CloseAll closes every live session.
func (m *Manager) CloseAll(ctx context.Context) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Close(ctx, id)
	}
}

func (m *Manager) persist(meta Meta) {
	if m.store == nil {
		return
	}
	_ = m.store.Save(Record{
		ID:             meta.ID,
		Provider:       meta.Provider,
		Name:           meta.Name,
		CWD:            meta.CWD,
		AgentSessionID: meta.AgentSessionID,
		CreatedAt:      meta.CreatedAt,
		Status:         meta.Status,
	})
}

func (m *Manager) persistLocked(meta Meta) {
	// Caller holds m.mu; store has its own lock.
	m.persist(meta)
}
