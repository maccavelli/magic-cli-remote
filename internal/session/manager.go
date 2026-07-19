// Package session manages live agent sessions and event fan-out.
package session

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Meta is public session metadata.
type Meta struct {
	ID        string      `json:"id"`
	Provider  provider.ID `json:"provider"`
	Name      string      `json:"name"`
	CreatedAt time.Time   `json:"created_at"`
	Status    string      `json:"status"`
}

type entry struct {
	meta    Meta
	sess    provider.Session
	cancel  context.CancelFunc
}

// EventHandler is called for every session event (e.g. WS broadcast).
type EventHandler func(ev event.Event)

// Manager tracks sessions created via providers.
type Manager struct {
	reg     *provider.Registry
	log     *slog.Logger
	onEvent EventHandler

	mu       sync.RWMutex
	sessions map[string]*entry
}

// NewManager creates a session manager.
func NewManager(reg *provider.Registry, log *slog.Logger, onEvent EventHandler) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		reg:      reg,
		log:      log.With(slog.String("component", "session")),
		onEvent:  onEvent,
		sessions: make(map[string]*entry),
	}
}

// Create starts a new session with the given provider.
func (m *Manager) Create(ctx context.Context, providerID provider.ID, opts provider.StartOptions) (Meta, error) {
	p, err := m.reg.Get(providerID)
	if err != nil {
		return Meta{}, err
	}
	sess, err := p.Start(ctx, opts)
	if err != nil {
		return Meta{}, err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	meta := Meta{
		ID:        sess.ID(),
		Provider:  providerID,
		Name:      opts.Name,
		CreatedAt: time.Now().UTC(),
		Status:    "idle",
	}

	m.mu.Lock()
	m.sessions[sess.ID()] = &entry{meta: meta, sess: sess, cancel: cancel}
	m.mu.Unlock()

	go m.pump(runCtx, sess)

	m.log.Info("session created",
		slog.String("session_id", meta.ID),
		slog.String("provider", string(providerID)),
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
				return
			}
			if ev.Type == event.TypeSessionStatus && ev.Status != "" {
				m.mu.Lock()
				if e, ok := m.sessions[sess.ID()]; ok {
					e.meta.Status = ev.Status
				}
				m.mu.Unlock()
			}
			if m.onEvent != nil {
				m.onEvent(ev)
			}
		}
	}
}

// Get returns session metadata.
func (m *Manager) Get(id string) (Meta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.sessions[id]
	if !ok {
		return Meta{}, fmt.Errorf("session %q not found", id)
	}
	return e.meta, nil
}

// List returns all sessions.
func (m *Manager) List() []Meta {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Meta, 0, len(m.sessions))
	for _, e := range m.sessions {
		out = append(out, e.meta)
	}
	return out
}

// Prompt sends a text prompt to a session.
func (m *Manager) Prompt(ctx context.Context, id, text string) error {
	m.mu.RLock()
	e, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %q not found", id)
	}
	return e.sess.Prompt(ctx, []provider.Content{{Type: "text", Text: text}})
}

// Cancel cancels the in-flight turn on a session.
func (m *Manager) Cancel(ctx context.Context, id string) error {
	m.mu.RLock()
	e, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session %q not found", id)
	}
	return e.sess.Cancel(ctx)
}

// Close closes and removes a session.
func (m *Manager) Close(ctx context.Context, id string) error {
	m.mu.Lock()
	e, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("session %q not found", id)
	}
	delete(m.sessions, id)
	m.mu.Unlock()

	e.cancel()
	if err := e.sess.Close(ctx); err != nil {
		return err
	}
	m.log.Info("session closed", slog.String("session_id", id))
	return nil
}

// CloseAll closes every session.
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
