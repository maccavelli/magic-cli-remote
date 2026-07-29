package grok

import (
	"slices"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestNewDefaults(t *testing.T) {
	p := New(Config{})
	if p.ID() != provider.IDGrok {
		t.Fatalf("id = %q", p.ID())
	}
}

func TestDefaultArgs(t *testing.T) {
	got := defaultArgs(Config{})
	want := []string{"agent", "--no-leader", "stdio"}
	if len(got) != len(want) {
		t.Fatalf("args = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
}

func TestDefaultArgsWithModelAndApprove(t *testing.T) {
	got := defaultArgs(Config{AlwaysApprove: true, Model: "m1"})
	want := []string{"-m", "m1", "--always-approve", "agent", "--no-leader", "stdio"}
	if len(got) != len(want) {
		t.Fatalf("args = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
}

// The spec's per-session model override must rebuild default args with the
// model flag while preserving ReasoningEffort (MADR 0037 D1).
func TestSpecModelArgs(t *testing.T) {
	got := spec.ModelArgs(Config{AlwaysApprove: true, ReasoningEffort: "high", Args: []string{"custom"}}, "m2")
	want := []string{"-m", "m2", "--reasoning-effort", "high", "--always-approve", "agent", "--no-leader", "stdio"}
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
}

func TestDefaultArgsWithReasoningEffort(t *testing.T) {
	got := defaultArgs(Config{ReasoningEffort: "high"})
	want := []string{"--reasoning-effort", "high", "agent", "--no-leader", "stdio"}
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
}

func TestSpecModelArgsPolicyFlags(t *testing.T) {
	cfg := Config{
		AlwaysApprove:    true,
		ReasoningEffort:  "high",
		PermissionMode:   "acceptEdits",
		AllowedTools:     []string{"bash", "read"},
		DisallowedTools:  []string{"write"},
		AllowRules:       []string{"rule1"},
		DenyRules:        []string{"rule2"},
		NoSubagents:      true,
		DisableWebSearch: true,
	}
	got := spec.ModelArgs(cfg, "m-new")
	want := []string{
		"-m", "m-new",
		"--reasoning-effort", "high",
		"--always-approve",
		"--permission-mode", "acceptEdits",
		"--tools", "bash,read",
		"--disallowed-tools", "write",
		"--allow", "rule1",
		"--deny", "rule2",
		"--no-subagents",
		"--disable-web-search",
		"agent", "--no-leader", "stdio",
	}
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// Grok is the only provider whose menu could not reach auto-approve: its own
// auto is `--permission-mode auto`, a flag on the process, so the per-session
// mode has to be advertised and enforced by the daemon (MADR 0049).
func TestSpecSynthesizesAutoMode(t *testing.T) {
	if !spec.SynthesizeAutoMode {
		t.Fatal("grok must opt into the daemon-enforced auto mode")
	}
	// `auto` is deliberately not in the static list — it is not an ACP mode id
	// grok would accept, so it must never be sent to the agent.
	for _, m := range spec.StaticModes {
		if m.ID == "auto" {
			t.Fatal("auto must be synthesized, not declared as a grok ACP mode")
		}
	}
	if spec.DefaultModeID != "default" {
		t.Fatalf("default mode = %q, want default", spec.DefaultModeID)
	}
}

// Pins the *shape* rather than a literal vector: every element before the
// `agent` subcommand is a global flag or its value, and the tail is exactly
// `agent --no-leader stdio`. `grok agent` rejects the global flags outright, so
// anything drifting back after `agent` breaks session start (MADR 0050 D1).
//
// This cannot catch grok *relocating* a flag — it asserts the argv we build,
// which is exactly what passed while all seven options were broken. That is
// what live_argv_test.go is for.
func TestDefaultArgsPutsGlobalsBeforeSubcommand(t *testing.T) {
	cfg := Config{
		Model:            "m",
		ReasoningEffort:  "high",
		AlwaysApprove:    true,
		PermissionMode:   "default",
		AllowedTools:     []string{"Bash"},
		DisallowedTools:  []string{"Write"},
		AllowRules:       []string{"r1"},
		DenyRules:        []string{"r2"},
		NoSubagents:      true,
		DisableWebSearch: true,
	}
	got := defaultArgs(cfg)

	at := -1
	for i, a := range got {
		if a == "agent" {
			at = i
			break
		}
	}
	if at < 0 {
		t.Fatalf("no agent subcommand in %v", got)
	}
	tail := got[at:]
	wantTail := []string{"agent", "--no-leader", "stdio"}
	if len(tail) != len(wantTail) {
		t.Fatalf("tail = %v, want %v", tail, wantTail)
	}
	for i := range wantTail {
		if tail[i] != wantTail[i] {
			t.Fatalf("tail = %v, want %v", tail, wantTail)
		}
	}

	// Every configured global must appear before the subcommand.
	for _, flag := range []string{
		"-m", "--reasoning-effort", "--always-approve", "--permission-mode",
		"--tools", "--disallowed-tools", "--allow", "--deny",
		"--no-subagents", "--disable-web-search",
	} {
		if !slices.Contains(got[:at], flag) {
			t.Errorf("%s must be emitted before the agent subcommand; got %v", flag, got)
		}
	}
}
