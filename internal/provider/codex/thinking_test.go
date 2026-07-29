package codex

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Captured from codex 0.145.0 model/list shapes (MADR 0052 §1 / §2.1): terra
// ships the six-rung ladder including ultra; 5.4-mini stops at xhigh. Defaults
// and descriptions are the live prose, not invented.
const thinkingModelsPage = `{"data":[
  {
    "id":"gpt-5.6-terra",
    "displayName":"GPT-5.6-Terra",
    "description":"Balanced.",
    "hidden":false,
    "isDefault":true,
    "defaultReasoningEffort":"medium",
    "inputModalities":["text","image"],
    "supportedReasoningEfforts":[
      {"reasoningEffort":"low","description":"Fast responses with lighter reasoning"},
      {"reasoningEffort":"medium","description":"Balances speed and reasoning depth for everyday tasks"},
      {"reasoningEffort":"high","description":"Greater reasoning depth for complex problems"},
      {"reasoningEffort":"xhigh","description":"Extra high reasoning depth for complex problems"},
      {"reasoningEffort":"max","description":"Maximum reasoning depth for the hardest problems"},
      {"reasoningEffort":"ultra","description":"Maximum reasoning with automatic task delegation"}
    ]
  },
  {
    "id":"gpt-5.4-mini",
    "displayName":"GPT-5.4 Mini",
    "description":"Fast.",
    "hidden":false,
    "isDefault":false,
    "defaultReasoningEffort":"medium",
    "inputModalities":["text"],
    "supportedReasoningEfforts":[
      {"reasoningEffort":"low","description":"Fast responses with lighter reasoning"},
      {"reasoningEffort":"medium","description":"Balances speed and reasoning depth for everyday tasks"},
      {"reasoningEffort":"high","description":"Greater reasoning depth for complex problems"},
      {"reasoningEffort":"xhigh","description":"Extra high reasoning depth for complex problems"}
    ]
  },
  {
    "id":"no-ladder",
    "displayName":"No Ladder",
    "hidden":false,
    "isDefault":false,
    "defaultReasoningEffort":"medium",
    "supportedReasoningEfforts":[]
  }
]}`

func TestListModelsThinkingLevelsFromFixture(t *testing.T) {
	cat, err := listModelsVia(context.Background(), codexLike(thinkingModelsPage), "", silentLogger())
	if err != nil {
		t.Fatal(err)
	}

	byID := map[string]picker.Option{}
	for _, o := range cat.Options {
		byID[o.ID] = o
	}

	terra, ok := byID["gpt-5.6-terra"]
	if !ok {
		t.Fatal("terra missing from catalog")
	}
	wantTerra := []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	if got := levelIDs(terra.ThinkingLevels); !equalStrings(got, wantTerra) {
		t.Errorf("terra levels = %v, want %v", got, wantTerra)
	}
	if d := picker.DefaultThinkingLevel(terra.ThinkingLevels); d != "medium" {
		t.Errorf("terra default = %q, want medium", d)
	}
	// codex ships prose, not labels — clients fall back to ID.
	for _, l := range terra.ThinkingLevels {
		if l.Label != "" {
			t.Errorf("terra %q: unexpected label %q", l.ID, l.Label)
		}
		if l.Description == "" {
			t.Errorf("terra %q: missing description", l.ID)
		}
	}

	mini, ok := byID["gpt-5.4-mini"]
	if !ok {
		t.Fatal("5.4-mini missing from catalog")
	}
	wantMini := []string{"low", "medium", "high", "xhigh"}
	if got := levelIDs(mini.ThinkingLevels); !equalStrings(got, wantMini) {
		t.Errorf("mini levels = %v, want %v", got, wantMini)
	}
	if d := picker.DefaultThinkingLevel(mini.ThinkingLevels); d != "medium" {
		t.Errorf("mini default = %q, want medium", d)
	}

	bare, ok := byID["no-ladder"]
	if !ok {
		t.Fatal("no-ladder missing from catalog")
	}
	if len(bare.ThinkingLevels) != 0 {
		t.Errorf("empty supportedReasoningEfforts must yield no levels, got %v",
			levelIDs(bare.ThinkingLevels))
	}
	// Badge meta still carries the default even when the list is empty.
	if bare.Meta["reasoning_effort"] != "medium" {
		t.Errorf("no-ladder meta reasoning_effort = %q, want medium", bare.Meta["reasoning_effort"])
	}
}

// TestTurnStartEffortOmitsWhenUnset is the load-bearing omission check: "Provider
// default" means the key is absent, not effort:"". A hard-coded empty string
// would silently override the model default — this test fails if that creeps in.
func TestTurnStartEffortOmitsWhenUnset(t *testing.T) {
	params := captureTurnStartParams(t, "")
	if _, present := params["effort"]; present {
		t.Errorf("effort key present with value %#v; want the key omitted entirely", params["effort"])
	}
}

func TestTurnStartEffortCarriesWhenSet(t *testing.T) {
	params := captureTurnStartParams(t, "xhigh")
	got, ok := params["effort"].(string)
	if !ok {
		t.Fatalf("effort missing or wrong type: %#v", params["effort"])
	}
	if got != "xhigh" {
		t.Errorf("effort = %q, want xhigh", got)
	}
}

// StartOptions.ThinkingLevel seeds the session so the first turn/start carries
// effort without an explicit SetThinkingLevel (create-session path, A3/A4).
func TestTurnStartEffortFromStartOptions(t *testing.T) {
	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})

	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})

	p := &Provider{log: testLogger(t)}
	p.eng = &engine{conn: c, dead: make(chan struct{})}

	s := newSession(p, Config{}, provider.StartOptions{
		LocalSessionID: "local-seed",
		CWD:            t.TempDir(),
		ThinkingLevel:  "high",
	}, testLogger(t))
	s.agentID = "thread-seed"
	if s.ThinkingLevel() != "high" {
		t.Fatalf("ThinkingLevel() = %q, want high from StartOptions", s.ThinkingLevel())
	}

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		done <- s.beginTurn(ctx, []provider.Content{{Type: "text", Text: "hi"}}, true)
	}()

	var req struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
		ID     int64          `json:"id"`
	}
	if err := json.NewDecoder(engineR).Decode(&req); err != nil {
		t.Fatalf("read request: %v", err)
	}
	if req.Method != "turn/start" {
		t.Fatalf("method = %q, want turn/start", req.Method)
	}
	if got, _ := req.Params["effort"].(string); got != "high" {
		t.Errorf("effort = %#v, want high", req.Params["effort"])
	}

	resp, err := json.Marshal(map[string]any{
		"id":     req.ID,
		"result": map[string]any{"turn": map[string]any{"id": "turn-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engineW.Write(append(resp, '\n')); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("beginTurn hang")
	}
}

func captureTurnStartParams(t *testing.T, level string) map[string]any {
	t.Helper()

	engineR, sessionW := io.Pipe()
	sessionR, engineW := io.Pipe()
	t.Cleanup(func() {
		_ = sessionW.Close()
		_ = engineW.Close()
		_ = engineR.Close()
		_ = sessionR.Close()
	})

	c := newConn(sessionW, sessionR, testLogger(t))
	go c.readPump(func(string, json.RawMessage) {}, func(string, json.RawMessage, json.RawMessage) {})

	p := &Provider{log: testLogger(t)}
	p.eng = &engine{conn: c, dead: make(chan struct{})}

	s := newSession(p, Config{}, provider.StartOptions{
		LocalSessionID: "local-thinking", CWD: t.TempDir(),
	}, testLogger(t))
	s.agentID = "thread-thinking"
	if err := s.SetThinkingLevel(context.Background(), level); err != nil {
		t.Fatalf("SetThinkingLevel: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		done <- s.beginTurn(ctx, []provider.Content{{Type: "text", Text: "hi"}}, true)
	}()

	var req struct {
		ID     int64          `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(engineR).Decode(&req); err != nil {
		t.Fatalf("read request: %v", err)
	}
	if req.Method != "turn/start" {
		t.Fatalf("method = %q, want turn/start", req.Method)
	}

	resp, err := json.Marshal(map[string]any{
		"id":     req.ID,
		"result": map[string]any{"turn": map[string]any{"id": "turn-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engineW.Write(append(resp, '\n')); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Logf("beginTurn: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("beginTurn hang")
	}
	return req.Params
}

func levelIDs(levels []picker.ThinkingLevel) []string {
	out := make([]string, len(levels))
	for i, l := range levels {
		out[i] = l.ID
	}
	return out
}

func equalStrings(a, b []string) bool {
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
