package event_test

import (
	"encoding/json"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func TestCodexEventTypesAreBoundedControlEvents(t *testing.T) {
	want := []event.Type{
		event.TypeCodexProgress,
		event.TypeCodexWarning,
		event.TypeCodexModelReroute,
		event.TypeCodexModelVerification,
		event.TypeCodexTerminalInteraction,
		event.TypeCodexUnsupportedItem,
	}
	types := map[event.Type]bool{}
	for _, typ := range event.Types() {
		types[typ] = true
	}
	for _, typ := range want {
		if !types[typ] {
			t.Errorf("event.Types missing %q", typ)
		}
		if !event.IsControl(typ) {
			t.Errorf("event.IsControl(%q) = false", typ)
		}
	}

	ev := event.Event{Type: event.TypeCodexProgress, Codex: &event.CodexPayload{
		Key: "item:42", Kind: "mcp_progress", Status: "running",
		Title: "Tool progress", Text: string(make([]byte, event.MaxCodexTextBytes+32)),
	}}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > event.MaxCodexTextBytes+1024 {
		t.Fatalf("bounded event encoded to %d bytes", len(b))
	}
}
