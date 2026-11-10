package agenterr

import (
	"regexp"
	"strconv"
	"strings"
)

// Limit describes the usage limit a quota or rate-limit error hit, from the
// most explicit source the message offers (MADR 0243 D3). Each field is empty
// when no source states it: a window is never inferred from how long a wait
// is.
type Limit struct {
	// Scope is the kind of limit: one of the Scope constants, an open set.
	Scope string
	// WindowMinutes is the rolling window's length, when a source states it.
	WindowMinutes int
	// Provider is the upstream the agent named, such as "opencode-zen".
	Provider string
	// Code is the provider's own error code, such as "FreeUsageLimitError".
	Code string
	// ProviderMessage is the provider's own sentences, with the agent's and
	// the SDK's wrappers removed (D4).
	ProviderMessage string
}

// The limit scopes (MADR 0243 D3). Clients must treat an unknown value as no
// scope.
const (
	// ScopeRate is transient throttling: requests or tokens per short period.
	ScopeRate = "rate"
	// ScopeWindow is a rolling window of stated length, such as 5 hours.
	ScopeWindow = "window"
	// ScopeDaily is a quota for the day.
	ScopeDaily = "daily"
	// ScopeWeekly is a quota for the week.
	ScopeWeekly = "weekly"
	// ScopeMonthly is a quota for the month or billing period.
	ScopeMonthly = "monthly"
	// ScopeCredits is a balance or billing limit that does not lift by time.
	ScopeCredits = "credits"
	// ScopeFreeTier is a free tier's limit.
	ScopeFreeTier = "free_tier"
	// ScopePlan is a plan's usage limit whose window the source does not
	// state.
	ScopePlan = "plan"
)

// codeScopes maps provider error codes whose meaning is documented to a
// scope, lowercased (go-llmprovider-sdk 0012-MADR §1.1's classification).
var codeScopes = map[string]string{
	"freeusagelimiterror":                       ScopeFreeTier,
	"promotion_model_limit_reached":             ScopeFreeTier,
	"monthlylimiterror":                         ScopeMonthly,
	"creditserror":                              ScopeCredits,
	"insufficient_quota":                        ScopeCredits,
	"insufficient_balance":                      ScopeCredits,
	"gousagelimiterror":                         ScopePlan,
	"blackusagelimiterror":                      ScopePlan,
	"usage_limit_reached":                       ScopePlan,
	"subscription_sharing_usage_limit_exceeded": ScopePlan,
	"ratelimiterror":                            ScopeRate,
	"rate_limit_error":                          ScopeRate,
	"rate_limit_exceeded":                       ScopeRate,
}

// codeOrder fixes the order codes are looked for in text, so the result does
// not depend on map iteration: the longest first, so a code that contains
// another wins.
var codeOrder = func() []string {
	out := make([]string, 0, len(codeScopes))
	for c := range codeScopes {
		out = append(out, c)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && (len(out[j]) > len(out[j-1]) || len(out[j]) == len(out[j-1]) && out[j] < out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}()

// reSDKSegment is go-llmprovider-sdk's error header: "<provider> HTTP <status>
// <code> (retry-after <d>): " or "<provider> stream <code>: ", each part after
// the provider optional but one of HTTP and stream (llmprovider APIError.Error).
var reSDKSegment = regexp.MustCompile(`(?:^|[\s:])([a-z0-9][a-z0-9._-]*(?:/[a-z0-9._-]+)?) (?:HTTP (\d{3})|stream)(?: ([A-Za-z][A-Za-z0-9_.-]*))?(?: \(retry-after [^)]*\))?: `)

// reWrapper is one agent or SDK prefix ahead of a provider's words: gobble's
// "llm: api status 429: " and "llm: quota exhausted: ", the SDK's
// "llmprovider: rate limited: ", and a JSON-RPC wrapper's "Internal error ".
var reWrapper = regexp.MustCompile(`^(?:Internal error |llm: api status \d{3}: |llm: [a-z][a-z -]*: |llmprovider: [a-z][a-z -]*: )`)

// Phrases that state a window (D3 source 3), matched in lowercase text.
var (
	reHourWindow  = regexp.MustCompile(`\b(\d{1,2})[- ]?(?:hours?|hrs?|h)\b(?: usage| rate)? limit`)
	reWeeklyLimit = regexp.MustCompile(`\bweekly (?:usage )?(?:limit|quota)\b|\bper week\b`)
	reMonthLimit  = regexp.MustCompile(`\bmonthly (?:usage )?(?:limit|quota)\b|\bper month\b`)
	// Gemini's quotaId runs words together: "GenerateRequestsPerDayPerProject…".
	reDailyLimit = regexp.MustCompile(`\bdaily (?:usage )?(?:limit|quota)\b|\bper day\b|perday`)
	// reHTTP402 is an explicit HTTP 402, Payment Required: a balance or
	// billing block, which go-llmprovider-sdk classifies as quota. A bare
	// "402" (a port, an id) is not one.
	reHTTP402 = regexp.MustCompile(`(?i)\b(?:http|status) 402\b|\b402 payment required\b`)
)

// describeLimit is msg's limit, for a quota or rate-limit kind; the zero
// Limit for any other kind.
func describeLimit(msg string, kind Kind) Limit {
	if kind != KindQuota && kind != KindRateLimit {
		return Limit{}
	}
	var l Limit
	status := ""
	if m := reSDKSegment.FindStringSubmatchIndex(msg); m != nil {
		provider := msg[m[2]:m[3]]
		if i := strings.IndexByte(provider, '/'); i > 0 {
			provider = provider[:i]
		}
		l.Provider = provider
		if m[4] >= 0 {
			status = msg[m[4]:m[5]]
		}
		if m[6] >= 0 {
			l.Code = msg[m[6]:m[7]]
		}
		l.ProviderMessage = strings.TrimSpace(msg[m[1]:])
	} else if stripped := stripWrappers(msg); stripped != strings.TrimSpace(msg) {
		l.ProviderMessage = stripped
	}

	lower := strings.ToLower(msg)
	if l.Code != "" {
		l.Scope = codeScopes[strings.ToLower(l.Code)]
	}
	if l.Scope == "" {
		for _, c := range codeOrder {
			if strings.Contains(lower, c) {
				l.Scope = codeScopes[c]
				break
			}
		}
	}
	if l.Scope == "" && kind == KindQuota && (status == "402" || reHTTP402.MatchString(lower)) {
		l.Scope = ScopeCredits
	}
	// A phrase that states the window says more than a code that does not
	// ("plan"), or than nothing.
	if l.Scope == "" || l.Scope == ScopePlan {
		switch {
		case reHourWindow.MatchString(lower):
			h, _ := strconv.Atoi(reHourWindow.FindStringSubmatch(lower)[1])
			l.Scope, l.WindowMinutes = ScopeWindow, h*60
		case reWeeklyLimit.MatchString(lower):
			l.Scope = ScopeWeekly
		case reMonthLimit.MatchString(lower):
			l.Scope = ScopeMonthly
		case reDailyLimit.MatchString(lower):
			l.Scope = ScopeDaily
		}
	}
	if l.Scope == "" && kind == KindRateLimit {
		l.Scope = ScopeRate
	}
	return l
}

// stripWrappers removes leading agent and SDK prefixes from msg.
func stripWrappers(msg string) string {
	s := strings.TrimSpace(msg)
	for {
		loc := reWrapper.FindStringIndex(s)
		if loc == nil {
			return s
		}
		s = strings.TrimSpace(s[loc[1]:])
	}
}
