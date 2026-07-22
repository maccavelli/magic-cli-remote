package httpagent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// fakeDialect implements Dialect for unit tests without a real engine.
type fakeDialect struct {
	id provider.ID
}

func (d *fakeDialect) ID() provider.ID                          { return d.id }
func (d *fakeDialect) DefaultBin() string                       { return "false" }
func (d *fakeDialect) ServeArgs(int) []string                   { return nil }
func (d *fakeDialect) HealthPath() string                       { return "/health" }
func (d *fakeDialect) EventsPath() string                       { return "/events" }
func (d *fakeDialect) AfterBoot(context.Context, API)           {}
func (d *fakeDialect) DecodeFrame([]byte) (string, json.RawMessage, string, bool) {
	return "", nil, "", false
}
func (d *fakeDialect) NewSession(h Host) DialectSession {
	return &fakeDialectSession{h: h}
}

type fakeDialectSession struct {
	h       Host
	mu      sync.Mutex
	deleted bool
	createN int
}

func (s *fakeDialectSession) Create(context.Context, provider.StartOptions) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createN++
	return "agent-1", nil
}
func (s *fakeDialectSession) Resume(context.Context, string) (string, error) {
	return "agent-1", nil
}
func (s *fakeDialectSession) Replay(context.Context) {}
func (s *fakeDialectSession) Prompt(context.Context, []provider.Content) error {
	return nil
}
func (s *fakeDialectSession) Abort(context.Context) error { return nil }
func (s *fakeDialectSession) RespondPermission(context.Context, string, string, bool) error {
	return nil
}
func (s *fakeDialectSession) Delete(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = true
	return nil
}
func (s *fakeDialectSession) HandleEvent(string, json.RawMessage) {}

func TestRespondPermissionEmitsCancelledStatus(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	// Build a session without a real engine: register manually.
	s := &session{
		p:       p,
		localID: "local-1",
		agentID: "agent-1",
		events:  make(chan event.Event, 8),
		done:    make(chan struct{}),
		pending: map[string]struct{}{"perm-1": {}},
		log:     p.log,
	}
	s.ds = &fakeDialectSession{h: s}

	if err := s.RespondPermission(context.Background(), "perm-1", "", true); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-s.events:
		if ev.Type != event.TypePermissionResolved {
			t.Fatalf("type=%s", ev.Type)
		}
		if ev.Status != event.PermissionStatusCancelled {
			t.Fatalf("status=%q want cancelled", ev.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("no permission_resolved event")
	}
}

func TestRespondPermissionEmitsResolvedStatus(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s := &session{
		p:       p,
		localID: "local-1",
		agentID: "agent-1",
		events:  make(chan event.Event, 8),
		done:    make(chan struct{}),
		pending: map[string]struct{}{"perm-2": {}},
		log:     p.log,
	}
	s.ds = &fakeDialectSession{h: s}

	if err := s.RespondPermission(context.Background(), "perm-2", "once", false); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-s.events:
		if ev.Status != event.PermissionStatusResolved {
			t.Fatalf("status=%q want resolved", ev.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
}

func TestPurgeDeletesEngineSession(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	ds := &fakeDialectSession{}
	s := &session{
		p:       p,
		localID: "local-1",
		agentID: "agent-1",
		events:  make(chan event.Event, 8),
		done:    make(chan struct{}),
		pending: map[string]struct{}{},
		log:     p.log,
		ds:      ds,
	}
	p.register(s)

	if err := s.Purge(context.Background()); err != nil {
		t.Fatal(err)
	}
	ds.mu.Lock()
	deleted := ds.deleted
	ds.mu.Unlock()
	if !deleted {
		t.Fatal("expected dialect Delete to be called")
	}
	// Local state closed and unregistered.
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if !closed {
		t.Fatal("expected session closed")
	}
	p.mu.Lock()
	_, still := p.sessions["agent-1"]
	p.mu.Unlock()
	if still {
		t.Fatal("expected unregister after purge")
	}
}

func TestCloseDoesNotDeleteEngineSession(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	ds := &fakeDialectSession{}
	s := &session{
		p:       p,
		localID: "local-1",
		agentID: "agent-1",
		events:  make(chan event.Event, 8),
		done:    make(chan struct{}),
		pending: map[string]struct{}{},
		log:     p.log,
		ds:      ds,
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ds.deleted {
		t.Fatal("soft Close must not Delete server-side session")
	}
}

func TestLineRingKeepsTail(t *testing.T) {
	w := &lineRing{max: 3}
	_, _ = w.Write([]byte("a\nb\nc\nd\n"))
	got := w.tail()
	if got != "b\nc\nd" {
		t.Fatalf("tail=%q", got)
	}
}
