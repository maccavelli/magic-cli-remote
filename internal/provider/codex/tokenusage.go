package codex

import "github.com/maccavelli/magic-cli-remote/internal/event"

// codexTokenCounts is one side of `thread/tokenUsage/updated` — the `total`
// (whole thread) or `last` (most recent turn) object.
//
// Codex reports the cached share of its input, so a turn that reads a warm
// prefix is distinguishable from one that pays a full prefill. mcremote used to
// decode this field as a bare int, which failed and cost every codex session
// its usage reporting entirely (MADR 0137, second correction).
type codexTokenCounts struct {
	TotalTokens           int `json:"totalTokens"`
	InputTokens           int `json:"inputTokens"`
	CachedInputTokens     int `json:"cachedInputTokens"`
	CacheWriteInputTokens int `json:"cacheWriteInputTokens"`
	OutputTokens          int `json:"outputTokens"`
	ReasoningOutputTokens int `json:"reasoningOutputTokens"`
}

// usage maps the counts onto the daemon's shape.
//
// Input is reported as codex sends it: `inputTokens` is the whole input and
// `cachedInputTokens` is the part of it served from cache, so the two are not
// added. Subtracting to get a "fresh" figure would invent a number codex does
// not report and would go negative on any accounting mismatch.
func (c codexTokenCounts) usage(window int) *event.Usage {
	return &event.Usage{
		Used:       c.TotalTokens,
		Size:       window,
		Input:      int64(c.InputTokens),
		Output:     int64(c.OutputTokens),
		Reasoning:  int64(c.ReasoningOutputTokens),
		CacheRead:  int64(c.CachedInputTokens),
		CacheWrite: int64(c.CacheWriteInputTokens),
	}
}

// codexTokenUsageParams is the `thread/tokenUsage/updated` params object.
//
// It is a named type, used by both the notification handler and its test, so
// the test exercises the production decode rather than a copy of it. A copy is
// how this defect survived: the shape was wrong in exactly one place, and
// nothing that duplicated it could notice.
type codexTokenUsageParams struct {
	TokenUsage struct {
		Total              codexTokenCounts `json:"total"`
		Last               codexTokenCounts `json:"last"`
		ModelContextWindow int              `json:"modelContextWindow"`
	} `json:"tokenUsage"`
}
