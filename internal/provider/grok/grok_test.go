package grok

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/maccavelli/go-llmprovider-sdk/llmprovider"
	"github.com/maccavelli/go-llmprovider-sdk/llmprovider/catalog"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

func TestNewDefaults(t *testing.T) {
	p := New(Config{})
	if p.ID() != provider.IDGrok {
		t.Fatalf("id = %q", p.ID())
	}
}

func TestGrokSessionMeta(t *testing.T) {
	// Nothing requested means nothing *requestable* is sent — but the meta is
	// no longer empty: x.ai/restore_code always rides along, because omitting
	// it hands the value to the operator's grok config (MADR 0138 Phase 11).
	// Asserted as an exact key set rather than a length, so a future addition
	// has to come here and be justified.
	got := grokSessionMeta(provider.StartOptions{}, Config{})
	if len(got) != 1 {
		t.Fatalf("empty = %#v, want only x.ai/restore_code", got)
	}
	if v, ok := got["x.ai/restore_code"]; !ok || v != false {
		t.Fatalf("empty = %#v, want x.ai/restore_code:false", got)
	}
	got = grokSessionMeta(provider.StartOptions{}, Config{Model: "grok-4.5"})
	if got["modelId"] != "grok-4.5" || got["reasoningEffort"] != nil {
		t.Fatalf("model only = %#v", got)
	}
	got = grokSessionMeta(provider.StartOptions{}, Config{ReasoningEffort: "low"})
	if got["reasoningEffort"] != "low" || got["modelId"] != nil {
		t.Fatalf("effort only = %#v", got)
	}
	got = grokSessionMeta(provider.StartOptions{Model: "grok-4.5", ThinkingLevel: "xhigh"}, Config{Model: "grok-4.6", ReasoningEffort: "high"})
	if got["modelId"] != "grok-4.5" || got["reasoningEffort"] != "xhigh" {
		t.Fatalf("opts override cfg = %#v", got)
	}
	got = grokSessionMeta(provider.StartOptions{Model: "  ", ThinkingLevel: " "}, Config{Model: "grok-4.6"})
	if got["modelId"] != "grok-4.6" || got["reasoningEffort"] != nil {
		t.Fatalf("whitespace omitted = %#v", got)
	}
}

func TestGrokSessionMetaOmitsPermissionSeeds(t *testing.T) {
	got := grokSessionMeta(provider.StartOptions{Model: "grok-4.5", ThinkingLevel: "low"}, Config{AlwaysApprove: true})
	if _, ok := got["yoloMode"]; ok {
		t.Fatal("yoloMode must not appear")
	}
	if _, ok := got["autoMode"]; ok {
		t.Fatal("autoMode must not appear")
	}
}

// T-A1: the static floor is the SDK's grok list in the SDK's rank order,
// under the SDK's names, its first id the default (MADR 0081 P1.1; MADR 0235
// D4, as amended). The expected ids and names are those of
// go-llmprovider-sdk v1.27.0: a pin move that changes them fails here, so the
// change is seen and recorded.
func TestStaticModelsFloor(t *testing.T) {
	want := []string{"grok-4.7", "grok-4.6", "grok-4.5", "grok-4.3", "grok-build-0.1"}
	wantLabels := []string{"Grok 4.7", "Grok 4.6", "Grok 4.5", "Grok 4.3", "Grok Build 0.1"}
	got := make([]string, 0, len(staticModels))
	labels := make([]string, 0, len(staticModels))
	for _, o := range staticModels {
		got = append(got, o.ID)
		labels = append(labels, o.Label)
		switch o.ID {
		case "grok-code-fast-1", "grok-4", "grok-build":
			t.Errorf("stale or docs-only id %q on the floor", o.ID)
		}
		if o.Group != "xai" {
			t.Errorf("%s group=%q, want xai", o.ID, o.Group)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("floor = %v, want %v", got, want)
	}
	if !slices.Equal(labels, wantLabels) {
		t.Errorf("floor labels = %q, want %q", labels, wantLabels)
	}
	sdk := catalog.SortByRank(llmprovider.ProviderGrok, catalog.Static(llmprovider.ProviderGrok), nil)
	if !slices.Equal(got, sdk) {
		t.Errorf("floor = %v, want the SDK's ranked list %v", got, sdk)
	}
	if spec.StaticDefaultModel != "grok-4.7" {
		t.Errorf("floor default = %q, want grok-4.7", spec.StaticDefaultModel)
	}
}

// TestTheFloorIsInTheKnownGoodCapture: every floor id is a model the pinned
// grok offered, so the SDK's list cannot put a model grok refuses on the
// picker (MADR 0235 D4). A new SDK list needs a new capture, or a recorded
// reason, before the pin moves.
func TestTheFloorIsInTheKnownGoodCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/wire/" + KnownGoodVersion + "/frames.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	offered := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		var frame any
		if strings.TrimSpace(line) == "" || json.Unmarshal([]byte(line), &frame) != nil {
			continue
		}
		collectAvailableModels(frame, offered)
	}
	if len(offered) == 0 {
		t.Fatalf("the %s capture holds no availableModels", KnownGoodVersion)
	}
	for _, o := range staticModels {
		if !offered[o.ID] {
			t.Errorf("floor id %q is not among the models grok %s offered", o.ID, KnownGoodVersion)
		}
	}
}

// collectAvailableModels adds every modelId of every availableModels list in v.
func collectAvailableModels(v any, into map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		if list, ok := x["availableModels"].([]any); ok {
			for _, m := range list {
				if mm, ok := m.(map[string]any); ok {
					if id, ok := mm["modelId"].(string); ok {
						into[id] = true
					}
				}
			}
		}
		for _, child := range x {
			collectAvailableModels(child, into)
		}
	case []any:
		for _, child := range x {
			collectAvailableModels(child, into)
		}
	}
}

// TestFloorLabelsAreTheSDKNames: the floor names each model with the SDK's
// display name, which is what a signed-in grok calls it, never the SDK's
// padded terminal label, and carries no note (MADR 0235 D4, amendment of
// 2026-11-06).
func TestFloorLabelsAreTheSDKNames(t *testing.T) {
	for _, o := range staticModels {
		if want := catalog.LabelName(llmprovider.ProviderGrok, o.ID); o.Label != want {
			t.Errorf("%s label %q, want the SDK's name %q", o.ID, o.Label, want)
		}
		if o.Description != "" {
			t.Errorf("%s description %q, want none: the SDK's notes are not used", o.ID, o.Description)
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
// model flag while preserving ReasoningEffort (MADR 0037 D1). Start applies
// model via SessionMeta instead (MADR 0106); this still pins ModelArgs.
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
		NoSubagents:      true,
		DisableWebSearch: true,
	}
	got := spec.ModelArgs(cfg, "m-new")
	want := []string{
		"-m", "m-new",
		"--reasoning-effort", "high",
		"--always-approve",
		"--permission-mode", "acceptEdits",
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
