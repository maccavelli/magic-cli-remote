package httpagent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// newCoalesceSession builds a registered session with a short coalescing
// window, bypassing Start (no engine). buf sizes the event channel so a test
// can deliberately starve the pump.
func newCoalesceSession(t *testing.T, win time.Duration, buf int) *session {
	t.Helper()
	p := NewWithLogger(&fakeDialect{id: "test"}, Config{StreamCoalesce: &win}, nil)
	s := &session{
		p:          p,
		localID:    "local-1",
		agentID:    "agent-1",
		events:     make(chan event.Event, buf),
		done:       make(chan struct{}),
		pending:    map[string]struct{}{},
		permOrigin: map[string]string{},
		treeNodes:  map[string]NodeStatus{},
		log:        p.log,
	}
	s.ds = &fakeDialectSession{h: s}
	return s
}

// drainEvents collects everything queued on s.events without blocking.
func drainEvents(s *session) []event.Event {
	var out []event.Event
	for {
		select {
		case ev := <-s.events:
			out = append(out, ev)
		default:
			return out
		}
	}
}

// waitForEvents drains until want events have arrived or the deadline passes.
func waitForEvents(t *testing.T, s *session, want int) []event.Event {
	t.Helper()
	var out []event.Event
	deadline := time.Now().Add(2 * time.Second)
	for len(out) < want && time.Now().Before(deadline) {
		select {
		case ev := <-s.events:
			out = append(out, ev)
		case <-time.After(5 * time.Millisecond):
		}
	}
	return out
}

func chunkText(evs []event.Event) string {
	var b strings.Builder
	for _, e := range evs {
		if e.Type == event.TypeAssistantChunk {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

func countType(evs []event.Event, typ event.Type) int {
	n := 0
	for _, e := range evs {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// TestCoalesceBoundsFramesAndPreservesText is the regression that makes MADR
// 0024 non-reversible: many token-sized emits must reassemble byte-for-byte
// while costing far fewer events than they did pre-coalescing.
func TestCoalesceBoundsFramesAndPreservesText(t *testing.T) {
	const win = 20 * time.Millisecond
	s := newCoalesceSession(t, win, 4096)

	const n = 500
	var want strings.Builder
	start := time.Now()
	for i := range n {
		text := string(rune('a'+i%26)) + " "
		want.WriteString(text)
		s.Emit(event.Event{Type: event.TypeAssistantChunk, Text: text})
		time.Sleep(200 * time.Microsecond)
	}
	elapsed := time.Since(start)
	s.Emit(event.Event{Type: event.TypeTurnComplete, StopReason: "end_turn"})

	got := drainEvents(s)
	if text := chunkText(got); text != want.String() {
		t.Fatalf("reassembled text differs from input\n got %q\nwant %q", text, want.String())
	}

	frames := countType(got, event.TypeAssistantChunk)
	// One leading edge, one per elapsed window, one boundary flush, plus slack
	// for scheduler jitter. The point is that it is a small multiple of
	// elapsed/window rather than a multiple of n.
	limit := int(elapsed/win) + 8
	if frames > limit {
		t.Errorf("emitted %d chunk frames for %d chunks over %v, want <= %d",
			frames, n, elapsed, limit)
	}
	if frames >= n {
		t.Errorf("emitted %d frames for %d chunks — no coalescing happened", frames, n)
	}
	t.Logf("coalesced %d chunks into %d frames over %v", n, frames, elapsed)
}

func TestCoalesceKeepsTimeToFirstToken(t *testing.T) {
	s := newCoalesceSession(t, time.Second, 8)
	s.Emit(event.Event{Type: event.TypeAssistantChunk, Text: "first"})

	select {
	case ev := <-s.events:
		if ev.Text != "first" {
			t.Fatalf("Text = %q, want %q", ev.Text, "first")
		}
	default:
		t.Fatal("the first chunk of a run must not wait for the window")
	}
}

func TestCoalesceFlushesTailBeforeTurnComplete(t *testing.T) {
	s := newCoalesceSession(t, time.Second, 8)
	s.Emit(event.Event{Type: event.TypeAssistantChunk, Text: "lead "})
	s.Emit(event.Event{Type: event.TypeAssistantChunk, Text: "tail"})
	s.Emit(event.Event{Type: event.TypeTurnComplete, StopReason: "end_turn"})

	got := drainEvents(s)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(got), got)
	}
	if got[1].Type != event.TypeAssistantChunk || got[1].Text != "tail" {
		t.Errorf("got[1] = %+v, want the buffered tail", got[1])
	}
	if got[2].Type != event.TypeTurnComplete {
		t.Errorf("got[2] = %+v, want turn_complete last", got[2])
	}
}

func TestCoalesceFlushesOnTimer(t *testing.T) {
	s := newCoalesceSession(t, 10*time.Millisecond, 8)
	s.Emit(event.Event{Type: event.TypeAssistantChunk, Text: "lead"})
	<-s.events
	s.Emit(event.Event{Type: event.TypeAssistantChunk, Text: "buffered"})

	got := waitForEvents(t, s, 1)
	if len(got) != 1 || got[0].Text != "buffered" {
		t.Fatalf("the window must flush without a boundary event, got %+v", got)
	}
}

// TestCoalesceOrderingUnderConcurrentEmitters mirrors the real shape: the
// engine-wide SSE reader streaming text while the MADR 0014 resync goroutine
// emits on the same session.
func TestCoalesceOrderingUnderConcurrentEmitters(t *testing.T) {
	s := newCoalesceSession(t, 5*time.Millisecond, 8192)

	const n = 300
	var want strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range n {
			text := string(rune('a' + i%26))
			want.WriteString(text)
			s.Emit(event.Event{Type: event.TypeAssistantChunk, Text: text})
		}
	}()
	go func() {
		defer wg.Done()
		for i := range 40 {
			s.Emit(event.Event{
				Type:   event.TypeToolUpdate,
				ToolID: "t1",
				Status: "running",
			})
			if i%4 == 0 {
				s.Emit(event.Event{Type: event.TypeUsage, Usage: &event.Usage{Used: i, Size: 100}})
			}
			time.Sleep(100 * time.Microsecond)
		}
	}()
	wg.Wait()
	s.Emit(event.Event{Type: event.TypeTurnComplete, StopReason: "end_turn"})

	got := drainEvents(s)
	if text := chunkText(got); text != want.String() {
		t.Errorf("text lost or reordered under concurrency\n got %q\nwant %q", text, want.String())
	}
	last := got[len(got)-1]
	if last.Type != event.TypeTurnComplete {
		t.Errorf("last event = %s, want turn_complete after every chunk", last.Type)
	}
}

// TestCoalesceRetriesRatherThanDroppingText pins the improvement over the
// pre-0024 path, which logged "dropping event; slow consumer" and lost the
// tokens.
func TestCoalesceRetriesRatherThanDroppingText(t *testing.T) {
	const win = 5 * time.Millisecond
	s := newCoalesceSession(t, win, 2)

	var want strings.Builder
	for i := range 50 {
		text := string(rune('a'+i%26)) + "x"
		want.WriteString(text)
		s.Emit(event.Event{Type: event.TypeAssistantChunk, Text: text})
	}

	// The channel is full by now; drain it so the retries can land, then let
	// the coalescer catch up.
	var got []event.Event
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got = append(got, drainEvents(s)...)
		if chunkText(got) == want.String() {
			break
		}
		time.Sleep(2 * win)
	}
	if text := chunkText(got); text != want.String() {
		t.Errorf("text lost under backpressure\n got %q\nwant %q", text, want.String())
	}
}

func TestCloseDrainsBufferedTail(t *testing.T) {
	s := newCoalesceSession(t, time.Hour, 8)
	s.Emit(event.Event{Type: event.TypeAssistantChunk, Text: "lead "})
	<-s.events
	s.Emit(event.Event{Type: event.TypeAssistantChunk, Text: "orphan tail"})

	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := drainEvents(s)
	if chunkText(got) != "orphan tail" {
		t.Errorf("Close must flush buffered text, got %+v", got)
	}
}

// TestCoalesceDisabledMatchesPreChangeBehaviour keeps the kill switch honest.
func TestCoalesceDisabledMatchesPreChangeBehaviour(t *testing.T) {
	s := newCoalesceSession(t, 0, 16)
	for i := range 5 {
		s.Emit(event.Event{Type: event.TypeAssistantChunk, Text: string(rune('a' + i))})
	}
	got := drainEvents(s)
	if len(got) != 5 {
		t.Fatalf("got %d events, want one per chunk: %+v", len(got), got)
	}
	if chunkText(got) != "abcde" {
		t.Errorf("text = %q, want %q", chunkText(got), "abcde")
	}
}
