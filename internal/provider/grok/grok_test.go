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

// T-A1: static floor is grok-4.6 then grok-4.5 (MADR 0081 P1.1).
func TestStaticModelsFloor(t *testing.T) {
	if len(staticModels) != 2 {
		t.Fatalf("len=%d, want 2", len(staticModels))
	}
	if staticModels[0].ID != "grok-4.6" || staticModels[1].ID != "grok-4.5" {
		t.Fatalf("ids=%v,%v want grok-4.6, grok-4.5", staticModels[0].ID, staticModels[1].ID)
	}
	for _, o := range staticModels {
		switch o.ID {
		case "grok-code-fast-1", "grok-4", "grok-build":
			t.Errorf("stale or docs-only id %q on the floor", o.ID)
		}
		if o.Group != "xai" {
			t.Errorf("%s group=%q, want xai", o.ID, o.Group)
		}
	}
}

func TestDefaultArgs(t *testing.T) {
	got := defaultArgs(Config{})
	want := []string{"--no-auto-update", "agent", "--no-leader", "stdio"}
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
	want := []string{"-m", "m1", "--always-approve", "--no-auto-update", "agent", "--no-leader", "stdio"}
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
	want := []string{"-m", "m2", "--reasoning-effort", "high", "--always-approve", "--no-auto-update", "agent", "--no-leader", "stdio"}
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
	want := []string{"--reasoning-effort", "high", "--no-auto-update", "agent", "--no-leader", "stdio"}
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
}

// Pins that a per-session ThinkingLevel rebuild (via ModelArgs/DefaultArgs)
// keeps --reasoning-effort before the agent subcommand. Placing it after is
// the MADR 0050 failure mode; a session-level effort must not reintroduce it.
func TestModelArgsThinkingLevelStaysGlobal(t *testing.T) {
	got := spec.ModelArgs(Config{ReasoningEffort: "low"}, "grok-4.5")
	at := slices.Index(got, "agent")
	if at < 0 {
		t.Fatalf("no agent subcommand in %v", got)
	}
	flagAt := slices.Index(got, "--reasoning-effort")
	if flagAt < 0 || flagAt > at {
		t.Fatalf("--reasoning-effort must precede agent; got %v", got)
	}
	if flagAt+1 >= len(got) || got[flagAt+1] != "low" {
		t.Fatalf("effort value not next to flag: %v", got)
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
		"--no-auto-update",
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
		"--no-subagents", "--disable-web-search", "--no-auto-update",
	} {
		if !slices.Contains(got[:at], flag) {
			t.Errorf("%s must be emitted before the agent subcommand; got %v", flag, got)
		}
	}
}

// grok's own sandbox profile (--sandbox). Global flag, so it must land before
// the subcommand like the rest (MADR 0050 D4).
func TestDefaultArgsSandboxProfile(t *testing.T) {
	got := defaultArgs(Config{Sandbox: "workspace"})
	want := []string{"--sandbox", "workspace", "--no-auto-update", "agent", "--no-leader", "stdio"}
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
	// Empty omits the flag entirely, leaving grok's own default.
	if slices.Contains(defaultArgs(Config{}), "--sandbox") {
		t.Fatal("an empty sandbox must not emit --sandbox")
	}
}

// T-P4: daemon argv must not grow TUI/headless-only flags (MADR 0081).
func TestDefaultArgsDoesNotEmitP4Flags(t *testing.T) {
	got := defaultArgs(Config{Model: "grok-4.6", ReasoningEffort: "xhigh", AlwaysApprove: true})
	forbidden := []string{
		"--worktree", "--worktree-ref", "--minimal", "--fullscreen",
		"--cwd", "--oauth", "--json-schema", "--max-turns",
		"--experimental-memory", "--no-memory", "--restore-code",
		"--verbatim", "--include-partial-messages", "--no-ask-user",
		"--no-plan",
	}
	for _, f := range forbidden {
		if slices.Contains(got, f) {
			t.Errorf("P4 flag %s leaked into defaultArgs: %v", f, got)
		}
	}
}
