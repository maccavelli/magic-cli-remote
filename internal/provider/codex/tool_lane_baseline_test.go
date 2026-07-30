package codex

import (
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// TestToolUpdatesPassThroughWithoutToolLane is MADR 0057 Phase 0 / M-2 baseline:
// codex builds chunkbuf without WithToolLane, so non-terminal updates pass
// through one-for-one (Phase B enables the lane).
func TestToolUpdatesPassThroughWithoutToolLane(t *testing.T) {
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

	var updates int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeToolUpdate {
				updates++
			}
			if ev.Type == event.TypeTurnComplete {
				if updates != n {
					t.Fatalf("tool_call_update count = %d, want %d (no tool lane)", updates, n)
				}
				return
			}
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	t.Fatalf("timeout; tool updates seen = %d, want %d", updates, n)
}
