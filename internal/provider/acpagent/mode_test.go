package acpagent

import (
	"log/slog"
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func modeSession(t *testing.T) *session {
	t.Helper()
	return &session{
		localID: "local-1",
		agentID: "agent-1",
		events:  make(chan event.Event, 8),
		log:     slog.Default(),
		staticModes: []event.SessionMode{
			{ID: "default", Name: "Build"},
			{ID: "plan", Name: "Plan"},
		},
		defaultModeID: "default",
	}
}

// An agent that advertises no modes (grok returns modes:null from session/new)
// must still get a mode list, or no client can offer the plan switch.
func TestEmitModesFallsBackToStaticWhenAgentHasNone(t *testing.T) {
	tests := []struct {
		name  string
		state *acp.SessionModeState
	}{
		{name: "nil mode state", state: nil},
		{name: "empty mode list", state: &acp.SessionModeState{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := modeSession(t)
			s.emitModesOrStatic(tt.state)

			ev := recvEvent(t, s.events)
			if ev.Type != event.TypeMode {
				t.Fatalf("want mode event, got %s", ev.Type)
			}
			if len(ev.Modes) != 2 || ev.Modes[1].ID != "plan" {
				t.Fatalf("modes = %+v", ev.Modes)
			}
			if ev.CurrentModeID != "default" {
				t.Fatalf("current mode = %q, want default", ev.CurrentModeID)
			}
			if !s.syntheticModes {
				t.Error("fallback must mark the vocabulary as ours")
			}
		})
	}
}

// An agent-supplied list always wins: the static list is a fallback, not an
// override, and it must not start validating ids the agent owns.
func TestEmitModesPrefersAgentList(t *testing.T) {
	s := modeSession(t)
	s.emitModesOrStatic(&acp.SessionModeState{
		CurrentModeId: "yolo",
		AvailableModes: []acp.SessionMode{
			{Id: "yolo", Name: "Yolo"},
		},
	})

	ev := recvEvent(t, s.events)
	if len(ev.Modes) != 1 || ev.Modes[0].ID != "yolo" {
		t.Fatalf("modes = %+v, want the agent's own list", ev.Modes)
	}
	if ev.CurrentModeID != "yolo" {
		t.Fatalf("current mode = %q, want yolo", ev.CurrentModeID)
	}
	if s.syntheticModes {
		t.Error("agent-supplied modes must not be marked synthetic")
	}
}

// With no static list and a silent agent there is nothing to advertise.
func TestEmitModesSilentWithoutStaticModes(t *testing.T) {
	s := &session{
		localID: "local-1",
		events:  make(chan event.Event, 4),
		log:     slog.Default(),
	}
	s.emitModesOrStatic(nil)
	select {
	case ev := <-s.events:
		t.Fatalf("unexpected event %s", ev.Type)
	default:
	}
}

// Grok accepts any mode id and echoes it back, so an unchecked typo would look
// like a successful switch. Ids are validated whenever the list is ours.
func TestSetModeRejectsUnknownSyntheticMode(t *testing.T) {
	s := modeSession(t)
	s.syntheticModes = true

	err := s.SetMode(t.Context(), "bogus-xyz")
	if err == nil {
		t.Fatal("want an error for an id outside our own mode list")
	}
	// A known id gets as far as the transport (nil conn ⇒ panic-free failure is
	// not assertable here); the guard above is the behavior under test.
}

// When the agent published the list, it is the authority on what is valid — the
// daemon must not second-guess ids it never defined.
func TestSetModeSkipsValidationForAgentModes(t *testing.T) {
	s := modeSession(t)
	s.syntheticModes = false
	s.closed = true // stop before the transport call

	err := s.SetMode(t.Context(), "some-agent-mode")
	if err == nil || err.Error() != "session closed" {
		t.Fatalf("err = %v, want the closed-session error (i.e. validation skipped)", err)
	}
}
