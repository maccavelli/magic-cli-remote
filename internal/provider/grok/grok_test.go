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
