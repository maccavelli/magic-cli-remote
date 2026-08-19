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

// Captured from grok 1.0.3 initialize._meta.modelState on 2026-08-12
// (MADR 0081 P1.4). grok-4.6 marks both xhigh and high default:true.
const grok46ModelsJSON = `[
  {
    "modelId": "grok-4.6",
    "name": "Grok 4.6",
    "description": "SpaceXAI's latest frontier model",
    "_meta": {
      "totalContextTokens": 500000,
      "agentType": "grok-build-plan",
      "supportsReasoningEffort": true,
      "reasoningEffort": "high",
      "reasoningEfforts": [
        {"id":"xhigh","value":"xhigh","label":"Extra High Effort",
         "description":"Highest effort and reasoning level","default":true},
        {"id":"high","value":"high","label":"High Effort",
         "description":"Higher implementation quality with extensive reasoning","default":true},
        {"id":"medium","value":"medium","label":"Medium Effort",
         "description":"Balanced effort with standard implementation and testing","default":false},
        {"id":"low","value":"low","label":"Low Effort",
         "description":"Quick, fast implementations","default":false}
      ]
    }
  },
  {
    "modelId": "grok-4.5",
    "name": "Grok 4.5",
    "_meta": {
      "totalContextTokens": 500000,
      "supportsReasoningEffort": true,
      "reasoningEffort": "high",
      "reasoningEfforts": [
        {"id":"high","value":"high","label":"High Effort",
         "description":"Highest implementation quality with extensive reasoning","default":true},
        {"id":"medium","value":"medium","label":"Medium Effort",
         "description":"Balanced effort with standard implementation and testing","default":false},
        {"id":"low","value":"low","label":"Low Effort",
         "description":"Quick, fast implementations","default":false}
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

// T-E1: grok-4.6 xhigh is parsed, cheapest-first, default stays high (MADR 0081).
func TestModelsToCatalogGrok46XHigh(t *testing.T) {
	var models []GrokAvailableModel
	if err := json.Unmarshal([]byte(grok46ModelsJSON), &models); err != nil {
		t.Fatal(err)
	}
	cat := modelsToCatalog("grok-4.6", models)
	byID := map[string]picker.Option{}
	for _, o := range cat.Options {
		byID[o.ID] = o
	}
	g, ok := byID["grok-4.6"]
	if !ok {
		t.Fatal("grok-4.6 missing")
	}
	want := []string{"low", "medium", "high", "xhigh"}
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
	for _, l := range g.ThinkingLevels {
		if l.Label == "" {
			t.Errorf("%q missing label", l.ID)
		}
	}
	g45, ok := byID["grok-4.5"]
	if !ok {
		t.Fatal("grok-4.5 missing")
	}
	for _, l := range g45.ThinkingLevels {
		if l.ID == "xhigh" {
			t.Fatal("grok-4.5 must not include xhigh")
		}
	}
}

func TestSetThinkingRequestShape(t *testing.T) {
	req := setThinkingRequest("sid", "grok-4.6", "low")
	if req["sessionId"] != "sid" || req["modelId"] != "grok-4.6" {
		t.Fatalf("ids = %#v", req)
	}
	if _, ok := req["reasoningEffort"]; ok {
		t.Fatal("top-level reasoningEffort must not be sent")
	}
	meta, _ := req["_meta"].(map[string]any)
	if meta["reasoningEffort"] != "low" {
		t.Fatalf("_meta = %#v", req["_meta"])
	}
	if _, ok := req["lastTurnId"]; ok {
		t.Fatal("lastTurnId must not be sent")
	}
	snap := resumeSnapshotRequest("sid", "/tmp/cwd")
	if snap["sessionId"] != "sid" || snap["cwd"] != "/tmp/cwd" {
		t.Fatalf("resume = %#v", snap)
	}
	if _, ok := snap["_meta"]; ok {
		t.Fatal("resume read-back must omit _meta")
	}
}

func TestSetThinkingLevelRequiresIdentity(t *testing.T) {
	s := &session{}
	err := s.SetThinkingLevel(context.Background(), "low")
	if err == nil {
		t.Fatal("empty session: want error")
	}
	if errors.Is(err, provider.ErrThinkingLevelFixed) {
		t.Fatalf("err = %v, must not be ErrThinkingLevelFixed", err)
	}
	if s.ThinkingLevel() != "" {
		t.Errorf("failed SetThinkingLevel must not mutate state, got %q", s.ThinkingLevel())
	}

	s.closed = true
	s.agentID = "sid"
	s.currentModelID = "grok-4.6"
	err = s.SetThinkingLevel(context.Background(), "low")
	if err == nil || err.Error() != "session closed" {
		t.Fatalf("closed session err = %v, want session closed", err)
	}
}

func TestSpawnArgsIgnoresPerSessionModelAndThinking(t *testing.T) {
	baked := []string{"--reasoning-effort", "medium", "agent", "--no-leader", "stdio"}
	p := &Provider{
		cfg: Config{
			Args:            baked,
			ReasoningEffort: "medium",
		},
	}
	got := p.spawnArgs(provider.StartOptions{})
	if !equalStr(got, baked) {
		t.Errorf("no override: got %v, want %v", got, baked)
	}
	got = p.spawnArgs(provider.StartOptions{ThinkingLevel: "low", Model: "grok-4.5"})
	if !equalStr(got, baked) {
		t.Errorf("per-session model/thinking must not rebuild argv: got %v, want %v", got, baked)
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
