package session_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

type fastProvider struct {
	mu       sync.Mutex
	hasFast  bool
	supports bool
	nextTurn bool
	fail     error
	setFast  []bool
	setPers  []string
}

func (p *fastProvider) ID() provider.ID { return "fastie" }
func (p *fastProvider) Ready() bool     { return true }
func (p *fastProvider) CommandTable() command.Table {
	return command.Table{
		"fast":        {Kind: command.KindOp, Op: command.OpServiceTier},
		"personality": {Kind: command.KindOp, Op: command.OpPersonality},
		"model":       {Kind: command.KindOp, Op: command.OpSetModel},
	}
}
func (p *fastProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	id := opts.LocalSessionID
	if id == "" {
		id = "fast-1"
	}
	s := &fastSession{
		id: id, p: p, events: make(chan event.Event, 8),
		model: opts.Model, tier: opts.ServiceTier, persona: opts.Personality,
	}
	s.events <- event.Event{Type: event.TypeSessionStatus, SessionID: id, Status: "idle"}
	return s, nil
}

type fastSession struct {
	id, model, tier, persona string
	p                        *fastProvider
	events                   chan event.Event
}

func (s *fastSession) ID() string                 { return s.id }
func (s *fastSession) ProviderID() provider.ID    { return "fastie" }
func (s *fastSession) AgentSessionID() string     { return s.id }
func (s *fastSession) Events() <-chan event.Event { return s.events }
func (s *fastSession) Prompt(context.Context, []provider.Content) error {
	return nil
}
func (s *fastSession) Cancel(context.Context) error { return nil }
func (s *fastSession) Close(context.Context) error  { return nil }
func (s *fastSession) HasFast() bool                { return s.p.hasFast }
func (s *fastSession) ServiceTier() string          { return s.tier }
func (s *fastSession) SetServiceTier(_ context.Context, on bool) error {
	s.p.mu.Lock()
	s.p.setFast = append(s.p.setFast, on)
	fail, next := s.p.fail, s.p.nextTurn
	s.p.mu.Unlock()
	if fail != nil {
		return fail
	}
	if on {
		s.tier = "priority"
	} else {
		s.tier = ""
	}
	if next {
		return provider.ErrAppliesNextTurn
	}
	return nil
}
func (s *fastSession) PersonalitySupported() bool { return s.p.supports }
func (s *fastSession) Personality() string        { return s.persona }
func (s *fastSession) SetPersonality(_ context.Context, value string) error {
	s.p.mu.Lock()
	s.p.setPers = append(s.p.setPers, value)
	fail, next := s.p.fail, s.p.nextTurn
	s.p.mu.Unlock()
	if fail != nil {
		return fail
	}
	s.persona = value
	if next {
		return provider.ErrAppliesNextTurn
	}
	return nil
}
func (s *fastSession) SetModel(_ context.Context, model string) error {
	s.model = model
	if model != "gpt-5.5" {
		s.persona = ""
		s.p.supports = false
	} else {
		s.p.supports = true
	}
	if model != "gpt-5.6-sol" {
		s.tier = ""
		s.p.hasFast = false
	} else {
		s.p.hasFast = true
	}
	return nil
}

func newFastManager(t *testing.T, p *fastProvider) (*session.Manager, *eventSink, session.Meta) {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register(p)
	sink := &eventSink{}
	mgr := session.NewManager(reg, nil, nil, sink.handle)
	meta, err := mgr.Create(context.Background(), "fastie", provider.StartOptions{
		Name: "s", LocalSessionID: "sess-f", Model: "gpt-5.6-sol",
	}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	return mgr, sink, meta
}

func TestFastToggleOnOffIdempotent(t *testing.T) {
	p := &fastProvider{hasFast: true}
	mgr, sink, meta := newFastManager(t, p)
	if err := mgr.Prompt(context.Background(), meta.ID, "/fast", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if !sink.hasNoticeContaining("Fast is on") {
		t.Fatalf("notices = %v", sink.notices())
	}
	if err := mgr.Prompt(context.Background(), meta.ID, "/fast on", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if !sink.hasNoticeContaining("already on") {
		t.Fatalf("repeat on: %v", sink.notices())
	}
	if err := mgr.Prompt(context.Background(), meta.ID, "/fast off", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if !sink.hasNoticeContaining("Fast is off") {
		t.Fatalf("off: %v", sink.notices())
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.setFast) != 2 || p.setFast[0] != true || p.setFast[1] != false {
		t.Fatalf("setFast = %v", p.setFast)
	}
}

func TestFastAppliesNextTurn(t *testing.T) {
	p := &fastProvider{hasFast: true, nextTurn: true}
	mgr, sink, meta := newFastManager(t, p)
	if err := mgr.Prompt(context.Background(), meta.ID, "/fast on", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if !sink.hasNoticeContaining("applies next turn") {
		t.Fatalf("notices = %v", sink.notices())
	}
}

func TestFastUnsupportedAndFailure(t *testing.T) {
	p := &fastProvider{hasFast: false}
	mgr, sink, meta := newFastManager(t, p)
	if err := mgr.Prompt(context.Background(), meta.ID, "/fast on", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if !sink.hasNoticeContaining("isn't available") && !sink.hasNoticeContaining("no Fast") {
		t.Fatalf("unsupported: %v", sink.notices())
	}
	p.hasFast = true
	p.fail = errors.New("engine down")
	mgr2, sink2, meta2 := newFastManager(t, p)
	if err := mgr2.Prompt(context.Background(), meta2.ID, "/fast on", nil, "dev-a"); err == nil {
		t.Fatal("provider failure must surface")
	}
	if !sink2.hasNoticeContaining("Fast switch failed") {
		t.Fatalf("fail notices = %v", sink2.notices())
	}
}

func TestPersonalityListSetAndNoneEnum(t *testing.T) {
	p := &fastProvider{supports: true}
	mgr, sink, meta := newFastManager(t, p)
	if err := mgr.Prompt(context.Background(), meta.ID, "/personality", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if !sink.hasNoticeContaining("provider default") {
		t.Fatalf("list = %v", sink.notices())
	}
	if err := mgr.Prompt(context.Background(), meta.ID, "/personality default", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if !sink.hasNoticeContaining("Usage:") {
		t.Fatalf("default alias: %v", sink.notices())
	}
	if err := mgr.Prompt(context.Background(), meta.ID, "/personality none", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	got := append([]string(nil), p.setPers...)
	p.mu.Unlock()
	if len(got) != 1 || got[0] != "none" {
		t.Fatalf("must send enum none, got %v", got)
	}
	if !sink.hasNoticeContaining("Personality is now none") {
		t.Fatalf("notices = %v", sink.notices())
	}
}

func TestModelSwitchReadvertisesFastPersonality(t *testing.T) {
	p := &fastProvider{hasFast: true, supports: false}
	mgr, sink, meta := newFastManager(t, p)
	sink.waitForAdvertised(t, "fast", true)
	if c, ok := sink.advertisedCommand(t, "personality"); !ok || c.Available {
		t.Fatalf("personality should start unavailable: %+v", c)
	}
	if err := mgr.Prompt(context.Background(), meta.ID, "/model gpt-5.5", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	sink.waitForAdvertised(t, "fast", false)
	sink.waitForAdvertised(t, "personality", true)
	n := 0
	for _, ev := range sink.snapshot() {
		if ev.Type == event.TypeRemoteCommands {
			n++
		}
	}
	if n < 2 {
		t.Fatalf("want a changed remote_commands after model switch, got %d", n)
	}
}
