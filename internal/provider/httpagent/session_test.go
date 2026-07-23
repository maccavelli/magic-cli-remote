package httpagent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func (d *fakeDialect) ID() provider.ID                { return d.id }
func (d *fakeDialect) DefaultBin() string             { return "false" }
func (d *fakeDialect) ServeArgs(int) []string         { return nil }
func (d *fakeDialect) HealthPath() string             { return "/health" }
func (d *fakeDialect) EventsPath() string             { return "/events" }
func (d *fakeDialect) AfterBoot(context.Context, API) {}
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
	// resyncN counts Resync calls; resyncFn (optional) runs on each.
	resyncN  int
	resyncFn func(context.Context, time.Time)
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
func (s *fakeDialectSession) Resync(ctx context.Context, turnStartedAt time.Time) {
	s.mu.Lock()
	s.resyncN++
	fn := s.resyncFn
	s.mu.Unlock()
	if fn != nil {
		fn(ctx, turnStartedAt)
	}
}
func (s *fakeDialectSession) resyncCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resyncN
}

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

// M13: two local sessions must not share one agent-side id. The second register
// is rejected (the incumbent is live and streaming it), and a late unregister
// from the loser must not evict the survivor.
func TestRegisterRejectsDuplicateAndUnregisterIsIdentityChecked(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s1 := &session{p: p, agentID: "agent-x", localID: "l1"}
	s2 := &session{p: p, agentID: "agent-x", localID: "l2"}

	if err := p.register(s1); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := p.register(s2); err == nil {
		t.Fatal("duplicate agent id should be rejected")
	}
	// Re-registering the same session is idempotent.
	if err := p.register(s1); err != nil {
		t.Fatalf("re-register same session: %v", err)
	}

	// A late Close/unregister from the REJECTED session must not evict s1.
	p.unregister(s2)
	p.mu.Lock()
	got, still := p.sessions["agent-x"]
	p.mu.Unlock()
	if !still || got != s1 {
		t.Fatal("rejected session's unregister evicted the incumbent")
	}

	// The incumbent's own unregister works.
	p.unregister(s1)
	p.mu.Lock()
	_, present := p.sessions["agent-x"]
	p.mu.Unlock()
	if present {
		t.Fatal("incumbent unregister did not remove it")
	}
}

// M7: an SSE line longer than the cap must be flagged and skippable without
// desyncing the stream — the following line still parses. (A bufio.Scanner would
// instead die permanently, aborting the shared stream for every session.)
func TestReadSSELineSkipsOversized(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("data: short\n")
	buf.WriteString("data: " + strings.Repeat("x", 20) + "\n")  // 26 bytes: exceeds the reader buffer, not the cap
	buf.WriteString("data: " + strings.Repeat("y", 100) + "\n") // 106 bytes: exceeds the cap
	buf.WriteString("data: after\n")

	r := bufio.NewReaderSize(&buf, 16) // small buffer forces the ErrBufferFull path
	const max = 64
	var got []string
	for {
		line, tooLong, err := readSSELine(r, max)
		switch {
		case tooLong:
			got = append(got, "<toolong>")
		case len(line) > 0:
			got = append(got, string(line))
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
	}
	want := []string{"data: short", "data: " + strings.Repeat("x", 20), "<toolong>", "data: after"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// newTestSession builds a registered session wired to a fake dialect session,
// bypassing Start (no engine).
func newTestSession(p *Provider) (*session, *fakeDialectSession) {
	s := &session{
		p:       p,
		localID: "local-1",
		agentID: "agent-1",
		events:  make(chan event.Event, 64),
		done:    make(chan struct{}),
		pending: map[string]struct{}{},
		log:     p.log,
	}
	ds := &fakeDialectSession{h: s}
	s.ds = ds
	return s, ds
}

// H4: resync must only run for a session that actually has in-flight state to
// reconcile — an idle session, a closed one, or one whose prompt submit is
// still in flight (the engine doesn't know the turn yet) must all be skipped.
func TestResyncGatesOnTurnState(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)

	s, ds := newTestSession(p)
	s.resync() // idle: no turn to reconcile
	if n := ds.resyncCount(); n != 0 {
		t.Fatalf("idle session resynced %d times, want 0", n)
	}

	s.mu.Lock()
	s.turnActive = true
	s.promptInFlight = true
	s.mu.Unlock()
	s.resync() // prompt submit in flight: engine state would be stale evidence
	if n := ds.resyncCount(); n != 0 {
		t.Fatalf("in-flight prompt resynced %d times, want 0", n)
	}

	s.mu.Lock()
	s.promptInFlight = false
	s.turnStartedAt = time.Unix(100, 0)
	s.mu.Unlock()
	s.resync()
	if n := ds.resyncCount(); n != 1 {
		t.Fatalf("turn-active resync count = %d, want 1", n)
	}
	ds.mu.Lock()
	ds.resyncFn = func(_ context.Context, started time.Time) {
		if !started.Equal(time.Unix(100, 0)) {
			t.Errorf("turnStartedAt = %v, want %v", started, time.Unix(100, 0))
		}
	}
	ds.mu.Unlock()
	s.resync()
}

// H4: a (re)connected SSE stream must reconcile turn-active sessions — frames
// emitted while the stream was down are gone, and a lost turn-end would leave
// the session "running" forever.
func TestStreamConnectTriggersResync(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// No frames: connect, then EOF — exactly the reconnect-after-gap shape.
	}))
	defer ts.Close()

	s, ds := newTestSession(p)
	s.mu.Lock()
	s.turnActive = true
	s.mu.Unlock()
	if err := p.register(s); err != nil {
		t.Fatal(err)
	}

	if err := p.streamOnce(ts.URL, 0); err != nil {
		t.Fatalf("streamOnce: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for ds.resyncCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("stream connect did not trigger resync of turn-active session")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// H4: when the stall watchdog fires and resync discovers the turn already
// finished engine-side, the ghost turn ends silently — no "still waiting"
// notice for a turn that is not running.
func TestStallResyncEndsGhostTurnWithoutNotice(t *testing.T) {
	old := stallTickInterval
	stallTickInterval = 10 * time.Millisecond
	defer func() { stallTickInterval = old }()

	p := NewWithLogger(&fakeDialect{id: "test"}, Config{TurnStallNotice: time.Millisecond}, nil)
	s, ds := newTestSession(p)
	s.mu.Lock()
	s.turnActive = true
	s.mu.Unlock()
	ds.mu.Lock()
	ds.resyncFn = func(context.Context, time.Time) { s.EndTurn() }
	ds.mu.Unlock()

	go s.watchStall()
	deadline := time.Now().Add(2 * time.Second)
	for ds.resyncCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("stall watchdog never resynced")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Give the watcher a few more ticks: it must exit without a notice.
	time.Sleep(50 * time.Millisecond)
	close(s.done)
	for {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeNotice {
				t.Fatalf("ghost turn produced a stall notice: %q", ev.Text)
			}
		default:
			return
		}
	}
}

// The counterpart: when resync finds the turn genuinely still running, the
// stall notice must still be delivered.
func TestStallNoticeStillFiresWhenTurnIsLive(t *testing.T) {
	old := stallTickInterval
	stallTickInterval = 10 * time.Millisecond
	defer func() { stallTickInterval = old }()

	p := NewWithLogger(&fakeDialect{id: "test"}, Config{TurnStallNotice: time.Millisecond}, nil)
	s, _ := newTestSession(p)
	s.mu.Lock()
	s.turnActive = true
	s.mu.Unlock()

	go s.watchStall()
	defer close(s.done)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeNotice {
				return
			}
		case <-deadline:
			t.Fatal("no stall notice for a live turn")
		}
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
