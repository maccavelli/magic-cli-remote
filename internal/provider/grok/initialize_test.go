package grok

import (
	"bufio"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"testing"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
)

// From acpagent/initialize_test.go.

// grokInitializeResult returns the `result` of the initialize response in
// grok's recorded wire fixture — the first frame, agent to client.
//
// It fails rather than skips when the fixture is missing: the file is tracked,
// and a skip would let the tests below pass by checking nothing.
func grokInitializeResult(t *testing.T) json.RawMessage {
	t.Helper()
	f, err := os.Open("../grok/testdata/wire/1.0.13/frames.jsonl")
	if err != nil {
		t.Fatalf("open grok fixture: %v", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	if !sc.Scan() {
		t.Fatalf("grok fixture is empty: %v", sc.Err())
	}
	var frame struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(sc.Bytes(), &frame); err != nil {
		t.Fatalf("decode first grok frame: %v", err)
	}
	if len(frame.Result) == 0 {
		t.Fatal("first grok frame has no result; it is not the initialize response")
	}
	return frame.Result
}

// TestGrokInitializeMetaSurvivesTheTypedResponse pins MADR 0167 F15: the typed
// InitializeResponse keeps grok's _meta block, so initialize needs no raw path.
//
// The response is decoded exactly as the SDK's SendRequest decodes it, then
// compared field-for-field with the raw decode the typed call replaced. Equality
// is the claim; the explicit values guard against both sides being empty.
func TestGrokInitializeMetaSurvivesTheTypedResponse(t *testing.T) {
	result := grokInitializeResult(t)

	var typed acp.InitializeResponse
	if err := json.Unmarshal(result, &typed); err != nil {
		t.Fatalf("decode typed InitializeResponse: %v", err)
	}
	got := decodeGrokInitializeMeta(typed.Meta)

	var raw grokInitializeMeta
	if err := json.Unmarshal(result, &raw); err != nil {
		t.Fatalf("decode raw result: %v", err)
	}
	if !reflect.DeepEqual(got, raw) {
		t.Fatalf("typed path lost information the raw path had:\n typed: %+v\n   raw: %+v", got.Meta, raw.Meta)
	}

	if want := "cached_token"; got.Meta.DefaultAuthMethodID != want {
		t.Errorf("defaultAuthMethodId = %q, want %q", got.Meta.DefaultAuthMethodID, want)
	}
	if want := "1.0.13"; got.Meta.AgentVersion != want {
		t.Errorf("agentVersion = %q, want %q", got.Meta.AgentVersion, want)
	}
	if want := "grok-4.6"; got.Meta.ModelState.CurrentModelID != want {
		t.Errorf("modelState.currentModelId = %q, want %q", got.Meta.ModelState.CurrentModelID, want)
	}
	models := got.Meta.ModelState.AvailableModels
	if len(models) != 2 {
		t.Fatalf("availableModels = %d, want 2", len(models))
	}
	for _, m := range models {
		if m.Meta.TotalContextTokens <= 0 {
			t.Errorf("%s: totalContextTokens = %d; a JSON number did not survive the map round trip",
				m.ModelID, m.Meta.TotalContextTokens)
		}
	}
}

// TestDecodeGrokInitializeMetaIsBestEffort keeps the old raw decode's contract:
// an agent that sends no vendor block, or an unexpected one, must not fail
// initialize — it just yields no vendor fields.
func TestDecodeGrokInitializeMetaIsBestEffort(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]any
	}{
		{"nil", nil},
		{"empty", map[string]any{}},
		{"unrelated keys only", map[string]any{"somethingElse": 1}},
		{"wrong shape", map[string]any{"modelState": "not an object"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeGrokInitializeMeta(tc.meta)
			if got.Meta.ModelState.CurrentModelID != "" || len(got.Meta.ModelState.AvailableModels) != 0 {
				t.Errorf("got model state %+v from %v, want none", got.Meta.ModelState, tc.meta)
			}
		})
	}
}

// From acpagent/thinking_test.go.

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
	var models []availableModel
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
	var models []availableModel
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

// TestInitializeMetaReadsWhatGrokReported is the grok half of acpagent's
// TestApplyInitializeResponseRecordsWhatTheSpecRead: grok's real initialize
// response, read by grok's Spec.InitializeMeta.
func TestInitializeMetaReadsWhatGrokReported(t *testing.T) {
	var resp acp.InitializeResponse
	if err := json.Unmarshal(grokInitializeResult(t), &resp); err != nil {
		t.Fatalf("decode typed InitializeResponse: %v", err)
	}
	info := initializeMeta(resp.Meta)
	if want := "grok-4.6"; info.CurrentModel != want {
		t.Errorf("CurrentModel = %q, want %q (from _meta.modelState.currentModelId)", info.CurrentModel, want)
	}
	if info.Models == nil {
		t.Fatal("no model catalog read from _meta.modelState.availableModels")
	}
	ids := make([]string, 0, len(info.Models.Options))
	for _, o := range info.Models.Options {
		ids = append(ids, o.ID)
	}
	if !slices.Contains(ids, "grok-4.6") || !slices.Contains(ids, "grok-4.5") || len(ids) != 2 {
		t.Errorf("catalog options = %v, want exactly grok-4.6 and grok-4.5", ids)
	}
	if want := "1.0.13"; info.Version != want {
		t.Errorf("Version = %q, want %q (from _meta.agentVersion)", info.Version, want)
	}
}
