package acpagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// grok's runtime surface, transcribed from
// ~/gitrepos/grok-build/crates/codegen/xai-grok-shell/src/extensions/usage.rs
// and .../extensions/billing.rs (MADR 0138 Phase 10).
//
// Two methods answering two different questions, with two different costs:
//
//	_x.ai/session/usage  one session, from an in-process ledger. No auth, no
//	                     network, no model tokens.
//	_x.ai/billing        the account, by HTTP to grok's CLI chat proxy using the
//	                     operator's credentials, behind a grok.com auth gate.
//
// MADR 0138 F9 named `x.ai/limit` for the second of these. No such method
// exists — it is a `_meta` page-size key for session listing — and
// `x.ai/session/usage` was not named at all. See the F9 amendment.

var _ provider.RuntimeSession = (*session)(nil)

// usdTicksPerUSD is grok's cost unit on the ACP wire: 1e10 ticks to the dollar
// (`USD_TICKS_PER_USD`, notification.rs). Not to be confused with the billing
// API's [cent], which is a different unit in the other method's response.
const usdTicksPerUSD = 10_000_000_000

// sessionUsageRequest is `_x.ai/session/usage`. camelCase
// (`SessionUsageRequest`, `rename_all = "camelCase"`).
type sessionUsageRequest struct {
	SessionID string `json:"sessionId"`
}

type sessionUsageResponse struct {
	Usage promptUsage `json:"usage"`
}

// promptUsage is grok's `PromptUsage`. Its `totals` field is `#[serde(flatten)]`,
// so every token count sits at the top level of `usage` beside `numTurns` —
// there is no `totals` object on the wire despite the Rust field's name.
type promptUsage struct {
	promptUsageModel
	ModelUsage map[string]promptUsageModel `json:"modelUsage"`
	NumTurns   uint64                      `json:"numTurns"`
	// UsageIsIncomplete means the bill may under-count: subagents still open,
	// usage not applied, or a drain timeout.
	UsageIsIncomplete bool `json:"usageIsIncomplete"`
}

type promptUsageModel struct {
	// InputTokens is the full prompt including cache reads, not the uncached
	// share. The headless shape projects it the other way; this is the ACP one.
	InputTokens         uint64 `json:"inputTokens"`
	OutputTokens        uint64 `json:"outputTokens"`
	TotalTokens         uint64 `json:"totalTokens"`
	CachedReadTokens    uint64 `json:"cachedReadTokens"`
	CacheCreationTokens uint64 `json:"cacheCreationTokens"`
	ReasoningTokens     uint64 `json:"reasoningTokens"`
	ModelCalls          uint64 `json:"modelCalls"`
	APIDurationMS       uint64 `json:"apiDurationMs"`
	// CostUsdTicks is a pointer because absent is not zero: grok omits it when
	// the bill was scrubbed or partial. Reporting a scrubbed bill as $0.00
	// would be a wrong number rather than a missing one.
	CostUsdTicks *int64 `json:"costUsdTicks"`
	// CostIsPartial means "do not trust this row's cost".
	CostIsPartial bool `json:"costIsPartial"`
}

// RuntimeUsage implements [provider.RuntimeSession] over grok's session ledger.
//
// This is cumulative and includes folded subagent spend, which is what makes it
// worth a call: the per-turn `turn_completed` notification this package already
// consumes (xaiusage.go) reports one turn's tokens and never aggregates.
//
// Errors are returned as text with a nil error, following codex
// (internal/provider/codex/runtime.go). internal/session's cmdRuntime
// propagates a returned error instead of displaying it, so an error here makes
// `/usage` fail silently rather than explain itself.
func (s *session) RuntimeUsage(ctx context.Context) (string, error) {
	agentID := s.AgentSessionID()
	if agentID == "" {
		return "Grok usage is unavailable: this session has no agent session id.", nil
	}

	var res sessionUsageResponse
	if err := callAgentExtension(ctx, s, "x.ai/session/usage",
		sessionUsageRequest{SessionID: agentID}, &res); err != nil {
		return "Grok usage is unavailable: " + unavailableReason(err), nil
	}
	return formatSessionUsage(res.Usage), nil
}

// formatSessionUsage renders the ledger, saying what it does not know.
func formatSessionUsage(u promptUsage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Usage: %d input + %d output tokens", u.InputTokens, u.OutputTokens)
	if u.CachedReadTokens > 0 {
		fmt.Fprintf(&b, " (%d cached)", u.CachedReadTokens)
	}
	if u.ReasoningTokens > 0 {
		fmt.Fprintf(&b, " · %d reasoning", u.ReasoningTokens)
	}

	turns := "turns"
	if u.NumTurns == 1 {
		turns = "turn"
	}
	fmt.Fprintf(&b, " · %d %s", u.NumTurns, turns)

	if n := len(u.ModelUsage); n > 1 {
		fmt.Fprintf(&b, " · %d models", n)
	}

	// Three distinct states, and only one of them is a number. An absent cost
	// on a session that spent money must not read as free.
	switch {
	case u.CostUsdTicks == nil:
		b.WriteString(" · cost unavailable")
	case u.CostIsPartial:
		fmt.Fprintf(&b, " · at least %s (partial)", formatUSDTicks(*u.CostUsdTicks))
	default:
		fmt.Fprintf(&b, " · %s", formatUSDTicks(*u.CostUsdTicks))
	}

	if u.UsageIsIncomplete {
		b.WriteString(" · totals may under-count (subagents still reporting)")
	}
	return b.String()
}

// formatUSDTicks renders grok's 1e10-ticks-per-dollar integer.
//
// Four decimals below a dollar: a turn commonly costs fractions of a cent, and
// two decimals would render most real sessions as "$0.00" — the same wrong
// number this code goes out of its way to avoid for an absent cost.
func formatUSDTicks(ticks int64) string {
	usd := float64(ticks) / usdTicksPerUSD
	if usd >= 1 || usd <= -1 {
		return fmt.Sprintf("$%.2f", usd)
	}
	return fmt.Sprintf("$%.4f", usd)
}

// cent is the billing API's money unit — USD **cents**, not the ticks used by
// session usage. proto3 JSON omits zero-valued scalars, so `$0` arrives as `{}`
// and Val is left at its zero value, which is the right answer.
type cent struct {
	Val int64 `json:"val"`
}

// billingResponse is `_x.ai/billing`.
//
// The casing here is mixed and it is not a transcription slip:
// `BillingConfigResponse` carries no `rename_all`, so its own fields are
// snake_case, while the `BillingConfig` nested inside it is
// `rename_all = "camelCase"`, and the `Cent` inside that is snake_case again.
// Three levels, alternating.
type billingResponse struct {
	Config          *billingConfig `json:"config"`
	OnDemandEnabled *bool          `json:"on_demand_enabled"`
	// SubscriptionTier is the friendly plan name, e.g. "SuperGrok Heavy".
	SubscriptionTier string `json:"subscription_tier"`
}

type billingConfig struct {
	// CreditUsagePercent is 0..100 of the included allowance. grok's own
	// comment says to prefer it over deriving from MonthlyLimit/Used, and it
	// is a pointer because 0% and "not reported" are different answers.
	CreditUsagePercent *float64     `json:"creditUsagePercent"`
	CurrentPeriod      *usagePeriod `json:"currentPeriod"`
	// MonthlyLimit and Used are deprecated in grok's own words; kept as the
	// fallback for a server that does not send CreditUsagePercent.
	MonthlyLimit       *cent  `json:"monthlyLimit"`
	Used               *cent  `json:"used"`
	OnDemandCap        *cent  `json:"onDemandCap"`
	OnDemandUsed       *cent  `json:"onDemandUsed"`
	PrepaidBalance     *cent  `json:"prepaidBalance"`
	BillingPeriodStart string `json:"billingPeriodStart"`
	BillingPeriodEnd   string `json:"billingPeriodEnd"`
	// IsUnifiedBillingUser and History are carried but never displayed. They
	// are part of the documented response, and a struct that omits them cannot
	// be strict-decoded against a live grok — which is how the live check tells
	// a shape drift from a field it was simply never told about.
	IsUnifiedBillingUser *bool                `json:"isUnifiedBillingUser"`
	History              []billingPeriodUsage `json:"history"`
}

// billingPeriodUsage is one past billing period.
type billingPeriodUsage struct {
	BillingCycle *billingCycle `json:"billingCycle"`
	IncludedUsed *cent         `json:"includedUsed"`
	OnDemandUsed *cent         `json:"onDemandUsed"`
	TotalUsed    *cent         `json:"totalUsed"`
}

type billingCycle struct {
	Year  int `json:"year"`
	Month int `json:"month"`
}

type usagePeriod struct {
	// Type is the proto enum name, e.g. USAGE_PERIOD_TYPE_WEEKLY.
	Type  string `json:"type"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// RuntimeStatus implements [provider.RuntimeSession] over grok's billing
// config.
//
// Unlike every other extension call in this package, this one leaves the
// machine: grok fetches it over HTTP from its CLI chat proxy using the
// operator's credentials, behind a `require_xai_auth` gate. An install
// authenticated by API key — which mcremote supports and lists in
// SafeAuthMethodIDs — can never satisfy that gate, so "unavailable" is an
// ordinary outcome here rather than a fault, and is reported as text.
func (s *session) RuntimeStatus(ctx context.Context) (string, error) {
	var res billingResponse
	// The handler takes no params; an empty object is what grok's own clients
	// send.
	if err := callAgentExtension(ctx, s, "x.ai/billing", map[string]any{}, &res); err != nil {
		return "Grok billing is unavailable: " + unavailableReason(err), nil
	}
	return formatBilling(res), nil
}

// formatBilling renders the account line.
func formatBilling(res billingResponse) string {
	var parts []string
	if tier := strings.TrimSpace(res.SubscriptionTier); tier != "" {
		parts = append(parts, "plan "+tier)
	}

	cfg := res.Config
	if cfg == nil {
		if len(parts) == 0 {
			return "Grok billing reported no configuration."
		}
		return "Grok · " + strings.Join(parts, " · ") + " · no credit configuration reported"
	}

	switch {
	case cfg.CreditUsagePercent != nil:
		parts = append(parts, fmt.Sprintf("credits %.1f%% used", *cfg.CreditUsagePercent))
	case cfg.MonthlyLimit != nil && cfg.Used != nil && cfg.MonthlyLimit.Val > 0:
		// The deprecated shape, and only when the limit is non-zero: a zero
		// limit would make the percentage a division by nothing rather than
		// "nothing used".
		parts = append(parts, fmt.Sprintf("credits %s of %s used",
			formatCents(cfg.Used), formatCents(cfg.MonthlyLimit)))
	}

	if cfg.PrepaidBalance != nil && cfg.PrepaidBalance.Val > 0 {
		parts = append(parts, "prepaid "+formatCents(cfg.PrepaidBalance))
	}
	if cfg.OnDemandCap != nil && cfg.OnDemandCap.Val > 0 {
		used := ""
		if cfg.OnDemandUsed != nil {
			used = formatCents(cfg.OnDemandUsed) + " of "
		}
		parts = append(parts, "on-demand "+used+formatCents(cfg.OnDemandCap))
	} else if res.OnDemandEnabled != nil && !*res.OnDemandEnabled {
		parts = append(parts, "on-demand off")
	}

	if p := billingPeriod(cfg); p != "" {
		parts = append(parts, p)
	}

	if len(parts) == 0 {
		return "Grok billing reported no usable figures."
	}
	return "Grok · " + strings.Join(parts, " · ")
}

// billingPeriod prefers the current-period object over the two deprecated
// timestamp fields, as grok's own comments direct.
func billingPeriod(cfg *billingConfig) string {
	start, end := cfg.BillingPeriodStart, cfg.BillingPeriodEnd
	if cfg.CurrentPeriod != nil {
		start, end = cfg.CurrentPeriod.Start, cfg.CurrentPeriod.End
	}
	start, end = billingDate(start), billingDate(end)
	if end == "" {
		return ""
	}
	if start == "" {
		return "period ends " + end
	}
	return "period " + start + " to " + end
}

// billingDate shortens a billing timestamp to its calendar date.
//
// grok sends full RFC 3339 with microseconds — measured live on 1.0.13:
// "2026-09-02T11:15:25.027200+00:00". Two of those in a one-line status notice
// is 60 characters of precision nobody asked a billing period for. The raw
// value is kept when it does not parse, rather than dropped: an unexpected
// format is still information.
func billingDate(ts string) string {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t.UTC().Format("2006-01-02")
	}
	return ts
}

// formatCents renders the billing API's USD cents.
func formatCents(c *cent) string {
	if c == nil {
		return "$0.00"
	}
	return fmt.Sprintf("$%.2f", float64(c.Val)/100)
}

// unavailableReason turns an extension-call failure into one clause a person
// can act on, keeping the three cases distinct: a build without the method, an
// account that cannot use it, and everything else.
func unavailableReason(err error) string {
	switch {
	case errors.Is(err, provider.ErrNotImplemented):
		return "this grok build does not have the method."
	case isAuthRequired(err):
		return "it requires grok.com authentication (run `grok login`); " +
			"an API-key install cannot read it."
	default:
		return sanitizeUserFacingErr(err)
	}
}

// isAuthRequired reports whether err is ACP's -32000 "Authentication required",
// which is what grok's require_xai_auth gate returns.
func isAuthRequired(err error) bool {
	var re *acp.RequestError
	if errors.As(err, &re) {
		return re.Code == -32000
	}
	return false
}
