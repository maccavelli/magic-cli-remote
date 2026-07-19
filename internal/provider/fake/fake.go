// Package fake implements a deterministic provider for tests and smoke demos.
package fake

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Provider is the fake agent provider.
type Provider struct{}

// New returns a fake provider.
func New() *Provider { return &Provider{} }

func (p *Provider) ID() provider.ID { return provider.IDFake }
func (p *Provider) Ready() bool     { return true }

func (p *Provider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	_ = ctx
	_ = opts
	s := &session{
		id:     uuid.NewString(),
		events: make(chan event.Event, 32),
	}
	s.emit(event.Event{
		Type:      event.TypeSessionStatus,
		SessionID: s.id,
		Timestamp: time.Now().UTC(),
		Status:    "idle",
	})
	return s, nil
}

type session struct {
	id     string
	events chan event.Event

	mu     sync.Mutex
	closed bool
	cancel context.CancelFunc
}

func (s *session) ID() string                 { return s.id }
func (s *session) ProviderID() provider.ID    { return provider.IDFake }
func (s *session) AgentSessionID() string     { return s.id }
func (s *session) Events() <-chan event.Event { return s.events }

func (s *session) Prompt(ctx context.Context, parts []provider.Content) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	if s.cancel != nil {
		s.cancel()
	}
	turnCtx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.mu.Unlock()

	var text string
	for _, p := range parts {
		if p.Type == "" || p.Type == "text" {
			text += p.Text
		}
	}

	s.emit(event.Event{
		Type:      event.TypeUserMessage,
		SessionID: s.id,
		Timestamp: time.Now().UTC(),
		Text:      text,
	})
	s.emit(event.Event{
		Type:      event.TypeSessionStatus,
		SessionID: s.id,
		Timestamp: time.Now().UTC(),
		Status:    "running",
	})

	go s.runTurn(ctx, turnCtx, text)
	return nil
}

func (s *session) runTurn(parent, turnCtx context.Context, userText string) {
	chunks := []string{
		"Echo from fake provider: ",
		userText,
		"\n(Phase 1 scaffolding — replace with Grok ACP in Phase 2.)",
	}
	for _, c := range chunks {
		select {
		case <-parent.Done():
			return
		case <-turnCtx.Done():
			s.emit(event.Event{
				Type:      event.TypeSessionStatus,
				SessionID: s.id,
				Timestamp: time.Now().UTC(),
				Status:    "idle",
			})
			return
		case <-time.After(20 * time.Millisecond):
			s.emit(event.Event{
				Type:      event.TypeAssistantChunk,
				SessionID: s.id,
				Timestamp: time.Now().UTC(),
				Text:      c,
			})
		}
	}
	s.emit(event.Event{
		Type:      event.TypeTurnComplete,
		SessionID: s.id,
		Timestamp: time.Now().UTC(),
		Status:    "end_turn",
	})
	s.emit(event.Event{
		Type:      event.TypeSessionStatus,
		SessionID: s.id,
		Timestamp: time.Now().UTC(),
		Status:    "idle",
	})
}

func (s *session) Cancel(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("session closed")
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	return nil
}

func (s *session) Close(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	close(s.events)
	return nil
}

func (s *session) emit(ev event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.events <- ev:
	default:
		// Drop if slow consumer; Phase 1 is best-effort for fake.
	}
}
