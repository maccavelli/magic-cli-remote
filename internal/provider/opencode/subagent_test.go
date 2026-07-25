package opencode

import (
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// The frame order below is copied from a real OpenCode 1.18.4 /global/event
// capture of a turn that used the task tool: the child's post-completion
// session.updated (final token counts / summary) lands AFTER the child has
// already idled, and before the parent idles.
//
//	session.status  CHILD  busy
//	session.status  CHILD  idle
//	session.idle    CHILD
//	session.updated CHILD          <-- metadata, NOT a restart
//	session.status  PARENT busy
//	session.status  PARENT idle
//	session.idle    PARENT
//
// Treating that session.updated as "child busy" wedged the turn forever: the
// tree never looked idle again and no further frame arrived for the child.
func TestChildSessionUpdatedAfterIdleDoesNotBlockEndTurn(t *testing.T) {
	h := newTreeHost()
	h.nodes["parent1"] = httpagent.NodeBusy
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	const childCreated = `{"info":{"id":"child1","parentID":"parent1","title":"Reply with banana"}}`

	s.HandleEvent("session.created", json.RawMessage(childCreated))
	s.HandleEvent("session.status", json.RawMessage(`{"sessionID":"child1","status":{"type":"busy"}}`))
	s.HandleEvent("session.status", json.RawMessage(`{"sessionID":"child1","status":{"type":"idle"}}`))
	s.HandleEvent("session.idle", json.RawMessage(`{"sessionID":"child1"}`))

	// The frame that used to poison the tree.
	s.HandleEvent("session.updated", json.RawMessage(childCreated))

	h.mu.Lock()
	if got := h.nodes["child1"]; httpagent.NodeBusyForEndTurn(got) {
		h.mu.Unlock()
		t.Fatalf("child1 status=%q after post-idle session.updated; want a non-busy status", got)
	}
	h.mu.Unlock()

	s.HandleEvent("session.status", json.RawMessage(`{"sessionID":"parent1","status":{"type":"busy"}}`))
	s.HandleEvent("session.status", json.RawMessage(`{"sessionID":"parent1","status":{"type":"idle"}}`))
	s.HandleEvent("session.idle", json.RawMessage(`{"sessionID":"parent1"}`))

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.endViaTree != 1 {
		t.Fatalf("endViaTree=%d want 1 — turn hung after subagent finished", h.endViaTree)
	}
	if h.turnOn {
		t.Fatal("turn still active after whole tree idled")
	}
}

// A child we have never seen must not be conjured into a busy tree node by a
// bare metadata update — only session.created / session.status=busy mean work.
func TestUnknownChildSessionUpdatedIsNotBusy(t *testing.T) {
	h := newTreeHost()
	h.nodes["parent1"] = httpagent.NodeIdle
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	s.HandleEvent("session.updated", json.RawMessage(
		`{"info":{"id":"stale-child","parentID":"parent1","title":"old work"}}`))

	h.mu.Lock()
	defer h.mu.Unlock()
	if got := h.nodes["stale-child"]; httpagent.NodeBusyForEndTurn(got) {
		t.Fatalf("stale-child status=%q; a metadata update must not mark it busy", got)
	}
	// Still bound: its frames must keep routing into this transcript.
	if len(h.bound) != 1 || h.bound[0] != "stale-child" {
		t.Fatalf("bound=%v want [stale-child]", h.bound)
	}
	for _, ev := range h.events {
		if ev.ToolID == "subagent:stale-child" {
			t.Fatalf("unexpected subagent card for an unknown child: %+v", ev)
		}
	}
}

// A completed subagent card must never reopen: OpenCode keeps emitting
// session.updated for a child long after it finished.
func TestCompletedSubagentCardDoesNotReopen(t *testing.T) {
	h := newTreeHost()
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	const child = `{"info":{"id":"c1","parentID":"parent1","title":"T"}}`
	s.HandleEvent("session.created", json.RawMessage(child))
	s.HandleEvent("session.idle", json.RawMessage(`{"sessionID":"c1"}`))
	s.HandleEvent("session.updated", json.RawMessage(child))
	s.HandleEvent("session.updated", json.RawMessage(child))

	h.mu.Lock()
	defer h.mu.Unlock()
	var statuses []string
	for _, ev := range h.events {
		if ev.ToolID == "subagent:c1" {
			statuses = append(statuses, ev.Status)
		}
	}
	if len(statuses) != 2 || statuses[0] != "running" || statuses[1] != "completed" {
		t.Fatalf("card status sequence=%v want [running completed]", statuses)
	}
}

// The child's own idle should close its card immediately, rather than leaving
// it spinning until the parent's turn ends.
func TestChildIdleCompletesSubagentCard(t *testing.T) {
	h := newTreeHost()
	h.nodes["parent1"] = httpagent.NodeBusy
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	s.HandleEvent("session.created", json.RawMessage(
		`{"info":{"id":"c1","parentID":"parent1","title":"T"}}`))
	s.HandleEvent("session.idle", json.RawMessage(`{"sessionID":"c1"}`))

	h.mu.Lock()
	defer h.mu.Unlock()
	// Parent is still busy, so the turn must not have ended...
	if h.endViaTree != 0 {
		t.Fatalf("endViaTree=%d want 0 while parent busy", h.endViaTree)
	}
	// ...but the child card is already closed.
	var completed bool
	for _, ev := range h.events {
		if ev.ToolID == "subagent:c1" && ev.Status == "completed" {
			completed = true
		}
	}
	if !completed {
		t.Fatal("child idle must complete its subagent card")
	}
}

// session.status idle for a child closes its card too (some engines emit only
// the status form).
func TestChildStatusIdleCompletesSubagentCard(t *testing.T) {
	h := newTreeHost()
	h.nodes["parent1"] = httpagent.NodeBusy
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	s.HandleEvent("session.created", json.RawMessage(
		`{"info":{"id":"c1","parentID":"parent1","title":"T"}}`))
	s.HandleEvent("session.status", json.RawMessage(`{"sessionID":"c1","status":{"type":"idle"}}`))

	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ev := range h.events {
		if ev.ToolID == "subagent:c1" && ev.Status == "completed" {
			return
		}
	}
	t.Fatal("child session.status=idle must complete its subagent card")
}

// A parent metadata update is not a tree event at all.
func TestParentSessionUpdatedIgnored(t *testing.T) {
	h := newTreeHost()
	h.nodes["parent1"] = httpagent.NodeIdle
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	s.HandleEvent("session.updated", json.RawMessage(
		`{"info":{"id":"parent1","title":"chat"}}`))

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.bound) != 0 {
		t.Fatalf("parent must not be bound as its own child: %v", h.bound)
	}
	if h.nodes["parent1"] != httpagent.NodeIdle {
		t.Fatalf("parent status changed to %q", h.nodes["parent1"])
	}
}

// An errored turn must reset per-turn state; otherwise the next turn's first
// tool arrives as tool_call_update with no preceding tool_call.
func TestSessionErrorResetsTurnState(t *testing.T) {
	h := newTreeHost()
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	s.HandleEvent("message.updated", json.RawMessage(`{"info":{"id":"m1","role":"assistant"}}`))
	s.HandleEvent("message.part.updated", json.RawMessage(`{
		"part":{"id":"p1","messageID":"m1","type":"tool","tool":"bash","callID":"call1",
		        "state":{"status":"running","title":"ls"}}
	}`))
	s.HandleEvent("session.error", json.RawMessage(
		`{"sessionID":"parent1","error":{"name":"ProviderError","data":{"message":"boom"}}}`))

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.seenTools) != 0 {
		t.Fatalf("seenTools=%v; errored turn must reset per-turn state", s.seenTools)
	}
}

// Sanity: the first tool sighting is tool_call, later ones tool_call_update.
func TestToolCallThenUpdate(t *testing.T) {
	h := newTreeHost()
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	s.HandleEvent("message.updated", json.RawMessage(`{"info":{"id":"m1","role":"assistant"}}`))
	for _, st := range []string{"pending", "running", "completed"} {
		s.HandleEvent("message.part.updated", json.RawMessage(`{
			"part":{"id":"p1","messageID":"m1","type":"tool","tool":"bash","callID":"call1",
			        "state":{"status":"`+st+`","title":"ls"}}
		}`))
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	var types []event.Type
	for _, ev := range h.events {
		if ev.ToolID == "call1" {
			types = append(types, ev.Type)
		}
	}
	want := []event.Type{event.TypeToolCall, event.TypeToolUpdate, event.TypeToolUpdate}
	if len(types) != len(want) {
		t.Fatalf("tool event types=%v want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("tool event types=%v want %v", types, want)
		}
	}
}
