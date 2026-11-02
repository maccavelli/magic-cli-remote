package grok

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	acp "github.com/maccavelli/acp-go-sdk2"
	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// grok's session operations beyond standard ACP, on acpagent.Host (MADR 0208 D2).

// forkRequest is the pinned grok 1.0.4 _x.ai/session/fork body
// (MADR 0092 P1.1). newCwd == sourceCwd == the daemon session cwd.
func forkRequest(agentID, cwd string) map[string]any {
	return map[string]any{
		"sourceSessionId": agentID,
		"sourceCwd":       cwd,
		"newCwd":          cwd,
	}
}

// forkSession is grok's Ops.Fork, via _x.ai/session/fork. LastTurnID is ignored:
// grok has no turn-boundary field.
func forkSession(ctx context.Context, h acpagent.Host, _ provider.ForkOptions) (provider.ForkResult, error) {
	agentID, cwd := h.AgentSessionID(), h.CWD()
	if agentID == "" || cwd == "" {
		return provider.ForkResult{}, fmt.Errorf("fork: missing session id or cwd")
	}
	var resp struct {
		NewSessionID    string `json:"newSessionId"`
		ParentSessionID string `json:"parentSessionId"`
	}
	if err := h.RawRequest(ctx, "_x.ai/session/fork", forkRequest(agentID, cwd), &resp); err != nil {
		return provider.ForkResult{}, err
	}
	if resp.NewSessionID == "" {
		return provider.ForkResult{}, fmt.Errorf("fork: empty newSessionId")
	}
	parent := resp.ParentSessionID
	if parent == "" {
		parent = agentID
	}
	return provider.ForkResult{AgentSessionID: resp.NewSessionID, ForkedFromID: parent}, nil
}

// grok's rewind surface, transcribed from
// ~/gitrepos/grok-build/crates/codegen/xai-grok-shell/src/session/acp_types.rs
// and .../extensions/rewind.rs (MADR 0138 Phase 9).
//
// The response types carry **no** `#[serde(rename_all)]`, so they arrive in
// snake_case — unlike `_x.ai/session/fork`, whose response on the same
// transport is camelCase. Two vendor methods, two conventions; assuming either
// one produces a struct full of zero values and no error.

// rewindModeAll rolls back the conversation *and* the files.
//
// It is sent explicitly on every call, never omitted. `RewindRequest.mode`
// carries the comment "Clients must specify this explicitly. Defaults to `All`
// for backwards compatibility with older clients" — so omitting it happens to
// mean the same thing today and would silently change meaning if grok ever
// moved the default.
//
// `All` is also what `/undo` already means elsewhere: kilo's UndoLast reverts
// the last user message through an endpoint its engine documents as "undoing
// its effects and restoring the previous state". One button, one meaning.
const rewindModeAll = "all"

type rewindPointsRequest struct {
	SessionID string `json:"sessionId"`
}

type rewindPointsResponse struct {
	RewindPoints []rewindPoint `json:"rewind_points"`
}

type rewindPoint struct {
	PromptIndex    int    `json:"prompt_index"`
	CreatedAt      string `json:"created_at"`
	NumFileSnaps   int    `json:"num_file_snapshots"`
	HasFileChanges bool   `json:"has_file_changes"`
	PromptPreview  string `json:"prompt_preview"`
}

type rewindExecuteRequest struct {
	SessionID         string `json:"sessionId"`
	TargetPromptIndex int    `json:"targetPromptIndex"`
	// Force is always false. A conflict means the working tree diverged from
	// the snapshot — the operator edited files since the turn ran — and forcing
	// discards those edits. The conflicts are reported instead.
	Force bool   `json:"force"`
	Mode  string `json:"mode"`
}

type rewindExecuteResponse struct {
	Success           bool             `json:"success"`
	TargetPromptIndex int              `json:"target_prompt_index"`
	Mode              string           `json:"mode"`
	RevertedFiles     []string         `json:"reverted_files"`
	CleanFiles        []string         `json:"clean_files"`
	Conflicts         []rewindConflict `json:"conflicts"`
	PromptText        string           `json:"prompt_text"`
	Error             string           `json:"error"`
}

type rewindConflict struct {
	Path string `json:"path"`
	// Kind is one of missing_file, extra_file, content_mismatch.
	Kind string `json:"conflict_type"`
}

// undoLast is grok's Ops.UndoLast, over its rewind extension.
//
// UndoSession and not RevertSession: Revert takes a provider-native message id,
// and grok emits none — its rewind is indexed by prompt position. UndoSession
// is defined as resolving "the last turn" itself, which is what the two calls
// below do.
//
// There is deliberately no Unrevert. grok has no un-rewind, so `/redo` stays
// unavailable on it rather than being wired to something that only resembles
// one.
func undoLast(ctx context.Context, h acpagent.Host) (string, error) {
	agentID := h.AgentSessionID()
	if agentID == "" {
		return "", errors.New("session has no agent session id")
	}

	var points rewindPointsResponse
	if err := h.CallExtension(ctx, "_x.ai/rewind/points",
		rewindPointsRequest{SessionID: agentID}, &points); err != nil {
		return "", err
	}
	target, ok := lastRewindPoint(points.RewindPoints)
	if !ok {
		// Same wording as kilo's for the same state, so the phone shows one
		// message regardless of provider.
		return "", errors.New("nothing to undo in this session")
	}

	var res rewindExecuteResponse
	if err := h.CallExtension(ctx, "_x.ai/rewind/execute", rewindExecuteRequest{
		SessionID:         agentID,
		TargetPromptIndex: target.PromptIndex,
		Force:             false,
		Mode:              rewindModeAll,
	}, &res); err != nil {
		return "", err
	}
	if !res.Success {
		return "", rewindFailure(res)
	}
	return rewindSummary(res, target), nil
}

// lastRewindPoint returns the most recent checkpoint.
//
// By highest PromptIndex rather than by position: the field is documented as
// monotonically increasing per session, which is a property worth relying on
// explicitly instead of assuming the array is ordered.
func lastRewindPoint(points []rewindPoint) (rewindPoint, bool) {
	var best rewindPoint
	found := false
	for _, p := range points {
		if !found || p.PromptIndex > best.PromptIndex {
			best, found = p, true
		}
	}
	return best, found
}

// rewindFailure turns an unsuccessful rewind into an error the operator can act
// on.
//
// A conflict list is the useful case: it names the files that changed since the
// snapshot, which is what the operator has to reconcile before an undo can be
// safe. Nothing was reverted.
func rewindFailure(res rewindExecuteResponse) error {
	if len(res.Conflicts) > 0 {
		paths := make([]string, 0, len(res.Conflicts))
		for _, c := range res.Conflicts {
			if c.Kind != "" {
				paths = append(paths, fmt.Sprintf("%s (%s)", c.Path, c.Kind))
				continue
			}
			paths = append(paths, c.Path)
		}
		return fmt.Errorf("undo would conflict with changes made since that turn; "+
			"nothing was reverted: %s", strings.Join(paths, ", "))
	}
	if msg := strings.TrimSpace(res.Error); msg != "" {
		return errors.New(msg)
	}
	return errors.New("undo failed")
}

// rewindSummary says what was undone, not merely that something was.
func rewindSummary(res rewindExecuteResponse, target rewindPoint) string {
	var b strings.Builder
	switch n := len(res.RevertedFiles); {
	case n == 1:
		b.WriteString("Undid the last turn and reverted 1 file")
	case n > 1:
		fmt.Fprintf(&b, "Undid the last turn and reverted %d files", n)
	default:
		b.WriteString("Undid the last turn; no files had changed")
	}
	if preview := strings.TrimSpace(target.PromptPreview); preview != "" {
		fmt.Fprintf(&b, " (%q)", truncateRunes(preview, 80))
	}
	return b.String()
}

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
//
// `/usage` and `/status` pass both answers through as grok sent them (MADR 0208
// D12). Only the quota check (confirmLimit) still decodes billing.

// usdTicksPerUSD is grok's cost unit on the ACP wire: 1e10 ticks to the dollar
// (`USD_TICKS_PER_USD`, notification.rs). Not to be confused with the billing
// API's [cent], which is a different unit in the other method's response.
const usdTicksPerUSD = 10_000_000_000

// sessionUsageRequest is `_x.ai/session/usage`. camelCase
// (`SessionUsageRequest`, `rename_all = "camelCase"`).
type sessionUsageRequest struct {
	SessionID string `json:"sessionId"`
}

// runtimeUsage is grok's Ops.RuntimeUsage, over its session ledger.
//
// This is cumulative and includes folded subagent spend, which is what makes it
// worth a call: the per-turn `turn_completed` notification this package already
// consumes (xaiusage.go) reports one turn's tokens and never aggregates.
//
// Errors are returned as text with a nil error, following codex
// (internal/provider/codex/runtime.go). internal/session's cmdRuntime
// propagates a returned error instead of displaying it, so an error here makes
// `/usage` fail silently rather than explain itself.
func runtimeUsage(ctx context.Context, h acpagent.Host) (provider.Result, error) {
	agentID := h.AgentSessionID()
	if agentID == "" {
		return provider.Result{Text: "Grok usage is unavailable: this session has no agent session id."}, nil
	}
	return extensionResult(ctx, h, "_x.ai/session/usage",
		sessionUsageRequest{SessionID: agentID}, "Grok usage"), nil
}

// runtimeStatus is grok's Ops.RuntimeStatus, over its billing config.
//
// Unlike every other extension call in this package, this one leaves the
// machine: grok fetches it over HTTP from its CLI chat proxy using the
// operator's credentials, behind a `require_xai_auth` gate. An install
// authenticated by API key — which mcremote supports and lists in
// SafeAuthMethodIDs — can never satisfy that gate, so "unavailable" is an
// ordinary outcome here rather than a fault, and is reported as text.
func runtimeStatus(ctx context.Context, h acpagent.Host) (provider.Result, error) {
	// The handler takes no params; an empty object is what grok's own clients
	// send.
	return extensionResult(ctx, h, "_x.ai/billing", map[string]any{}, "Grok billing"), nil
}

// extensionResult calls one grok extension and returns its response body
// exactly as grok sent it. A call that fails comes back as one line saying why,
// named by what.
func extensionResult(ctx context.Context, h acpagent.Host, method string, params any, what string) provider.Result {
	var body json.RawMessage
	if err := h.CallExtension(ctx, method, params, &body); err != nil {
		return provider.Result{Text: what + " is unavailable: " + unavailableReason(err)}
	}
	if len(body) == 0 {
		return provider.Result{Text: what + " returned no answer."}
	}
	return provider.Result{Method: method, Body: body}
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
// "2026-09-02T11:15:25.027200+00:00". Two of those in a one-line limit notice
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
	case acp.IsAuthRequired(err):
		return "it requires grok.com authentication (run `grok login`); " +
			"an API-key install cannot read it."
	default:
		return sanitizeUserFacingErr(err)
	}
}

// compactSession is grok's Ops.Compact, over `x.ai/compact_conversation`.
func compactSession(ctx context.Context, h acpagent.Host) error {
	agentID := h.AgentSessionID()
	if agentID == "" {
		return errors.New("session has no agent session id")
	}
	return h.CallExtension(ctx, "_x.ai/compact_conversation", map[string]any{
		"sessionId": agentID,
	}, nil)
}

// renameSession is grok's Ops.Rename, over `x.ai/session/rename`.
func renameSession(ctx context.Context, h acpagent.Host, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("title is empty")
	}
	agentID := h.AgentSessionID()
	if agentID == "" {
		return errors.New("session has no agent session id")
	}
	return h.CallExtension(ctx, "_x.ai/session/rename", map[string]any{
		"sessionId": agentID,
		"title":     title,
	}, nil)
}

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
func confirmLimit(ctx context.Context, h acpagent.Host, kind agenterr.Kind) (summary string, confirmed bool) {
	if kind != agenterr.KindQuota {
		return "", false
	}
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), quotaProbeTimeout)
	defer cancel()

	var res billingResponse
	if err := h.CallExtension(callCtx, "_x.ai/billing", map[string]any{}, &res); err != nil {
		// Unreachable is not the same as unconfirmed: say so, and never treat a
		// failed probe as evidence that the prose was wrong. An API-key install
		// takes this path on every limit, by design.
		h.Log().Debug("grok billing probe failed",
			slog.String("agent_session_id", h.AgentSessionID()),
			slog.String("err", err.Error()))
		return "", false
	}
	if out := exhaustedCredits(res); out != "" {
		return out, true
	}

	h.Log().Warn(
		"engine reported a limit in prose that its billing API does not confirm",
		slog.String("agent_session_id", h.AgentSessionID()),
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

// truncateRunes cuts s to at most max bytes on a rune boundary, marking the
// cut with an ellipsis. It is acpagent's, which keeps it private.
func truncateRunes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// sanitizeUserFacingErr keeps an error short enough to show: its first line,
// at most 400 bytes. It is acpagent's, which keeps it private.
func sanitizeUserFacingErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if i := strings.Index(msg, "\n"); i > 0 && i < 200 {
		msg = msg[:i] + "…"
	}
	return truncateRunes(msg, 400)
}
