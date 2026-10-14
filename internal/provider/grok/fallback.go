package grok

import (
	"context"
	"fmt"

	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// grok's model and thinking fallbacks (MADR 0184 D6). acpagent runs these only
// for a grok that offers no standard `model` or `thought_level` session config
// option: 1.0.13, the known-good pin, offers none; 1.0.50 offers both and never
// reaches them. The methods they send are grok's, not ACP's, so they live here.

// setModelResponse is grok's session/set_model answer: `_meta.model.Ok` is
// the model id grok accepted, `_meta.model.Err` its refusal.
type setModelResponse struct {
	Meta struct {
		Model struct {
			Ok  string `json:"Ok,omitempty"`
			Err string `json:"Err,omitempty"`
		} `json:"model"`
	} `json:"_meta"`
}

// setModelFallback switches the model with grok's session/set_model, which
// schema 1.25 does not define (verified live against grok 0.2.112; MADR 0039
// D1).
func setModelFallback(ctx context.Context, rc acpagent.RawCaller, model string) (string, error) {
	var resp setModelResponse
	if err := rc.RawRequest(ctx, "session/set_model",
		map[string]any{"sessionId": rc.AgentSessionID(), "modelId": model}, &resp); err != nil {
		return "", err
	}
	if resp.Meta.Model.Err != "" {
		return "", fmt.Errorf("set_model: %s", resp.Meta.Model.Err)
	}
	if resp.Meta.Model.Ok != "" {
		return resp.Meta.Model.Ok, nil
	}
	return model, nil
}

// setThinkingFallback applies grok 1.0.5 session/set_model
// `_meta.reasoningEffort` and returns the effort session/resume reads back
// (MADR 0106).
func setThinkingFallback(ctx context.Context, rc acpagent.RawCaller, modelID, level string) (string, error) {
	var resp setModelResponse
	if err := rc.RawRequest(ctx, "session/set_model",
		setThinkingRequest(rc.AgentSessionID(), modelID, level), &resp); err != nil {
		return "", err
	}
	if resp.Meta.Model.Err != "" {
		return "", fmt.Errorf("set_model: %s", resp.Meta.Model.Err)
	}
	var snap resumeSnapshot
	if err := rc.RawRequest(ctx, "session/resume",
		resumeSnapshotRequest(rc.AgentSessionID(), rc.CWD()), &snap); err != nil {
		return "", fmt.Errorf("thinking read-back: %w", err)
	}
	return snap.appliedEffort(modelID), nil
}

func setThinkingRequest(agentID, modelID, level string) map[string]any {
	return map[string]any{
		"sessionId": agentID,
		"modelId":   modelID,
		"_meta":     map[string]any{"reasoningEffort": level},
	}
}

func resumeSnapshotRequest(agentID, cwd string) map[string]any {
	return map[string]any{"sessionId": agentID, "cwd": cwd}
}

// resumeSnapshot is the part of grok's session/resume answer the thinking
// read-back needs.
type resumeSnapshot struct {
	Models struct {
		CurrentModelID  string                        `json:"currentModelId"`
		AvailableModels []acpagent.GrokAvailableModel `json:"availableModels"`
	} `json:"models"`
	Meta map[string]any `json:"_meta"`
}

func (sn resumeSnapshot) appliedEffort(modelID string) string {
	for _, m := range sn.Models.AvailableModels {
		if m.ModelID == modelID && m.Meta.ReasoningEffort != "" {
			return m.Meta.ReasoningEffort
		}
	}
	return appliedEffortFromMeta(sn.Meta)
}

// harvestSessionMeta reads the model and effort grok applied from its
// session/new|load `_meta`: `x.ai/sessionDetail` and `x.ai/sessionConfig`.
func harvestSessionMeta(meta map[string]any) (model, effort string) {
	return appliedModelFromMeta(meta), appliedEffortFromMeta(meta)
}

func appliedModelFromMeta(meta map[string]any) string {
	if det := metaObject(meta, "x.ai/sessionDetail"); det != nil {
		if id, _ := det["currentModelId"].(string); id != "" {
			return id
		}
	}
	return selectedConfigID(meta, "model")
}

func appliedEffortFromMeta(meta map[string]any) string {
	return selectedConfigID(meta, "mode")
}

func selectedConfigID(meta map[string]any, category string) string {
	cfg := metaObject(meta, "x.ai/sessionConfig")
	if cfg == nil {
		return ""
	}
	opts, _ := cfg["options"].([]any)
	for _, raw := range opts {
		o, _ := raw.(map[string]any)
		if o == nil {
			continue
		}
		cat, _ := o["category"].(string)
		sel, _ := o["selected"].(bool)
		if cat == category && sel {
			id, _ := o["id"].(string)
			if id != "" {
				return id
			}
		}
	}
	return ""
}

// metaObject returns parent[key] as an object, or nil.
func metaObject(parent map[string]any, key string) map[string]any {
	if parent == nil {
		return nil
	}
	m, _ := parent[key].(map[string]any)
	return m
}
