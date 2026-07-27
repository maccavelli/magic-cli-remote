package codex

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// TestTurnPlanUpdatedEmitsTypePlan: codex's turn/plan/updated notification
// must surface as a TypePlan event with the daemon's vocabulary
// (pending|in_progress|completed) and a default medium priority.
//
// Wire shape: {threadId, turnId, plan: [{step, status}], explanation?}
// per /tmp/codex-spike-0.145.0/schema/v2/TurnPlanUpdatedNotification.json.
func TestTurnPlanUpdatedEmitsTypePlan(t *testing.T) {
	s := &session{
		events:  make(chan event.Event, 8),
		done:    make(chan struct{}),
		agentID: "thread-x",
		log:     silentLogger(),
	}
	defer close(s.done)

	frame := `{
		"threadId": "thread-x",
		"turnId": "turn-1",
		"plan": [
			{"step": "first step", "status": "completed"},
			{"step": "second step", "status": "inProgress"},
			{"step": "third step", "status": "pending"}
		]
	}`
	s.handleNotification("turn/plan/updated", []byte(frame))

	events := drain(s)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Type != event.TypePlan {
		t.Fatalf("type = %s, want plan", events[0].Type)
	}
	want := []event.PlanEntry{
		{Content: "first step", Status: "completed", Priority: "medium"},
		{Content: "second step", Status: "in_progress", Priority: "medium"},
		{Content: "third step", Status: "pending", Priority: "medium"},
	}
	if len(events[0].Entries) != len(want) {
		t.Fatalf("entries = %d, want %d", len(events[0].Entries), len(want))
	}
	for i, e := range events[0].Entries {
		if e.Content != want[i].Content || e.Status != want[i].Status || e.Priority != want[i].Priority {
			t.Errorf("entry[%d] = %+v, want %+v", i, e, want[i])
		}
	}
}

// TestTurnPlanUpdatedEmptyEmitsPlanClear: an empty plan (plan: [] or
// plan: null) emits a TypePlan with an empty non-nil slice so the
// phone-side panel renders an empty list, not a stale prior plan
// (replace-semantics).
func TestTurnPlanUpdatedEmptyEmitsPlanClear(t *testing.T) {
	s := &session{
		events:  make(chan event.Event, 4),
		done:    make(chan struct{}),
		agentID: "thread-x",
		log:     silentLogger(),
	}
	defer close(s.done)

	frame := `{
		"threadId": "thread-x",
		"turnId": "turn-1",
		"plan": []
	}`
	s.handleNotification("turn/plan/updated", []byte(frame))

	events := drain(s)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Type != event.TypePlan {
		t.Fatalf("type = %s, want plan", events[0].Type)
	}
	if events[0].Entries == nil {
		t.Error("entries must be a non-nil empty slice (replace-semantics)")
	}
	if len(events[0].Entries) != 0 {
		t.Errorf("entries len = %d, want 0", len(events[0].Entries))
	}
}

// TestCodexPlanStatusMapping pins the v2 → daemon vocabulary mapping.
func TestCodexPlanStatusMapping(t *testing.T) {
	cases := map[string]string{
		"pending":    "pending",
		"inProgress": "in_progress",
		"completed":  "completed",
		"unknown":    "pending",
		"":           "pending",
	}
	for in, want := range cases {
		if got := codexPlanStatus(in); got != want {
			t.Errorf("codexPlanStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestPlanItemDoesNotEmitToolCard: MADR 0035 D1 follow-on. The `plan`
// item type stays in the silent class (it is a summary; the structured
// plan comes via turn/plan/updated). A future codex release that
// promotes a plan item to a tool-like status would need a new
// itemsRenderedAsTools entry, plus this regression guard.
func TestPlanItemDoesNotEmitToolCard(t *testing.T) {
	if _, isTool := itemsRenderedAsTools["plan"]; isTool {
		t.Fatal("plan is in the allowlist; remove this guard if intentionally promoted")
	}
}
