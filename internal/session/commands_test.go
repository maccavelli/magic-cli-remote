package session_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// recordingProvider captures the StartOptions of every session it starts, so
// tests can assert what model a relaunch used.
type recordingProvider struct {
	// modes, when set, is advertised by every session it starts (as the real
	// transports do at create) and makes /plan and /mode reachable.
	modes []event.SessionMode

	mu       sync.Mutex
	starts   []provider.StartOptions
	prompts  []string // text forwarded to any session's Prompt
	setModes []string // mode ids passed to SetMode
	live     []*recordingSession
}

func (p *recordingProvider) modeSwitches() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.setModes)
}

// emitCommands makes the agent advertise slash commands, as an ACP
// available_commands_update would.
func (p *recordingProvider) emitCommands(sessionID string, names ...string) {
	cmds := make([]event.AvailableCommand, 0, len(names))
	for _, n := range names {
		cmds = append(cmds, event.AvailableCommand{Name: n})
	}
	p.mu.Lock()
	sessions := slices.Clone(p.live)
	p.mu.Unlock()
	for _, s := range sessions {
		if s.id != sessionID {
			continue
		}
		s.events <- event.Event{
			Type:      event.TypeAvailableCommands,
			SessionID: sessionID,
			Commands:  cmds,
		}
	}
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
	p.mu.Lock()
	p.live = append(p.live, s)
	p.mu.Unlock()
	if len(p.modes) > 0 {
		s.events <- event.Event{
			Type:          event.TypeMode,
			SessionID:     id,
			Modes:         p.modes,
			CurrentModeID: p.modes[0].ID,
		}
	}
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

// SetMode records the switch and echoes it back as the real transports do
// (grok via current_mode_update, OpenCode via the transport's own echo).
func (s *recordingSession) SetMode(_ context.Context, modeID string) error {
	s.prov.mu.Lock()
	s.prov.setModes = append(s.prov.setModes, modeID)
	s.prov.mu.Unlock()
	select {
	case s.events <- event.Event{
		Type:          event.TypeMode,
		SessionID:     s.id,
		CurrentModeID: modeID,
	}:
	default:
	}
	return nil
}

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

// waitForNoticeContaining blocks until a notice event contains sub. Live
// tests need this because daemon notices can arrive after Prompt returns
// (pump goroutine), whereas unit tests often assert synchronously after a
// fake that emits under the call.
func (s *eventSink) waitForNoticeContaining(t *testing.T, sub string) {
	t.Helper()
	waitFor(t, "notice containing "+sub, func() bool {
		return s.hasNoticeContaining(sub)
	})
}

func (s *eventSink) snapshot() []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.events)
}

// waitForMode blocks until a session_mode event reports modeID as current. The
// manager learns the active mode from that event, so this is the point after
// which /plan and /mode see the switch.
func (s *eventSink) waitForMode(t *testing.T, modeID string) {
	t.Helper()
	waitFor(t, "mode "+modeID, func() bool {
		return slices.ContainsFunc(s.snapshot(), func(ev event.Event) bool {
			return ev.Type == event.TypeMode && ev.CurrentModeID == modeID
		})
	})
}

// advertisedCommand returns a canonical command from the most recent
// remote_commands event — what a client would actually render.
func (s *eventSink) advertisedCommand(t *testing.T, name string) (event.RemoteCommand, bool) {
	t.Helper()
	var list []event.RemoteCommand
	for _, ev := range s.snapshot() {
		if ev.Type == event.TypeRemoteCommands {
			list = ev.RemoteCommands
		}
	}
	for _, c := range list {
		if c.Name == name {
			return c, true
		}
	}
	return event.RemoteCommand{}, false
}

// waitForAdvertised blocks until the advertised list marks name with the wanted
// availability. Resolution is re-run on the pump goroutine as the session learns
// about commands, modes and usage, so the answer can arrive after the trigger.
func (s *eventSink) waitForAdvertised(t *testing.T, name string, available bool) {
	t.Helper()
	waitFor(t, "/"+name+" advertised", func() bool {
		c, ok := s.advertisedCommand(t, name)
		return ok && c.Available == available
	})
}

// waitFor polls cond, which must become true within a generous deadline. Used
// for state the manager settles on its pump goroutine (advertised commands,
// modes) rather than synchronously in the op that triggered it.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// waitForEvent blocks until the sink has seen an event of typ. The manager
// updates its per-session bookkeeping (advertised commands, modes) before
// broadcasting, so observing the event here means that state is in place.
func (s *eventSink) waitForEvent(t *testing.T, typ event.Type) {
	t.Helper()
	waitFor(t, string(typ)+" event", func() bool {
		return slices.ContainsFunc(s.snapshot(), func(ev event.Event) bool {
			return ev.Type == typ
		})
	})
}

func newCmdManager(t *testing.T) (*session.Manager, *recordingProvider, *eventSink, session.Meta) {
	t.Helper()
	return newCmdManagerWithModes(t, nil)
}

// newCmdManagerWithModes builds a manager whose agent advertises modes, so the
// mode-backed built-ins (/plan, /mode) are reachable.
func newCmdManagerWithModes(t *testing.T, modes []event.SessionMode) (*session.Manager, *recordingProvider, *eventSink, session.Meta) {
	t.Helper()
	p := &recordingProvider{modes: modes}
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
	if len(modes) > 0 {
		sink.waitForEvent(t, event.TypeMode)
	}
	return mgr, p, sink, meta
}

func TestHelpEmitsNoticeAndDoesNotPrompt(t *testing.T) {
	mgr, p, sink, meta := newCmdManager(t)
	if err := mgr.Prompt(context.Background(), meta.ID, "/help", nil, "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if p.promptCount() != 0 {
		t.Fatalf("/help must not reach the agent; got %d prompts", p.promptCount())
	}
	if !sink.hasNoticeContaining("/model") || !sink.hasNoticeContaining("/clear") {
		t.Fatalf("help notice missing commands: %v", sink.notices())
	}
}

func TestNormalPromptForwardsToAgent(t *testing.T) {
	mgr, p, _, meta := newCmdManager(t)
	if err := mgr.Prompt(context.Background(), meta.ID, "just a message", nil, "dev-a"); err != nil {
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
	if err := mgr.Prompt(context.Background(), meta.ID, "/context", nil, "dev-a"); err != nil {
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
	if err := mgr.Prompt(context.Background(), meta.ID, "/etc/hosts please check", nil, "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if p.promptCount() != 1 {
		t.Fatalf("a path-like message should be forwarded; got %d prompts", p.promptCount())
	}
}

func TestModelNoArgShowsCurrentWithoutRelaunch(t *testing.T) {
	mgr, p, sink, meta := newCmdManager(t)
	starts := p.startCount()
	if err := mgr.Prompt(context.Background(), meta.ID, "/model", nil, "dev-a"); err != nil {
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
	if err := mgr.Prompt(context.Background(), meta.ID, "/model grok-4", nil, "dev-a"); err != nil {
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
	if err := mgr.Prompt(context.Background(), meta.ID, "/reset", nil, "dev-a"); err != nil {
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
	if err := mgr.Prompt(context.Background(), meta.ID, "/new scratch", nil, "dev-a"); err != nil {
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

func TestNewInheritsCurrentModel(t *testing.T) {
	mgr, p, _, meta := newCmdManager(t)
	// The parent session runs on "base-model" (see newCmdManager). /new must
	// carry that model into the new session's StartOptions rather than dropping
	// it and silently falling back to the provider default.
	if err := mgr.Prompt(context.Background(), meta.ID, "/new scratch", nil, "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	last := p.lastStart()
	if last.LocalSessionID == meta.ID {
		t.Fatalf("/new must start a fresh id, not reuse %q", meta.ID)
	}
	if last.Model != "base-model" {
		t.Fatalf("/new dropped the model: got %q, want %q", last.Model, "base-model")
	}
}
