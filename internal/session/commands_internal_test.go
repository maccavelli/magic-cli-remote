package session

import (
	"strings"
	"testing"
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
			"s": {agentCommands: []string{"web", "search"}},
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
}
