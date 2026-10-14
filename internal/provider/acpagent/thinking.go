package acpagent

import (
	"context"
	"fmt"
	"strings"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// SetThinkingLevel sets the session's thinking level. The standard path is the
// agent's `thought_level` config option, whose set_config_option answer is
// the read-back (MADR 0184 D6); an agent that offers none gets its Spec's
// SetThinkingFallback, which for grok is set_model `_meta.reasoningEffort`
// confirmed by session/resume (MADR 0106). It does not return
// ErrThinkingLevelFixed.
func (s *session) SetThinkingLevel(ctx context.Context, level string) error {
	level = strings.TrimSpace(level)
	if level == "" {
		return fmt.Errorf("thinking level required")
	}
	s.mu.Lock()
	closed := s.closed
	agentID := s.agentID
	modelID := s.currentModelID
	current := s.thinkingLevel
	s.mu.Unlock()
	if closed {
		return fmt.Errorf("session closed")
	}
	if level == current {
		return nil
	}
	got, handled, err := s.setSelectOption(ctx, acp.SessionConfigOptionCategoryThoughtLevel, level)
	if !handled {
		fallback := s.spec().SetThinkingFallback
		if fallback == nil {
			return fmt.Errorf("thinking: the agent offers no thought_level config option: %w", provider.ErrNotImplemented)
		}
		if agentID == "" || modelID == "" {
			return fmt.Errorf("thinking: missing session id or model")
		}
		got, err = fallback(ctx, s, modelID, level)
	}
	if err != nil {
		return err
	}
	if got != level {
		return fmt.Errorf("thinking level %q not applied (agent reports %q)", level, got)
	}
	s.mu.Lock()
	s.thinkingLevel = level
	s.mu.Unlock()
	return nil
}
