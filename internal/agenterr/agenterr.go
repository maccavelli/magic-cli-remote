// Package agenterr classifies agent/provider error messages so clients can
// render actionable cards ("quota exceeded, resets at 3pm") instead of raw
// provider dumps. Classification is best-effort substring/regex matching over
// the message text — the only signal CLI agents forward.
package agenterr

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Kind labels the error class carried on event.Event.ErrorKind.
type Kind string

const (
	// KindNone means the message matched no known class.
	KindNone Kind = ""
	// KindQuota is a hard usage/credit/spend limit: retrying immediately
	// will not help; the user must wait for a reset or change plan/model.
	KindQuota Kind = "quota"
	// KindRateLimit is transient throttling (requests/tokens per window):
	// waiting briefly and retrying usually works.
	KindRateLimit Kind = "rate_limit"
)

// Classification is the result of Classify.
type Classification struct {
	Kind Kind
	// ResetAt is when the limit is expected to lift, when the message said
	// so (zero otherwise).
	ResetAt time.Time
}

// Real-world phrasings catalogued from xAI/Grok, OpenCode Zen, Anthropic,
// OpenAI, and Gemini error bodies (see the quota-error research in ADR 0011's
// lineage): matched as lowercase substrings.
var quotaWords = []string{
	"insufficient_quota",
	"insufficient quota",
	"exceeded your current quota",
	"usage quota",
	"quota exceeded",
	"quota_exceeded",
	"out of credits",
	"insufficient credits",
	"insufficient balance", // OpenCode Zen hard block
	"credit balance",       // Anthropic "credit balance is too low"
	"credits exhausted",
	"run out of credits",
	"available credits", // xAI "used all available credits"
	"purchase more credits",
	"spending limit",
	"spend limit",
	"usage limit",
	"usage cap",
	"billing hard limit",
	"check your plan and billing",
	"limit reached", // e.g. Claude "5-hour limit reached ∙ resets 3am"
	"free usage",    // Zen "Free usage exceeded, add credits …"
	"freeusagelimiterror",
	"upgrade your plan",
}

var rateWords = []string{
	"rate limit",
	"rate_limit",
	"ratelimit",        // also catches Gemini's camelCase rateLimitExceeded
	"too many request", // singular on purpose: Zen sends "too many request"
	"too many tokens",  // xAI TPM overflow
	"tokens per minute",
	"resource has been exhausted",
	"resource_exhausted",
	"resource exhausted",
	"throttl",
	"overloaded_error",
	"429",
}

// Billing-ish words that force KindQuota even when rate-limit words are also
// present (e.g. OpenAI's insufficient_quota errors mention "rate limit"
// docs links).
var hardQuotaWords = []string{
	"insufficient_quota",
	"billing",
	"credit",
	"quota",
	"spend",
	"plan",
}

// Classify inspects an agent/provider error message. now anchors relative
// reset phrases ("try again in 20s"); pass time.Now() outside tests.
func Classify(msg string, now time.Time) Classification {
	m := strings.ToLower(msg)
	quota := containsAny(m, quotaWords)
	rate := containsAny(m, rateWords)
	var kind Kind
	switch {
	case quota && rate:
		if containsAny(m, hardQuotaWords) {
			kind = KindQuota
		} else {
			kind = KindRateLimit
		}
	case quota:
		kind = KindQuota
	case rate:
		kind = KindRateLimit
	default:
		return Classification{}
	}
	return Classification{Kind: kind, ResetAt: parseReset(msg, now)}
}

func containsAny(m string, words []string) bool {
	for _, w := range words {
		if strings.Contains(m, w) {
			return true
		}
	}
	return false
}

// --- reset-time extraction -------------------------------------------------

var (
	// Claude subscription format: "…usage limit reached|1760000400" — the
	// pipe-separated unix reset instant is the machine-readable channel.
	rePipeUnix = regexp.MustCompile(`(?i)limit reached\s*\|\s*(1[5-9]\d{8})\b`)
	// Gemini google.rpc.RetryInfo embedded in the body: "retryDelay": "53s".
	reRetryDelay = regexp.MustCompile(`(?i)"retryDelay"\s*:\s*"(\d+(?:\.\d+)?)s"`)
	// Compound durations near a retry/reset word, spaces tolerated between
	// components: "1h13m", "10h 37m" (OpenCode's retry banner), "2m30s".
	reCompound = regexp.MustCompile(`(?i)(?:try again|retry(?:ing)?|resets?)(?:[^.\d]{0,20})\bin\s+((?:\d+h\s*)?(?:\d+m\s*)?(?:\d+(?:\.\d+)?s\s*)?)`)
	// "try again in 20s", "please retry in 45.06576809s", "retry after 30
	// seconds", "try again in 2 minutes", "retry in 250 ms".
	reRelative = regexp.MustCompile(`(?i)(?:try again|retry(?:ing)?|resets?|available)(?:[^.\d]{0,20})\bin\s+(\d+(?:\.\d+)?)\s*(ms|milliseconds?|s|secs?|seconds?|m|mins?|minutes?|h|hrs?|hours?|d|days?)\b`)
	// "retry after 30 seconds" / bare Retry-After: 45 (seconds).
	reAfter = regexp.MustCompile(`(?i)retry[- ]after[:\s]+(\d+(?:\.\d+)?)\s*(ms|s|secs?|seconds?|m|mins?|minutes?|h|hrs?|hours?)?\b`)
	// RFC3339 timestamp anywhere in the message.
	reRFC3339 = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?`)
	// "reset at Oct 7", "resets Oct 7, 1am" — weekly-limit style date.
	reMonthDay = regexp.MustCompile(`(?i)resets?\s*(?:at\s+|on\s+)?(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.?\s+(\d{1,2})(?:,?\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?)?\b`)
	// "resets 3am", "resets at 3:30pm", "resets at 15:04" — clock time,
	// interpreted as the next such wall-clock instant (local time).
	reClock = regexp.MustCompile(`(?i)resets?\s*(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\b`)
	// 10-digit unix timestamp near a reset word.
	reUnix = regexp.MustCompile(`(?i)reset[^0-9]{0,20}\b(1[5-9]\d{8})\b`)
)

var unitDur = map[string]time.Duration{
	"ms": time.Millisecond, "millisecond": time.Millisecond, "milliseconds": time.Millisecond,
	"s": time.Second, "sec": time.Second, "secs": time.Second, "second": time.Second, "seconds": time.Second,
	"m": time.Minute, "min": time.Minute, "mins": time.Minute, "minute": time.Minute, "minutes": time.Minute,
	"h": time.Hour, "hr": time.Hour, "hrs": time.Hour, "hour": time.Hour, "hours": time.Hour,
	"d": 24 * time.Hour, "day": 24 * time.Hour, "days": 24 * time.Hour,
}

// parseReset extracts a reset instant from msg, trying the most explicit
// (machine-readable) formats first. Returns the zero time when nothing
// parses.
func parseReset(msg string, now time.Time) time.Time {
	if m := rePipeUnix.FindStringSubmatch(msg); m != nil {
		if t := unixTime(m[1], now); !t.IsZero() {
			return t
		}
	}
	if m := reRetryDelay.FindStringSubmatch(msg); m != nil {
		if d := unitDuration(m[1], "s"); d > 0 {
			return now.Add(d)
		}
	}
	if m := reCompound.FindStringSubmatch(msg); m != nil {
		// Strip the tolerated inner spaces ("10h 37m" → "10h37m") for
		// time.ParseDuration.
		compact := strings.ReplaceAll(strings.TrimSpace(strings.ToLower(m[1])), " ", "")
		if compact != "" {
			if d, err := time.ParseDuration(compact); err == nil && d > 0 {
				return now.Add(d)
			}
		}
	}
	if m := reRelative.FindStringSubmatch(msg); m != nil {
		if d := unitDuration(m[1], m[2]); d > 0 {
			return now.Add(d)
		}
	}
	if m := reAfter.FindStringSubmatch(msg); m != nil {
		unit := m[2]
		if unit == "" {
			unit = "s" // bare Retry-After counts seconds
		}
		if d := unitDuration(m[1], unit); d > 0 {
			return now.Add(d)
		}
	}
	if m := reUnix.FindStringSubmatch(msg); m != nil {
		if t := unixTime(m[1], now); !t.IsZero() {
			return t
		}
	}
	if m := reRFC3339.FindString(msg); m != "" {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05Z07:00", "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
			if t, err := time.ParseInLocation(layout, m, now.Location()); err == nil {
				if t.After(now) && t.Before(now.Add(365*24*time.Hour)) {
					return t
				}
				break
			}
		}
	}
	if m := reMonthDay.FindStringSubmatch(msg); m != nil {
		if t := nextMonthDay(m, now); !t.IsZero() {
			return t
		}
	}
	if m := reClock.FindStringSubmatch(msg); m != nil {
		return nextClock(m[1], m[2], m[3], now)
	}
	return time.Time{}
}

// unixTime parses a 10-digit epoch, accepting only plausible future instants.
func unixTime(s string, now time.Time) time.Time {
	sec, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}
	}
	t := time.Unix(sec, 0)
	if t.After(now) && t.Before(now.Add(365*24*time.Hour)) {
		return t
	}
	return time.Time{}
}

var monthByName = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March,
	"apr": time.April, "may": time.May, "jun": time.June,
	"jul": time.July, "aug": time.August, "sep": time.September,
	"oct": time.October, "nov": time.November, "dec": time.December,
}

// nextMonthDay resolves "resets Oct 7[, 1am]" to the next such date after
// now (this year, else next year), defaulting to midnight when no clock is
// given.
func nextMonthDay(m []string, now time.Time) time.Time {
	month, ok := monthByName[strings.ToLower(m[1])]
	if !ok {
		return time.Time{}
	}
	day, err := strconv.Atoi(m[2])
	if err != nil || day < 1 || day > 31 {
		return time.Time{}
	}
	hour, minute := 0, 0
	if m[3] != "" {
		if hour, err = strconv.Atoi(m[3]); err != nil {
			return time.Time{}
		}
		if m[4] != "" {
			if minute, err = strconv.Atoi(m[4]); err != nil {
				return time.Time{}
			}
		}
		switch strings.ToLower(m[5]) {
		case "am":
			if hour == 12 {
				hour = 0
			}
		case "pm":
			if hour < 12 {
				hour += 12
			}
		}
		if hour > 23 || minute > 59 {
			return time.Time{}
		}
	}
	t := time.Date(now.Year(), month, day, hour, minute, 0, 0, now.Location())
	if !t.After(now) {
		t = time.Date(now.Year()+1, month, day, hour, minute, 0, 0, now.Location())
	}
	return t
}

func unitDuration(value, unit string) time.Duration {
	v, err := strconv.ParseFloat(value, 64)
	if err != nil || v <= 0 {
		return 0
	}
	u, ok := unitDur[strings.ToLower(strings.TrimSpace(unit))]
	if !ok {
		return 0
	}
	return time.Duration(v * float64(u))
}

// nextClock resolves "3am" / "3:30pm" / "15:04" to the next such wall-clock
// instant after now, in now's location.
func nextClock(hourStr, minStr, ampm string, now time.Time) time.Time {
	hour, err := strconv.Atoi(hourStr)
	if err != nil {
		return time.Time{}
	}
	minute := 0
	if minStr != "" {
		if minute, err = strconv.Atoi(minStr); err != nil {
			return time.Time{}
		}
	}
	switch strings.ToLower(ampm) {
	case "am":
		if hour == 12 {
			hour = 0
		}
	case "pm":
		if hour < 12 {
			hour += 12
		}
	case "":
		// 24h clock only when plausible; a bare "resets 3" is too vague.
		if minStr == "" {
			return time.Time{}
		}
	}
	if hour > 23 || minute > 59 {
		return time.Time{}
	}
	t := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if !t.After(now) {
		t = t.Add(24 * time.Hour)
	}
	return t
}
