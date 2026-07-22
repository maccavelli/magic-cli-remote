package acpagent

import (
	"log/slog"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// Chunk text held back under back-pressure during session/load replay must keep
// its Replay flag when flushed at the next boundary. Otherwise the manager
// (which broadcasts only non-Replay events) re-sends the whole prior transcript
// live on every resume of a long session.
func TestCoalescedFlushPreservesReplay(t *testing.T) {
	s := &session{
		localID: "l1",
		agentID: "a1",
		events:  make(chan event.Event, 1),
		log:     slog.Default(),
		loading: true, // session/load replay in progress
	}

	// First chunk fills the single-slot buffer; the second must coalesce.
	s.emit(event.Event{Type: event.TypeAssistantChunk, Text: "one"})
	s.emit(event.Event{Type: event.TypeAssistantChunk, Text: "two"})

	s.mu.Lock()
	c := s.coalesced[event.TypeAssistantChunk]
	s.mu.Unlock()
	if c.text != "two" || !c.replay {
		t.Fatalf("coalesced = %+v, want {text:two replay:true}", c)
	}

	// Drain the buffered chunk so the boundary flush can enqueue, and end the
	// load phase: the flushed chunk must STILL be Replay even though loading is
	// now false (prepareEvent does not run on flushed events).
	first := recvEvent(t, s.events)
	if first.Text != "one" || !first.Replay {
		t.Fatalf("first event = %+v, want replay chunk \"one\"", first)
	}
	s.mu.Lock()
	s.loading = false
	s.mu.Unlock()

	// A boundary event flushes coalesced text first, then delivers itself.
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
