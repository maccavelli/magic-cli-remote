package session_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

type goalProvider struct {
	mu       sync.Mutex
	calls    []provider.GoalMutation
	goal     provider.Goal
	present  bool
	fail     error
	deferExp bool
}

func (p *goalProvider) ID() provider.ID { return "goaler" }
func (p *goalProvider) Ready() bool     { return true }
func (p *goalProvider) CommandTable() command.Table {
	return command.Table{
		"goal": {Kind: command.KindOp, Op: command.OpGoal},
		"plan": {Kind: command.KindCollaborationMode, ModeID: "plan"},
		"fork": {Kind: command.KindOp, Op: command.OpFork},
	}
}
func (p *goalProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	id := opts.LocalSessionID
	if id == "" {
		id = "g1"
	}
	s := &goalSession{id: id, p: p, events: make(chan event.Event, 8), collab: "default"}
	s.events <- event.Event{
		Type: event.TypeCollaboration, SessionID: id,
		CollaborationModes:         []event.CollaborationMode{{ID: "plan"}, {ID: "default"}},
		CurrentCollaborationModeID: "default",
	}
	s.events <- event.Event{Type: event.TypeSessionStatus, SessionID: id, Status: "idle"}
	return s, nil
}

type goalSession struct {
	id     string
	p      *goalProvider
	events chan event.Event
	collab string
}

func (s *goalSession) ID() string                 { return s.id }
func (s *goalSession) ProviderID() provider.ID    { return "goaler" }
func (s *goalSession) AgentSessionID() string     { return s.id }
func (s *goalSession) Events() <-chan event.Event { return s.events }
func (s *goalSession) Prompt(context.Context, []provider.Content) error {
	return nil
}
func (s *goalSession) Cancel(context.Context) error { return nil }
func (s *goalSession) Close(context.Context) error  { return nil }
func (s *goalSession) CurrentGoal() (provider.Goal, bool) {
	return s.p.goal, s.p.present
}
func (s *goalSession) ApplyGoal(_ context.Context, mut provider.GoalMutation) (provider.Goal, error) {
	s.p.mu.Lock()
	s.p.calls = append(s.p.calls, mut)
	fail := s.p.fail
	s.p.mu.Unlock()
	if fail != nil {
		return provider.Goal{}, fail
	}
	switch mut.Kind {
	case provider.GoalClear:
		s.p.present = false
		s.p.goal = provider.Goal{}
		return provider.Goal{}, nil
	case provider.GoalPause:
		s.p.goal.Status = provider.GoalStatusPaused
	case provider.GoalResume, provider.GoalReplace:
		if mut.Objective != "" {
			s.p.goal.Objective = mut.Objective
		}
		s.p.goal.Status = provider.GoalStatusActive
		s.p.present = true
	case provider.GoalEdit:
		s.p.goal.Objective = mut.Objective
		s.p.present = true
	}
	return s.p.goal, nil
}
func (s *goalSession) HydrateGoal(context.Context) error { return nil }
func (s *goalSession) CollaborationModes() ([]provider.CollaborationMode, string, error) {
	return []provider.CollaborationMode{{ID: "plan"}, {ID: "default"}}, s.collab, nil
}
func (s *goalSession) SetCollaborationMode(_ context.Context, modeID string) error {
	s.collab = modeID
	return nil
}
func (s *goalSession) Fork(_ context.Context, opts provider.ForkOptions) (provider.ForkResult, error) {
	if opts.DeferGoalContinuation && !s.p.deferExp {
		return provider.ForkResult{}, fmtDefer()
	}
	return provider.ForkResult{AgentSessionID: "child", ForkedFromID: s.id}, nil
}

func fmtDefer() error { return errDefer }

var errDefer = errStr("defer goal continuation requires experimental API")

type errStr string

func (e errStr) Error() string { return string(e) }

func newGoalManager(t *testing.T, p *goalProvider) (*session.Manager, *eventSink, session.Meta) {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register(p)
	sink := &eventSink{}
	mgr := session.NewManager(reg, nil, nil, sink.handle)
	meta, err := mgr.Create(context.Background(), "goaler", provider.StartOptions{
		Name: "s", LocalSessionID: "sess-g",
	}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	sink.waitForEvent(t, event.TypeCollaboration)
	return mgr, sink, meta
}

func TestGoalGrammarAndLimits(t *testing.T) {
	p := &goalProvider{}
	mgr, sink, meta := newGoalManager(t, p)
	if err := mgr.Prompt(context.Background(), meta.ID, "/goal", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if !sink.hasNoticeContaining("No goal is set") {
		t.Fatalf("%v", sink.notices())
	}
	long := string(make([]rune, 4001))
	for i := range []rune(long) {
		_ = i
	}
	obj := make([]rune, 4001)
	for i := range obj {
		obj[i] = 'é'
	}
	if utf8.RuneCountInString(string(obj)) != 4001 {
		t.Fatal("fixture")
	}
	if err := mgr.Prompt(context.Background(), meta.ID, "/goal "+string(obj), nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if !sink.hasNoticeContaining("Usage:") {
		t.Fatalf("4001: %v", sink.notices())
	}
}

func TestGoalPlanMatrixViaCommands(t *testing.T) {
	p := &goalProvider{present: true, goal: provider.Goal{Objective: "x", Status: provider.GoalStatusActive}}
	mgr, sink, meta := newGoalManager(t, p)
	if err := mgr.Prompt(context.Background(), meta.ID, "/plan", nil, "dev-a"); err == nil {
		t.Fatal("active goal must block Plan")
	}
	if !sink.hasNoticeContaining("Pause or clear") {
		t.Fatalf("%v", sink.notices())
	}
}

func TestForkActiveGoalWithoutExperimental(t *testing.T) {
	p := &goalProvider{present: true, goal: provider.Goal{Objective: "x", Status: provider.GoalStatusActive}}
	mgr, _, meta := newGoalManager(t, p)
	_, err := mgr.Fork(context.Background(), meta.ID, "", "dev-a")
	if err == nil {
		t.Fatal("must reject")
	}
	if !strings.Contains(err.Error(), "pause or clear") {
		t.Fatalf("err = %v", err)
	}
}
