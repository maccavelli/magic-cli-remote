package grok

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestAppliedModelAndEffortFromMeta(t *testing.T) {
	xhigh := map[string]any{
		"x.ai/sessionDetail": map[string]any{"currentModelId": "grok-4.6"},
		"x.ai/sessionConfig": map[string]any{
			"options": []any{
				map[string]any{"id": "grok-4.6", "category": "model", "selected": true},
				map[string]any{"id": "grok-4.5", "category": "model", "selected": false},
				map[string]any{"id": "xhigh", "category": "mode", "selected": true},
				map[string]any{"id": "high", "category": "mode", "selected": false},
			},
		},
	}
	if got := appliedModelFromMeta(xhigh); got != "grok-4.6" {
		t.Errorf("xhigh model = %q, want grok-4.6", got)
	}
	if got := appliedEffortFromMeta(xhigh); got != "xhigh" {
		t.Errorf("xhigh effort = %q, want xhigh", got)
	}

	low := map[string]any{
		"x.ai/sessionDetail": map[string]any{"currentModelId": "grok-4.6"},
		"x.ai/sessionConfig": map[string]any{
			"options": []any{
				map[string]any{"id": "grok-4.6", "category": "model", "selected": true},
				map[string]any{"id": "low", "category": "mode", "selected": true},
				map[string]any{"id": "high", "category": "mode", "selected": false},
			},
		},
	}
	if got := appliedEffortFromMeta(low); got != "low" {
		t.Errorf("low effort = %q, want low", got)
	}

	high := map[string]any{
		"x.ai/sessionDetail": map[string]any{"currentModelId": "grok-4.6"},
		"x.ai/sessionConfig": map[string]any{
			"options": []any{
				map[string]any{"id": "grok-4.6", "category": "model", "selected": true},
				map[string]any{"id": "high", "category": "mode", "selected": true},
				map[string]any{"id": "low", "category": "mode", "selected": false},
			},
		},
	}
	if got := appliedEffortFromMeta(high); got != "high" {
		t.Errorf("baseline effort = %q, want high", got)
	}

	if got := appliedModelFromMeta(nil); got != "" {
		t.Errorf("nil meta model = %q", got)
	}
	if got := appliedEffortFromMeta(map[string]any{}); got != "" {
		t.Errorf("empty meta effort = %q", got)
	}
	if m, e := harvestSessionMeta(xhigh); m != "grok-4.6" || e != "xhigh" {
		t.Errorf("harvestSessionMeta = %q, %q; want grok-4.6, xhigh", m, e)
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

// rawCall is one request a fallback sent.
type rawCall struct {
	method string
	params any
}

// fakeRawCaller answers each method with a canned JSON result and records
// what was sent.
type fakeRawCaller struct {
	answers map[string]string
	fail    map[string]error
	calls   []rawCall
}

func (f *fakeRawCaller) RawRequest(_ context.Context, method string, params, out any) error {
	f.calls = append(f.calls, rawCall{method, params})
	if err := f.fail[method]; err != nil {
		return err
	}
	return json.Unmarshal([]byte(f.answers[method]), out)
}

func (f *fakeRawCaller) AgentSessionID() string { return "sid" }
func (f *fakeRawCaller) CWD() string            { return "/tmp/cwd" }

func TestSetModelFallback(t *testing.T) {
	ctx := context.Background()
	rc := &fakeRawCaller{answers: map[string]string{"session/set_model": `{"_meta":{"model":{"Ok":"grok-4.6"}}}`}}
	got, err := setModelFallback(ctx, rc, "grok-4.6-alias")
	if err != nil || got != "grok-4.6" {
		t.Fatalf("Ok answer: got %q, %v; want grok-4.6", got, err)
	}
	if len(rc.calls) != 1 || rc.calls[0].method != "session/set_model" {
		t.Fatalf("calls = %+v, want one session/set_model", rc.calls)
	}
	if p, _ := rc.calls[0].params.(map[string]any); p["sessionId"] != "sid" || p["modelId"] != "grok-4.6-alias" {
		t.Fatalf("params = %#v", rc.calls[0].params)
	}

	rc = &fakeRawCaller{answers: map[string]string{"session/set_model": `{}`}}
	if got, err := setModelFallback(ctx, rc, "grok-4.5"); err != nil || got != "grok-4.5" {
		t.Fatalf("bare answer: got %q, %v; want the requested id", got, err)
	}

	rc = &fakeRawCaller{answers: map[string]string{"session/set_model": `{"_meta":{"model":{"Err":"unknown model"}}}`}}
	if _, err := setModelFallback(ctx, rc, "grok-9"); err == nil || err.Error() != "set_model: unknown model" {
		t.Fatalf("Err answer: err = %v", err)
	}
}

func TestSetThinkingFallbackReadsBackFromResume(t *testing.T) {
	ctx := context.Background()
	rc := &fakeRawCaller{answers: map[string]string{
		"session/set_model": `{}`,
		"session/resume": `{"models":{"currentModelId":"grok-4.6","availableModels":[
			{"modelId":"grok-4.6","name":"Grok 4.6","_meta":{"reasoningEffort":"low"}}]}}`,
	}}
	got, err := setThinkingFallback(ctx, rc, "grok-4.6", "low")
	if err != nil || got != "low" {
		t.Fatalf("got %q, %v; want low", got, err)
	}
	if len(rc.calls) != 2 || rc.calls[0].method != "session/set_model" || rc.calls[1].method != "session/resume" {
		t.Fatalf("calls = %+v, want set_model then resume", rc.calls)
	}

	// What resume reports is the answer, not what was asked for.
	rc = &fakeRawCaller{answers: map[string]string{
		"session/set_model": `{}`,
		"session/resume":    `{"_meta":{"x.ai/sessionConfig":{"options":[{"id":"high","category":"mode","selected":true}]}}}`,
	}}
	if got, err := setThinkingFallback(ctx, rc, "grok-4.6", "low"); err != nil || got != "high" {
		t.Fatalf("kept level: got %q, %v; want the read-back high", got, err)
	}

	boom := errors.New("boom")
	rc = &fakeRawCaller{answers: map[string]string{"session/set_model": `{}`}, fail: map[string]error{"session/resume": boom}}
	if _, err := setThinkingFallback(ctx, rc, "grok-4.6", "low"); !errors.Is(err, boom) {
		t.Fatalf("resume failure: err = %v, want it wrapped", err)
	}
}

func TestSpecWiresGrokFallbacks(t *testing.T) {
	if spec.SetModelFallback == nil || spec.SetThinkingFallback == nil || spec.HarvestSessionMeta == nil {
		t.Fatal("grok releases up to the known-good pin offer no standard model/thought_level option; its Spec must carry all three fallbacks")
	}
}
