// Package fake implements a deterministic provider for tests and smoke demos.
//
// It exists to stand in for a real agent backend (ACP/HTTP) without a
// subprocess, so it must honor the same [provider.Session] contract the real
// transports do — otherwise manager/ws tests pass against behavior production
// never exhibits. In particular it matches them on three points that used to
// diverge: a second Prompt while a turn is active is rejected (not silently
// restarted); Cancel ends the turn with a turn_complete{cancelled}; and control
// events (status, turn lifecycle, permissions, tool state) are delivered with
// back-pressure, never dropped, per [event.IsControl].
package fake

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Modes is the mode list every fake session advertises at start, mirroring the
// real transports (grok's static default/plan pair, OpenCode's primary agents).
// Keeping the ids realistic is what lets manager tests exercise /plan and
// session.set_mode without a live agent.
var Modes = []event.SessionMode{
	{ID: "default", Name: "Build", Description: "Full tool access"},
	{ID: "plan", Name: "Plan", Description: "Research and plan only; no edits"},
}

// Provider is the fake agent provider.
type Provider struct{}

// New returns a fake provider.
func New() *Provider { return &Provider{} }

// ID implements [provider.Provider].
func (p *Provider) ID() provider.ID { return provider.IDFake }

// Ready implements [provider.Provider].
func (p *Provider) Ready() bool { return true }

// ListModels implements [provider.ModelCatalog].
func (p *Provider) ListModels(ctx context.Context) (picker.Catalog, error) {
	_ = ctx
	return picker.SingleCatalog(picker.SourceStatic, []picker.Option{
		{ID: "fake-echo", Label: "Echo", Description: "Deterministic test model", Group: "fake"},
		{ID: "fake-slow", Label: "Slow", Description: "Simulated latency (tests)", Group: "fake"},
	}, "fake-echo", true), nil
}

// Start implements [provider.Provider].
func (p *Provider) Start(ctx context.Context, opts provider.StartOptions) (provider.Session, error) {
	_ = ctx
	id := opts.LocalSessionID
	if id == "" {
		id = uuid.NewString()
	}
	s := &session{
		id:     id,
		events: make(chan event.Event, 32),
		done:   make(chan struct{}),
	}
	// Control events before the manager attaches its pump; the buffer holds
	// them, mirroring the real transports (a mode list then an idle status).
	s.emit(event.Event{
		Type:          event.TypeMode,
		Modes:         Modes,
		CurrentModeID: Modes[0].ID,
	})
	s.emit(event.Event{Type: event.TypeSessionStatus, Status: "idle"})
	return s, nil
}

type session struct {
	id     string
	events chan event.Event
	// done is closed by Close so a blocked control-event send unblocks and any
	// in-flight turn stops. events is never closed (matching the real
	// transports), so a concurrent emit can never send on a closed channel.
	done chan struct{}

	mu         sync.Mutex
	closed     bool
	turnActive bool
	turnCancel context.CancelFunc
}

// The fake mirrors the real ACP transport's optional capabilities so
// protocol/manager round-trips for modes and config options are testable
// without a live agent.
var _ provider.ModeSession = (*session)(nil)
var _ provider.ConfigSession = (*session)(nil)

func (s *session) ID() string                 { return s.id }
func (s *session) ProviderID() provider.ID    { return provider.IDFake }
func (s *session) AgentSessionID() string     { return s.id }
func (s *session) Events() <-chan event.Event { return s.events }

// SetMode echoes the switch back as a session_mode event, mirroring how a real
// agent confirms via current_mode_update.
func (s *session) SetMode(ctx context.Context, modeID string) error {
	_ = ctx
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return fmt.Errorf("session closed")
	}
	s.emit(event.Event{Type: event.TypeMode, CurrentModeID: modeID})
	return nil
}

// SetConfigOption echoes the change back as a session_config event.
func (s *session) SetConfigOption(ctx context.Context, optionID, kind, value string) error {
	_ = ctx
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return fmt.Errorf("session closed")
	}
	opt := event.ConfigOption{ID: optionID, Name: optionID, Kind: kind}
	if kind == "boolean" {
		opt.BoolValue = value == "true"
	} else {
		opt.CurrentValue = value
	}
	s.emit(event.Event{Type: event.TypeSessionConfig, ConfigOptions: []event.ConfigOption{opt}})
	return nil
}

func (s *session) Prompt(ctx context.Context, parts []provider.Content) error {
	_ = ctx // the turn streams asynchronously; the request ctx does not cancel it.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("session closed")
	}
	if s.turnActive {
		s.mu.Unlock()
		return provider.ErrTurnBusy
	}
	turnCtx, cancel := context.WithCancel(context.Background())
	s.turnActive = true
	s.turnCancel = cancel
	s.mu.Unlock()

	var text strings.Builder
	var attachments []event.AttachmentInfo
	for _, p := range parts {
		switch p.Type {
		case "", "text":
			text.WriteString(p.Text)
		case "image", "audio":
			attachments = append(attachments, event.AttachmentInfo{Kind: p.Type, MimeType: p.MimeType})
		}
	}

	s.emit(event.Event{Type: event.TypeUserMessage, Text: text.String(), Attachments: attachments})
	s.emit(event.Event{Type: event.TypeSessionStatus, Status: "running"})

	go s.runTurn(turnCtx, text.String())
	return nil
}

func (s *session) runTurn(turnCtx context.Context, userText string) {
	chunks := []string{"Echo from fake provider: ", userText, "\n"}
	for _, c := range chunks {
		select {
		case <-s.done:
			// Session torn down mid-turn: emits are suppressed once closed, so
			// just stop — Close already ended the turn bookkeeping.
			return
		case <-turnCtx.Done():
			// Cancel(): end the turn like the real transports do, so a consumer
			// waiting on turn_complete after a stop cannot hang.
			s.emit(event.Event{
				Type:       event.TypeTurnComplete,
				StopReason: "cancelled",
				Status:     "cancelled",
			})
			s.finishTurn()
			s.emit(event.Event{Type: event.TypeSessionStatus, Status: "idle"})
			return
		case <-time.After(20 * time.Millisecond):
			s.emit(event.Event{Type: event.TypeAssistantChunk, Text: c})
		}
	}
	s.emit(event.Event{Type: event.TypeTurnComplete, Status: "end_turn"})
	s.finishTurn()
	s.emit(event.Event{Type: event.TypeSessionStatus, Status: "idle"})
}

// finishTurn clears turn bookkeeping. The cancel func is released so the next
// Prompt starts clean.
func (s *session) finishTurn() {
	s.mu.Lock()
	s.turnActive = false
	s.turnCancel = nil
	s.mu.Unlock()
}

func (s *session) Cancel(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("session closed")
	}
	if s.turnCancel != nil {
		s.turnCancel()
	}
	return nil
}

func (s *session) Close(ctx context.Context) error {
	_ = ctx
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	if s.turnCancel != nil {
		s.turnCancel()
		s.turnCancel = nil
	}
	s.mu.Unlock()
	close(s.done)
	return nil
}

// emit delivers ev with the same back-pressure guarantees as the real
// transports: control events block until consumed (or the session closes);
// stream chunks drop when the buffer is full.
func (s *session) emit(ev event.Event) {
	if ev.SessionID == "" {
		ev.SessionID = s.id
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	if event.IsControl(ev.Type) {
		select {
		case s.events <- ev:
		case <-s.done:
		}
		return
	}
	select {
	case s.events <- ev:
	default:
	}
}
