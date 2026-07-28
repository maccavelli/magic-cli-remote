package opencode

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
)

func modeIDs(modes []event.SessionMode) []string {
	out := make([]string, 0, len(modes))
	for _, m := range modes {
		out = append(out, m.ID)
	}
	return out
}

// TestAutoModeIsAdvertisedLast pins the ordering session.defaultMode depends on:
// it resolves "return to normal" as build, else the first non-plan mode. Auto
// anywhere earlier would let `/plan off` drop the user into auto-approve.
func TestAutoModeIsAdvertisedLast(t *testing.T) {
	t.Run("static", func(t *testing.T) {
		got := modeIDs(staticModes())
		if len(got) == 0 || got[len(got)-1] != autoModeID {
			t.Fatalf("static modes = %v, want auto last", got)
		}
		if got[0] != "build" {
			t.Errorf("static modes = %v, want build first so /plan off resolves to it", got)
		}
	})

	t.Run("from_agent_catalog", func(t *testing.T) {
		got := modeIDs(withAutoMode(modesFromAgents([]agentInfo{
			{Name: "build", Mode: "primary"},
			{Name: "plan", Mode: "primary"},
			{Name: "reviewer", Mode: "primary"},
		})))
		want := []string{"build", "plan", "reviewer", "auto"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("modes = %v, want %v", got, want)
		}
	})
}

func TestAutoModeIsMarkedDangerous(t *testing.T) {
	for _, m := range staticModes() {
		if m.ID != autoModeID {
			if m.Dangerous {
				t.Errorf("mode %q must not be marked dangerous", m.ID)
			}
			continue
		}
		if !m.Dangerous {
			t.Fatal("auto must be marked dangerous so the client can confirm before arming")
		}
	}
}

func TestNormalAgentID(t *testing.T) {
	tests := []struct {
		name  string
		modes []event.SessionMode
		want  string
	}{
		{"prefers_build", staticModes(), "build"},
		{"first_non_plan_non_auto", []event.SessionMode{
			{ID: "plan"}, {ID: "reviewer"}, {ID: autoModeID},
		}, "reviewer"},
		{"nothing_usable", []event.SessionMode{{ID: "plan"}, {ID: autoModeID}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalAgentID(tt.modes); got != tt.want {
				t.Fatalf("normalAgentID = %q, want %q", got, tt.want)
			}
		})
	}
}

// modeSession builds a dialect session whose agent catalog fetch fails, so
// sessionModes falls back to staticModes — enough to exercise SetMode without
// standing up an engine.
func modeSession(t *testing.T) (*httpSession, *captureHost) {
	t.Helper()
	h := &captureHost{api: func(context.Context, string, string, any, any) error {
		return context.DeadlineExceeded
	}}
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	s := d.NewSession(h).(*httpSession)
	h.mu.Lock()
	h.ds = s
	h.mu.Unlock()
	return s, h
}

func TestSetModeAutoArmsAndKeepsNormalAgent(t *testing.T) {
	s, h := modeSession(t)

	got, err := s.SetMode(context.Background(), autoModeID)
	if err != nil {
		t.Fatalf("SetMode(auto): %v", err)
	}
	if got != autoModeID {
		t.Errorf("SetMode returned %q, want %q", got, autoModeID)
	}
	if !h.AutoApprove() {
		t.Error("auto mode must arm auto-approve")
	}
	// The critical assertion: auto is in the advertised list, so a SetMode that
	// fell through to the agent loop would set the agent to "auto" — an agent
	// the engine does not have.
	if agent := h.Agent(); agent != "build" {
		t.Fatalf("agent = %q, want build; auto must not be set as an agent name", agent)
	}
}

func TestSetModeAwayFromAutoDisarms(t *testing.T) {
	for _, target := range []string{"build", "plan"} {
		t.Run(target, func(t *testing.T) {
			s, h := modeSession(t)
			if _, err := s.SetMode(context.Background(), autoModeID); err != nil {
				t.Fatal(err)
			}
			if !h.AutoApprove() {
				t.Fatal("setup: auto not armed")
			}

			got, err := s.SetMode(context.Background(), target)
			if err != nil {
				t.Fatalf("SetMode(%s): %v", target, err)
			}
			if got != target {
				t.Errorf("SetMode returned %q, want %q", got, target)
			}
			if h.AutoApprove() {
				t.Error("switching to a normal mode must disarm auto-approve")
			}
			if agent := h.Agent(); agent != target {
				t.Errorf("agent = %q, want %q", agent, target)
			}
		})
	}
}

// TestSetModeUnknownLeavesAutoUntouched: a failed switch must not silently
// disarm a safety-relevant control.
func TestSetModeUnknownLeavesAutoUntouched(t *testing.T) {
	s, h := modeSession(t)
	if _, err := s.SetMode(context.Background(), autoModeID); err != nil {
		t.Fatal(err)
	}

	if _, err := s.SetMode(context.Background(), "bogus-mode"); err == nil {
		t.Fatal("unknown mode must error")
	}
	if !h.AutoApprove() {
		t.Error("a failed mode switch must not disarm auto-approve")
	}
}

// TestCurrentModeReportsAutoWhenArmed: the chip must never read "build" on a
// session that is silently answering every permission.
func TestCurrentModeReportsAutoWhenArmed(t *testing.T) {
	s, h := modeSession(t)
	modes := staticModes()

	h.SetAgent("build")
	if got := s.currentMode(modes); got != "build" {
		t.Fatalf("currentMode = %q, want build when auto is off", got)
	}

	h.SetAutoApprove(true)
	if got := s.currentMode(modes); got != autoModeID {
		t.Fatalf("currentMode = %q, want %q while auto is armed", got, autoModeID)
	}
}

// TestArmingAutoSweepsPendingPermissions covers the main use case: arming auto
// to unblock an agent that is already waiting.
func TestArmingAutoSweepsPendingPermissions(t *testing.T) {
	log := &replyLog{}
	h := &captureHost{api: func(ctx context.Context, method, path string, body, out any) error {
		if method == "GET" && strings.HasPrefix(path, "/permission") {
			// The engine's open-permission list.
			raw := []json.RawMessage{
				json.RawMessage(`{"id":"per_waiting","sessionID":"ses_test","permission":"bash","patterns":["make test"]}`),
				json.RawMessage(`{"id":"per_other_session","sessionID":"ses_other","permission":"edit","patterns":["x.go"]}`),
			}
			if dst, ok := out.(*[]json.RawMessage); ok {
				*dst = raw
			}
			return nil
		}
		return log.api(ctx, method, path, body, out)
	}}
	d := &httpDialect{log: slog.Default(), defaultModelProvider: "opencode", defaultModelID: zenDefaultModel}
	s := d.NewSession(h).(*httpSession)
	h.mu.Lock()
	h.ds = s
	h.mu.Unlock()

	// A permission this session already surfaced and is waiting on.
	h.TrackPermission("per_waiting")

	if _, err := s.SetMode(context.Background(), autoModeID); err != nil {
		t.Fatalf("SetMode(auto): %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(log.replies()) == 0 {
		time.Sleep(2 * time.Millisecond)
	}

	got := log.replies()
	if len(got) != 1 {
		t.Fatalf("sweep sent %v, want exactly one reply for the waiting permission", got)
	}
	if !strings.Contains(got[0], "per_waiting") {
		t.Errorf("sweep replied to %q, want per_waiting", got[0])
	}
	// The engine is shared: another session's pending permission is not ours to
	// answer.
	for _, r := range got {
		if strings.Contains(r, "per_other_session") {
			t.Error("sweep answered a permission belonging to another session")
		}
	}
	if n := countEvents(h, event.TypePermissionResolved); n != 1 {
		t.Errorf("permission_resolved = %d, want exactly 1 — the transport emits "+
			"it, the sweep must not emit a second", n)
	}
}

// TestAutoApproveNotRestoredOnResume: the flag is session state, never
// persisted, so a fresh dialect session starts disarmed (MADR 0044 D8).
func TestAutoApproveNotRestoredOnResume(t *testing.T) {
	_, h := modeSession(t)
	if h.AutoApprove() {
		t.Fatal("a new session must start with auto-approve off")
	}
}
