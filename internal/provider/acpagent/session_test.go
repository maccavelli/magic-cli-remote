package acpagent

import (
	"context"
	"log/slog"
	"strings"
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

// Close with a pending permission must unblock the waiter and emit
// permission_resolved (Phase 2.5 / B.2).
func TestCloseUnblocksPendingPermission(t *testing.T) {
	s := &session{
		localID:    "l",
		agentID:    "a",
		events:     make(chan event.Event, 16),
		done:       make(chan struct{}),
		log:        slog.Default(),
		pending:    make(map[string]chan permResult),
		procExited: true, // skip process kill
	}
	ch := make(chan permResult, 1)
	s.pending["p1"] = ch

	// Close emits permission_resolved into events (cap 16) and unblocks ch.
	if err := s.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case res := <-ch:
		if !res.cancelled {
			t.Fatal("want cancelled result on waiter")
		}
	default:
		t.Fatal("permission waiter not unblocked by Close")
	}
	// Client-facing cancellation notice must land in the event stream.
	select {
	case ev := <-s.events:
		if ev.Type != event.TypePermissionResolved || ev.Status != event.PermissionStatusCancelled {
			t.Fatalf("event=%+v", ev)
		}
	default:
		t.Fatal("expected permission_resolved event on close")
	}
}

// A remote permission request that is never answered must time out and resolve
// as cancelled (fail safe) so the agent stops waiting, with a notice explaining
// why.
func TestPermissionTimeoutCancels(t *testing.T) {
	s := &session{
		localID: "l",
		events:  make(chan event.Event, 16),
		log:     slog.Default(),
		pending: make(map[string]chan permResult),
		cfg:     Config{PermissionTimeout: 40 * time.Millisecond},
	}
	title := "Bash"
	resp, err := s.RequestPermission(t.Context(), acp.RequestPermissionRequest{
		Options: []acp.PermissionOption{
			{OptionId: "allow", Name: "Allow", Kind: "allow_once"},
		},
		ToolCall: acp.ToolCallUpdate{Title: &title},
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp.Outcome.Cancelled == nil {
		t.Fatalf("expected cancelled outcome, got %+v", resp.Outcome)
	}

	var sawReq, sawNotice, sawResolved bool
	deadline := time.After(time.Second)
loop:
	for {
		select {
		case ev := <-s.events:
			switch ev.Type {
			case event.TypePermission:
				sawReq = true
				if ev.Text != "Bash" {
					t.Fatalf("want detail fallback to title, got %q", ev.Text)
				}
			case event.TypeNotice:
				if strings.Contains(ev.Text, "timed out") {
					sawNotice = true
				}
			case event.TypePermissionResolved:
				if ev.Status == event.PermissionStatusCancelled {
					sawResolved = true
				}
			}
			if sawReq && sawNotice && sawResolved {
				break loop
			}
		case <-deadline:
			break loop
		}
	}
	if !sawReq || !sawNotice || !sawResolved {
		t.Fatalf("req=%v notice=%v resolved=%v", sawReq, sawNotice, sawResolved)
	}
}

// Control events must not be dropped when the event buffer is full of chunks,
// and a chunk that hit the full buffer must be coalesced (flushed intact ahead
// of the next boundary event) rather than silently dropped.
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
	// Second chunk hits the full buffer: it is held back (coalesced), not lost.
	s.emit(event.Event{
		Type:      event.TypeAssistantChunk,
		SessionID: "local-1",
		Timestamp: time.Now().UTC(),
		Text:      "held",
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

	// Drain continuously: the coalesced chunk flushes ahead of the control
	// event (both need buffer slots the single-slot channel only frees as we
	// read), and the control event must eventually arrive.
	var assistant strings.Builder
	var sawResolved bool
	deadline := time.After(2 * time.Second)
loop:
	for {
		select {
		case ev := <-s.events:
			switch ev.Type {
			case event.TypeAssistantChunk:
				assistant.WriteString(ev.Text)
			case event.TypePermissionResolved:
				if ev.PermissionID != "p1" {
					t.Fatalf("want permission id p1, got %+v", ev)
				}
				sawResolved = true
				break loop
			}
		case <-deadline:
			t.Fatal("control emit still blocked / dropped")
		}
	}

	<-done
	if !sawResolved {
		t.Fatal("permission_resolved was never delivered")
	}
	if got := assistant.String(); !strings.Contains(got, "held") {
		t.Fatalf("coalesced chunk text was dropped: assistant stream = %q", got)
	}
}

// Chunk text held back under back-pressure must be merged into the next
// same-type chunk, so a slow consumer batches reply text but never loses it.
func TestChunksCoalesceUnderBackpressure(t *testing.T) {
	s := &session{
		localID: "local-1",
		agentID: "agent-1",
		events:  make(chan event.Event, 1),
		log:     slog.Default(),
	}
	// First send takes the only slot.
	s.emit(event.Event{Type: event.TypeAssistantChunk, SessionID: "local-1", Text: "A"})
	// These three all hit the full buffer and accumulate in one pending run.
	s.emit(event.Event{Type: event.TypeAssistantChunk, SessionID: "local-1", Text: "B"})
	s.emit(event.Event{Type: event.TypeAssistantChunk, SessionID: "local-1", Text: "C"})
	s.emit(event.Event{Type: event.TypeAssistantChunk, SessionID: "local-1", Text: "D"})

	// Drain the first slot; the next same-type chunk carries the merged tail.
	first := recvEvent(t, s.events)
	if first.Text != "A" {
		t.Fatalf("want first chunk A, got %q", first.Text)
	}
	s.emit(event.Event{Type: event.TypeAssistantChunk, SessionID: "local-1", Text: "E"})
	merged := recvEvent(t, s.events)
	if merged.Text != "BCDE" {
		t.Fatalf("want coalesced BCDE, got %q", merged.Text)
	}
}
