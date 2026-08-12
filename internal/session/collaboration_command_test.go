package session_test

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/command"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

type collabProvider struct {
	mu        sync.Mutex
	setCollab []string
	prompts   [][]provider.Content
	live      []*collabSession
}

func (p *collabProvider) ID() provider.ID { return "collab" }
func (p *collabProvider) Ready() bool     { return true }
func (p *collabProvider) CommandTable() command.Table {
	return command.Table{
		"plan":        {Kind: command.KindCollaborationMode, ModeID: "plan"},
		"mode":        {Kind: command.KindMode},
		"permissions": {Kind: command.KindMode},
	}
}

func (p *collabProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	id := opts.LocalSessionID
	if id == "" {
		id = "c1"
	}
	s := &collabSession{
		id:      id,
		prov:    p,
		events:  make(chan event.Event, 16),
		current: "default",
	}
	p.mu.Lock()
	p.live = append(p.live, s)
	p.mu.Unlock()
	s.events <- event.Event{
		Type: event.TypeMode, SessionID: id,
		Modes:         []event.SessionMode{{ID: "default"}, {ID: "read-only"}, {ID: "auto"}},
		CurrentModeID: "default",
	}
	s.events <- event.Event{
		Type: event.TypeCollaboration, SessionID: id,
		CollaborationModes: []event.CollaborationMode{
			{ID: "plan", Name: "Plan"},
			{ID: "default", Name: "Default"},
		},
		CurrentCollaborationModeID: "default",
	}
	s.events <- event.Event{Type: event.TypeSessionStatus, SessionID: id, Status: "idle"}
	return s, nil
}

type collabSession struct {
	id      string
	prov    *collabProvider
	events  chan event.Event
	mu      sync.Mutex
	current string
	closed  bool
}

func (s *collabSession) ID() string                   { return s.id }
func (s *collabSession) ProviderID() provider.ID      { return "collab" }
func (s *collabSession) AgentSessionID() string       { return s.id }
func (s *collabSession) Events() <-chan event.Event   { return s.events }
func (s *collabSession) Cancel(context.Context) error { return nil }
func (s *collabSession) SetMode(context.Context, string) error {
	return nil
}
func (s *collabSession) CollaborationModes() ([]provider.CollaborationMode, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return []provider.CollaborationMode{{ID: "plan", Name: "Plan"}, {ID: "default", Name: "Default"}}, s.current, nil
}
func (s *collabSession) SetCollaborationMode(_ context.Context, modeID string) error {
	s.mu.Lock()
	s.current = modeID
	s.mu.Unlock()
	s.prov.mu.Lock()
	s.prov.setCollab = append(s.prov.setCollab, modeID)
	s.prov.mu.Unlock()
	s.events <- event.Event{
		Type: event.TypeCollaboration, SessionID: s.id,
		CurrentCollaborationModeID: modeID,
	}
	return nil
}
func (s *collabSession) Prompt(_ context.Context, parts []provider.Content) error {
	s.prov.mu.Lock()
	s.prov.prompts = append(s.prov.prompts, parts)
	s.prov.mu.Unlock()
	return nil
}
func (s *collabSession) Close(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.events)
	}
	return nil
}

func newCollabManager(t *testing.T) (*session.Manager, *collabProvider, *eventSink, session.Meta) {
	t.Helper()
	p := &collabProvider{}
	reg := provider.NewRegistry()
	reg.Register(p)
	sink := &eventSink{}
	mgr := session.NewManager(reg, nil, nil, sink.handle)
	meta, err := mgr.Create(context.Background(), "collab", provider.StartOptions{
		Name: "s", LocalSessionID: "sess-c",
	}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	sink.waitForEvent(t, event.TypeCollaboration)
	return mgr, p, sink, meta
}

func waitForCollab(t *testing.T, sink *eventSink, id string) {
	t.Helper()
	waitFor(t, "collaboration "+id, func() bool {
		return slices.ContainsFunc(sink.snapshot(), func(ev event.Event) bool {
			return ev.Type == event.TypeCollaboration && ev.CurrentCollaborationModeID == id
		})
	})
}

func TestCollaborationPlanBareAndOff(t *testing.T) {
	mgr, p, sink, meta := newCollabManager(t)
	if err := mgr.Prompt(context.Background(), meta.ID, "/plan", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	waitForCollab(t, sink, "plan")
	if got := slices.Clone(p.setCollab); !slices.Equal(got, []string{"plan"}) {
		t.Fatalf("set = %v", got)
	}
	if !sink.hasNoticeContaining("Plan collaboration on") {
		t.Fatalf("notices = %v", sink.notices())
	}
	if err := mgr.Prompt(context.Background(), meta.ID, "/plan off leftover", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if len(p.setCollab) != 1 {
		t.Fatalf("leftover after off must not switch: %v", p.setCollab)
	}
	if err := mgr.Prompt(context.Background(), meta.ID, "/plan off", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if got := p.setCollab; !slices.Equal(got, []string{"plan", "default"}) {
		t.Fatalf("set = %v", got)
	}
}

func TestCollaborationPlanInlinePromptOnce(t *testing.T) {
	mgr, p, sink, meta := newCollabManager(t)
	img := []provider.Content{{Type: "image", MimeType: "image/png", Data: "abc"}}
	if err := mgr.Prompt(context.Background(), meta.ID, "/plan explain the migration", img, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.setCollab, []string{"plan"}) {
		t.Fatalf("set = %v", p.setCollab)
	}
	if len(p.prompts) != 1 {
		t.Fatalf("prompts = %d", len(p.prompts))
	}
	if p.prompts[0][0].Text != "explain the migration" {
		t.Fatalf("text = %#v", p.prompts[0])
	}
	if len(p.prompts[0]) != 2 || p.prompts[0][1].Type != "image" {
		t.Fatalf("attachments dropped: %#v", p.prompts[0])
	}
	texts := 0
	for _, ev := range sink.snapshot() {
		if ev.Type == event.TypeUserMessage && ev.Text == "explain the migration" {
			texts++
		}
	}
	if texts != 1 {
		t.Fatalf("user echoes = %d", texts)
	}
}

func TestCollaborationPlanAlreadyOnSubmitsRemainder(t *testing.T) {
	mgr, p, sink, meta := newCollabManager(t)
	if err := mgr.Prompt(context.Background(), meta.ID, "/plan", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	waitForCollab(t, sink, "plan")
	if err := mgr.Prompt(context.Background(), meta.ID, "/plan keep going", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if len(p.setCollab) != 1 {
		t.Fatalf("already-in-plan must skip RPC: %v", p.setCollab)
	}
	if len(p.prompts) != 1 || p.prompts[0][0].Text != "keep going" {
		t.Fatalf("prompts = %#v", p.prompts)
	}
}

func TestModePlanPointsToCollaborationPlan(t *testing.T) {
	mgr, _, sink, meta := newCollabManager(t)
	if err := mgr.Prompt(context.Background(), meta.ID, "/mode plan", nil, "dev-a"); err != nil {
		t.Fatal(err)
	}
	if !sink.hasNoticeContaining("Use /plan") {
		t.Fatalf("notices = %v", sink.notices())
	}
}

func TestPersistOptionalSettingsRoundTrip(t *testing.T) {
	store, err := session.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec := session.Record{
		ID: "s1", Provider: "codex", Name: "n",
		ModeID: "auto", CollaborationModeID: "plan",
		ServiceTier: "priority", Personality: "friendly",
	}
	if err := store.Save(rec); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ModeID != "auto" || got.CollaborationModeID != "plan" ||
		got.ServiceTier != "priority" || got.Personality != "friendly" {
		t.Fatalf("got %+v", got)
	}
	old := session.Record{ID: "s2", Provider: "codex", Name: "old"}
	if err := store.Save(old); err != nil {
		t.Fatal(err)
	}
	got2, err := store.Get("s2")
	if err != nil {
		t.Fatal(err)
	}
	if got2.ModeID != "" || got2.CollaborationModeID != "" {
		t.Fatalf("old record must decode empty axes: %+v", got2)
	}
}
