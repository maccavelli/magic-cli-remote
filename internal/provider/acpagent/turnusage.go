package acpagent

import (
	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// beginTurnUsage starts a turn's usage accounting: nothing reported yet.
func (s *session) beginTurnUsage() {
	s.mu.Lock()
	s.turnUsageReported = false
	s.mu.Unlock()
}

// emitTurnUsage publishes the token split an agent's prompt answer carries, the
// unstable `PromptResponse.usage`, unless the Spec reported this turn on its
// own channel first (Host.ReportUsage), as grok does before the answer (frames
// 82 and 85 of grok 1.0.13's wire capture), so a turn is never counted twice
// (MADR 0207 amendment H, D25). The accounting of MADR 0189 and 0199 is
// unchanged: no cost travels here.
//
// The phone replaces its usage with each report's, so this report carries the
// context gauge the last usage_update set. Before any, Used is the turn's total
// and Size 0, as emitXAIUsage reports it.
func (s *session) emitTurnUsage(resp acp.PromptResponse) {
	//lint:ignore SA1019 the unstable ACP schema's turn usage (MADR 0207 D25); 0207 amendment H
	u := resp.Usage
	if u == nil {
		return
	}
	s.mu.Lock()
	reported, used, size := s.turnUsageReported, s.gaugeUsed, s.gaugeSize
	s.mu.Unlock()
	if reported {
		return
	}
	if used == 0 && size == 0 {
		used = saturatingInt(u.TotalTokens)
	}
	usage := &event.Usage{
		Used:   used,
		Size:   size,
		Input:  int64(saturatingInt(u.InputTokens)),
		Output: int64(saturatingInt(u.OutputTokens)),
	}
	if u.ThoughtTokens != nil {
		usage.Reasoning = int64(saturatingInt(*u.ThoughtTokens))
	}
	if u.CachedReadTokens != nil {
		usage.CacheRead = int64(saturatingInt(*u.CachedReadTokens))
	}
	if u.CachedWriteTokens != nil {
		usage.CacheWrite = int64(saturatingInt(*u.CachedWriteTokens))
	}
	s.emit(event.Event{
		Type:      event.TypeUsage,
		SessionID: s.localID,
		Usage:     usage,
	})
}
