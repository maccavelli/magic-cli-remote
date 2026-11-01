package acpagent

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	acp "github.com/maccavelli/acp-go-sdk2"
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

	status, err := s.RuntimeStatus(context.Background())
	if err != nil {
		t.Fatalf("RuntimeStatus returned an error (%v); cmdRuntime would swallow it and /status "+
			"would say nothing at all", err)
	}
	if !strings.Contains(status.Text, "unavailable") || len(status.Body) != 0 {
		t.Errorf("status = %+v, want only text saying billing is unavailable", status)
	}

	usage, err := s.RuntimeUsage(context.Background())
	if err != nil {
		t.Fatalf("RuntimeUsage returned an error: %v", err)
	}
	if !strings.Contains(usage.Text, "unavailable") || len(usage.Body) != 0 {
		t.Errorf("usage = %+v, want only text saying usage is unavailable", usage)
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
	if !acp.IsAuthRequired(authErr) {
		t.Fatal("a -32000 response was not recognised as an auth requirement")
	}
	if got := unavailableReason(authErr); !strings.Contains(got, "grok login") {
		t.Errorf("reason = %q, want it to name the command that fixes it", got)
	}
	if acp.IsAuthRequired(provider.ErrNotImplemented) {
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

// capturedResult returns the result of the one frame in grok 1.0.70's capture
// whose result has the top-level key key, exactly as grok sent it.
func capturedResult(t *testing.T, key string) string {
	t.Helper()
	f, err := os.Open("../grok/testdata/wire/1.0.70/frames.jsonl")
	if err != nil {
		t.Fatalf("open capture: %v", err)
	}
	defer func() { _ = f.Close() }()

	var found []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		var frame struct {
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(sc.Bytes(), &frame) != nil || frame.Method != "" || len(frame.Result) == 0 {
			continue
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(frame.Result, &fields) != nil {
			continue
		}
		if _, ok := fields[key]; ok {
			found = append(found, string(frame.Result))
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read capture: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("capture has %d results with a top-level %q, want 1", len(found), key)
	}
	return found[0]
}

// TestUsagePassesGrokAnswerThrough is 0208-PLAN P5 step 3 for `/usage`: the
// result is grok 1.0.70's captured answer, byte for byte, with nothing added.
func TestUsagePassesGrokAnswerThrough(t *testing.T) {
	body := capturedResult(t, "usage")
	s, agent := newScriptedSession(t, map[string]string{"_x.ai/session/usage": body})

	res, err := s.RuntimeUsage(context.Background())
	if err != nil {
		t.Fatalf("RuntimeUsage: %v", err)
	}
	if string(res.Body) != body {
		t.Errorf("body = %s\nwant grok's answer unchanged: %s", res.Body, body)
	}
	if res.Method != "_x.ai/session/usage" || res.Text != "" {
		t.Errorf("method = %q, text = %q; want the wire method and no text", res.Method, res.Text)
	}
	if params := agent.paramsFor(t, "_x.ai/session/usage"); params["sessionId"] != "agent-1" {
		t.Errorf("sessionId = %v, want agent-1", params["sessionId"])
	}
}

// TestStatusPassesGrokBillingThrough is 0208-PLAN P5 step 3 for `/status`.
// The capture's `28.0` is the canary: a decode and re-encode writes `28`.
func TestStatusPassesGrokBillingThrough(t *testing.T) {
	body := capturedResult(t, "subscription_tier")
	s, _ := newScriptedSession(t, map[string]string{"_x.ai/billing": body})

	res, err := s.RuntimeStatus(context.Background())
	if err != nil {
		t.Fatalf("RuntimeStatus: %v", err)
	}
	if string(res.Body) != body {
		t.Errorf("body = %s\nwant grok's answer unchanged: %s", res.Body, body)
	}
	if res.Method != "_x.ai/billing" || res.Text != "" {
		t.Errorf("method = %q, text = %q; want the wire method and no text", res.Method, res.Text)
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
