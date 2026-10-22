package acpagent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// TestRefusedUpdateIsNoticedBeforeTurnComplete is MADR 0207 D13 over a real
// connection, with production's options: an update the SDK refuses is still
// dropped, and the turn ends with exactly one notice saying so, before
// turn_complete, while a valid update in the same turn is delivered.
//
// The refused update is a tool_call whose location has a relative path: it
// decodes, and the SDK's validation refuses it (ToolCallLocation.validate,
// acp-go-sdk2 types_gen.go; "a failure drops the whole notification").
func TestRefusedUpdateIsNoticedBeforeTurnComplete(t *testing.T) {
	s, a := newWireSession(t, `{}`)
	if err := s.Prompt(context.Background(), []provider.Content{{Type: "text", Text: "go"}}); err != nil {
		t.Fatal(err)
	}
	req := a.request("session/prompt", wireBudget)
	update := func(u map[string]any) {
		a.send(map[string]any{"jsonrpc": "2.0", "method": "session/update",
			"params": map[string]any{"sessionId": "agent-1", "update": u}})
	}
	update(map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "read",
		"locations": []map[string]any{{"path": "relative/file.go"}}})
	update(map[string]any{"sessionUpdate": "notice", "severity": "info", "title": "still delivered"})
	a.respond(req, map[string]any{"stopReason": "end_turn"})

	var evs []event.Event
	deadline := time.After(wireBudget)
	for done := false; !done; {
		select {
		case ev := <-s.events:
			evs = append(evs, ev)
			done = ev.Type == event.TypeTurnComplete
		case <-deadline:
			t.Fatalf("no turn_complete; %d events so far", len(evs))
		}
	}

	var refused []string
	delivered, toolCall := false, false
	for _, ev := range evs {
		switch {
		case ev.Type == event.TypeNotice && strings.Contains(ev.Text, "the protocol does not allow"):
			refused = append(refused, ev.Text)
		case ev.Type == event.TypeAgentNotice && ev.NoticeTitle == "still delivered":
			delivered = true
		case ev.Type == event.TypeToolCall && ev.ToolID == "t1":
			toolCall = true
		}
	}
	if len(refused) != 1 {
		t.Fatalf("got %d refused-message notices %q, want 1", len(refused), refused)
	}
	if want := "The agent sent 1 message the protocol does not allow, which was dropped (last: session/update)."; refused[0] != want {
		t.Fatalf("notice = %q, want %q", refused[0], want)
	}
	if toolCall {
		t.Fatal("the refused tool_call reached the session: the SDK's verdict was not kept")
	}
	if !delivered {
		t.Fatal("the valid update in the same turn was not delivered")
	}
}
