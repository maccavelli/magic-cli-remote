package session

import (
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestIsCommandName(t *testing.T) {
	cases := map[string]bool{
		"model":      true,
		"web-search": true,
		"a1_b":       true,
		"Help":       true,
		"":           false,
		"etc/hosts":  false, // a path, not a command
		"usr/bin":    false,
		"-x":         false, // may not start with a dash
		"a b":        false, // whitespace already stripped by the caller, but guard anyway
		"a.b":        false,
	}
	for in, want := range cases {
		if got := isCommandName(in); got != want {
			t.Errorf("isCommandName(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestAgentAdvertisesAndHelp(t *testing.T) {
	m := &Manager{
		sessions: map[string]*entry{
			"s": {
				meta:          Meta{Provider: provider.IDGrok},
				agentCommands: []string{"web", "search"},
			},
		},
	}
	if !m.agentAdvertises("s", "web") {
		t.Error("want web advertised")
	}
	if !m.agentAdvertises("s", "WEB") {
		t.Error("advertise match should be case-insensitive")
	}
	if m.agentAdvertises("s", "nope") {
		t.Error("unadvertised command reported as advertised")
	}
	if m.agentAdvertises("missing", "web") {
		t.Error("unknown session reported a command")
	}

	h := m.helpText("s")
	for _, want := range []string{"/model", "/reset", "/new", "/help", "/web", "/search", "terminal-only"} {
		if !strings.Contains(h, want) {
			t.Errorf("help text missing %q:\n%s", want, h)
		}
	}
	// No modes advertised: /plan and /mode would only fail, so they are not
	// offered here.
	// ("/mode [id]" spelled in full: "/mode" alone is a prefix of "/model".)
	for _, unwanted := range []string{"/plan [off]", "/mode [id]"} {
		if strings.Contains(h, unwanted) {
			t.Errorf("help text offers %q on a modeless agent:\n%s", unwanted, h)
		}
	}
}

// helpText adapts to the session: mode commands appear only where the agent has
// modes, and grok's terminal-only caveat only on grok.
func TestHelpTextModesAndProviderCaveat(t *testing.T) {
	m := &Manager{
		sessions: map[string]*entry{
			"oc": {
				meta: Meta{Provider: provider.IDOpencode},
				agentModes: []event.SessionMode{
					{ID: "build", Name: "build"},
					{ID: "plan", Name: "plan"},
				},
				currentModeID: "plan",
			},
		},
	}
	h := m.helpText("oc")
	for _, want := range []string{"/plan [off]", "/mode [id]", "Modes: build, plan (current)"} {
		if !strings.Contains(h, want) {
			t.Errorf("help text missing %q:\n%s", want, h)
		}
	}
	if strings.Contains(h, "terminal-only") {
		t.Errorf("grok-specific caveat leaked into an opencode session:\n%s", h)
	}
}
