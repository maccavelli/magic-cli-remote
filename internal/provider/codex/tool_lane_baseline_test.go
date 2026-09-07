package codex

import (
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// TestToolLaneConcatenatesNonTerminalUpdates is MADR 0057 M-2 for codex, with
// the measurement M-2 asked for and did not get.
//
// M-2 said: "Measure Codex item streams and Goose tool updates before
// defaulting — opt-in flag first if behavior differs." Codex was opted into the
// *replacing* lane without that measurement. It differs: every codex
// `tool_call_update` sets Text to a delta, not to the current whole
// (notifications.go item/fileChange/outputDelta, session.go
// item/commandExecution/outputDelta), so replacing discarded a line of the
// agent's command output for every pair of deltas inside one coalesce window
// (MADR 0138 F2).
//
// This test asserted the discard. It now asserts that the output survives, and
// that the coalescing M-2 wanted still happens — 8 deltas, 1 frame.
func TestToolLaneConcatenatesNonTerminalUpdates(t *testing.T) {
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
				var want string
				for i := 0; i < n; i++ {
					want += "out " + strconv.Itoa(i)
				}
				if updates[0].Text != want {
					t.Fatalf("text = %q, want every delta concatenated (%q)", updates[0].Text, want)
				}
				return
			}
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	t.Fatalf("timeout; updates=%d", len(updates))
}

// TestToolLaneTerminalFlushesImmediately: a finished card must not wait on the
// window. The held text still rides along with it — the terminal update is the
// last delta of the stream, not a replacement for everything before it.
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
		if ev.Text != "partialdone" {
			t.Fatalf("text = %q, want the held output plus the terminal delta", ev.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for terminal tool update")
	}
}

// TestOutputDeltaNotificationsSurviveTheLane drives the real notification
// shapes through the real decode path, not synthesised events.
//
// The captured codex fixture (testdata/wire/0.152.1) is a `hi` turn and
// contains **zero** outputDelta frames — it never ran a command — so it cannot
// exercise this. Feeding the wire JSON to handleNotification covers the same
// ground: the decode in notifications.go / session.go and the append lane it
// feeds, together.
func TestOutputDeltaNotificationsSurviveTheLane(t *testing.T) {
	for _, method := range []string{
		"item/commandExecution/outputDelta",
		"item/fileChange/outputDelta",
	} {
		t.Run(method, func(t *testing.T) {
			win := time.Hour
			s := &session{
				localID: "local-1",
				agentID: "thread-1",
				log:     slog.Default(),
				events:  make(chan event.Event, 64),
				done:    make(chan struct{}),
				cfg:     Config{StreamCoalesce: &win},
			}

			lines := []string{"compiling...\n", "running tests\n", "3 passed\n"}
			for _, line := range lines {
				s.handleNotification(method, json.RawMessage(
					`{"itemId":"exec-1","delta":`+strconv.Quote(line)+`}`))
			}
			// A boundary drains the lane.
			s.emit(event.Event{Type: event.TypeTurnComplete, StopReason: "end_turn"})

			var got string
			var updates int
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				select {
				case ev := <-s.events:
					if ev.Type == event.TypeToolUpdate {
						updates++
						got += ev.Text
					}
					if ev.Type == event.TypeTurnComplete {
						want := strings.Join(lines, "")
						if got != want {
							t.Fatalf("delivered %q, want %q — every byte of command output must survive", got, want)
						}
						if updates != 1 {
							t.Fatalf("delivered %d frames, want 1: the lane must still coalesce", updates)
						}
						return
					}
				default:
					time.Sleep(2 * time.Millisecond)
				}
			}
			t.Fatal("timeout waiting for turn_complete")
		})
	}
}
