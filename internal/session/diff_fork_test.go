package session_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

type forkProvider struct {
	mu     sync.Mutex
	starts []provider.StartOptions
	forks  []provider.ForkOptions
	n      atomic.Int64
}

func (p *forkProvider) ID() provider.ID { return "forker" }
func (p *forkProvider) Ready() bool     { return true }
func (p *forkProvider) CommandTable() command.Table {
	return command.Table{
		"fork": {Kind: command.KindOp, Op: command.OpFork},
	}
}
func (p *forkProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	p.mu.Lock()
	p.starts = append(p.starts, opts)
	p.mu.Unlock()
	id := opts.LocalSessionID
	if id == "" {
		id = "local-" + strconv.Itoa(int(p.n.Add(1)))
	}
	agent := opts.AgentSessionID
	if agent == "" {
		agent = "agent-" + id
	}
	s := &forkSession{
		id:     id,
		agent:  agent,
		opts:   opts,
		prov:   p,
		events: make(chan event.Event, 8),
	}
	s.events <- event.Event{Type: event.TypeSessionStatus, SessionID: id, Status: "idle"}
	return s, nil
}

type forkSession struct {
	id     string
	agent  string
	opts   provider.StartOptions
	prov   *forkProvider
	events chan event.Event
}

func (s *forkSession) ID() string                 { return s.id }
func (s *forkSession) ProviderID() provider.ID    { return "forker" }
func (s *forkSession) AgentSessionID() string     { return s.agent }
func (s *forkSession) CWD() string                { return s.opts.CWD }
func (s *forkSession) Events() <-chan event.Event { return s.events }
func (s *forkSession) Prompt(context.Context, []provider.Content) error {
	return nil
}
func (s *forkSession) Cancel(context.Context) error { return nil }
func (s *forkSession) Close(context.Context) error  { return nil }
func (s *forkSession) Fork(_ context.Context, opts provider.ForkOptions) (provider.ForkResult, error) {
	s.prov.mu.Lock()
	s.prov.forks = append(s.prov.forks, opts)
	s.prov.mu.Unlock()
	return provider.ForkResult{AgentSessionID: "child-agent", ForkedFromID: s.agent}, nil
}

func TestManagerForkInheritsSettingsAndNotices(t *testing.T) {
	p := &forkProvider{}
	reg := provider.NewRegistry()
	reg.Register(p)
	sink := &eventSink{}
	mgr := session.NewManager(reg, nil, nil, sink.handle)
	src, err := mgr.Create(context.Background(), "forker", provider.StartOptions{
		Name:                "work",
		CWD:                 "/tmp/repo",
		Model:               "gpt-test",
		ThinkingLevel:       "high",
		ModeID:              "auto",
		CollaborationModeID: "plan",
		ServiceTier:         "priority",
		Personality:         "friendly",
		LocalSessionID:      "src-1",
	}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}

	if err := mgr.Prompt(context.Background(), src.ID, "/fork", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if !sink.hasNoticeContaining("Forked to session") || !sink.hasNoticeContaining("(work (fork))") {
		t.Fatalf("notices = %v", sink.notices())
	}

	var child session.Meta
	for _, m := range mgr.ListFor("dev-a") {
		if m.ID != src.ID {
			child = m
		}
	}
	if child.ID == "" {
		t.Fatal("child session not registered")
	}
	if child.OwnerDeviceID != "dev-a" {
		t.Fatalf("owner = %q", child.OwnerDeviceID)
	}
	if child.CWD != "/tmp/repo" || child.Model != "gpt-test" || child.ThinkingLevel != "high" ||
		child.ModeID != "auto" || child.CollaborationModeID != "plan" ||
		child.ServiceTier != "priority" || child.Personality != "friendly" {
		t.Fatalf("child = %+v", child)
	}
	if child.AgentSessionID != "child-agent" {
		t.Fatalf("agent = %q", child.AgentSessionID)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.forks) != 1 || p.forks[0].LastTurnID != "" || p.forks[0].DeferGoalContinuation {
		t.Fatalf("forks = %+v", p.forks)
	}
}

func TestManagerForkRejectsOtherDevice(t *testing.T) {
	p := &forkProvider{}
	reg := provider.NewRegistry()
	reg.Register(p)
	mgr := session.NewManager(reg, nil, nil, nil)
	src, err := mgr.Create(context.Background(), "forker", provider.StartOptions{
		Name: "work", LocalSessionID: "src-2",
	}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.Fork(context.Background(), src.ID, "", "dev-b")
	if err == nil {
		t.Fatal("other device must not fork")
	}
	if errors.Is(err, provider.ErrForkNothing) {
		t.Fatalf("wrong error: %v", err)
	}
}
