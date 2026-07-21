package session_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// recordingProvider captures the StartOptions of every session it starts, so
// tests can assert what model a relaunch used.
type recordingProvider struct {
	mu      sync.Mutex
	starts  []provider.StartOptions
	prompts []string // text forwarded to any session's Prompt
}

func (p *recordingProvider) ID() provider.ID { return "rec" }
func (p *recordingProvider) Ready() bool     { return true }

func (p *recordingProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	p.mu.Lock()
	p.starts = append(p.starts, opts)
	p.mu.Unlock()
	id := opts.LocalSessionID
	if id == "" {
		id = "auto-" + opts.Name
	}
	s := &recordingSession{id: id, prov: p, events: make(chan event.Event, 8)}
	s.events <- event.Event{Type: event.TypeSessionStatus, SessionID: id, Status: "idle"}
	return s, nil
}

func (p *recordingProvider) startCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.starts)
}

func (p *recordingProvider) lastStart() provider.StartOptions {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.starts[len(p.starts)-1]
}

func (p *recordingProvider) promptCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.prompts)
}

type recordingSession struct {
	id     string
	prov   *recordingProvider
	events chan event.Event
	mu     sync.Mutex
	closed bool
}

func (s *recordingSession) ID() string                   { return s.id }
func (s *recordingSession) ProviderID() provider.ID      { return "rec" }
func (s *recordingSession) AgentSessionID() string       { return s.id }
func (s *recordingSession) Events() <-chan event.Event   { return s.events }
func (s *recordingSession) Cancel(context.Context) error { return nil }

func (s *recordingSession) Prompt(_ context.Context, parts []provider.Content) error {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	s.prov.mu.Lock()
	s.prov.prompts = append(s.prov.prompts, b.String())
	s.prov.mu.Unlock()
	return nil
}

func (s *recordingSession) Close(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.events)
	return nil
}

// eventSink collects broadcast events for assertions.
type eventSink struct {
	mu     sync.Mutex
	events []event.Event
}

func (s *eventSink) handle(ev event.Event) {
	s.mu.Lock()
	s.events = append(s.events, ev)
	s.mu.Unlock()
}

func (s *eventSink) notices() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, ev := range s.events {
		if ev.Type == event.TypeNotice {
			out = append(out, ev.Text)
		}
	}
	return out
}

func (s *eventSink) hasNoticeContaining(sub string) bool {
	for _, n := range s.notices() {
		if strings.Contains(n, sub) {
			return true
		}
	}
	return false
}

func newCmdManager(t *testing.T) (*session.Manager, *recordingProvider, *eventSink, session.Meta) {
	t.Helper()
	p := &recordingProvider{}
	reg := provider.NewRegistry()
	reg.Register(p)
	sink := &eventSink{}
	mgr := session.NewManager(reg, nil, nil, sink.handle)
	meta, err := mgr.Create(context.Background(), "rec", provider.StartOptions{
		Name:           "s",
		LocalSessionID: "sess-1",
		Model:          "base-model",
	}, "dev-a")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return mgr, p, sink, meta
}

func TestHelpEmitsNoticeAndDoesNotPrompt(t *testing.T) {
	mgr, p, sink, meta := newCmdManager(t)
	if err := mgr.Prompt(context.Background(), meta.ID, "/help", "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if p.promptCount() != 0 {
		t.Fatalf("/help must not reach the agent; got %d prompts", p.promptCount())
	}
	if !sink.hasNoticeContaining("/model") || !sink.hasNoticeContaining("/reset") {
		t.Fatalf("help notice missing commands: %v", sink.notices())
	}
}

func TestNormalPromptForwardsToAgent(t *testing.T) {
	mgr, p, _, meta := newCmdManager(t)
	if err := mgr.Prompt(context.Background(), meta.ID, "just a message", "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if p.promptCount() != 1 {
		t.Fatalf("a normal prompt should reach the agent; got %d prompts", p.promptCount())
	}
}

func TestUnknownSlashReportsUnavailableAndDoesNotForward(t *testing.T) {
	mgr, p, sink, meta := newCmdManager(t)
	// The recording agent advertises no commands, so an unknown /command is not
	// forwarded as confusing literal text — it reports as unavailable.
	if err := mgr.Prompt(context.Background(), meta.ID, "/context", "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if p.promptCount() != 0 {
		t.Fatalf("unknown /command must not reach the agent; got %d prompts", p.promptCount())
	}
	if !sink.hasNoticeContaining("isn't available") {
		t.Fatalf("expected an unavailable notice, got: %v", sink.notices())
	}
}

func TestSlashLikePathIsSentAsPrompt(t *testing.T) {
	mgr, p, _, meta := newCmdManager(t)
	// A leading path is not a command name, so it goes to the agent as a prompt.
	if err := mgr.Prompt(context.Background(), meta.ID, "/etc/hosts please check", "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if p.promptCount() != 1 {
		t.Fatalf("a path-like message should be forwarded; got %d prompts", p.promptCount())
	}
}

func TestModelNoArgShowsCurrentWithoutRelaunch(t *testing.T) {
	mgr, p, sink, meta := newCmdManager(t)
	starts := p.startCount()
	if err := mgr.Prompt(context.Background(), meta.ID, "/model", "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if p.startCount() != starts {
		t.Fatalf("bare /model must not relaunch; starts %d -> %d", starts, p.startCount())
	}
	if !sink.hasNoticeContaining("base-model") {
		t.Fatalf("expected current model in notice: %v", sink.notices())
	}
}

func TestModelSwitchRelaunchesWithNewModel(t *testing.T) {
	mgr, p, sink, meta := newCmdManager(t)
	before := p.startCount()
	if err := mgr.Prompt(context.Background(), meta.ID, "/model grok-4", "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if p.startCount() != before+1 {
		t.Fatalf("model switch should start one new agent; starts %d -> %d", before, p.startCount())
	}
	last := p.lastStart()
	if last.Model != "grok-4" || last.LocalSessionID != meta.ID {
		t.Fatalf("relaunch used wrong opts: %+v", last)
	}
	if !sink.hasNoticeContaining("Model is now grok-4") {
		t.Fatalf("expected switch confirmation: %v", sink.notices())
	}
	// The session id is preserved and still live.
	if _, err := mgr.Get(meta.ID); err != nil {
		t.Fatalf("session should still be live after switch: %v", err)
	}
}

func TestResetRelaunchesWithSameModel(t *testing.T) {
	mgr, p, sink, meta := newCmdManager(t)
	before := p.startCount()
	if err := mgr.Prompt(context.Background(), meta.ID, "/reset", "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if p.startCount() != before+1 {
		t.Fatalf("reset should start one new agent; starts %d -> %d", before, p.startCount())
	}
	if last := p.lastStart(); last.Model != "base-model" {
		t.Fatalf("reset must keep the model, got %q", last.Model)
	}
	if !sink.hasNoticeContaining("fresh context") {
		t.Fatalf("expected reset confirmation: %v", sink.notices())
	}
}

func TestNewCreatesSeparateSession(t *testing.T) {
	mgr, p, sink, meta := newCmdManager(t)
	before := p.startCount()
	if err := mgr.Prompt(context.Background(), meta.ID, "/new scratch", "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if p.startCount() != before+1 {
		t.Fatalf("/new should start one new agent; starts %d -> %d", before, p.startCount())
	}
	// Original session stays live and distinct.
	if _, err := mgr.Get(meta.ID); err != nil {
		t.Fatalf("original session should stay live: %v", err)
	}
	live := 0
	for _, m := range mgr.List() {
		if m.Live {
			live++
		}
	}
	if live != 2 {
		t.Fatalf("expected 2 live sessions after /new, got %d", live)
	}
	if !sink.hasNoticeContaining("Started new session") {
		t.Fatalf("expected new-session notice: %v", sink.notices())
	}
}
