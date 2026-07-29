package opencode

import (
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

// Sub-agent output must not reach the transcript (MADR 0051 D6/D7).
//
// The frames below are the shapes measured on opencode 1.18.7 during a turn
// that spawned one `general` sub-agent to read two files. In that capture the
// child produced 43–81% of the turn's streamed text, plus its own tool cards
// and token counts — all of it routed to the parent's transcript, because the
// demux binds child aliases (which turn accounting needs) and nothing then
// filtered on origin.
//
// Each test drives the same frame twice, once as the parent and once as the
// child, so it asserts suppression rather than "emits nothing".
func childSuppressionSession(t *testing.T) (*httpSession, *treeHost) {
	t.Helper()
	h := newTreeHost()
	h.nodes["parent1"] = httpagent.NodeBusy
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	// Bind the child so frames for it are legitimately routed here.
	s.HandleEvent("session.created", json.RawMessage(
		`{"info":{"id":"c1","parentID":"parent1","title":"Read the files","agent":"general"}}`))
	h.mu.Lock()
	h.events = nil
	h.mu.Unlock()
	return s, h
}

func typesEmitted(h *treeHost) []event.Type {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]event.Type, 0, len(h.events))
	for _, ev := range h.events {
		out = append(out, ev.Type)
	}
	return out
}

func TestChildAssistantTextIsSuppressed(t *testing.T) {
	s, h := childSuppressionSession(t)

	// Role must be known or the delta is skipped for an unrelated reason.
	s.HandleEvent("message.updated", json.RawMessage(
		`{"info":{"id":"m1","role":"assistant"}}`))
	const delta = `{"messageID":"m1","partID":"p1","field":"text","delta":"hello"}`

	s.HandleEvent("message.part.delta", json.RawMessage(delta))
	if got := typesEmitted(h); len(got) == 0 {
		t.Fatal("a parent-origin delta must still stream to the transcript")
	}

	h.mu.Lock()
	h.events = nil
	h.mu.Unlock()
	h.asChild("c1", func() {
		s.HandleEvent("message.part.delta", json.RawMessage(delta))
	})
	if got := typesEmitted(h); len(got) != 0 {
		t.Fatalf("child-origin delta emitted %v; sub-agent text must not reach chat", got)
	}

	// And it must not leave the child's part id behind in the per-turn maps.
	s.mu.Lock()
	_, tracked := s.partText["p1-child"]
	s.mu.Unlock()
	if tracked {
		t.Error("child part id leaked into partText")
	}
}

func TestChildToolCallsAreSuppressed(t *testing.T) {
	s, h := childSuppressionSession(t)
	s.HandleEvent("message.updated", json.RawMessage(
		`{"info":{"id":"m1","role":"assistant"}}`))
	// Distinct call ids on purpose: reusing one would let the tool-emit dedupe
	// (noteTool/noteToolEmit) swallow the child frame, and the test would pass
	// whether or not the origin guard exists.
	toolPart := func(callID string) json.RawMessage {
		return json.RawMessage(`{"part":{"id":"p-` + callID + `","messageID":"m1",` +
			`"type":"tool","tool":"read","callID":"` + callID + `",` +
			`"state":{"status":"completed","title":"read a.txt"}}}`)
	}

	s.HandleEvent("message.part.updated", toolPart("parent-call"))
	var sawTool bool
	for _, ty := range typesEmitted(h) {
		if ty == event.TypeToolCall || ty == event.TypeToolUpdate {
			sawTool = true
		}
	}
	if !sawTool {
		t.Fatal("a parent-origin tool part must still render a card")
	}

	h.mu.Lock()
	h.events = nil
	h.mu.Unlock()
	h.asChild("c1", func() {
		s.HandleEvent("message.part.updated", toolPart("child-call"))
	})
	if got := typesEmitted(h); len(got) != 0 {
		t.Fatalf("child-origin tool part emitted %v; sub-agent tools must not reach chat", got)
	}
}

// A sub-agent's todo list is not the parent's plan. handleTodoUpdated
// deliberately accepted any tree-scoped sid, so a child's todowrite replaced
// the parent's plan panel wholesale under the event's replace semantics.
func TestChildTodosDoNotReplaceTheParentPlan(t *testing.T) {
	s, h := childSuppressionSession(t)
	const todos = `{"sessionID":"c1","todos":[{"id":"1","content":"child step","status":"pending"}]}`

	s.HandleEvent("todo.updated", json.RawMessage(
		`{"sessionID":"parent1","todos":[{"id":"1","content":"parent step","status":"pending"}]}`))
	var plans int
	for _, ty := range typesEmitted(h) {
		if ty == event.TypePlan {
			plans++
		}
	}
	if plans != 1 {
		t.Fatalf("parent todo emitted %d plan events, want 1", plans)
	}

	h.mu.Lock()
	h.events = nil
	h.mu.Unlock()
	h.asChild("c1", func() {
		s.HandleEvent("todo.updated", json.RawMessage(todos))
	})
	for _, ty := range typesEmitted(h) {
		if ty == event.TypePlan {
			t.Fatal("a child's todo list must not replace the parent's plan panel")
		}
	}
}

// The context-window indicator reports the conversation the user is having,
// not a sub-agent's private window.
func TestChildUsageDoesNotMoveTheContextIndicator(t *testing.T) {
	s, h := childSuppressionSession(t)
	const msg = `{"info":{"id":"m9","role":"assistant","modelID":"m","providerID":"p",` +
		`"tokens":{"input":100,"output":10,"reasoning":0,"cache":{"read":0,"write":0}}}}`

	s.HandleEvent("message.updated", json.RawMessage(msg))
	var usage int
	for _, ty := range typesEmitted(h) {
		if ty == event.TypeUsage {
			usage++
		}
	}
	if usage != 1 {
		t.Fatalf("parent message emitted %d usage events, want 1", usage)
	}

	h.mu.Lock()
	h.events = nil
	h.mu.Unlock()
	h.asChild("c1", func() {
		s.HandleEvent("message.updated", json.RawMessage(
			`{"info":{"id":"m10","role":"assistant","modelID":"m","providerID":"p",`+
				`"tokens":{"input":999,"output":99,"reasoning":0,"cache":{"read":0,"write":0}}}}`))
	})
	for _, ty := range typesEmitted(h) {
		if ty == event.TypeUsage {
			t.Fatal("a child's token counts must not be reported as session usage")
		}
	}
}

// Suppressing content must not disturb the turn lifecycle: tree-idle
// accounting is what stops a turn ending while a child is still working
// (MADR 0020), and it is independent of what reaches the transcript.
func TestSuppressionLeavesTurnLifecycleIntact(t *testing.T) {
	h := newTreeHost()
	h.nodes["parent1"] = httpagent.NodeBusy
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	s.HandleEvent("session.created", json.RawMessage(
		`{"info":{"id":"c1","parentID":"parent1","title":"T","agent":"general"}}`))
	h.asChild("c1", func() {
		s.HandleEvent("message.updated", json.RawMessage(`{"info":{"id":"m1","role":"assistant"}}`))
		s.HandleEvent("message.part.delta", json.RawMessage(
			`{"messageID":"m1","partID":"p1","field":"text","delta":"working"}`))
	})

	// Parent idles first; the child is still busy, so the turn must not end.
	s.HandleEvent("session.idle", json.RawMessage(`{"sessionID":"parent1"}`))
	h.mu.Lock()
	early := h.endViaTree
	h.mu.Unlock()
	if early != 0 {
		t.Fatalf("endViaTree=%d while the child is still working", early)
	}

	s.HandleEvent("session.idle", json.RawMessage(`{"sessionID":"c1"}`))
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.endViaTree != 1 {
		t.Fatalf("endViaTree=%d want 1 once the whole tree idled", h.endViaTree)
	}
	// Turn end clears the panel.
	var last *event.Event
	for i := range h.events {
		if h.events[i].Type == event.TypeSubagents {
			last = &h.events[i]
		}
	}
	if last == nil {
		t.Fatal("no subagents event at all")
	}
	if len(last.Subagents) != 0 {
		t.Fatalf("final subagents=%v, want the panel cleared at turn end", last.Subagents)
	}
}

// The clear is only for sessions that actually ran a sub-agent: emitting one
// on every turn end would put a control event on the wire to undo something
// that never happened.
func TestNoSubagentsMeansNoSubagentEvents(t *testing.T) {
	h := newTreeHost()
	h.nodes["parent1"] = httpagent.NodeBusy
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)

	s.HandleEvent("session.idle", json.RawMessage(`{"sessionID":"parent1"}`))
	for _, ty := range typesEmitted(h) {
		if ty == event.TypeSubagents {
			t.Fatal("a turn with no sub-agents must emit no subagents event")
		}
	}
}
