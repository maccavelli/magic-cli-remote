package session

import (
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
	"github.com/maccavelli/magic-cli-remote/internal/provider/opencode"
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

// helpTextManager builds a manager with the real provider registry, since /help
// now renders from each provider's declared command table (MADR 0023).
func helpTextManager(sessions map[string]*entry) *Manager {
	reg := provider.NewRegistry()
	reg.Register(grok.New(grok.Config{}))
	reg.Register(opencode.NewHTTP(opencode.Config{}))
	return &Manager{reg: reg, sessions: sessions}
}

func TestAgentAdvertisesAndHelp(t *testing.T) {
	m := helpTextManager(map[string]*entry{
		"s": {
			meta:          Meta{Provider: provider.IDGrok},
			agentCommands: []string{"web", "search"},
		},
	})
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
	// Daemon-side commands work on every provider; the agent's own commands are
	// listed separately; grok's caveat covers the rest of its TUI catalog.
	for _, want := range []string{"/model", "/clear", "/new", "/help", "/sessions",
		"/web", "/search", "terminal UI"} {
		if !strings.Contains(h, want) {
			t.Errorf("help text missing %q:\n%s", want, h)
		}
	}
	// No modes advertised and no usage seen: these would only fail here, so they
	// must not be offered. Checked against the offered segment alone, since the
	// same names appear in the reasons that follow.
	// ("/mode [id]" spelled in full: "/mode" alone is a prefix of "/model".)
	offered, rest, found := strings.Cut(h, ". Not here:")
	if !found {
		t.Fatalf("help must say what is unavailable and why:\n%s", h)
	}
	for _, unwanted := range []string{"/plan [off]", "/mode [id]", "/compact"} {
		if strings.Contains(offered, unwanted) {
			t.Errorf("help offers %q where it cannot work:\n%s", unwanted, offered)
		}
	}
	for _, explained := range []string{"/plan", "/mode", "/compact", "/diff", "/undo"} {
		if !strings.Contains(rest, explained) {
			t.Errorf("help does not explain why %s is missing:\n%s", explained, rest)
		}
	}
	// grok's own words for why /compact is out, not a generic shrug.
	if !strings.Contains(h, "grok compacts only in its own terminal UI") {
		t.Errorf("help text lost the provider's reason:\n%s", h)
	}
}

// helpText adapts to the session: mode commands appear only where the agent has
// modes, and grok's caveat only on grok.
func TestHelpTextModesAndProviderCaveat(t *testing.T) {
	m := helpTextManager(map[string]*entry{
		"oc": {
			meta: Meta{Provider: provider.IDOpencode},
			agentModes: []event.SessionMode{
				{ID: "build", Name: "build"},
				{ID: "plan", Name: "plan"},
			},
			currentModeID: "plan",
		},
	})
	h := m.helpText("oc")
	for _, want := range []string{"/plan [off]", "/mode [id]", "Modes: build, plan (current)"} {
		if !strings.Contains(h, want) {
			t.Errorf("help text missing %q:\n%s", want, h)
		}
	}
	if strings.Contains(h, "terminal UI") {
		t.Errorf("grok-specific caveat leaked into an opencode session:\n%s", h)
	}
	// OpenCode says /goal is not a thing it does; the reason must be its own.
	if !strings.Contains(h, "no goal loop") {
		t.Errorf("help text missing OpenCode's reason for /goal:\n%s", h)
	}
}

// A manager without a provider registry (bookkeeping unit tests) must still
// render help from the canonical defaults rather than crash.
func TestHelpTextWithoutARegistry(t *testing.T) {
	m := &Manager{sessions: map[string]*entry{"s": {meta: Meta{Provider: "rec"}}}}
	h := m.helpText("s")
	if !strings.Contains(h, "/help") {
		t.Fatalf("help text = %q", h)
	}
}

func TestFormatCount(t *testing.T) {
	cases := map[int]string{
		0: "0", 7: "7", 999: "999", 1000: "1,000",
		12480: "12,480", 128000: "128,000", 1000000: "1,000,000",
	}
	for in, want := range cases {
		if got := formatCount(in); got != want {
			t.Errorf("formatCount(%d) = %q, want %q", in, got, want)
		}
	}
}
