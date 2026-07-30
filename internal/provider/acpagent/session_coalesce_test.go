package acpagent

import (
	"log/slog"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// Chunk text held back under back-pressure during session/load replay must keep
// its Replay flag when flushed at the next boundary. Otherwise the manager
// (which broadcasts only non-Replay events) re-sends the whole prior transcript
// live on every resume of a long session.
func TestCoalescedFlushPreservesReplay(t *testing.T) {
	win := time.Hour
	s := &session{
		localID:  "l1",
		agentID:  "a1",
		events:   make(chan event.Event, 1),
		log:      slog.Default(),
		loading:  true,
		attached: true,
		done:     make(chan struct{}),
		cfg:      Config{StreamCoalesce: &win},
	}

	s.emit(event.Event{Type: event.TypeAssistantChunk, Text: "one"})
	s.emit(event.Event{Type: event.TypeAssistantChunk, Text: "two"})

	first := recvEvent(t, s.events)
	if first.Text != "one" || !first.Replay {
		t.Fatalf("first event = %+v, want replay chunk \"one\"", first)
	}
	s.mu.Lock()
	s.loading = false
	s.mu.Unlock()

	go s.emit(event.Event{Type: event.TypeTurnComplete, Status: "end_turn"})

	flushed := recvEvent(t, s.events)
	if flushed.Type != event.TypeAssistantChunk || flushed.Text != "two" {
		t.Fatalf("flushed event = %+v, want assistant chunk \"two\"", flushed)
	}
	if !flushed.Replay {
		t.Fatal("flushed replay chunk lost its Replay flag; would be re-broadcast live")
	}
	boundary := recvEvent(t, s.events)
	if boundary.Type != event.TypeTurnComplete {
		t.Fatalf("boundary event = %+v, want turn_complete", boundary)
	}
}
