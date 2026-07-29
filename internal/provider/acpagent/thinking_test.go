package acpagent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Captured shape from grok 0.2.114 initialize._meta.modelState (MADR 0052 §2.2):
// expensive-first high/medium/low, high marked default, labels present.
const grokModelsJSON = `[
  {
    "modelId": "grok-4.5",
    "name": "Grok 4.5",
    "_meta": {
      "supportsReasoningEffort": true,
      "reasoningEffort": "high",
      "reasoningEfforts": [
        {"id":"high","value":"high","label":"High Effort",
         "description":"Highest implementation quality with extensive reasoning","default":true},
        {"id":"medium","value":"medium","label":"Medium Effort",
         "description":"Balanced speed and quality","default":false},
        {"id":"low","value":"low","label":"Low Effort",
         "description":"Quick, fast implementations","default":false}
      ]
    }
  },
  {
    "modelId": "no-effort",
    "name": "No Effort",
    "_meta": {
      "supportsReasoningEffort": false,
      "reasoningEfforts": [
        {"id":"high","value":"high","label":"High","default":true}
      ]
    }
  }
]`

func TestModelsToCatalogThinkingLevels(t *testing.T) {
	var models []GrokAvailableModel
	if err := json.Unmarshal([]byte(grokModelsJSON), &models); err != nil {
		t.Fatal(err)
	}

	cat := modelsToCatalog("grok-4.5", models)
	byID := map[string]picker.Option{}
	for _, o := range cat.Options {
		byID[o.ID] = o
	}

	g, ok := byID["grok-4.5"]
	if !ok {
		t.Fatal("grok-4.5 missing")
	}
	// NormalizeThinkingLevels reorders expensive-first → cheapest-first.
	want := []string{"low", "medium", "high"}
	got := make([]string, len(g.ThinkingLevels))
	for i, l := range g.ThinkingLevels {
		got[i] = l.ID
	}
	if len(got) != len(want) {
		t.Fatalf("levels = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("levels = %v, want %v", got, want)
		}
	}
	if d := picker.DefaultThinkingLevel(g.ThinkingLevels); d != "high" {
		t.Errorf("default = %q, want high", d)
	}
	// grok ships labels — keep them for the client.
	for _, l := range g.ThinkingLevels {
		if l.Label == "" {
			t.Errorf("%q missing label", l.ID)
		}
	}

	bare := byID["no-effort"]
	if len(bare.ThinkingLevels) != 0 {
		t.Errorf("supportsReasoningEffort=false must emit no levels, got %v", bare.ThinkingLevels)
	}
}

func TestSetThinkingLevelFixed(t *testing.T) {
	s := &session{}
	err := s.SetThinkingLevel(context.Background(), "low")
	if !errors.Is(err, provider.ErrThinkingLevelFixed) {
		t.Fatalf("err = %v, want ErrThinkingLevelFixed", err)
	}
	if s.ThinkingLevel() != "" {
		t.Errorf("failed SetThinkingLevel must not mutate state, got %q", s.ThinkingLevel())
	}
}

func TestSpawnArgsThinkingPrecedence(t *testing.T) {
	defaultArgs := func(cfg Config) []string {
		var args []string
		if cfg.Model != "" {
			args = append(args, "-m", cfg.Model)
		}
		if cfg.ReasoningEffort != "" {
			args = append(args, "--reasoning-effort", cfg.ReasoningEffort)
		}
		return append(args, "agent", "--no-leader", "stdio")
	}
	p := &Provider{
		spec: Spec{
			DefaultArgs: defaultArgs,
			ModelArgs: func(cfg Config, model string) []string {
				cfg.Model = model
				return defaultArgs(cfg)
			},
		},
		cfg: Config{
			// Baked argv as New would leave it after DefaultArgs(cfg).
			Args:            []string{"--reasoning-effort", "medium", "agent", "--no-leader", "stdio"},
			ReasoningEffort: "medium",
		},
	}

	// No override → baked argv.
	got := p.spawnArgs(provider.StartOptions{})
	want := []string{"--reasoning-effort", "medium", "agent", "--no-leader", "stdio"}
	if !equalStr(got, want) {
		t.Errorf("no override: got %v, want %v", got, want)
	}

	// Per-session thinking wins over config.
	got = p.spawnArgs(provider.StartOptions{ThinkingLevel: "low"})
	want = []string{"--reasoning-effort", "low", "agent", "--no-leader", "stdio"}
	if !equalStr(got, want) {
		t.Errorf("thinking override: got %v, want %v", got, want)
	}
	// Flag must precede the subcommand.
	if idx := indexOf(got, "--reasoning-effort"); idx < 0 || idx > indexOf(got, "agent") {
		t.Errorf("--reasoning-effort must precede agent; got %v", got)
	}

	// Per-session model + thinking rebuild together.
	got = p.spawnArgs(provider.StartOptions{Model: "grok-4.5", ThinkingLevel: "high"})
	want = []string{"-m", "grok-4.5", "--reasoning-effort", "high", "agent", "--no-leader", "stdio"}
	if !equalStr(got, want) {
		t.Errorf("model+thinking: got %v, want %v", got, want)
	}
}

func equalStr(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func indexOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return -1
}
