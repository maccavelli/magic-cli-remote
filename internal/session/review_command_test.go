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

type reviewProvider struct {
	mu      sync.Mutex
	targets []provider.ReviewTarget
	busy    bool
	fail    error
}

func (p *reviewProvider) ID() provider.ID { return "reviewer" }
func (p *reviewProvider) Ready() bool     { return true }
func (p *reviewProvider) CommandTable() command.Table {
	return command.Table{"review": {Kind: command.KindOp, Op: command.OpReview}}
}
func (p *reviewProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	id := opts.LocalSessionID
	if id == "" {
		id = "r1"
	}
	s := &reviewSession{id: id, p: p, events: make(chan event.Event, 8)}
	s.events <- event.Event{Type: event.TypeSessionStatus, SessionID: id, Status: "idle"}
	return s, nil
}

type reviewSession struct {
	id     string
	p      *reviewProvider
	events chan event.Event
}

func (s *reviewSession) ID() string                 { return s.id }
func (s *reviewSession) ProviderID() provider.ID    { return "reviewer" }
func (s *reviewSession) AgentSessionID() string     { return s.id }
func (s *reviewSession) Events() <-chan event.Event { return s.events }
func (s *reviewSession) Prompt(context.Context, []provider.Content) error {
	return nil
}
func (s *reviewSession) Cancel(context.Context) error { return nil }
func (s *reviewSession) Close(context.Context) error  { return nil }
func (s *reviewSession) StartReview(_ context.Context, target provider.ReviewTarget) error {
	s.p.mu.Lock()
	defer s.p.mu.Unlock()
	if s.p.busy {
		return provider.ErrTurnBusy
	}
	if s.p.fail != nil {
		return s.p.fail
	}
	s.p.targets = append(s.p.targets, target)
	s.p.busy = true
	s.events <- event.Event{Type: event.TypeNotice, SessionID: s.id, Text: "Entered review mode"}
	return nil
}

func TestReviewCommandGrammar(t *testing.T) {
	p := &reviewProvider{}
	reg := provider.NewRegistry()
	reg.Register(p)
	sink := &eventSink{}
	mgr := session.NewManager(reg, nil, nil, sink.handle)
	meta, err := mgr.Create(context.Background(), "reviewer", provider.StartOptions{
		Name: "s", LocalSessionID: "sess-r",
	}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Prompt(context.Background(), meta.ID, "/review", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	p.busy = false
	if err := mgr.Prompt(context.Background(), meta.ID, "/review base", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if !sink.hasNoticeContaining("Usage:") {
		t.Fatalf("empty base: %v", sink.notices())
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.targets) != 1 || p.targets[0].Kind != provider.ReviewUncommitted {
		t.Fatalf("targets = %+v", p.targets)
	}
}

func TestReviewBusyNotice(t *testing.T) {
	p := &reviewProvider{busy: true}
	reg := provider.NewRegistry()
	reg.Register(p)
	sink := &eventSink{}
	mgr := session.NewManager(reg, nil, nil, sink.handle)
	meta, err := mgr.Create(context.Background(), "reviewer", provider.StartOptions{
		LocalSessionID: "sess-r2",
	}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	err = mgr.Prompt(context.Background(), meta.ID, "/review uncommitted", nil, "dev-a")
	if !errors.Is(err, provider.ErrTurnBusy) {
		t.Fatalf("err = %v", err)
	}
	if !sink.hasNoticeContaining("Can't start a review") {
		t.Fatalf("%v", sink.notices())
	}
}
