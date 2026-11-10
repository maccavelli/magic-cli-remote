package agenterr

import (
	"strings"
	"testing"
	"time"
)

// gobbleQuotaData is gobble 0.4.0's error data for an OpenCode Zen free-tier
// limit, captured by 0179 PLAN P3's second run (MADR 0243 F1).
const gobbleQuotaData = "llm: api status 429: llm: quota exhausted: llmprovider: quota exhausted: opencode-zen/chat_completions HTTP 429 FreeUsageLimitError (retry-after 1h33m21s): Free usage limit reached. Free models work in OpenCode (https://opencode.ai); subscribe to OpenCode Go to keep going: https://opencode.ai/go"

// gobbleQuotaError is that data as the JSON-RPC error gobble sent.
const gobbleQuotaError = `{"code":-32603,"message":"Internal error","data":{"error":"` + gobbleQuotaData + `"}}`

// gobbleProviderMessage is the provider's own two sentences in it.
const gobbleProviderMessage = "Free usage limit reached. Free models work in OpenCode (https://opencode.ai); subscribe to OpenCode Go to keep going: https://opencode.ai/go"

// TestGobbleQuotaEndToEnd is MADR 0243 Confirmation 1: gobble's captured quota
// error reads as the quota it is, with its reset and the provider's words.
func TestGobbleQuotaEndToEnd(t *testing.T) {
	c := Present(gobbleQuotaError, now)
	if c.Kind != KindQuota {
		t.Errorf("kind = %q, want quota (D1: the cause is in data.error)", c.Kind)
	}
	if want := now.Add(time.Hour + 33*time.Minute + 21*time.Second); !c.ResetAt.Equal(want) {
		t.Errorf("reset = %v, want %v (D2: a Go-duration retry-after)", c.ResetAt, want)
	}
	if !strings.HasPrefix(c.Message, gobbleProviderMessage) {
		t.Errorf("message = %q, want the provider's own words first (D4)", c.Message)
	}
	if !strings.Contains(c.Message, "Resets in about") {
		t.Errorf("message = %q, want when it lifts after the provider's words", c.Message)
	}
	want := Limit{Scope: ScopeFreeTier, Provider: "opencode-zen", Code: "FreeUsageLimitError", ProviderMessage: gobbleProviderMessage}
	if c.Limit != want {
		t.Errorf("limit = %+v, want %+v (D3)", c.Limit, want)
	}
	// The bare data string, as an agent that sends no JSON-RPC wrapper would.
	if bare := Present(gobbleQuotaData, now); bare.Limit != want || bare.Kind != KindQuota {
		t.Errorf("bare data: kind %q limit %+v, want quota %+v", bare.Kind, bare.Limit, want)
	}
}

// TestRefineGivesAScopeItsKind is D3 source 1: an agent's structured scope
// makes an unclassified error a quota, or a rate limit for ScopeRate, and
// leaves an auth error alone.
func TestRefineGivesAScopeItsKind(t *testing.T) {
	for _, tc := range []struct {
		from  Kind
		scope string
		want  Kind
	}{
		{KindNone, ScopeWeekly, KindQuota},
		{KindServer, ScopeFreeTier, KindQuota},
		{KindNone, ScopeRate, KindRateLimit},
		{KindAuth, ScopeCredits, KindAuth},
	} {
		got := Refine(Classification{Kind: tc.from}, Limit{Scope: tc.scope}, time.Time{}, now)
		if got.Kind != tc.want || got.Limit.Scope != tc.scope {
			t.Errorf("Refine(%q, %q) = kind %q scope %q, want %q", tc.from, tc.scope, got.Kind, got.Limit.Scope, tc.want)
		}
	}
}

// TestRetryAfterGoDuration is D2: the SDK's Go-duration retry-after, compound
// and fractional, sets the reset; the older formats still parse.
func TestRetryAfterGoDuration(t *testing.T) {
	for _, tc := range []struct {
		msg  string
		want time.Duration
	}{
		{"x HTTP 429 (retry-after 1h33m21s): y", time.Hour + 33*time.Minute + 21*time.Second},
		{"x HTTP 429 (retry-after 2h9m59.87321s): y", 2*time.Hour + 9*time.Minute + 59873210*time.Microsecond},
		{"x HTTP 429 (retry-after 45s): y", 45 * time.Second},
		{"x HTTP 429 (retry-after 250ms): y", 250 * time.Millisecond},
		{"Retry-After: 30", 30 * time.Second},
		{"Rate limit reached. Please try again in 20s.", 20 * time.Second},
	} {
		got := parseReset(tc.msg, now)
		if got.IsZero() || got.Sub(now) != tc.want {
			t.Errorf("parseReset(%q) = %v from now, want %v", tc.msg, got.Sub(now), tc.want)
		}
	}
}

// TestLimitScopes is D3: the provider's code, then a phrase that states a
// window, sets the scope; a long wait alone never makes a window.
func TestLimitScopes(t *testing.T) {
	sdk := func(code, msg string) string {
		return "llmprovider: quota exhausted: opencode-go/chat_completions HTTP 429 " + code + " (retry-after 4h): " + msg
	}
	for _, tc := range []struct {
		name, msg string
		scope     string
		window    int
		code      string
	}{
		{"free tier code", sdk("FreeUsageLimitError", "Free usage limit reached."), ScopeFreeTier, 0, "FreeUsageLimitError"},
		{"plan code, no window stated", sdk("GoUsageLimitError", "Usage limit reached."), ScopePlan, 0, "GoUsageLimitError"},
		{"plan code, a 5-hour window stated", sdk("GoUsageLimitError", "5-hour usage limit reached."), ScopeWindow, 300, "GoUsageLimitError"},
		{"plan code, a weekly limit stated", sdk("GoUsageLimitError", "Weekly usage limit reached. Resets in 4 days."), ScopeWeekly, 0, "GoUsageLimitError"},
		{"monthly code", "llmprovider: quota exhausted: opencode-zen HTTP 401 MonthlyLimitError: Monthly limit reached.", ScopeMonthly, 0, "MonthlyLimitError"},
		{"credits by code in text", `{"error":{"code":"insufficient_quota","message":"You exceeded your current quota."}}`, ScopeCredits, 0, ""},
		{"credits by 402", "llmprovider: quota exhausted: kilo HTTP 402: Insufficient account funds.", ScopeCredits, 0, ""},
		{"claude 5-hour phrase", "5-hour limit reached ∙ resets 3am", ScopeWindow, 300, ""},
		{"gemini per day", "RESOURCE_EXHAUSTED quota exceeded for quotaId GenerateRequestsPerDayPerProjectPerModel; billing", ScopeDaily, 0, ""},
		{"plain rate limit", "llmprovider: rate limited: claude HTTP 429 rate_limit_error (retry-after 12s): Number of request tokens has exceeded your per-minute rate limit", ScopeRate, 0, "rate_limit_error"},
		{"quota, nothing stated, a long wait", "llmprovider: quota exhausted: someprovider HTTP 429 SomeLimit (retry-after 5h): Limit reached.", "", 0, "SomeLimit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Present(tc.msg, now)
			if c.Limit.Scope != tc.scope || c.Limit.WindowMinutes != tc.window || c.Limit.Code != tc.code {
				t.Errorf("limit = %+v (kind %q), want scope %q window %d code %q", c.Limit, c.Kind, tc.scope, tc.window, tc.code)
			}
		})
	}
}

// TestProviderWordsWithoutWrappers is D4: the agent's and the SDK's prefixes
// go, the provider's words stay; a message with no wrapper is left alone; an
// auth error keeps its fixed copy and has no limit.
func TestProviderWordsWithoutWrappers(t *testing.T) {
	retried := "llmprovider: opencode-zen failed after 4 attempts: llmprovider: rate limited: opencode-zen/chat_completions HTTP 429 RateLimitError (retry-after 30s): Too many requests, slow down."
	if c := Present(retried, now); c.Limit.ProviderMessage != "Too many requests, slow down." || c.Limit.Provider != "opencode-zen" {
		t.Errorf("after retries: limit %+v", c.Limit)
	}
	stream := "llm: rate limited: llmprovider: rate limited: claude stream rate_limit_error: Number of requests has exceeded your rate limit."
	if c := Present(stream, now); c.Limit.ProviderMessage != "Number of requests has exceeded your rate limit." || c.Limit.Provider != "claude" {
		t.Errorf("stream failure: limit %+v", c.Limit)
	}
	// gobble's prefixes with no SDK header, as from a failure the SDK did not
	// classify.
	wrapped := "Internal error llm: api status 429: llm: quota exhausted: Weekly usage limit reached for this plan."
	if c := Present(wrapped, now); c.Limit.ProviderMessage != "Weekly usage limit reached for this plan." || c.Limit.Scope != ScopeWeekly {
		t.Errorf("wrappers, no SDK header: limit %+v", c.Limit)
	}
	plain := "Insufficient balance. Please add credits to continue."
	if c := Present(plain, now); c.Limit.ProviderMessage != "" || c.Kind != KindQuota {
		t.Errorf("no wrapper: kind %q limit %+v, want quota and no provider message", c.Kind, c.Limit)
	}
	grok401 := `{"code":-32603,"message":"Internal error","data":"Unauthorized (401) from https://api.x.ai: Invalid or expired credentials"}`
	if c := Present(grok401, now); c.Kind != KindAuth || c.Limit != (Limit{}) || !strings.HasPrefix(c.Message, "The model provider rejected") {
		t.Errorf("grok 401: kind %q limit %+v message %q", c.Kind, c.Limit, c.Message)
	}
}
