package session_test

import (
	"context"
	"slices"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// planModes mirrors what a real provider advertises: a normal working mode and
// a plan mode (grok "default"/"plan", OpenCode "build"/"plan").
var planModes = []event.SessionMode{
	{ID: "default", Name: "Build"},
	{ID: "plan", Name: "Plan", Description: "Research and plan only; no edits"},
}

func TestPlanSwitchesModeAndDoesNotPrompt(t *testing.T) {
	mgr, p, sink, meta := newCmdManagerWithModes(t, planModes)
	if err := mgr.Prompt(context.Background(), meta.ID, "/plan", nil, "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if got := p.modeSwitches(); !slices.Equal(got, []string{"plan"}) {
		t.Fatalf("mode switches = %v, want [plan]", got)
	}
	if p.promptCount() != 0 {
		t.Fatalf("/plan must not reach the agent; got %d prompts", p.promptCount())
	}
	if !sink.hasNoticeContaining("Plan mode on") {
		t.Fatalf("expected a plan-mode notice, got: %v", sink.notices())
	}
	// A daemon-handled command still shows in the transcript as the user's own
	// message (the agent never sees it, so it cannot echo it).
	if !slices.ContainsFunc(sink.snapshot(), func(ev event.Event) bool {
		return ev.Type == event.TypeUserMessage && ev.Text == "/plan"
	}) {
		t.Fatal("/plan was not echoed as a user message")
	}
}

func TestPlanOffReturnsToDefaultMode(t *testing.T) {
	mgr, p, sink, meta := newCmdManagerWithModes(t, planModes)
	if err := mgr.Prompt(context.Background(), meta.ID, "/plan", nil, "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	sink.waitForMode(t, "plan")
	if err := mgr.Prompt(context.Background(), meta.ID, "/plan off", nil, "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if got := p.modeSwitches(); !slices.Equal(got, []string{"plan", "default"}) {
		t.Fatalf("mode switches = %v, want [plan default]", got)
	}
	if !sink.hasNoticeContaining("Plan mode off") {
		t.Fatalf("expected a plan-off notice, got: %v", sink.notices())
	}
}

// The provider's mode echo keeps the daemon's view current, so a repeat is
// recognised instead of firing a redundant switch.
func TestPlanTwiceReportsAlreadyInPlanMode(t *testing.T) {
	mgr, p, sink, meta := newCmdManagerWithModes(t, planModes)
	if err := mgr.Prompt(context.Background(), meta.ID, "/plan", nil, "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	sink.waitForMode(t, "plan")
	if err := mgr.Prompt(context.Background(), meta.ID, "/plan", nil, "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if got := p.modeSwitches(); len(got) != 1 {
		t.Fatalf("mode switches = %v, want a single switch", got)
	}
	if !sink.hasNoticeContaining("Already in plan mode") {
		t.Fatalf("expected an already-in-plan notice, got: %v", sink.notices())
	}
}

func TestPlanOnModelessAgentReportsUnavailable(t *testing.T) {
	mgr, p, sink, meta := newCmdManager(t)
	if err := mgr.Prompt(context.Background(), meta.ID, "/plan", nil, "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if got := p.modeSwitches(); len(got) != 0 {
		t.Fatalf("mode switches = %v, want none", got)
	}
	if p.promptCount() != 0 {
		t.Fatalf("/plan must not reach the agent; got %d prompts", p.promptCount())
	}
	if !sink.hasNoticeContaining("doesn't offer a plan mode") {
		t.Fatalf("expected a no-plan-mode notice, got: %v", sink.notices())
	}
}

func TestPlanRejectsUnknownArgument(t *testing.T) {
	mgr, p, sink, meta := newCmdManagerWithModes(t, planModes)
	if err := mgr.Prompt(context.Background(), meta.ID, "/plan sideways", nil, "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if got := p.modeSwitches(); len(got) != 0 {
		t.Fatalf("mode switches = %v, want none", got)
	}
	if !sink.hasNoticeContaining("Usage: /plan") {
		t.Fatalf("expected a usage notice, got: %v", sink.notices())
	}
}

func TestModeListsAndSwitches(t *testing.T) {
	mgr, p, sink, meta := newCmdManagerWithModes(t, planModes)
	if err := mgr.Prompt(context.Background(), meta.ID, "/mode", nil, "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if !sink.hasNoticeContaining("Modes: default (current), plan") {
		t.Fatalf("expected a mode list, got: %v", sink.notices())
	}
	if got := p.modeSwitches(); len(got) != 0 {
		t.Fatalf("bare /mode must not switch; got %v", got)
	}

	if err := mgr.Prompt(context.Background(), meta.ID, "/mode plan", nil, "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if got := p.modeSwitches(); !slices.Equal(got, []string{"plan"}) {
		t.Fatalf("mode switches = %v, want [plan]", got)
	}
}

func TestModeRejectsUnknownID(t *testing.T) {
	mgr, p, sink, meta := newCmdManagerWithModes(t, planModes)
	if err := mgr.Prompt(context.Background(), meta.ID, "/mode yolo", nil, "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if got := p.modeSwitches(); len(got) != 0 {
		t.Fatalf("mode switches = %v, want none", got)
	}
	if !sink.hasNoticeContaining("Unknown mode") {
		t.Fatalf("expected an unknown-mode notice, got: %v", sink.notices())
	}
}

// /plan and /mode are soft built-ins: an agent that advertises the name itself
// owns it, and the prompt goes through untouched.
func TestAgentAdvertisedPlanCommandWins(t *testing.T) {
	mgr, p, sink, meta := newCmdManagerWithModes(t, planModes)
	p.emitCommands(meta.ID, "plan")
	sink.waitForEvent(t, event.TypeAvailableCommands)

	if err := mgr.Prompt(context.Background(), meta.ID, "/plan the migration", nil, "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if got := p.modeSwitches(); len(got) != 0 {
		t.Fatalf("the daemon intercepted a command the agent owns: %v", got)
	}
	if p.promptCount() != 1 {
		t.Fatalf("an agent-advertised /plan must be forwarded; got %d prompts", p.promptCount())
	}
}

// Modes reach the daemon from provider events, so the direct op and the
// built-ins agree on what is switchable.
func TestSetModeOpAndBuiltinShareState(t *testing.T) {
	mgr, p, sink, meta := newCmdManagerWithModes(t, planModes)
	if err := mgr.SetMode(context.Background(), meta.ID, "plan", "dev-a"); err != nil {
		t.Fatalf("set_mode: %v", err)
	}
	if got := p.modeSwitches(); !slices.Equal(got, []string{"plan"}) {
		t.Fatalf("mode switches = %v, want [plan]", got)
	}
	// The provider's echo must land before /plan can see the new state.
	sink.waitForMode(t, "plan")
	// The op's echo moved the daemon's view: /plan now recognises the state.
	if err := mgr.Prompt(context.Background(), meta.ID, "/plan", nil, "dev-a"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	if !sink.hasNoticeContaining("Already in plan mode") {
		t.Fatalf("expected /plan to see the op's mode change, got: %v", sink.notices())
	}
}

// The mode built-ins are advertised to clients like every other built-in.
func TestModeBuiltinsAreAdvertised(t *testing.T) {
	names := make([]string, 0, len(session.BuiltinCommands))
	for _, c := range session.BuiltinCommands {
		names = append(names, c.Name)
	}
	for _, want := range []string{"plan", "mode"} {
		if !slices.Contains(names, want) {
			t.Errorf("built-in %q missing from %v", want, names)
		}
	}
}
