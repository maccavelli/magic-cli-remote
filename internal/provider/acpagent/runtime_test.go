package acpagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"github.com/maccavelli/magic-cli-remote/internal/agenterr"
	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// liveBillingPayload is the response shape transcribed from
// extensions/billing.rs. Its casing is the point: the outer object is
// snake_case (BillingConfigResponse has no rename_all), the `config` inside it
// is camelCase (BillingConfig is rename_all = "camelCase"), and the amounts
// inside that are snake_case again (Cent has none).
const liveBillingPayload = `{
	"config": {
		"creditUsagePercent": 99.4,
		"currentPeriod": {"type":"USAGE_PERIOD_TYPE_WEEKLY","start":"2026-09-01","end":"2026-09-08"},
		"monthlyLimit": {"val": 2000},
		"used": {"val": 1988},
		"onDemandCap": {},
		"prepaidBalance": {"val": 500},
		"isUnifiedBillingUser": false,
		"history": []
	},
	"on_demand_enabled": true,
	"subscription_tier": "SuperGrok Heavy"
}`

// TestBillingDecodesBothCasingsInOneResponse is P1 and P2 together.
//
// Guessing one convention for the whole response fails silently: the fields
// under the wrong guess decode to zero values and json.Unmarshal returns nil.
// Both halves are asserted here because pinning one leaves the other a guess.
func TestBillingDecodesBothCasingsInOneResponse(t *testing.T) {
	var res billingResponse
	if err := json.Unmarshal([]byte(liveBillingPayload), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The outer half: snake_case.
	if res.Config == nil {
		t.Fatal("`config` did not decode — the outer object is snake_case " +
			"(BillingConfigResponse carries no rename_all)")
	}
	if res.SubscriptionTier != "SuperGrok Heavy" {
		t.Errorf("subscription_tier = %q, want the snake_case key to decode", res.SubscriptionTier)
	}
	if res.OnDemandEnabled == nil || !*res.OnDemandEnabled {
		t.Error("on_demand_enabled did not decode")
	}

	// The inner half: camelCase.
	if res.Config.CreditUsagePercent == nil {
		t.Fatal("`creditUsagePercent` did not decode — the nested config is camelCase " +
			"(BillingConfig is rename_all = \"camelCase\")")
	}
	if got := *res.Config.CreditUsagePercent; got != 99.4 {
		t.Errorf("creditUsagePercent = %v, want 99.4", got)
	}
	if res.Config.MonthlyLimit == nil || res.Config.MonthlyLimit.Val != 2000 {
		t.Errorf("monthlyLimit = %+v, want val 2000", res.Config.MonthlyLimit)
	}
	if res.Config.CurrentPeriod == nil || res.Config.CurrentPeriod.End != "2026-09-08" {
		t.Errorf("currentPeriod did not decode: %+v", res.Config.CurrentPeriod)
	}
}

// TestAZeroCentArrivesAsAnEmptyObject is half of P3.
//
// proto3 JSON omits zero-valued scalars, so `$0` is `{}` and not `{"val":0}`.
// The distinction that matters is between an amount that is present and zero
// and an amount that is absent entirely.
func TestAZeroCentArrivesAsAnEmptyObject(t *testing.T) {
	var res billingResponse
	if err := json.Unmarshal([]byte(liveBillingPayload), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Config.OnDemandCap == nil {
		t.Fatal("`onDemandCap: {}` decoded as absent; it is present and zero")
	}
	if res.Config.OnDemandCap.Val != 0 {
		t.Errorf("onDemandCap.val = %d, want 0", res.Config.OnDemandCap.Val)
	}
	if got := formatCents(res.Config.OnDemandCap); got != "$0.00" {
		t.Errorf("formatCents = %q, want $0.00", got)
	}
	// A nil Cent is a different thing and must not panic.
	if got := formatCents(nil); got != "$0.00" {
		t.Errorf("formatCents(nil) = %q", got)
	}
}

// usagePayload is grok's own wire shape, taken from the assertions in
// extensions/usage.rs (response_serializes_ledger_as_prompt_usage_wire_shape).
// Note that the token counts sit at the top level of `usage`, beside numTurns:
// PromptUsage flattens its `totals`, so there is no `totals` object on the wire.
const usagePayload = `{"usage":{
	"inputTokens":100,"outputTokens":10,"totalTokens":110,
	"cachedReadTokens":40,"cacheCreationTokens":0,"reasoningTokens":25,
	"modelCalls":1,"apiDurationMs":900,
	"numTurns":1,"costUsdTicks":20000000,
	"modelUsage":{"grok-build":{"inputTokens":100,"outputTokens":10}}}}`

// TestSessionUsageDecodesFlattenedTotals is P7.
//
// PromptUsage's `totals` is #[serde(flatten)]. A struct that nests them under a
// `totals` key decodes to zeros with no error, and `/usage` then reports a
// session that spent nothing.
func TestSessionUsageDecodesFlattenedTotals(t *testing.T) {
	var res sessionUsageResponse
	if err := json.Unmarshal([]byte(usagePayload), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Usage.InputTokens != 100 || res.Usage.OutputTokens != 10 {
		t.Fatalf("tokens = %d in / %d out, want 100/10 — the totals are flattened into "+
			"`usage`, not nested under a `totals` key",
			res.Usage.InputTokens, res.Usage.OutputTokens)
	}
	if res.Usage.NumTurns != 1 {
		t.Errorf("numTurns = %d, want 1", res.Usage.NumTurns)
	}
	if res.Usage.CachedReadTokens != 40 || res.Usage.ReasoningTokens != 25 {
		t.Errorf("cached = %d, reasoning = %d, want 40/25",
			res.Usage.CachedReadTokens, res.Usage.ReasoningTokens)
	}
	if res.Usage.CostUsdTicks == nil || *res.Usage.CostUsdTicks != 20_000_000 {
		t.Errorf("costUsdTicks = %v, want 20000000", res.Usage.CostUsdTicks)
	}
	if n := len(res.Usage.ModelUsage); n != 1 {
		t.Errorf("modelUsage has %d entries, want 1", n)
	}
}

// TestUsageNeverReportsAnAbsentCostAsFree is the other half of P3.
//
// grok omits costUsdTicks when the bill was scrubbed or partial. Formatting an
// absent cost as $0.00 reports a session that spent money as free, which is a
// wrong number rather than a missing one.
func TestUsageNeverReportsAnAbsentCostAsFree(t *testing.T) {
	base := promptUsage{
		promptUsageModel: promptUsageModel{InputTokens: 900, OutputTokens: 100},
		NumTurns:         3,
	}

	absent := formatSessionUsage(base)
	if strings.Contains(absent, "$0.00") || strings.Contains(absent, "$0.0000") {
		t.Errorf("summary = %q — an absent cost was rendered as zero", absent)
	}
	if !strings.Contains(absent, "cost unavailable") {
		t.Errorf("summary = %q, want it to say the cost is unavailable", absent)
	}

	ticks := int64(20_000_000) // $0.002
	partial := base
	partial.CostUsdTicks = &ticks
	partial.CostIsPartial = true
	got := formatSessionUsage(partial)
	if !strings.Contains(got, "partial") {
		t.Errorf("summary = %q, want a partial bill marked as such", got)
	}
	if !strings.Contains(got, "at least") {
		t.Errorf("summary = %q, want a partial cost stated as a floor", got)
	}

	complete := base
	complete.CostUsdTicks = &ticks
	got = formatSessionUsage(complete)
	if !strings.Contains(got, "$0.0020") {
		t.Errorf("summary = %q, want $0.0020 — 1e10 ticks to the dollar, and two "+
			"decimals would render a real turn as $0.00", got)
	}

	incomplete := complete
	incomplete.UsageIsIncomplete = true
	if got := formatSessionUsage(incomplete); !strings.Contains(got, "under-count") {
		t.Errorf("summary = %q, want the incomplete-bill warning", got)
	}
}

// TestRuntimeStatusExplainsItselfRatherThanErroring is P4.
//
// internal/session's cmdRuntime returns a non-nil error to its caller instead
// of showing it, so an error here makes /status fail silently. An API-key
// install cannot satisfy grok's require_xai_auth gate and takes this path on
// every call, which makes it an ordinary outcome rather than a fault.
func TestRuntimeStatusExplainsItselfRatherThanErroring(t *testing.T) {
	// No scripted reply: the agent answers -32601, the shape an older build
	// gives.
	s, _ := newScriptedSession(t, map[string]string{})

	msg, err := s.RuntimeStatus(context.Background())
	if err != nil {
		t.Fatalf("RuntimeStatus returned an error (%v); cmdRuntime would swallow it and /status "+
			"would say nothing at all", err)
	}
	if !strings.Contains(msg, "unavailable") {
		t.Errorf("message = %q, want it to say billing is unavailable", msg)
	}

	usage, err := s.RuntimeUsage(context.Background())
	if err != nil {
		t.Fatalf("RuntimeUsage returned an error: %v", err)
	}
	if !strings.Contains(usage, "unavailable") {
		t.Errorf("message = %q, want it to say usage is unavailable", usage)
	}
}

// TestUnavailableReasonNamesTheAuthGate keeps the three failure cases distinct:
// a build without the method, an account that cannot use it, and everything
// else. They call for different actions, so one message for all three would be
// worse than none.
func TestUnavailableReasonNamesTheAuthGate(t *testing.T) {
	// -32000 is ACP's "Authentication required", which is what grok's
	// require_xai_auth gate returns (extensions/auth_gate.rs), and it is what
	// an API-key install gets from x.ai/billing every time.
	authErr := error(acp.NewAuthRequired(
		"Billing data requires auth with grok.com. Run `grok login` to authenticate."))
	if !isAuthRequired(authErr) {
		t.Fatal("a -32000 response was not recognised as an auth requirement")
	}
	if got := unavailableReason(authErr); !strings.Contains(got, "grok login") {
		t.Errorf("reason = %q, want it to name the command that fixes it", got)
	}
	if isAuthRequired(provider.ErrNotImplemented) {
		t.Error("a missing method was read as an auth failure; they need different advice")
	}
	if got := unavailableReason(provider.ErrNotImplemented); !strings.Contains(got, "build") {
		t.Errorf("reason = %q, want it to blame the build, not the account", got)
	}
}

// TestBillingSummaryOnlyFiresWhenCreditsAreActuallySpent covers exhaustedCredits.
func TestBillingSummaryOnlyFiresWhenCreditsAreActuallySpent(t *testing.T) {
	var spent billingResponse
	if err := json.Unmarshal([]byte(liveBillingPayload), &spent); err != nil {
		t.Fatal(err)
	}
	got := exhaustedCredits(spent)
	if got == "" {
		t.Fatal("99.4% used did not read as exhausted")
	}
	if !strings.Contains(got, "SuperGrok Heavy") {
		t.Errorf("summary = %q, want the plan named", got)
	}

	// Well under the allowance: nothing to confirm.
	pct := 12.0
	healthy := billingResponse{Config: &billingConfig{CreditUsagePercent: &pct}}
	if got := exhaustedCredits(healthy); got != "" {
		t.Errorf("12%% used reported as exhausted: %q", got)
	}

	// An on-demand cap reached is its own wall.
	cap0 := billingResponse{Config: &billingConfig{
		CreditUsagePercent: &pct,
		OnDemandCap:        &cent{Val: 5000},
		OnDemandUsed:       &cent{Val: 5000},
	}}
	if got := exhaustedCredits(cap0); !strings.Contains(got, "on-demand cap") {
		t.Errorf("summary = %q, want the on-demand cap reported", got)
	}

	if got := exhaustedCredits(billingResponse{}); got != "" {
		t.Errorf("a response with no config reported %q", got)
	}
}

// TestConfirmLimitIsNotAskedAboutRateLimits is P5.
//
// grok's billing reports credits, a cap and a period. It says nothing about
// requests per window, so asking it to confirm throttling would produce a
// confident wrong answer. kilo's provider-usage does answer that directly,
// which is why the two implementations differ here.
func TestConfirmLimitIsNotAskedAboutRateLimits(t *testing.T) {
	s, agent := newScriptedSession(t, map[string]string{"_x.ai/billing": liveBillingPayload})

	for _, kind := range []agenterr.Kind{
		agenterr.KindRateLimit, agenterr.KindServer, agenterr.KindAuth, agenterr.KindNone,
	} {
		summary, confirmed := s.confirmLimit(context.Background(), kind)
		if summary != "" || confirmed {
			t.Errorf("kind %q was confirmed against billing (%q)", kind, summary)
		}
	}
	if agent.sent("_x.ai/billing") {
		t.Fatalf("billing was probed for a non-quota classification; the wire carried %v",
			agent.methods())
	}

	// The one kind that is asked.
	summary, confirmed := s.confirmLimit(context.Background(), agenterr.KindQuota)
	if !confirmed || summary == "" {
		t.Fatalf("a quota limit was not confirmed: %q", summary)
	}
	if !agent.sent("_x.ai/billing") {
		t.Error("a quota limit did not probe billing")
	}
}

// TestAnUnreachableProbeDoesNotSuppressTheLimit is P6.
//
// Unreachable is not disproved. An API-key install fails this probe on every
// limit by design, and the classified error must survive that unchanged.
func TestAnUnreachableProbeDoesNotSuppressTheLimit(t *testing.T) {
	// No billing reply scripted: the probe fails with -32601.
	s, _ := newScriptedSession(t, map[string]string{})

	summary, confirmed := s.confirmLimit(context.Background(), agenterr.KindQuota)
	if confirmed || summary != "" {
		t.Fatalf("a failed probe reported confirmation: %q", summary)
	}

	s.emitClassifiedTurnErrorConfirmed("Monthly usage limit reached. Resets in 4 days")

	var errEvent *event.Event
	deadline := time.After(5 * time.Second)
	for errEvent == nil {
		select {
		case ev := <-s.events:
			if ev.Type == event.TypeError {
				e := ev
				errEvent = &e
			}
		case <-deadline:
			t.Fatal("no error event was emitted after an unconfirmed limit")
		}
	}
	if errEvent.Error == "" {
		t.Fatal("the limit was reported with an empty message")
	}
	if strings.Contains(errEvent.Error, "plan usage") {
		t.Errorf("error = %q — an unconfirmed probe added a plan-usage clause", errEvent.Error)
	}
	if errEvent.ErrorKind != string(agenterr.KindQuota) {
		t.Errorf("kind = %q, want quota", errEvent.ErrorKind)
	}
}

// TestTheNonBlockingFunnelNeverProbesBilling pins the Phase 10 deviation.
//
// emitClassifiedTurnError is reached from the stderr copier goroutine and from
// the ACP SDK's notification consumer. Blocking the latter tears the connection
// down in 7.16 ms (MADR 0138 F5, measured in Phase 9's G2), and the billing
// probe is a network round trip bounded at six seconds — four orders of
// magnitude the wrong side of that.
func TestTheNonBlockingFunnelNeverProbesBilling(t *testing.T) {
	s, agent := newScriptedSession(t, map[string]string{"_x.ai/billing": liveBillingPayload})

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.emitClassifiedTurnError("Monthly usage limit reached. Resets in 4 days")
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the non-blocking funnel blocked")
	}

	if agent.sent("_x.ai/billing") {
		t.Fatalf("emitClassifiedTurnError probed billing. It runs on the SDK's notification "+
			"consumer and on the stderr copier; a six-second call there is F5. The wire "+
			"carried %v", agent.methods())
	}
}

// TestAnnotateLimitLeavesAnUnconfirmedMessageAlone.
func TestAnnotateLimitLeavesAnUnconfirmedMessageAlone(t *testing.T) {
	const msg = "Monthly usage limit reached."
	if got := annotateLimit(msg, ""); got != msg {
		t.Errorf("annotateLimit with no summary changed the message: %q", got)
	}
	got := annotateLimit(msg, "included credits 99.4% used")
	if !strings.Contains(got, "plan usage") || !strings.Contains(got, "99.4") {
		t.Errorf("annotateLimit = %q", got)
	}
}

// TestGrokImplementsRuntimeSession is acceptance 1 and 2's precondition.
func TestGrokImplementsRuntimeSession(t *testing.T) {
	var s any = &session{}
	if _, ok := s.(provider.RuntimeSession); !ok {
		t.Fatal("the grok session does not implement provider.RuntimeSession; " +
			"/status and /usage would stay unavailable")
	}
}

// TestRuntimeUsageReadsTheLedger is acceptance 1 over the wire.
func TestRuntimeUsageReadsTheLedger(t *testing.T) {
	s, agent := newScriptedSession(t, map[string]string{
		"_x.ai/session/usage": usagePayload,
	})

	msg, err := s.RuntimeUsage(context.Background())
	if err != nil {
		t.Fatalf("RuntimeUsage: %v", err)
	}
	body := agent.paramsFor(t, "_x.ai/session/usage")
	if body["sessionId"] != "agent-1" {
		t.Errorf("sessionId = %v, want agent-1", body["sessionId"])
	}
	for _, want := range []string{"100 input", "10 output", "40 cached", "1 turn", "$0.0020"} {
		if !strings.Contains(msg, want) {
			t.Errorf("usage = %q, want it to contain %q", msg, want)
		}
	}
}

// TestRuntimeStatusReadsBilling is acceptance 2 over the wire.
func TestRuntimeStatusReadsBilling(t *testing.T) {
	s, _ := newScriptedSession(t, map[string]string{"_x.ai/billing": liveBillingPayload})

	msg, err := s.RuntimeStatus(context.Background())
	if err != nil {
		t.Fatalf("RuntimeStatus: %v", err)
	}
	for _, want := range []string{"SuperGrok Heavy", "99.4% used", "prepaid $5.00", "period"} {
		if !strings.Contains(msg, want) {
			t.Errorf("status = %q, want it to contain %q", msg, want)
		}
	}
}

// TestBillingPeriodIsADateNotATimestamp.
//
// grok sends full RFC 3339 with microseconds; the live run on 1.0.13 returned
// "2026-09-02T11:15:25.027200+00:00", and two of those in a one-line notice is
// sixty characters of precision a billing period does not need.
func TestBillingPeriodIsADateNotATimestamp(t *testing.T) {
	cfg := &billingConfig{CurrentPeriod: &usagePeriod{
		Start: "2026-09-02T11:15:25.027200+00:00",
		End:   "2026-09-09T11:15:25.027200+00:00",
	}}
	got := billingPeriod(cfg)
	if got != "period 2026-09-02 to 2026-09-09" {
		t.Errorf("period = %q, want the calendar dates", got)
	}

	// An unparseable value is kept rather than dropped: an unexpected format is
	// still information.
	odd := &billingConfig{CurrentPeriod: &usagePeriod{Start: "", End: "week 37"}}
	if got := billingPeriod(odd); got != "period ends week 37" {
		t.Errorf("period = %q, want the raw value preserved", got)
	}

	// The deprecated pair is the fallback when currentPeriod is absent.
	dep := &billingConfig{
		BillingPeriodStart: "2026-09-02T11:15:25.027200+00:00",
		BillingPeriodEnd:   "2026-09-09T11:15:25.027200+00:00",
	}
	if got := billingPeriod(dep); got != "period 2026-09-02 to 2026-09-09" {
		t.Errorf("deprecated period = %q", got)
	}
	if got := billingPeriod(&billingConfig{}); got != "" {
		t.Errorf("a config with no period reported %q", got)
	}
}
