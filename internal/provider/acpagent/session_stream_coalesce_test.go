package acpagent

import (
	"log/slog"
	"strings"
	"testing"

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
func newStreamSession(buf int) *session {
	return &session{
		localID:  "local-1",
		agentID:  "agent-1",
		events:   make(chan event.Event, buf),
		done:     make(chan struct{}),
		log:      slog.Default(),
		attached: true,
		cfg:      Config{},
	}
}

// TestHealthyPathEmitsOneEventPerChunkWithoutPressure pins pre-Phase-A
// acpagent behaviour (MADR 0057 Phase 0 / H-1 baseline): when the consumer
// keeps up, every assistant chunk is one event. Timed coalescing (Phase A)
// changes the default; the kill switch StreamCoalesce=0 preserves this
// contract (see TestStreamCoalesceDisabledOnePerChunk after Phase A).
func TestHealthyPathEmitsOneEventPerChunkWithoutPressure(t *testing.T) {
	s := newStreamSession(64)
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
	frames := countAssistant(got)
	if frames != n {
		t.Fatalf("assistant frames = %d, want %d (one per chunk without timed coalesce)", frames, n)
	}
	if got[len(got)-1].Type != event.TypeTurnComplete {
		t.Fatalf("last = %s, want turn_complete", got[len(got)-1].Type)
	}
}
