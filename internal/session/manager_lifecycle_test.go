package session_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// trackingSession is a scripted provider.Session that records Close and
// supports emitting a disconnected status for lifecycle tests.
type trackingSession struct {
	id       string
	token    int32 // unique instance id
	events   chan event.Event
	closeN   *atomic.Int32
	promptN  *atomic.Int32
	lastTok  *atomic.Int32 // last token that received Prompt
	closed   atomic.Bool
}

func (s *trackingSession) ID() string                 { return s.id }
func (s *trackingSession) ProviderID() provider.ID    { return "track" }
func (s *trackingSession) AgentSessionID() string     { return s.id }
func (s *trackingSession) Events() <-chan event.Event { return s.events }

func (s *trackingSession) Prompt(_ context.Context, _ []provider.Content) error {
	if s.closed.Load() {
		return errors.New("session closed")
	}
	s.promptN.Add(1)
	s.lastTok.Store(s.token)
	return nil
}

func (s *trackingSession) Cancel(context.Context) error { return nil }

func (s *trackingSession) Close(context.Context) error {
	if s.closed.Swap(true) {
		return nil
	}
	s.closeN.Add(1)
	close(s.events)
	return nil
}

func (s *trackingSession) emitDisconnected() {
	s.events <- event.Event{
		Type:      event.TypeSessionStatus,
		SessionID: s.id,
		Timestamp: time.Now().UTC(),
		Status:    session.StatusDisconnected,
	}
}

type trackingProvider struct {
	nextToken atomic.Int32
	closeN    atomic.Int32
	promptN   atomic.Int32
	lastTok   atomic.Int32
	mu        sync.Mutex
	byID      map[string]*trackingSession
}

func newTrackingProvider() *trackingProvider {
	return &trackingProvider{byID: make(map[string]*trackingSession)}
}

func (p *trackingProvider) ID() provider.ID { return "track" }
func (p *trackingProvider) Ready() bool     { return true }

func (p *trackingProvider) Start(_ context.Context, opts provider.StartOptions) (provider.Session, error) {
	id := opts.LocalSessionID
	if id == "" {
		id = "auto"
	}
	tok := p.nextToken.Add(1)
	s := &trackingSession{
		id:      id,
		token:   tok,
		events:  make(chan event.Event, 8),
		closeN:  &p.closeN,
		promptN: &p.promptN,
		lastTok: &p.lastTok,
	}
	p.mu.Lock()
	p.byID[id] = s
	p.mu.Unlock()
	s.events <- event.Event{
		Type:      event.TypeSessionStatus,
		SessionID: id,
		Timestamp: time.Now().UTC(),
		Status:    "idle",
	}
	return s, nil
}

func (p *trackingProvider) get(id string) *trackingSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.byID[id]
}

func TestPromptFailsAfterDisconnectAutoClose(t *testing.T) {
	tp := newTrackingProvider()
	reg := provider.NewRegistry()
	reg.Register(tp)

	mgr := session.NewManager(reg, nil, nil, nil)
	ctx := context.Background()

	meta, err := mgr.Create(ctx, "track", provider.StartOptions{
		Name:           "t",
		LocalSessionID: "sess-dead",
	}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	if !meta.Live {
		t.Fatal("expected live meta")
	}
	if err := mgr.Prompt(ctx, meta.ID, "hi", "dev-a"); err != nil {
		t.Fatalf("prompt while live: %v", err)
	}

	s := tp.get(meta.ID)
	if s == nil {
		t.Fatal("missing tracking session")
	}
	s.emitDisconnected()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		err := mgr.Prompt(ctx, meta.ID, "again", "dev-a")
		if err != nil {
			if !errors.Is(err, session.ErrNotLive) {
				t.Fatalf("want ErrNotLive, got %v", err)
			}
			// Entry should be gone from live list.
			for _, m := range mgr.List() {
				if m.ID == meta.ID && m.Live {
					t.Fatalf("session still listed live after auto-close")
				}
			}
			if tp.closeN.Load() < 1 {
				t.Fatal("expected Close on provider session after auto-close")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Prompt still succeeded after disconnected status (auto-close missing)")
}

func TestCreateCloseAndReplaceLeavesActiveSession(t *testing.T) {
	tp := newTrackingProvider()
	reg := provider.NewRegistry()
	reg.Register(tp)

	mgr := session.NewManager(reg, nil, nil, nil)
	ctx := context.Background()
	const id = "sess-replace"

	meta1, err := mgr.Create(ctx, "track", provider.StartOptions{
		Name:           "first",
		LocalSessionID: id,
	}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	if meta1.ID != id || !meta1.Live {
		t.Fatalf("meta1=%+v", meta1)
	}
	firstTok := tp.get(id).token

	meta2, err := mgr.Create(ctx, "track", provider.StartOptions{
		Name:           "second",
		LocalSessionID: id,
	}, "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	if meta2.ID != id || !meta2.Live {
		t.Fatalf("meta2 must be live with same id, got %+v", meta2)
	}
	if tp.closeN.Load() != 1 {
		t.Fatalf("old session Close count = %d, want 1", tp.closeN.Load())
	}
	second := tp.get(id)
	if second == nil || second.token == firstTok {
		t.Fatalf("expected a new provider instance, firstTok=%d second=%v", firstTok, second)
	}
	if second.closed.Load() {
		t.Fatal("replacement session must not be closed")
	}

	if err := mgr.Prompt(ctx, id, "ping", "dev-a"); err != nil {
		t.Fatalf("prompt on replaced session: %v", err)
	}
	if got := tp.lastTok.Load(); got != second.token {
		t.Fatalf("prompt went to token %d, want %d (replacement)", got, second.token)
	}
}

