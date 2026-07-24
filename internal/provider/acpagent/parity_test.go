package acpagent

import (
	"context"
	"log/slog"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// newBareSession builds a session with just enough wiring to exercise the
// update→event mapping (no subprocess/connection).
func newBareSession(buf int) *session {
	return &session{
		localID:  "l1",
		agentID:  "a1",
		events:   make(chan event.Event, buf),
		log:      slog.Default(),
		attached: true,
		done:     make(chan struct{}),
	}
}

func TestSessionUpdateUsage(t *testing.T) {
	s := newBareSession(4)
	err := s.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{UsageUpdate: &acp.SessionUsageUpdate{Used: 1200, Size: 8000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeUsage {
		t.Fatalf("type = %s, want usage_update", ev.Type)
	}
	if ev.Usage == nil || ev.Usage.Used != 1200 || ev.Usage.Size != 8000 {
		t.Fatalf("usage = %+v, want {1200 8000}", ev.Usage)
	}
	// Usage is telemetry, not control (a stale count self-corrects).
	if event.IsControl(event.TypeUsage) {
		t.Fatal("usage_update must not be a control event")
	}
}

func TestSessionUpdateCurrentMode(t *testing.T) {
	s := newBareSession(4)
	err := s.SessionUpdate(context.Background(), acp.SessionNotification{
		Update: acp.SessionUpdate{CurrentModeUpdate: &acp.SessionCurrentModeUpdate{CurrentModeId: "code"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeMode || ev.CurrentModeID != "code" {
		t.Fatalf("event = %+v, want session_mode current=code", ev)
	}
	// A current-only update carries no available-mode list (client keeps its own).
	if ev.Modes != nil {
		t.Fatalf("current_mode_update should not carry Modes, got %v", ev.Modes)
	}
	if !event.IsControl(event.TypeMode) {
		t.Fatal("session_mode must be a control event")
	}
}

func TestEmitModes(t *testing.T) {
	s := newBareSession(4)
	desc := "write code"
	s.emitModes(&acp.SessionModeState{
		CurrentModeId: "code",
		AvailableModes: []acp.SessionMode{
			{Id: "ask", Name: "Ask"},
			{Id: "code", Name: "Code", Description: &desc},
		},
	})
	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeMode || ev.CurrentModeID != "code" || len(ev.Modes) != 2 {
		t.Fatalf("event = %+v, want session_mode with 2 modes current=code", ev)
	}
	if ev.Modes[1].ID != "code" || ev.Modes[1].Description != "write code" {
		t.Fatalf("mode[1] = %+v", ev.Modes[1])
	}
	// No modes → no event.
	s2 := newBareSession(1)
	s2.emitModes(nil)
	select {
	case ev := <-s2.events:
		t.Fatalf("emitModes(nil) emitted %+v", ev)
	default:
	}
}

func TestEmitConfigOptions(t *testing.T) {
	s := newBareSession(4)
	ungrouped := acp.SessionConfigSelectOptionsUngrouped{
		{Name: "Fast", Value: "fast"},
		{Name: "Smart", Value: "smart"},
	}
	s.emitConfigOptions([]acp.SessionConfigOption{
		{Select: &acp.SessionConfigOptionSelect{
			Id: "reasoning", Name: "Reasoning", CurrentValue: "smart",
			Options: acp.SessionConfigSelectOptions{Ungrouped: &ungrouped},
		}},
		{Boolean: &acp.SessionConfigOptionBoolean{Id: "web", Name: "Web", CurrentValue: true}},
	})
	ev := recvEvent(t, s.events)
	if ev.Type != event.TypeSessionConfig || len(ev.ConfigOptions) != 2 {
		t.Fatalf("event = %+v, want session_config with 2 options", ev)
	}
	sel := ev.ConfigOptions[0]
	if sel.Kind != "select" || sel.CurrentValue != "smart" || len(sel.Values) != 2 || sel.Values[0].ID != "fast" {
		t.Fatalf("select option = %+v", sel)
	}
	b := ev.ConfigOptions[1]
	if b.Kind != "boolean" || !b.BoolValue {
		t.Fatalf("boolean option = %+v", b)
	}
}
