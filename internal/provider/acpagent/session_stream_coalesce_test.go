package acpagent

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// drainAll collects queued events without blocking.
func drainAll(s *session) []event.Event {
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

func assistantText(evs []event.Event) string {
	var b strings.Builder
	for _, e := range evs {
		if e.Type == event.TypeAssistantChunk {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

func countAssistant(evs []event.Event) int {
	n := 0
	for _, e := range evs {
		if e.Type == event.TypeAssistantChunk {
			n++
		}
	}
	return n
}

// newStreamSession builds a minimal session for emit-path tests (no engine).
func newStreamSession(buf int, win time.Duration) *session {
	w := win
	return &session{
		localID:  "local-1",
		agentID:  "agent-1",
		events:   make(chan event.Event, buf),
		done:     make(chan struct{}),
		log:      slog.Default(),
		attached: true,
		cfg:      Config{StreamCoalesce: &w},
	}
}

// TestStreamCoalesceDisabledOnePerChunk is the kill switch (stream_coalesce_ms: 0).
func TestStreamCoalesceDisabledOnePerChunk(t *testing.T) {
	s := newStreamSession(64, 0)
	const n = 20
	var want strings.Builder
	for i := 0; i < n; i++ {
		text := string(rune('a' + i%26))
		want.WriteString(text)
		s.emit(event.Event{Type: event.TypeAssistantChunk, Text: text})
	}
	s.emit(event.Event{Type: event.TypeTurnComplete, StopReason: "end_turn"})

	got := drainAll(s)
	if text := assistantText(got); text != want.String() {
		t.Fatalf("text = %q, want %q", text, want.String())
	}
	if frames := countAssistant(got); frames != n {
		t.Fatalf("assistant frames = %d, want %d", frames, n)
	}
	if got[len(got)-1].Type != event.TypeTurnComplete {
		t.Fatalf("last = %s, want turn_complete", got[len(got)-1].Type)
	}
}

// TestTimedStreamCoalesceBoundsFrames is MADR 0057 H-1: many tokens reassemble
// while costing far fewer frames than tokens.
func TestTimedStreamCoalesceBoundsFrames(t *testing.T) {
	win := 20 * time.Millisecond
	s := newStreamSession(4096, win)

	const n = 200
	var want strings.Builder
	start := time.Now()
	for i := 0; i < n; i++ {
		text := string(rune('a'+i%26)) + " "
		want.WriteString(text)
		s.emit(event.Event{Type: event.TypeAssistantChunk, Text: text})
		time.Sleep(200 * time.Microsecond)
	}
	elapsed := time.Since(start)
	s.emit(event.Event{Type: event.TypeTurnComplete, StopReason: "end_turn"})

	got := drainAll(s)
	if text := assistantText(got); text != want.String() {
		t.Fatalf("reassembled text differs\n got %q\nwant %q", text, want.String())
	}
	frames := countAssistant(got)
	limit := int(elapsed/win) + 8
	if frames > limit {
		t.Errorf("emitted %d chunk frames for %d chunks over %v, want <= %d",
			frames, n, elapsed, limit)
	}
	if frames >= n {
		t.Errorf("emitted %d frames for %d chunks — timed coalescing did not run", frames, n)
	}
}

func TestTimedStreamCoalesceKeepsTimeToFirstToken(t *testing.T) {
	s := newStreamSession(8, time.Second)
	s.emit(event.Event{Type: event.TypeAssistantChunk, Text: "first"})
	select {
	case ev := <-s.events:
		if ev.Text != "first" {
			t.Fatalf("Text = %q, want first", ev.Text)
		}
	default:
		t.Fatal("leading-edge first chunk must not wait for the window")
	}
}

func TestTimedStreamCoalesceFlushesTailBeforeTurnComplete(t *testing.T) {
	s := newStreamSession(8, time.Second)
	s.emit(event.Event{Type: event.TypeAssistantChunk, Text: "lead "})
	s.emit(event.Event{Type: event.TypeAssistantChunk, Text: "tail"})
	s.emit(event.Event{Type: event.TypeTurnComplete, StopReason: "end_turn"})

	got := drainAll(s)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(got), got)
	}
	if got[1].Type != event.TypeAssistantChunk || got[1].Text != "tail" {
		t.Errorf("got[1] = %+v, want buffered tail", got[1])
	}
	if got[2].Type != event.TypeTurnComplete {
		t.Errorf("got[2] = %+v, want turn_complete last", got[2])
	}
}

func TestTimedStreamCoalesceFlushesOnTimer(t *testing.T) {
	s := newStreamSession(8, 10*time.Millisecond)
	s.emit(event.Event{Type: event.TypeAssistantChunk, Text: "lead"})
	<-s.events
	s.emit(event.Event{Type: event.TypeAssistantChunk, Text: "buffered"})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev := <-s.events:
			if ev.Text == "buffered" {
				return
			}
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	t.Fatal("window must flush without a boundary event")
}

func TestTimedStreamCoalescePreservesReplay(t *testing.T) {
	s := newStreamSession(1, time.Hour)
	s.loading = true

	s.emit(event.Event{Type: event.TypeAssistantChunk, Text: "one"})
	s.emit(event.Event{Type: event.TypeAssistantChunk, Text: "two"})

	first := recvEvent(t, s.events)
	if first.Text != "one" || !first.Replay {
		t.Fatalf("first = %+v, want replay chunk \"one\"", first)
	}
	s.mu.Lock()
	s.loading = false
	s.mu.Unlock()

	go s.emit(event.Event{Type: event.TypeTurnComplete, StopReason: "end_turn"})
	flushed := recvEvent(t, s.events)
	if flushed.Type != event.TypeAssistantChunk || flushed.Text != "two" {
		t.Fatalf("flushed = %+v, want assistant \"two\"", flushed)
	}
	if !flushed.Replay {
		t.Fatal("flushed replay chunk lost Replay flag")
	}
	if boundary := recvEvent(t, s.events); boundary.Type != event.TypeTurnComplete {
		t.Fatalf("boundary = %+v, want turn_complete", boundary)
	}
}
