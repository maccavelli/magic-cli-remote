package acphttp

import (
	"log/slog"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// TestAvailableCommandsDedupes proves goose's emit site routes through the
// shared deduper (MADR 0137 F2).
//
// It drives the real update handler with the raw JSON goose sends, rather than
// the helper, because a provider that still called emit directly would pass
// every deduper unit test in the tree and keep re-rendering an unchanged list
// on the phone.
func TestAvailableCommandsDedupes(t *testing.T) {
	s := &session{
		localID: "local-c",
		agentID: "agent-c",
		events:  make(chan event.Event, 64),
		done:    make(chan struct{}),
		log:     slog.Default(),
	}

	frame := func(desc string) []byte {
		return []byte(`{"sessionUpdate":"available_commands_update",` +
			`"availableCommands":[{"name":"compact","description":"` + desc + `"}]}`)
	}

	for i := 0; i < 10; i++ {
		s.handleUpdate(frame("compress history"))
	}
	if got := drainCommandEvents(s); got != 1 {
		t.Fatalf("ten identical updates produced %d events, want 1", got)
	}

	s.handleUpdate(frame("compress conversation history"))
	if got := drainCommandEvents(s); got != 1 {
		t.Fatalf("a changed description produced %d events, want 1", got)
	}
}

func drainCommandEvents(s *session) int {
	n := 0
	for {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeAvailableCommands {
				n++
			}
		default:
			return n
		}
	}
}
