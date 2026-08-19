package acpagent

import (
	"context"
	"fmt"
	"strings"
)

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

type grokResumeSnapshot struct {
	Models struct {
		CurrentModelID  string               `json:"currentModelId"`
		AvailableModels []GrokAvailableModel `json:"availableModels"`
	} `json:"models"`
	Meta map[string]any `json:"_meta"`
}

func (sn grokResumeSnapshot) appliedEffort(modelID string) string {
	for _, m := range sn.Models.AvailableModels {
		if m.ModelID == modelID && m.Meta.ReasoningEffort != "" {
			return m.Meta.ReasoningEffort
		}
	}
	return appliedEffortFromMeta(sn.Meta)
}

// SetThinkingLevel applies grok 1.0.5 session/set_model `_meta.reasoningEffort`
// and confirms via session/resume (MADR 0106). It does not return
// ErrThinkingLevelFixed.
func (s *session) SetThinkingLevel(ctx context.Context, level string) error {
	level = strings.TrimSpace(level)
	if level == "" {
		return fmt.Errorf("thinking level required")
	}
	s.mu.Lock()
	closed := s.closed
	agentID := s.agentID
	cwd := s.cwd
	modelID := s.currentModelID
	current := s.thinkingLevel
	s.mu.Unlock()
	if closed {
		return fmt.Errorf("session closed")
	}
	if agentID == "" || modelID == "" {
		return fmt.Errorf("thinking: missing session id or model")
	}
	if level == current {
		return nil
	}
	var setResp struct {
		Meta struct {
			Model struct {
				Ok  string `json:"Ok,omitempty"`
				Err string `json:"Err,omitempty"`
			} `json:"model"`
		} `json:"_meta"`
	}
	if err := s.rawRequest(ctx, "session/set_model", setThinkingRequest(agentID, modelID, level), &setResp); err != nil {
		return err
	}
	if setResp.Meta.Model.Err != "" {
		return fmt.Errorf("set_model: %s", setResp.Meta.Model.Err)
	}
	var snap grokResumeSnapshot
	if err := s.rawRequest(ctx, "session/resume", resumeSnapshotRequest(agentID, cwd), &snap); err != nil {
		return fmt.Errorf("thinking read-back: %w", err)
	}
	got := snap.appliedEffort(modelID)
	if got != level {
		return fmt.Errorf("thinking level %q not applied (agent reports %q)", level, got)
	}
	s.mu.Lock()
	s.thinkingLevel = level
	s.mu.Unlock()
	return nil
}
