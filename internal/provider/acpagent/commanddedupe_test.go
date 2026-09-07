package acpagent

import (
	"context"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// TestAvailableCommandsDedupes proves grok's emit site routes through the
// shared deduper (MADR 0137 F2).
//
// grok is the provider the finding came from: 22 `available_commands_update`
// frames inside one `hi` turn, each a full list crossing the websocket,
// landing in session history and re-rendering on the phone. This drives the
// real notification handler, not the helper, because a provider that still
// called emit directly would pass every deduper unit test and keep spamming.
func TestAvailableCommandsDedupes(t *testing.T) {
	s := newQueueTestSession()
	ctx := context.Background()

	note := func(desc string) acp.SessionNotification {
		return acp.SessionNotification{
			SessionId: acp.SessionId(s.agentID),
			Update: acp.SessionUpdate{
				AvailableCommandsUpdate: &acp.SessionAvailableCommandsUpdate{
					AvailableCommands: []acp.AvailableCommand{
						{Name: "compact", Description: desc},
					},
				},
			},
		}
	}

	for i := 0; i < 10; i++ {
		if err := s.SessionUpdate(ctx, note("compress history")); err != nil {
			t.Fatal(err)
		}
	}
	if got := drainCommandEvents(s); got != 1 {
		t.Fatalf("ten identical updates produced %d events, want 1", got)
	}

	if err := s.SessionUpdate(ctx, note("compress conversation history")); err != nil {
		t.Fatal(err)
	}
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
