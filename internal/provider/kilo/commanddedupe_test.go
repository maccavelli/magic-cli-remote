package kilo

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// TestAdvertiseCommandsDedupes proves this provider's emit site actually
// routes through the shared deduper (MADR 0137 F2).
//
// The helper is unit-tested in internal/event; what this pins is the wiring.
// A provider that kept calling h.Emit directly would pass every deduper test
// in the tree and still spam the phone, which is exactly the state 0137 found
// grok in — 22 available_commands_update frames inside one `hi` turn.
func TestAdvertiseCommandsDedupes(t *testing.T) {
	h := &captureHost{}
	o := &httpSession{h: h}

	list := []event.AvailableCommand{
		{Name: "init", Description: "guided AGENTS.md setup"},
		{Name: "compact", Description: "compress history"},
	}
	for i := 0; i < 10; i++ {
		o.emitCommands(list)
	}
	if got := countCommandEvents(h); got != 1 {
		t.Fatalf("ten identical advertisements produced %d events, want 1", got)
	}

	changed := []event.AvailableCommand{
		{Name: "init", Description: "guided AGENTS.md setup"},
		{Name: "compact", Description: "compress conversation history"},
	}
	o.emitCommands(changed)
	if got := countCommandEvents(h); got != 2 {
		t.Fatalf("a changed description produced %d events total, want 2", got)
	}
}

func countCommandEvents(h *captureHost) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, ev := range h.events {
		if ev.Type == event.TypeAvailableCommands {
			n++
		}
	}
	return n
}
