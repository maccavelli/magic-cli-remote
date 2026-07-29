package grok

import (
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
	want := []string{"agent", "--no-leader", "--always-approve", "-m", "m1", "stdio"}
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
	want := []string{"agent", "--no-leader", "--always-approve", "-m", "m2", "--reasoning-effort", "high", "stdio"}
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
	want := []string{"agent", "--no-leader", "--reasoning-effort", "high", "stdio"}
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
		"agent", "--no-leader", "--always-approve", "-m", "m-new",
		"--reasoning-effort", "high",
		"--permission-mode", "acceptEdits",
		"--tools", "bash,read",
		"--disallowed-tools", "write",
		"--allow", "rule1",
		"--deny", "rule2",
		"--no-subagents",
		"--disable-web-search",
		"stdio",
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
