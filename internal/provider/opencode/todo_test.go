package opencode

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider/httpagent"
)

func TestMapOpenCodeTodos(t *testing.T) {
	got := mapOpenCodeTodos([]openCodeTodo{
		{ID: "1", Content: "  a  ", Status: "pending", Priority: "high"},
		{ID: "2", Content: "b", Status: "in_progress", Priority: "medium"},
		{ID: "3", Content: "c", Status: "completed", Priority: "low"},
		{ID: "4", Content: "d", Status: "cancelled", Priority: "high"},
		{ID: "5", Content: "e", Status: "weird", Priority: "nope"},
		{ID: "6", Content: "  ", Status: "pending", Priority: "high"},
	})
	if len(got) != 5 {
		t.Fatalf("len=%d want 5 (empty content dropped)", len(got))
	}
	if got[0].Content != "a" || got[0].Status != "pending" || got[0].Priority != "high" {
		t.Fatalf("row0=%+v", got[0])
	}
	if got[1].Status != "in_progress" || got[2].Status != "completed" {
		t.Fatalf("row1/2=%+v %+v", got[1], got[2])
	}
	if got[3].Status != event.PlanStatusPending || !strings.HasPrefix(got[3].Content, "(cancelled) ") {
		t.Fatalf("cancelled mapping=%+v", got[3])
	}
	if got[3].Content != "(cancelled) d" {
		t.Fatalf("cancelled content=%q", got[3].Content)
	}
	if got[4].Status != event.PlanStatusPending || got[4].Priority != event.PlanPriorityMedium {
		t.Fatalf("unknown status/priority=%+v", got[4])
	}
}

func TestMapOpenCodeTodosEmptyClear(t *testing.T) {
	got := mapOpenCodeTodos(nil)
	if got == nil {
		t.Fatal("want non-nil empty slice for clear semantics")
	}
	if len(got) != 0 {
		t.Fatalf("len=%d", len(got))
	}
}

func TestTodoUpdatedEmitsPlan(t *testing.T) {
	h := &captureHost{}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.HandleEvent("todo.updated", json.RawMessage(`{
		"sessionID":"ses_test",
		"todos":[
			{"id":"1","content":"Read code","status":"completed","priority":"high"},
			{"id":"2","content":"Write fix","status":"in_progress","priority":"medium"},
			{"id":"3","content":"Skip me","status":"cancelled","priority":"low"}
		]
	}`))
	var plan *event.Event
	for i := range h.events {
		if h.events[i].Type == event.TypePlan {
			plan = &h.events[i]
		}
	}
	if plan == nil {
		t.Fatal("expected plan event")
	}
	if len(plan.Entries) != 3 {
		t.Fatalf("entries=%d", len(plan.Entries))
	}
	if plan.Entries[2].Content != "(cancelled) Skip me" || plan.Entries[2].Status != "pending" {
		t.Fatalf("cancelled entry=%+v", plan.Entries[2])
	}
}

func TestTodoUpdatedEmptyClearsPlan(t *testing.T) {
	h := &captureHost{}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.HandleEvent("todo.updated", json.RawMessage(`{"sessionID":"ses_test","todos":[]}`))
	if len(h.events) != 1 || h.events[0].Type != event.TypePlan {
		t.Fatalf("events=%v", h.eventTypes())
	}
	if h.events[0].Entries == nil || len(h.events[0].Entries) != 0 {
		t.Fatalf("want empty clear, got %+v", h.events[0].Entries)
	}
}

func TestResyncTodosWhileTreeBusy(t *testing.T) {
	h := newTreeHost()
	h.turnOn = true
	h.api = func(_ context.Context, method, path string, _, out any) error {
		switch {
		case strings.Contains(path, "/children"):
			return json.Unmarshal([]byte(`[{"id":"child1"}]`), out)
		case strings.Contains(path, "/session/status"):
			return json.Unmarshal([]byte(`{"parent1":{"type":"idle"},"child1":{"type":"busy"}}`), out)
		case strings.Contains(path, "/permission"), strings.Contains(path, "/question"):
			return json.Unmarshal([]byte(`[]`), out)
		case strings.Contains(path, "/todo"):
			return json.Unmarshal([]byte(`[
				{"id":"1","content":"step one","status":"pending","priority":"high"},
				{"id":"2","content":"step two","status":"in_progress","priority":"medium"}
			]`), out)
		case strings.Contains(path, "/message"):
			// Would recover EndTurn if we got here — tree busy must prevent that.
			return json.Unmarshal([]byte(`[
				{"info":{"id":"msg_a","role":"assistant","time":{"created":1,"completed":2}},
				 "parts":[{"id":"p","type":"text","text":"done"}]}
			]`), out)
		default:
			return nil
		}
	}
	d := &httpDialect{log: slog.Default()}
	s := d.NewSession(h).(*httpSession)
	s.Resync(context.Background(), time.Unix(0, 0))

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.endTurns != 0 || !h.turnOn {
		t.Fatalf("must not EndTurn while child busy: end=%d on=%v", h.endTurns, h.turnOn)
	}
	var plan *event.Event
	for i := range h.events {
		if h.events[i].Type == event.TypePlan {
			plan = &h.events[i]
		}
	}
	if plan == nil || len(plan.Entries) != 2 {
		t.Fatalf("want plan with 2 entries, plan=%v events=%v", plan, h.events)
	}
	if h.nodes["child1"] != httpagent.NodeBusy {
		t.Fatalf("child status=%q", h.nodes["child1"])
	}
}

func TestIsControlPlan(t *testing.T) {
	if !event.IsControl(event.TypePlan) {
		t.Fatal("TypePlan must be control (MADR 0020 Sprint 2)")
	}
}
