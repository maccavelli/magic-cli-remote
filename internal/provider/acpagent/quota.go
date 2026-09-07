package acpagent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
)

// quotaProbeTimeout bounds the billing lookup that follows a classified limit.
//
// Deliberately longer than kilo's 4 s (internal/provider/kilo/quota.go): kilo's
// probe is loopback to an engine on this machine, while grok's leaves the host
// for xAI's backend and grok bounds its own upstream at 15 s. Six seconds
// covers a normal round trip and gives up well before the operator is left
// staring at a turn that has already failed.
const quotaProbeTimeout = 6 * time.Second

// confirmLimit asks grok for account billing after the prose classifier has
// flagged a limit, and returns a summary of what it found.
//
// Why this exists: the daemon reconstructs quota state from English in engine
// log lines — 967 lines of regular expressions over vendor wording
// (internal/agenterr). grok answers the question structurally, so on grok the
// prose becomes a fallback rather than the source (MADR 0138 F9).
//
// **Quota only.** kilo's provider-usage reports a per-window
// `state: "exhausted"`, which answers "am I rate limited?" directly. grok's
// billing has no equivalent: it reports credits, an on-demand cap and a billing
// period, and says nothing about requests-per-window. A rate limit is therefore
// left to the prose classifier, and this is not called for it — asking a credits
// endpoint about throttling would produce a confident wrong answer.
//
// The second return reports whether billing confirmed the limit. A prose match
// that billing does not confirm is logged at warn: that is the day a vendor
// changed its wording, and it is the only signal there would be.
func (s *session) confirmLimit(ctx context.Context, kind agenterr.Kind) (summary string, confirmed bool) {
	if kind != agenterr.KindQuota {
		return "", false
	}
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), quotaProbeTimeout)
	defer cancel()

	var res billingResponse
	if err := callAgentExtension(callCtx, s, "x.ai/billing", map[string]any{}, &res); err != nil {
		// Unreachable is not the same as unconfirmed: say so, and never treat a
		// failed probe as evidence that the prose was wrong. An API-key install
		// takes this path on every limit, by design.
		s.log.Debug("grok billing probe failed",
			slog.String("session_id", s.localID),
			slog.String("err", err.Error()))
		return "", false
	}
	if out := exhaustedCredits(res); out != "" {
		return out, true
	}

	s.log.Warn(
		"engine reported a limit in prose that its billing API does not confirm",
		slog.String("session_id", s.localID),
		slog.String("kind", string(kind)),
		slog.Bool("config_present", res.Config != nil),
		slog.String("hint", "either the limit is not credit-level, or the vendor's wording changed "+
			"and internal/agenterr matched something it should not have"),
	)
	return "", false
}

// creditExhaustionPercent is the point at which included credits are treated as
// spent.
//
// Not 100: grok reports a percentage of an allowance it has already declined to
// serve against, and a limit that fires at 99.7% would otherwise read as
// unconfirmed. The threshold only decides whether an *already classified* limit
// gets a supporting sentence, so erring low costs a slightly generous summary
// and erring high costs the whole feature on rounding.
const creditExhaustionPercent = 99.0

// exhaustedCredits summarises what billing says is spent, or "" when it reports
// nothing exhausted.
func exhaustedCredits(res billingResponse) string {
	cfg := res.Config
	if cfg == nil {
		return ""
	}

	var parts []string
	switch {
	case cfg.CreditUsagePercent != nil && *cfg.CreditUsagePercent >= creditExhaustionPercent:
		parts = append(parts, fmt.Sprintf("included credits %.1f%% used", *cfg.CreditUsagePercent))
	case cfg.CreditUsagePercent == nil && cfg.MonthlyLimit != nil && cfg.Used != nil &&
		cfg.MonthlyLimit.Val > 0 && cfg.Used.Val >= cfg.MonthlyLimit.Val:
		// The deprecated shape, used only when the preferred field is absent.
		parts = append(parts, fmt.Sprintf("included credits %s of %s used",
			formatCents(cfg.Used), formatCents(cfg.MonthlyLimit)))
	}

	// An on-demand cap that is reached is its own wall, independent of the
	// included allowance.
	if cfg.OnDemandCap != nil && cfg.OnDemandUsed != nil &&
		cfg.OnDemandCap.Val > 0 && cfg.OnDemandUsed.Val >= cfg.OnDemandCap.Val {
		parts = append(parts, fmt.Sprintf("on-demand cap %s reached", formatCents(cfg.OnDemandCap)))
	}

	if len(parts) == 0 {
		return ""
	}
	if p := billingPeriod(cfg); p != "" {
		parts = append(parts, p)
	}
	if tier := strings.TrimSpace(res.SubscriptionTier); tier != "" {
		parts = append(parts, "plan "+tier)
	}
	return strings.Join(parts, "; ")
}

// annotateLimit appends the structured summary to a classified error message.
func annotateLimit(message, summary string) string {
	if summary == "" {
		return message
	}
	return fmt.Sprintf("%s (plan usage: %s)", strings.TrimRight(message, " ."), summary)
}
