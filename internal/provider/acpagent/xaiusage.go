package acpagent

import (
	"github.com/maccavelli/magic-cli-remote/internal/event"
)

// xaiUsage is grok's token accounting, carried on the `turn_completed` variant
// of `_x.ai/session_notification`.
//
// It exists because ACP's standard `SessionUsageUpdate` is `{used, size, cost}`
// and carries no cache fields (acp-go-sdk@v0.13.5 types_gen.go:5243). grok
// reports a cached input share — 11776 and 5888 tokens on observed turns — and
// with only the standard update mapped, every grok turn was recorded as having
// paid a full uncached prefill (MADR 0137, second correction).
type xaiUsage struct {
	InputTokens         int `json:"inputTokens"`
	OutputTokens        int `json:"outputTokens"`
	TotalTokens         int `json:"totalTokens"`
	CachedReadTokens    int `json:"cachedReadTokens"`
	CacheCreationTokens int `json:"cacheCreationTokens"`
	ReasoningTokens     int `json:"reasoningTokens"`
}

// emitXAIUsage publishes grok's token split.
//
// Size is left at whatever the standard usage_update reported, which is 0 here:
// grok's turn_completed carries no context-window size, and inventing one from
// the model catalog would report a limit this message never stated. A client
// renders a bare token count when size is 0, which is the honest rendering.
//
// Input is grok's `inputTokens` as sent, with `cachedReadTokens` alongside as
// the cached share of it. The two are not added — that would double-count the
// cached prefix.
func (s *session) emitXAIUsage(u *xaiUsage) {
	if u == nil || u.TotalTokens <= 0 {
		return
	}
	s.emit(event.Event{
		Type:      event.TypeUsage,
		SessionID: s.localID,
		Usage: &event.Usage{
			Used:       u.TotalTokens,
			Input:      int64(u.InputTokens),
			Output:     int64(u.OutputTokens),
			Reasoning:  int64(u.ReasoningTokens),
			CacheRead:  int64(u.CachedReadTokens),
			CacheWrite: int64(u.CacheCreationTokens),
		},
	})
}
