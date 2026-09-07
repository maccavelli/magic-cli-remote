package kilo

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
)

// providerUsage is kilo's `GET /kilocode/provider-usage` response —
// "cache-aware, secret-free provider plan usage and personal billing status".
//
// The shapes below are transcribed from the engine's own OpenAPI document
// (`GET /doc`, components.schemas.ProviderUsage*) on kilo 7.5.6, not inferred
// from a sample: this host's account returns an empty `items`, so a sample
// would have told us nothing about the fields that matter.
type providerUsage struct {
	Items       []providerUsageSnapshot `json:"items"`
	GeneratedAt string                  `json:"generatedAt"`
}

type providerUsageSnapshot struct {
	ProviderID    string                `json:"providerID"`
	ProviderLabel string                `json:"providerLabel"`
	PlanLabel     string                `json:"planLabel"`
	FetchState    string                `json:"fetchState"`
	PlanState     string                `json:"planState"`
	Windows       []providerUsageWindow `json:"windows"`
}

type providerUsageWindow struct {
	Resource  string  `json:"resource"`
	Unit      string  `json:"unit"`
	Used      float64 `json:"used"`
	Remaining float64 `json:"remaining"`
	Limit     float64 `json:"limit"`
	ResetAt   string  `json:"resetAt"`
	// State is one of active, exhausted, unlimited, not_in_plan, unknown.
	State string `json:"state"`
}

// quotaProbeTimeout bounds the structured lookup. It runs while a turn has
// already failed, so it must not add a visible wait on top of that.
const quotaProbeTimeout = 4 * time.Second

// exhaustedWindows returns a human-readable summary of every plan window the
// engine reports as exhausted, or "" when none is.
func (u providerUsage) exhaustedWindows() string {
	var parts []string
	for _, item := range u.Items {
		if item.FetchState == "error" || item.FetchState == "unavailable" {
			continue
		}
		for _, w := range item.Windows {
			if w.State != "exhausted" {
				continue
			}
			label := strings.TrimSpace(item.ProviderLabel)
			if label == "" {
				label = item.ProviderID
			}
			part := label
			if res := strings.TrimSpace(w.Resource); res != "" {
				part += " " + res
			}
			if reset := strings.TrimSpace(w.ResetAt); reset != "" {
				part += " (resets " + reset + ")"
			}
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}

// confirmLimit asks the engine for structured plan usage after the prose
// classifier has flagged a limit, and returns a summary of what it found.
//
// Why this exists: the daemon reconstructs rate-limit and quota state from
// English in engine log lines — 967 lines of regular expressions over vendor
// wording (internal/agenterr). kilo answers the question directly, so on kilo
// the prose is a fallback rather than the source (MADR 0138 F9).
//
// The second return reports whether the engine confirmed the limit. A prose
// match that structured usage does *not* confirm is logged at warn: that is the
// day a vendor changed its wording, and it is the only signal we would get.
func (o *httpSession) confirmLimit(ctx context.Context, kind agenterr.Kind) (summary string, confirmed bool) {
	if kind != agenterr.KindQuota && kind != agenterr.KindRateLimit {
		return "", false
	}
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), quotaProbeTimeout)
	defer cancel()

	var usage providerUsage
	if err := o.h.API()(callCtx, "GET", "/kilocode/provider-usage", nil, &usage); err != nil {
		// Unreachable is not the same as unconfirmed: say so, and do not treat
		// a failed probe as evidence that the prose was wrong.
		o.h.Log().Debug("kilo provider-usage probe failed", slog.String("err", err.Error()))
		return "", false
	}
	if s := usage.exhaustedWindows(); s != "" {
		return s, true
	}
	o.h.Log().Warn(
		"engine reported a limit in prose that its usage API does not confirm",
		slog.String("kind", string(kind)),
		slog.Int("plans_reported", len(usage.Items)),
		slog.String("hint", "either the limit is not plan-level, or the vendor's wording changed and "+
			"internal/agenterr matched something it should not have"),
	)
	return "", false
}

// annotateLimit appends the structured summary to a classified error message.
func annotateLimit(message, summary string) string {
	if summary == "" {
		return message
	}
	return fmt.Sprintf("%s (plan usage: %s)", strings.TrimRight(message, " ."), summary)
}
