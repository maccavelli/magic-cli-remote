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

// countingDialectSession counts Prompt calls and can hold a turn open.
type countingDialectSession struct {
	fakeDialectSession
	mu       sync.Mutex
	promptN  int
	promptCh chan struct{} // if non-nil, Prompt waits until closed
}

func (c *countingDialectSession) Prompt(ctx context.Context, parts []provider.Content) error {
	c.mu.Lock()
	c.promptN++
	ch := c.promptCh
	c.mu.Unlock()
	if ch != nil {
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (c *countingDialectSession) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.promptN
}

func TestPromptQueuesWhileTurnActive(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	hold := make(chan struct{})
	ds := &countingDialectSession{promptCh: hold}
	ds.h = s
	s.ds = ds

	// First prompt starts a turn (blocks on hold).
	done1 := make(chan error, 1)
	go func() {
		done1 <- s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "one"}})
	}()
	// Wait until first Prompt is inside dialect.
	deadline := time.Now().Add(2 * time.Second)
	for ds.count() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("first prompt never reached dialect")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Second prompt should queue (not ErrTurnBusy).
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "two"}}); err != nil {
		t.Fatalf("second prompt: %v", err)
	}
	s.mu.Lock()
	qn := len(s.promptQueue)
	s.mu.Unlock()
	if qn != 1 {
		t.Fatalf("queue len=%d want 1", qn)
	}
	if ds.count() != 1 {
		t.Fatalf("dialect prompts=%d want 1 (queued not submitted)", ds.count())
	}

	// End turn drains queue.
	close(hold)
	select {
	case err := <-done1:
		if err != nil {
			t.Fatalf("first prompt err: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first prompt hung")
	}
	// First Prompt returns after dialect returns; turn still active until
	// EndTurn. EndTurn only arms the drain — flushDrain runs it once the caller
	// has emitted its turn-end events (see TestQueueDrainRunsAfterTurnEndEvents).
	s.EndTurn()
	s.flushDrain()

	deadline = time.Now().Add(2 * time.Second)
	for ds.count() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("queued prompt never drained, count=%d", ds.count())
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.mu.Lock()
	qn = len(s.promptQueue)
	s.mu.Unlock()
	if qn != 0 {
		t.Fatalf("queue after drain=%d", qn)
	}
}

func TestPromptQueueOverflowReturnsTurnBusy(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	s.mu.Lock()
	s.turnActive = true
	s.mu.Unlock()

	for i := 0; i < maxPromptQueue; i++ {
		if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "q"}}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "overflow"}})
	if err != provider.ErrTurnBusy {
		t.Fatalf("overflow err=%v want ErrTurnBusy", err)
	}
}

func TestPromptQueueBlockedByPendingPermission(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, ds := newTestSession(p)
	cds := &countingDialectSession{}
	cds.h = s
	s.ds = cds
	_ = ds

	s.mu.Lock()
	s.turnActive = true
	s.promptQueue = []queuedPrompt{{parts: []provider.Content{{Type: "text", Text: "queued"}}}}
	s.pending = map[string]struct{}{"p1": {}}
	s.mu.Unlock()

	s.EndTurn() // would drain if no pending
	if cds.count() != 0 {
		t.Fatal("must not drain while permission pending")
	}

	// Resolve permission → drain.
	s.mu.Lock()
	delete(s.pending, "p1")
	s.mu.Unlock()
	s.tryDrainQueue()
	if cds.count() != 1 {
		t.Fatalf("after permission clear prompts=%d", cds.count())
	}
}

func TestCancelClearsPromptQueue(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	s.mu.Lock()
	s.turnActive = true
	s.promptQueue = []queuedPrompt{{parts: []provider.Content{{Type: "text", Text: "x"}}}}
	s.mu.Unlock()
	if err := s.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	n := len(s.promptQueue)
	s.mu.Unlock()
	if n != 0 {
		t.Fatalf("queue after cancel=%d", n)
	}
}

// endTurnDialect mimics the OpenCode shape: end the turn, THEN emit the
// turn-end events. Anything the drained queue entry emits must land after
// those, or the manager records the session as idle while a turn is running.
type endTurnDialect struct {
	countingDialectSession
	host Host
}

func (d *endTurnDialect) HandleEvent(string, json.RawMessage) {
	if !d.host.TryEndTurnIfTreeIdle() {
		return
	}
	d.host.Emit(event.Event{Type: event.TypeTurnComplete, Status: "end_turn"})
	d.host.Emit(event.Event{Type: event.TypeSessionStatus, Status: "idle"})
}

func TestQueueDrainRunsAfterTurnEndEvents(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	ds := &endTurnDialect{host: s}
	ds.h = s
	s.ds = ds

	s.mu.Lock()
	s.turnActive = true
	s.treeNodes = map[string]NodeStatus{s.agentID: NodeIdle}
	s.promptQueue = []queuedPrompt{{parts: []provider.Content{{Type: "text", Text: "queued"}}}}
	s.mu.Unlock()

	// Turn-end arrives over SSE, exactly as the pump delivers it.
	s.dispatch("session.idle", json.RawMessage(`{}`), s.agentID)

	if ds.count() != 1 {
		t.Fatalf("queued prompt submitted %d times, want 1", ds.count())
	}

	var order []string
	for {
		select {
		case ev := <-s.events:
			switch ev.Type {
			case event.TypeTurnComplete:
				order = append(order, "turn_complete")
			case event.TypeSessionStatus:
				order = append(order, "status:"+ev.Status)
			}
			continue
		default:
		}
		break
	}

	want := []string{"turn_complete", "status:idle", "status:running"}
	if len(order) != len(want) {
		t.Fatalf("event order=%v want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("event order=%v want %v (the drained turn's \"running\" must "+
				"not precede the previous turn's end, or the session reads as idle "+
				"while it is working)", order, want)
		}
	}
}

func TestQueuedPromptEmitsUserMessage(t *testing.T) {
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{}, nil)
	s, _ := newTestSession(p)
	s.mu.Lock()
	s.turnActive = true
	s.mu.Unlock()
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "hello-q"}}); err != nil {
		t.Fatal(err)
	}
	// Drain events for user_message + notice.
	var sawUser, sawNotice bool
	deadline := time.After(time.Second)
	for !sawUser || !sawNotice {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeUserMessage && ev.Text == "hello-q" {
				sawUser = true
			}
			if ev.Type == event.TypeNotice && len(ev.Text) > 0 {
				sawNotice = true
			}
		case <-deadline:
			t.Fatalf("sawUser=%v sawNotice=%v", sawUser, sawNotice)
		}
	}
}
