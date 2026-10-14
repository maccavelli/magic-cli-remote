package acpagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// configOptionsJSON is an agent's configOptions with a standard `model` and
// `thought_level` select option at the given current values.
func configOptionsJSON(model, effort string) string {
	return `[
	  {"type":"select","id":"model","name":"Model","category":"model","currentValue":"` + model + `",
	   "options":[{"value":"m1","name":"M1"},{"value":"m2","name":"M2"}]},
	  {"type":"select","id":"effort","name":"Effort","category":"thought_level","currentValue":"` + effort + `",
	   "options":[{"value":"low","name":"Low"},{"value":"medium","name":"Medium"},{"value":"high","name":"High"}]}
	]`
}

func parseConfigOptions(t *testing.T, raw string) []acp.SessionConfigOption {
	t.Helper()
	var opts []acp.SessionConfigOption
	if err := json.Unmarshal([]byte(raw), &opts); err != nil {
		t.Fatalf("config options: %v", err)
	}
	return opts
}

// refusingFallbacks is a Spec whose vendor hooks fail the test if they run.
func refusingFallbacks(t *testing.T) Spec {
	return Spec{
		SetModelFallback: func(context.Context, RawCaller, string) (string, error) {
			t.Error("SetModelFallback ran although the agent offers a model option")
			return "", errors.New("fallback")
		},
		SetThinkingFallback: func(context.Context, RawCaller, string, string) (string, error) {
			t.Error("SetThinkingFallback ran although the agent offers a thought_level option")
			return "", errors.New("fallback")
		},
		HarvestSessionMeta: func(map[string]any) (string, string) {
			return "harvested-model", "harvested-effort"
		},
	}
}

// setOptionCall runs call and plays the agent's side of the one
// session/set_config_option it must send: it checks the request names
// configID and value, and answers with configOptions answer. It returns
// call's error.
func setOptionCall(t *testing.T, a *wireAgent, call func() error, configID, value, answer string) error {
	t.Helper()
	errc := make(chan error, 1)
	go func() { errc <- call() }()
	m := a.next(5 * time.Second)
	if m.Method != "session/set_config_option" {
		t.Fatalf("client sent %q, want session/set_config_option", m.Method)
	}
	var p map[string]any
	if err := json.Unmarshal(m.Params, &p); err != nil {
		t.Fatalf("params: %v", err)
	}
	if p["sessionId"] != "agent-1" || p["configId"] != configID || p["value"] != value {
		t.Fatalf("set_config_option params = %s, want configId %q value %q", m.Params, configID, value)
	}
	a.respond(m, map[string]any{"configOptions": json.RawMessage(answer)})
	select {
	case err := <-errc:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("the call did not return after the agent answered")
		return nil
	}
}

// TestModelAndThinkingUseStandardConfigOptions is PLAN 0184 A3 step 5: an
// agent that offers `model` and `thought_level` config options is driven
// through session/set_config_option and read back from its answer, and no
// vendor fallback runs.
func TestModelAndThinkingUseStandardConfigOptions(t *testing.T) {
	ctx := context.Background()
	s, a := newWireSession(t, `{}`)
	p := &Provider{spec: refusingFallbacks(t)}
	s.provider = p

	s.mu.Lock()
	p.applySessionState(s, map[string]any{"any": "meta"}, parseConfigOptions(t, configOptionsJSON("m1", "low")))
	model, effort := s.currentModelID, s.thinkingLevel
	s.mu.Unlock()
	if model != "m1" || effort != "low" {
		t.Fatalf("after session/new: model %q effort %q, want the options' m1/low over the harvest", model, effort)
	}

	err := setOptionCall(t, a, func() error { return s.SetModel(ctx, "m2") }, "model", "m2", configOptionsJSON("m2", "low"))
	if err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if got := s.CurrentModel(); got != "m2" {
		t.Fatalf("CurrentModel = %q, want m2", got)
	}

	err = setOptionCall(t, a, func() error { return s.SetThinkingLevel(ctx, "high") }, "effort", "high", configOptionsJSON("m2", "high"))
	if err != nil {
		t.Fatalf("SetThinkingLevel: %v", err)
	}
	if got := s.ThinkingLevel(); got != "high" {
		t.Fatalf("ThinkingLevel = %q, want high", got)
	}

	// The answer is the read-back: an agent that keeps the old value has not
	// applied the change.
	err = setOptionCall(t, a, func() error { return s.SetThinkingLevel(ctx, "medium") }, "effort", "medium", configOptionsJSON("m2", "high"))
	if err == nil || !strings.Contains(err.Error(), `"medium" not applied`) {
		t.Fatalf("unapplied level: err = %v", err)
	}
	if got := s.ThinkingLevel(); got != "high" {
		t.Fatalf("ThinkingLevel after a refusal = %q, want high kept", got)
	}

	// Each answer is forwarded to the client as the session's config.
	configs := 0
	for len(s.events) > 0 {
		if ev := <-s.events; ev.Type == event.TypeSessionConfig {
			configs++
		}
	}
	if configs != 3 {
		t.Errorf("session_config events = %d, want one per answer (3)", configs)
	}
	if m, ok := a.quiet(200 * time.Millisecond); ok {
		t.Fatalf("client sent %q after the three calls; want nothing else", m.Method)
	}
}

// TestModelAndThinkingFallBackOnlyWithoutStandardOptions: an agent that offers
// no such option gets the Spec's fallbacks, with the session as their
// RawCaller, and nothing goes on the wire from acpagent itself.
func TestModelAndThinkingFallBackOnlyWithoutStandardOptions(t *testing.T) {
	ctx := context.Background()
	s, a := newWireSession(t, `{}`)
	var models, levels []string
	s.provider = &Provider{spec: Spec{
		SetModelFallback: func(_ context.Context, rc RawCaller, model string) (string, error) {
			if rc.AgentSessionID() != "agent-1" {
				t.Errorf("fallback RawCaller session = %q", rc.AgentSessionID())
			}
			models = append(models, model)
			return model, nil
		},
		SetThinkingFallback: func(_ context.Context, _ RawCaller, modelID, level string) (string, error) {
			levels = append(levels, modelID+"/"+level)
			return level, nil
		},
		HarvestSessionMeta: func(map[string]any) (string, string) { return "grok-4.6", "high" },
	}}

	s.mu.Lock()
	s.provider.applySessionState(s, map[string]any{}, nil)
	s.mu.Unlock()
	if s.CurrentModel() != "grok-4.6" || s.ThinkingLevel() != "high" {
		t.Fatalf("harvest: model %q effort %q, want grok-4.6/high", s.CurrentModel(), s.ThinkingLevel())
	}
	if err := s.SetModel(ctx, "grok-4.5"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	if err := s.SetThinkingLevel(ctx, "low"); err != nil {
		t.Fatalf("SetThinkingLevel: %v", err)
	}
	if len(models) != 1 || models[0] != "grok-4.5" || len(levels) != 1 || levels[0] != "grok-4.5/low" {
		t.Fatalf("fallback calls: models %v levels %v", models, levels)
	}
	if m, ok := a.quiet(200 * time.Millisecond); ok {
		t.Fatalf("acpagent itself sent %q; only the fallback may talk to the agent", m.Method)
	}
}

// TestModelAndThinkingWithNeitherPathIsNotImplemented: no standard option and
// no fallback is ErrNotImplemented, not a guess at a vendor method.
func TestModelAndThinkingWithNeitherPathIsNotImplemented(t *testing.T) {
	ctx := context.Background()
	s, a := newWireSession(t, `{}`)
	s.currentModelID = "m1"
	if err := s.SetModel(ctx, "m2"); !errors.Is(err, provider.ErrNotImplemented) {
		t.Fatalf("SetModel err = %v, want ErrNotImplemented", err)
	}
	if err := s.SetThinkingLevel(ctx, "high"); !errors.Is(err, provider.ErrNotImplemented) {
		t.Fatalf("SetThinkingLevel err = %v, want ErrNotImplemented", err)
	}
	if m, ok := a.quiet(200 * time.Millisecond); ok {
		t.Fatalf("client sent %q with no path to take", m.Method)
	}
}

// TestConfigOptionUpdateMovesModelAndThinking: a mid-session
// config_option_update is the agent's word on its current model and level.
func TestConfigOptionUpdateMovesModelAndThinking(t *testing.T) {
	s := &session{localID: "local-1", events: make(chan event.Event, 8), done: make(chan struct{})}
	s.mu.Lock()
	s.recordConfigOptions(parseConfigOptions(t, configOptionsJSON("m1", "low")))
	s.mu.Unlock()
	var n acp.SessionNotification
	raw := `{"sessionId":"agent-1","update":{"sessionUpdate":"config_option_update","configOptions":` +
		configOptionsJSON("m2", "medium") + `}}`
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		t.Fatalf("notification: %v", err)
	}
	if err := s.SessionUpdate(context.Background(), n); err != nil {
		t.Fatalf("SessionUpdate: %v", err)
	}
	if s.CurrentModel() != "m2" || s.ThinkingLevel() != "medium" {
		t.Fatalf("after update: model %q effort %q, want m2/medium", s.CurrentModel(), s.ThinkingLevel())
	}
}
