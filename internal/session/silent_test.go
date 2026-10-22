package session

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// silentProvider is an agent whose commands are scripted (MADR 0208 D10):
// /quiet and /loop finish their turn with bookkeeping only, /loud replies, and
// /modeonly only switches the mode. Its engine version can change mid-test.
type silentProvider struct {
	mu      sync.Mutex
	version string
	live    []*silentSession
}

func (p *silentProvider) ID() provider.ID { return "sil" }
func (p *silentProvider) Ready() bool     { return true }

func (p *silentProvider) EngineVersion() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.version
}

func (p *silentProvider) setVersion(v string) {
	p.mu.Lock()
	p.version = v
	p.mu.Unlock()
}

func (p *silentProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	s := &silentSession{id: opts.LocalSessionID, events: make(chan event.Event, 32)}
	p.mu.Lock()
	p.live = append(p.live, s)
	p.mu.Unlock()
	s.advertise()
	s.events <- event.Event{Type: event.TypeSessionStatus, SessionID: s.id, Status: "idle"}
	return s, nil
}

type silentSession struct {
	id     string
	events chan event.Event
	mu     sync.Mutex
	closed bool
}

func (s *silentSession) ID() string                   { return s.id }
func (s *silentSession) ProviderID() provider.ID      { return "sil" }
func (s *silentSession) AgentSessionID() string       { return s.id }
func (s *silentSession) Events() <-chan event.Event   { return s.events }
func (s *silentSession) Cancel(context.Context) error { return nil }

func (s *silentSession) advertise() {
	s.events <- event.Event{Type: event.TypeAvailableCommands, SessionID: s.id, Commands: []event.AvailableCommand{
		{Name: "quiet"}, {Name: "loud"}, {Name: "loop"}, {Name: "modeonly"},
	}}
}

func (s *silentSession) Prompt(_ context.Context, parts []provider.Content) error {
	text := parts[0].Text
	go func() {
		s.events <- event.Event{Type: event.TypeSessionStatus, SessionID: s.id, Status: "running"}
		s.events <- event.Event{Type: event.TypeUserMessage, SessionID: s.id, Text: text}
		switch {
		case strings.HasPrefix(text, "/loud"):
			s.events <- event.Event{Type: event.TypeAssistantChunk, SessionID: s.id, Text: "done"}
		case strings.HasPrefix(text, "/modeonly"):
			s.events <- event.Event{Type: event.TypeMode, SessionID: s.id, CurrentModeID: "plan"}
		default: // /quiet, /loop: bookkeeping only
			s.events <- event.Event{Type: event.TypeUsage, SessionID: s.id, Usage: &event.Usage{}}
		}
		s.events <- event.Event{Type: event.TypeTurnComplete, SessionID: s.id, StopReason: "end_turn"}
		s.events <- event.Event{Type: event.TypeSessionStatus, SessionID: s.id, Status: "idle"}
	}()
	return nil
}

func (s *silentSession) Close(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.events)
	}
	return nil
}

// silentSink records broadcast events.
type silentSink struct {
	mu     sync.Mutex
	events []event.Event
}

func (k *silentSink) handle(ev event.Event) {
	k.mu.Lock()
	k.events = append(k.events, ev)
	k.mu.Unlock()
}

func (k *silentSink) snapshot() []event.Event {
	k.mu.Lock()
	defer k.mu.Unlock()
	return slices.Clone(k.events)
}

func (k *silentSink) count(typ event.Type) int {
	n := 0
	for _, ev := range k.snapshot() {
		if ev.Type == typ {
			n++
		}
	}
	return n
}

// advertised returns the latest remote_commands entry for name.
func (k *silentSink) advertised(name string) (event.RemoteCommand, bool) {
	var list []event.RemoteCommand
	for _, ev := range k.snapshot() {
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

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// newSilentManager starts one "sil" session on a manager with a store in dir,
// on host "host-a".
func newSilentManager(t *testing.T, dir string) (*Manager, *silentProvider, *silentSink, string) {
	t.Helper()
	old := hostname
	hostname = func() (string, error) { return "host-a", nil }
	t.Cleanup(func() { hostname = old })
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := &silentProvider{version: "1.0"}
	reg := provider.NewRegistry()
	reg.Register(p)
	sink := &silentSink{}
	m := NewManager(reg, store, nil, sink.handle)
	t.Cleanup(func() { m.CloseAll(context.Background()) })
	meta, err := m.Create(context.Background(), "sil", provider.StartOptions{Name: "s", LocalSessionID: "sess-1"}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "commands advertised", func() bool { _, ok := sink.advertised("loop"); return ok })
	return m, p, sink, meta.ID
}

// run sends text and waits for its turn to end.
func run(t *testing.T, m *Manager, sink *silentSink, id, text string) {
	t.Helper()
	before := sink.count(event.TypeTurnComplete)
	if err := m.Prompt(context.Background(), id, text, nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, text+" turn", func() bool { return sink.count(event.TypeTurnComplete) > before })
}

func noticeContaining(sink *silentSink, sub string) bool {
	return slices.ContainsFunc(sink.snapshot(), func(ev event.Event) bool {
		return ev.Type == event.TypeNotice && strings.Contains(ev.Text, sub)
	})
}

// TestSilentCommandIsMarked: a forwarded command whose turn ends with
// bookkeeping only is marked for its provider, version and host, and the
// phone is told.
func TestSilentCommandIsMarked(t *testing.T) {
	dir := t.TempDir()
	m, _, sink, id := newSilentManager(t, dir)
	run(t, m, sink, id, "/quiet")
	waitUntil(t, "silent notice", func() bool { return noticeContaining(sink, "“/quiet” produced nothing") })

	marks, err := LoadSilentMarks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != 1 || marks[0].Provider != "sil" || marks[0].Version != "1.0" ||
		marks[0].Host != "host-a" || marks[0].Command != "quiet" {
		t.Fatalf("marks = %+v, want one sil/1.0/host-a/quiet", marks)
	}
}

// TestCommandsWithOutputAreNotMarked: a reply, or a mode switch and nothing
// else, is output; neither command is marked.
func TestCommandsWithOutputAreNotMarked(t *testing.T) {
	dir := t.TempDir()
	m, _, sink, id := newSilentManager(t, dir)
	run(t, m, sink, id, "/loud")
	run(t, m, sink, id, "/modeonly")
	time.Sleep(50 * time.Millisecond) // a wrong mark would be written after the turn's end
	if noticeContaining(sink, "produced nothing") {
		t.Fatal("a command with output was reported silent")
	}
	if marks, _ := LoadSilentMarks(dir); len(marks) != 0 {
		t.Fatalf("marks = %+v, want none", marks)
	}
}

// TestSilentMarkMakesTheCanonicalCommandUnavailable: /loop forwards to the
// agent's own /loop; once that is silent, remote_commands shows /loop
// unavailable with the silent reason. A changed agent version clears it.
func TestSilentMarkMakesTheCanonicalCommandUnavailable(t *testing.T) {
	m, p, sink, id := newSilentManager(t, t.TempDir())
	if c, _ := sink.advertised("loop"); !c.Available {
		t.Fatalf("/loop unavailable before any mark: %+v", c)
	}
	run(t, m, sink, id, "/loop")
	waitUntil(t, "/loop unavailable", func() bool {
		c, _ := sink.advertised("loop")
		return !c.Available && c.Reason == command.ReasonSilent
	})

	p.setVersion("2.0")
	m.advertiseCommands(id)
	waitUntil(t, "/loop available on a new version", func() bool {
		c, _ := sink.advertised("loop")
		return c.Available
	})
}

// TestMarkFromAnotherHostIsIgnored: a data directory copied from another host
// brings its marks along; they do not apply here.
func TestMarkFromAnotherHostIsIgnored(t *testing.T) {
	dir := t.TempDir()
	b, err := json.Marshal([]SilentMark{{Provider: "sil", Version: "1.0", Host: "host-b", Command: "loop"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SilentMarksPath(dir), b, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, sink, _ := newSilentManager(t, dir)
	if c, _ := sink.advertised("loop"); !c.Available {
		t.Fatalf("/loop was made unavailable by another host's mark: %+v", c)
	}
}
