package grok

import (
	"log/slog"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func TestAutoAllowPrefersAllowKind(t *testing.T) {
	resp := autoAllow(acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{
			{OptionId: "deny", Name: "Deny", Kind: "reject_once"},
			{OptionId: "allow", Name: "Allow", Kind: "allow_once"},
		},
	})
	if resp.Outcome.Selected == nil {
		t.Fatal("expected selected")
	}
	if string(resp.Outcome.Selected.OptionId) != "allow" {
		t.Fatalf("got %s", resp.Outcome.Selected.OptionId)
	}
}

func TestContentText(t *testing.T) {
	if contentText(acp.TextBlock("hi")) != "hi" {
		t.Fatal("expected hi")
	}
	if contentText(acp.ContentBlock{}) != "" {
		t.Fatal("expected empty")
	}
}

// A Plan update must emit a single plan event carrying the mapped entries,
// with status/priority normalised to the fixed vocabulary.
func TestSessionUpdatePlanEmitsMappedEntries(t *testing.T) {
	s := &session{
		localID: "local-1",
		agentID: "agent-1",
		events:  make(chan event.Event, 8),
		log:     slog.Default(),
	}
	err := s.SessionUpdate(t.Context(), acp.SessionNotification{
		Update: acp.SessionUpdate{
			Plan: &acp.SessionUpdatePlan{
				Entries: []acp.PlanEntry{
					{Content: "step one", Status: acp.PlanEntryStatusInProgress, Priority: acp.PlanEntryPriorityHigh},
					{Content: "step two", Status: acp.PlanEntryStatusPending, Priority: acp.PlanEntryPriorityLow},
					// Empty priority ("") is unknown and must fall back to medium.
					{Content: "step three", Status: acp.PlanEntryStatusCompleted},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := recvEvent(t, s.events)
	if ev.Type != event.TypePlan {
		t.Fatalf("want plan event, got %s", ev.Type)
	}
	if ev.SessionID != "local-1" {
		t.Fatalf("session id = %q", ev.SessionID)
	}
	want := []event.PlanEntry{
		{Content: "step one", Status: event.PlanStatusInProgress, Priority: event.PlanPriorityHigh},
		{Content: "step two", Status: event.PlanStatusPending, Priority: event.PlanPriorityLow},
		{Content: "step three", Status: event.PlanStatusCompleted, Priority: event.PlanPriorityMedium},
	}
	if len(ev.Entries) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(ev.Entries), len(want), ev.Entries)
	}
	for i, w := range want {
		if ev.Entries[i] != w {
			t.Fatalf("entry %d = %+v, want %+v", i, ev.Entries[i], w)
		}
	}
}

// PlanRemoved must emit a plan event whose entries are an empty (non-nil) list:
// the client interprets this as clearing the current plan (replace-semantics).
func TestSessionUpdatePlanRemovedEmitsEmptyPlan(t *testing.T) {
	s := &session{
		localID: "local-1",
		agentID: "agent-1",
		events:  make(chan event.Event, 8),
		log:     slog.Default(),
	}
	err := s.SessionUpdate(t.Context(), acp.SessionNotification{
		Update: acp.SessionUpdate{
			PlanRemoved: &acp.SessionUpdatePlanRemoved{Id: "plan-1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := recvEvent(t, s.events)
	if ev.Type != event.TypePlan {
		t.Fatalf("want plan event, got %s", ev.Type)
	}
	if ev.Entries == nil {
		t.Fatal("expected non-nil empty entries")
	}
	if len(ev.Entries) != 0 {
		t.Fatalf("expected empty entries, got %+v", ev.Entries)
	}
}

func recvEvent(t *testing.T, ch <-chan event.Event) event.Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return event.Event{}
	}
}

// UserMessageChunk must not emit user_message: Prompt already does, and ACP
// echoes the same prompt (often in chunks), which duplicated UI bubbles.
func TestSessionUpdateIgnoresUserMessageChunk(t *testing.T) {
	s := &session{
		localID: "local-1",
		agentID: "agent-1",
		events:  make(chan event.Event, 8),
		log:     slog.Default(),
	}
	err := s.SessionUpdate(t.Context(), acp.SessionNotification{
		Update: acp.SessionUpdate{
			UserMessageChunk: &acp.SessionUpdateUserMessageChunk{
				Content: acp.TextBlock("hello twice?"),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-s.events:
		t.Fatalf("expected no event, got %+v", ev)
	case <-time.After(20 * time.Millisecond):
		// ok
	}
}

// Control events must not be dropped when the event buffer is full of chunks.
func TestControlEventNotDroppedWhenBufferFull(t *testing.T) {
	s := &session{
		localID: "local-1",
		agentID: "agent-1",
		events:  make(chan event.Event, 1),
		log:     slog.Default(),
	}
	// Fill the buffer with a best-effort chunk.
	s.emit(event.Event{
		Type:      event.TypeAssistantChunk,
		SessionID: "local-1",
		Timestamp: time.Now().UTC(),
		Text:      "fill",
	})
	// Second chunk is dropped (buffer full) — that is OK.
	s.emit(event.Event{
		Type:      event.TypeAssistantChunk,
		SessionID: "local-1",
		Timestamp: time.Now().UTC(),
		Text:      "drop-me",
	})

	done := make(chan struct{})
	go func() {
		s.emit(event.Event{
			Type:         event.TypePermissionResolved,
			SessionID:    "local-1",
			Timestamp:    time.Now().UTC(),
			PermissionID: "p1",
			Status:       event.PermissionStatusResolved,
		})
		close(done)
	}()

	// Drain the filler so the control event can land.
	select {
	case <-s.events:
	case <-time.After(time.Second):
		t.Fatal("expected filler event")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("control emit still blocked / dropped")
	}

	ev := recvEvent(t, s.events)
	if ev.Type != event.TypePermissionResolved || ev.PermissionID != "p1" {
		t.Fatalf("want permission_resolved, got %+v", ev)
	}
}
