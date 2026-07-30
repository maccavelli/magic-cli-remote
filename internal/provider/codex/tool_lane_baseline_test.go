package codex

import (
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// TestToolLaneSupersedesNonTerminalUpdates is MADR 0057 M-2 for codex.
func TestToolLaneSupersedesNonTerminalUpdates(t *testing.T) {
	win := time.Hour
	s := &session{
		localID: "local-1",
		log:     slog.Default(),
		events:  make(chan event.Event, 64),
		done:    make(chan struct{}),
		cfg:     Config{StreamCoalesce: &win},
	}
	const n = 8
	for i := 0; i < n; i++ {
		s.emit(event.Event{
			Type:   event.TypeToolUpdate,
			ToolID: "cmd-1",
			Status: event.ToolStatusRunning,
			Text:   "out " + strconv.Itoa(i),
		})
	}
	s.emit(event.Event{Type: event.TypeTurnComplete, StopReason: "end_turn"})

	var updates []event.Event
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeToolUpdate {
				updates = append(updates, ev)
			}
			if ev.Type == event.TypeTurnComplete {
				if len(updates) != 1 {
					t.Fatalf("tool_call_update count = %d, want 1", len(updates))
				}
				if updates[0].Text != "out "+strconv.Itoa(n-1) {
					t.Fatalf("text = %q, want last", updates[0].Text)
				}
				return
			}
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	t.Fatalf("timeout; updates=%d", len(updates))
}

func TestToolLaneTerminalFlushesImmediately(t *testing.T) {
	win := time.Hour
	s := &session{
		localID: "local-1",
		log:     slog.Default(),
		events:  make(chan event.Event, 64),
		done:    make(chan struct{}),
		cfg:     Config{StreamCoalesce: &win},
	}
	s.emit(event.Event{
		Type:   event.TypeToolUpdate,
		ToolID: "cmd-1",
		Status: event.ToolStatusRunning,
		Text:   "partial",
	})
	s.emit(event.Event{
		Type:   event.TypeToolUpdate,
		ToolID: "cmd-1",
		Status: event.ToolStatusCompleted,
		Text:   "done",
	})
	select {
	case ev := <-s.events:
		if ev.Type != event.TypeToolUpdate || ev.Status != event.ToolStatusCompleted {
			t.Fatalf("want terminal update, got %+v", ev)
		}
		if ev.Text != "done" {
			t.Fatalf("text = %q, want done", ev.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for terminal tool update")
	}
}
