package acpagent

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"reflect"
	"slices"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

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

// TestApplyInitializeResponseRecordsWhatGrokReported is the wiring guard P2's
// fail-first run found missing: before it, discarding grok's _meta at the call
// site left the whole package green (PLAN 0167, deviation of 2026-09-22).
//
// It drives the real handler with grok's real initialize response and checks
// every piece of state spawnAgent relies on afterwards.
func TestApplyInitializeResponseRecordsWhatGrokReported(t *testing.T) {
	var resp acp.InitializeResponse
	if err := json.Unmarshal(grokInitializeResult(t), &resp); err != nil {
		t.Fatalf("decode typed InitializeResponse: %v", err)
	}
	p := &Provider{log: slog.Default()}
	s := &session{log: slog.Default()}

	p.applyInitializeResponse(s, resp)

	s.mu.Lock()
	engineModel := s.engineModelID
	s.mu.Unlock()
	if want := "grok-4.6"; engineModel != want {
		t.Errorf("engineModelID = %q, want %q (from _meta.modelState.currentModelId)", engineModel, want)
	}

	p.catalogMu.RLock()
	has, cat := p.catalogHas, p.catalogCache
	p.catalogMu.RUnlock()
	if !has {
		t.Fatal("no model catalog recorded from _meta.modelState.availableModels")
	}
	ids := make([]string, 0, len(cat.Options))
	for _, o := range cat.Options {
		ids = append(ids, o.ID)
	}
	if !slices.Contains(ids, "grok-4.6") || !slices.Contains(ids, "grok-4.5") || len(ids) != 2 {
		t.Errorf("catalog options = %v, want exactly grok-4.6 and grok-4.5", ids)
	}

	if want := "1.0.13"; p.EngineVersion() != want {
		t.Errorf("EngineVersion() = %q, want %q (from _meta.agentVersion)", p.EngineVersion(), want)
	}
	if !s.agentCaps.LoadSession {
		t.Error("agentCaps.LoadSession = false, want true")
	}
	if want := []string{"xai.api_key", "cached_token", "grok.com"}; !slices.Equal(s.advertisedAuth, want) {
		t.Errorf("advertisedAuth = %v, want %v", s.advertisedAuth, want)
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
