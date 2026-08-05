package agenterr

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

var now = time.Date(2026, 7, 22, 13, 0, 0, 0, time.UTC)

func TestClassifyKinds(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want Kind
	}{
		{"openai insufficient quota", "You exceeded your current quota, please check your plan and billing details.", KindQuota},
		{"openai insufficient_quota code", `{"error":{"code":"insufficient_quota","message":"..."} }`, KindQuota},
		{"anthropic credit balance", "Your credit balance is too low to access the Anthropic API.", KindQuota},
		{"claude limit reached", "5-hour limit reached ∙ resets 3am", KindQuota},
		{"generic usage limit", "You have hit your usage limit for today.", KindQuota},
		{"zen free usage", "You have exceeded your free usage quota for big-pickle.", KindQuota},
		{"spend limit", "Team spending limit exceeded", KindQuota},
		{"openai rate limit", "Rate limit reached for gpt-4o. Please try again in 20s.", KindRateLimit},
		{"anthropic rate_limit_error", `{"type":"rate_limit_error","message":"Number of request tokens has exceeded your per-minute rate limit"}`, KindRateLimit},
		{"http 429", "request failed: HTTP 429 Too Many Requests", KindRateLimit},
		{"bare status 429", "got status 429", KindRateLimit},
		{"http 529 overloaded", "API Error: 529 {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}", KindRateLimit},
		{"bare status 529", "upstream returned 529", KindRateLimit},
		{"http 503", "provider returned HTTP 503 Service Unavailable", KindRateLimit},
		{"gousagelimiterror", "GoUsageLimitError: Weekly usage limit reached. Resets in 4 days.", KindQuota},
		// "429" as a bare substring (a port, a long id) is not an HTTP status —
		// it must not be misread as a rate limit.
		{"port containing 429", "dial tcp 1.2.3.4:4290: connection refused", KindNone},
		{"id containing 429", "trace id 1429abc failed", KindNone},
		// Bare RESOURCE_EXHAUSTED is per-window throttling, not a billing
		// wall — rate_limit is the actionable classification.
		{"gemini resource exhausted", "Resource has been exhausted (e.g. check quota).", KindRateLimit},
		{"gemini retry", "RESOURCE_EXHAUSTED: Quota exceeded for quota metric. Please retry in 26.7s.", KindQuota},
		{"plain failure", "connection refused", KindNone},
		{"compile error mentioning nothing", "syntax error near unexpected token", KindNone},
		// Real-world messages catalogued from provider/agent research.
		{"xai credits exhausted", "Your team 16f2151a has either used all available credits or reached its monthly spending limit. To continue making API requests, please purchase more credits or raise your spending limit.", KindQuota},
		{"xai tpm overflow", "Too many tokens for team e9fe19cb and model grok-4-0709. Your team's rate limit is — Tokens per Minute (actual/limit): 65605/16000.", KindRateLimit},
		{"grok build free limit", "You've reached your free Grok Build usage limit for now. Get SuperGrok for much higher limits", KindQuota},
		{"zen free usage", "Free usage exceeded, add credits https://opencode.ai/zen", KindQuota},
		{"zen 429 typo", "too many request, please try again", KindRateLimit},
		{"zen insufficient balance", "Error: Insufficient Balance", KindQuota},
		{"claude oauth pipe", "Claude AI usage limit reached|1784728800", KindQuota},
		{"anthropic org limit", "This request would exceed your organization's rate limit of 80,000 input tokens per minute.", KindRateLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.msg, now)
			if got.Kind != tc.want {
				t.Fatalf("Classify(%q).Kind = %q, want %q", tc.msg, got.Kind, tc.want)
			}
		})
	}
}

func TestParseResetRelative(t *testing.T) {
	cases := []struct {
		msg  string
		want time.Duration
	}{
		{"Rate limit reached. Please try again in 20s.", 20 * time.Second},
		{"Please retry in 26.7s.", 26700 * time.Millisecond},
		{"rate limited, try again in 2 minutes", 2 * time.Minute},
		{"quota exceeded; retry after 30 seconds", 30 * time.Second},
		{"quota exceeded; Retry-After: 45", 45 * time.Second},
		{"limit reached, try again in 1h13m", time.Hour + 13*time.Minute},
		{"throttled: try again in 250 ms", 250 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.msg, func(t *testing.T) {
			got := parseReset(tc.msg, now)
			if got.IsZero() {
				t.Fatalf("no reset parsed from %q", tc.msg)
			}
			if d := got.Sub(now); d != tc.want {
				t.Fatalf("reset delta = %s, want %s (msg %q)", d, tc.want, tc.msg)
			}
		})
	}

	// End-to-end: a real rate-limit message parses through Classify.
	got := Classify("Rate limit reached for gpt-4o. Please try again in 20s.", now)
	if got.Kind != KindRateLimit || got.ResetAt.Sub(now) != 20*time.Second {
		t.Fatalf("Classify end-to-end = %+v", got)
	}
}

func TestRealWorldResets(t *testing.T) {
	// Claude subscription pipe format: unix reset instant.
	got := Classify("Claude AI usage limit reached|1784728800", now)
	if got.Kind != KindQuota || got.ResetAt.Unix() != 1784728800 {
		t.Fatalf("pipe format = %+v", got)
	}
	// OpenCode Zen with its retry banner: spaced compound duration.
	got = Classify("Free usage exceeded, add credits https://opencode.ai/zen [retrying in 10h 37m attempt 1", now)
	if got.Kind != KindQuota {
		t.Fatalf("zen kind = %q", got.Kind)
	}
	if d := got.ResetAt.Sub(now); d != 10*time.Hour+37*time.Minute {
		t.Fatalf("zen retry banner delta = %s, want 10h37m", d)
	}
	// Gemini structured RetryInfo in the JSON body.
	got = Classify(`RESOURCE_EXHAUSTED: quota exceeded. {"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"53s"}`, now)
	if d := got.ResetAt.Sub(now); d != 53*time.Second {
		t.Fatalf("retryDelay delta = %s, want 53s", d)
	}
	// Gemini free-tier long-decimal retry.
	got = Classify("Quota exceeded for metric: generate_content_free_tier_requests, limit: 20. Please retry in 45.06576809s.", now)
	if got.ResetAt.IsZero() || got.ResetAt.Sub(now) < 45*time.Second || got.ResetAt.Sub(now) > 46*time.Second {
		t.Fatalf("gemini decimal retry = %+v", got)
	}
	// Claude weekly-limit date format.
	got = Classify("Weekly limit reached ∙ resets Oct 7", now)
	want := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	if got.Kind != KindQuota || !got.ResetAt.Equal(want) {
		t.Fatalf("weekly reset = %+v, want %s", got, want)
	}
	// xAI credits message carries no reset — must stay zero, not hallucinate.
	got = Classify("Your team has either used all available credits or reached its monthly spending limit.", now)
	if !got.ResetAt.IsZero() {
		t.Fatalf("xai credits should have no reset, got %s", got.ResetAt)
	}
}

func TestParseResetClock(t *testing.T) {
	// "resets 3am" → next 3am after 13:00 = tomorrow 03:00.
	got := Classify("5-hour limit reached ∙ resets 3am", now)
	want := time.Date(2026, 7, 23, 3, 0, 0, 0, time.UTC)
	if !got.ResetAt.Equal(want) {
		t.Fatalf("resets 3am → %s, want %s", got.ResetAt, want)
	}
	// Same-day pm clock: "resets at 3:30pm" from 13:00 = today 15:30.
	got = Classify("usage limit reached — resets at 3:30pm", now)
	want = time.Date(2026, 7, 22, 15, 30, 0, 0, time.UTC)
	if !got.ResetAt.Equal(want) {
		t.Fatalf("resets 3:30pm → %s, want %s", got.ResetAt, want)
	}
}

func TestParseResetRFC3339(t *testing.T) {
	got := Classify("quota exceeded; resets_at=2026-07-22T15:04:05Z", now)
	want := time.Date(2026, 7, 22, 15, 4, 5, 0, time.UTC)
	if !got.ResetAt.Equal(want) {
		t.Fatalf("rfc3339 reset → %s, want %s", got.ResetAt, want)
	}
}

func TestParseResetUnix(t *testing.T) {
	// 2026-07-22 14:00:00 UTC.
	got := Classify("rate limit exceeded, reset at 1784728800", now)
	if got.ResetAt.IsZero() {
		t.Fatal("no unix reset parsed")
	}
	if got.ResetAt.Unix() != 1784728800 {
		t.Fatalf("unix reset = %d, want 1784728800", got.ResetAt.Unix())
	}
}

func TestNoResetStaysZero(t *testing.T) {
	got := Classify("You exceeded your current quota, please check your plan and billing details.", now)
	if !got.ResetAt.IsZero() {
		t.Fatalf("unexpected reset %s", got.ResetAt)
	}
}

func TestPastTimestampsIgnored(t *testing.T) {
	got := Classify("quota exceeded since 2020-01-01T00:00:00Z", now)
	if !got.ResetAt.IsZero() {
		t.Fatalf("past timestamp should not become a reset time, got %s", got.ResetAt)
	}
}

// MADR 0069 P1 (U4) — OS permission denials classify as KindPermission on
// both surfaces: forwarded message text and Go error chains.
func TestClassifyPermission(t *testing.T) {
	now := time.Now()
	for _, msg := range []string{
		"stat /Users/x/Documents: operation not permitted",
		"tool failed: EPERM",
		"open /etc/shadow: permission denied",
	} {
		if got := Classify(msg, now); got.Kind != KindPermission {
			t.Errorf("Classify(%q).Kind = %q, want permission", msg, got.Kind)
		}
	}
	// A user's tool-approval denial must NOT classify as an OS error: the
	// bare EACCES phrase counts only alongside a path-ish "/".
	for _, msg := range []string{
		"permission denied by the user",
		"tool permission denied",
	} {
		if got := Classify(msg, now); got.Kind != KindNone {
			t.Errorf("Classify(%q).Kind = %q, want none", msg, got.Kind)
		}
	}
	// Quota/rate classification keeps precedence on mixed text.
	if got := Classify("quota exceeded reading /tmp/x: permission denied", now); got.Kind != KindQuota {
		t.Errorf("mixed quota+permission = %q, want quota", got.Kind)
	}
}

func TestIsPermission(t *testing.T) {
	wrapped := fmt.Errorf("cwd: %w", &os.PathError{Op: "stat", Path: "/x", Err: syscall.EPERM})
	if !IsPermission(wrapped) {
		t.Fatal("EPERM chain not recognised")
	}
	if !IsPermission(&os.PathError{Op: "open", Path: "/x", Err: syscall.EACCES}) {
		t.Fatal("EACCES not recognised")
	}
	if IsPermission(fmt.Errorf("boom")) || IsPermission(nil) {
		t.Fatal("false positive")
	}
}

func TestPresentNaturalLanguage(t *testing.T) {
	// Opaque status-only text → templated copy naming the HTTP code.
	got := Present("HTTP 529", now)
	if got.Kind != KindRateLimit {
		t.Fatalf("529 kind = %q", got.Kind)
	}
	if !strings.Contains(got.Message, "overloaded") && !strings.Contains(got.Message, "529") {
		t.Fatalf("529 message not natural: %q", got.Message)
	}

	// Goose weekly quota — keep the provider prose (already readable) and
	// parse the reset delay.
	raw := "Weekly usage limit reached. Resets in 4 days."
	got = Present(raw, now)
	if got.Kind != KindQuota {
		t.Fatalf("weekly quota kind = %q", got.Kind)
	}
	if !strings.Contains(strings.ToLower(got.Message), "usage limit") {
		t.Fatalf("weekly message lost the reason: %q", got.Message)
	}
	if d := got.ResetAt.Sub(now); d < 3*24*time.Hour || d > 5*24*time.Hour {
		t.Fatalf("weekly reset delta = %s, want ~4 days", d)
	}

	// Backoff log line alone still yields a RetryAt.
	got = Present("Backing off for 3600s before retry", now)
	if got.ResetAt.IsZero() {
		t.Fatal("backoff should parse a reset")
	}
	if d := got.ResetAt.Sub(now); d != time.Hour {
		t.Fatalf("backoff delta = %s, want 1h", d)
	}
}

func TestExtractTextGooseJSON(t *testing.T) {
	line := `{"timestamp":"2026-08-05T23:15:39Z","level":"WARN","fields":{"message":"Provider request failed with status: 429 Too Many Requests. Payload: {\"type\":\"error\",\"error\":{\"type\":\"GoUsageLimitError\",\"message\":\"Weekly usage limit reached. Resets in 4 days.\"}}"},"target":"goose_providers::http_status"}`
	got := ExtractText(line)
	if !strings.Contains(got, "Weekly usage limit") {
		t.Fatalf("ExtractText = %q", got)
	}
	cls := Present(line, now)
	if cls.Kind != KindQuota {
		t.Fatalf("goose JSON line kind = %q (msg %q)", cls.Kind, cls.Message)
	}
}

func TestPresent429Template(t *testing.T) {
	got := Present("status 429", now)
	if got.Kind != KindRateLimit {
		t.Fatalf("kind = %q", got.Kind)
	}
	if !strings.Contains(got.Message, "429") && !strings.Contains(strings.ToLower(got.Message), "too many") {
		t.Fatalf("message = %q", got.Message)
	}
}
