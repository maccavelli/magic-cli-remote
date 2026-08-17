// Package agenterr classifies agent/provider error messages so clients can
// render actionable cards ("quota exceeded, resets at 3pm") instead of raw
// provider dumps. Classification is best-effort substring/regex matching over
// the message text — the only signal CLI agents forward.
//
// Present builds a short natural-language Error string for the phone while
// still leaving ErrorKind/RetryAt for the limit card. Engine stderr scrapers
// (goose silent 429 backoff) feed the same path via ExtractText + Present.
package agenterr

import (
	"encoding/json"
	"errors"
	"io/fs"
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
	// KindPermission is an OS-level permission denial (EPERM/EACCES): file
	// modes, a sandbox policy (codex Seatbelt/bwrap), or macOS privacy
	// protection (TCC). Retrying will not help; the remedy is a mode
	// change, chmod, or a privacy grant (MADR 0069 D4).
	KindPermission Kind = "permission"
	// KindAuth is a credentials rejection from the model provider (401/403,
	// invalid or expired API key/token). Retrying will not help; the remedy
	// is re-authenticating the agent CLI on the daemon host (MADR 0073
	// follow-up: grok wraps these as JSON-RPC "Internal error" with the real
	// cause in `data`).
	KindAuth Kind = "auth"
	// KindServer is a provider-side server failure (500/502/504, "internal
	// server error", "bad gateway"): usually transient, worth retrying in a
	// moment — grok's own retry classifier treats exactly this set as
	// retryable. 503/529 stay KindRateLimit (capacity, not breakage).
	KindServer Kind = "server"
)

// IsPermission reports whether err carries an OS permission denial
// anywhere in its chain. syscall.EPERM and syscall.EACCES both satisfy
// errors.Is against fs.ErrPermission (MADR 0069 P1).
func IsPermission(err error) bool {
	return errors.Is(err, fs.ErrPermission)
}

// Classification is the result of Classify / Present.
type Classification struct {
	Kind Kind
	// ResetAt is when the limit is expected to lift, when the message said
	// so (zero otherwise).
	ResetAt time.Time
	// Message is a short natural-language description safe for the chat
	// transcript. Empty when Kind is KindNone and the raw text was empty.
	Message string
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
	"gousagelimiterror", // OpenCode Go weekly/monthly hard block
	"upgrade your plan",
}

var rateWords = []string{
	"rate limit",
	"rate_limit",
	"ratelimit",        // also catches Gemini's camelCase rateLimitExceeded
	"too many request", // singular on purpose: Zen sends "too many request"
	"too many tokens",  // xAI TPM overflow
	"tokens per minute",
	"tokens per day",
	"requests per minute",
	"requests per day",
	"resource has been exhausted",
	"resource_exhausted",
	"resource exhausted",
	"throttl",
	"overloaded_error",
	"overloaded", // Anthropic 529 overloaded_error prose
	"service unavailable",
	"capacity exceeded",
	"server is busy",
	"temporarily unavailable",
	"backing off",  // goose/provider retry sleep after 429/quota
	"retry_delay",  // goose Debug: RateLimitExceeded { retry_delay: Some(3600s) }
	"rate limited", // OpenCode session.status retry message
}

// reHTTPLimit matches HTTP status codes that mean "back off" as standalone
// tokens, not any substring containing those digits (ports, trace ids).
// 429 = Too Many Requests, 529 = Anthropic overloaded, 503 = unavailable.
var reHTTPLimit = regexp.MustCompile(`\b(429|529|503)\b`)

// Credentials rejections — phrasings captured live from grok (the 401 probe
// in MADR 0073's follow-up: "Unauthorized (401) from https://…: Invalid or
// expired credentials"), plus the standard Anthropic/OpenAI/Gemini auth
// error vocabularies.
var authWords = []string{
	"unauthorized",
	"unauthenticated",
	"invalid or expired credentials",
	"expired credentials",
	"invalid credentials",
	"invalid api key",
	"invalid x-api-key",
	"incorrect api key",
	"api key not valid",
	"invalid_api_key",
	"authentication_error",
	"authentication error",
	"authentication failed",
	"authentication required",
	"not authenticated",
	"forbidden",
	"permission_error", // Anthropic 403 "your API key does not have permission"
	"token expired",
	"login required",
	"please sign in",
	"credentials rejected",
	// Revocation and token-validity vocabulary (MADR 0074 W4). A revoked key
	// reads as "configured" to the credential probe — it is present on disk —
	// so the turn error is the only place the difference shows up.
	"revoked",
	"invalid_token",
	"invalid token",
	"invalid authentication",
	"api key expired",
	"key has been disabled",
}

// reHTTPAuth matches auth-failure HTTP statuses as standalone tokens.
var reHTTPAuth = regexp.MustCompile(`\b(401|403)\b`)

// Provider-side server failures. Matches grok's own retryable-error set
// (recovered from the binary's classifier string: "status 500|error
// 500|internal server error|bad gateway|status 502|gateway timeout|status
// 504"); 503/529 deliberately stay in rateWords/reHTTPLimit.
var serverWords = []string{
	"internal server error",
	"internal error",
	"bad gateway",
	"gateway timeout",
	"upstream connect error",
	"server error",
}

// reHTTPServer matches server-failure HTTP statuses as standalone tokens.
var reHTTPServer = regexp.MustCompile(`\b(500|502|504)\b`)

// strongRateWords name rate limiting explicitly, as opposed to the weak
// transient vocabulary ("temporarily unavailable", "server is busy") that
// 5xx templates also use — grok's own 502 copy is "The model is temporarily
// unavailable. Please try again in a moment. (HTTP 502)." and should read as
// a server failure, not a rate limit. An explicit 500/502/504 token
// reclassifies a weak-word match to KindServer; these strong words (or an
// explicit 429/529/503) keep it KindRateLimit.
var strongRateWords = []string{
	"rate limit",
	"rate_limit",
	"ratelimit",
	"rate limited",
	"too many request",
	"too many tokens",
	"tokens per",
	"requests per",
	"throttl",
	"overloaded",
	"backing off",
	"retry_delay",
	"resource has been exhausted",
	"resource_exhausted",
	"resource exhausted",
	"capacity exceeded",
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
	"gousagelimiterror",
	"freeusagelimiterror",
	"usage limit",
	"weekly",
	"monthly",
	"daily limit",
	"weekly limit",
	"monthly limit",
}

// Classify inspects an agent/provider error message. now anchors relative
// reset phrases ("try again in 20s"); pass time.Now() outside tests.
// Message is left empty — call Present when the phone needs copy.
func Classify(msg string, now time.Time) Classification {
	msg = ExtractText(msg)
	m := strings.ToLower(msg)
	quota := containsAny(m, quotaWords)
	rate := containsAny(m, rateWords) || reHTTPLimit.MatchString(m)
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
		// A weak transient word alongside an explicit server-failure token
		// (500/502/504, no 429/529/503, no strong rate vocabulary) is a 5xx
		// template, not throttling — see strongRateWords.
		if reHTTPServer.MatchString(m) && !reHTTPLimit.MatchString(m) && !containsAny(m, strongRateWords) {
			kind = KindServer
		} else {
			kind = KindRateLimit
		}
	// Auth before server: grok's 401 arrives wrapped in a JSON-RPC
	// "Internal error" envelope, so after data extraction the text contains
	// both "internal error" (server vocabulary) and "unauthorized" (auth) —
	// the credentials rejection is the actionable cause, the wrapper isn't.
	case containsAny(m, authWords) || reHTTPAuth.MatchString(m):
		kind = KindAuth
	case containsAny(m, serverWords) || reHTTPServer.MatchString(m):
		kind = KindServer
	case isPermissionMsg(m):
		return Classification{Kind: KindPermission, Message: formatPermission(msg)}
	default:
		return Classification{}
	}
	reset := parseReset(msg, now)
	return Classification{Kind: kind, ResetAt: reset}
}

// Present classifies msg and fills Message with short natural-language copy
// suitable for event.Event.Error. Prefer this at TypeError emission sites.
func Present(msg string, now time.Time) Classification {
	msg = ExtractText(msg)
	cls := Classify(msg, now)
	// Long silent retry sleeps without explicit quota/rate words still need
	// a card — promote so every provider's stderr/file scraper shares one path.
	if cls.Kind == KindNone && LooksLikeLongBackoff(msg) {
		cls.Kind = KindRateLimit
		if cls.ResetAt.IsZero() {
			cls.ResetAt = parseReset(msg, now)
		}
		msg = "Provider is backing off after a rate limit or quota error: " + msg
	}
	if cls.Kind == KindNone && strings.TrimSpace(msg) == "" {
		return cls
	}
	if cls.Kind == KindNone {
		if s := formatToolSchema(msg); s != "" {
			cls.Message = s
			return cls
		}
		// Still clean opaque dumps so raw JSON-RPC blobs don't land in chat.
		cls.Message = cleanRaw(msg)
		return cls
	}
	if cls.Kind == KindPermission {
		cls.Message = formatPermission(msg)
		return cls
	}
	if cls.Kind == KindAuth {
		cls.Message = formatAuth(msg)
		return cls
	}
	cls.Message = formatLimit(cls.Kind, cls.ResetAt, msg, now)
	return cls
}

// IsLimit reports whether Present would classify msg as a usage/rate limit.
// Providers use this before aborting a silent mid-turn hang (goose file logs,
// codex/grok stderr, OpenCode long session.status retries).
func IsLimit(msg string, now time.Time) bool {
	cls := Present(msg, now)
	return cls.Kind == KindQuota || cls.Kind == KindRateLimit
}

// LongBackoffMin is the shortest silent retry delay that should end a turn
// rather than leave the phone on "running". Matches goose's 3600s backoff and
// OpenCode session.status next delays of a minute or more.
const LongBackoffMin = 60 * time.Second

// LooksLikeLongBackoff reports whether line describes a provider retry sleep
// of at least LongBackoffMin. Covers goose prose ("Backing off for 3600s")
// and Debug forms ("retry_delay: Some(3600s)").
func LooksLikeLongBackoff(line string) bool {
	for _, re := range []*regexp.Regexp{reBackoffSecs, reRetryDelaySome} {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		sec, err := strconv.ParseFloat(m[1], 64)
		if err == nil && sec >= LongBackoffMin.Seconds() {
			return true
		}
	}
	return false
}

// reBackoffSecs captures seconds from "Backing off for 3600s before retry".
// Unit-less or trailing "s" only — longer units go through parseReset.
var reBackoffSecs = regexp.MustCompile(`(?i)backing off for\s+(\d+(?:\.\d+)?)\s*s`)

// reRetryDelaySome matches goose/provider Debug: retry_delay: Some(3600s).
var reRetryDelaySome = regexp.MustCompile(`(?i)retry_delay:\s*Some\((\d+(?:\.\d+)?)s\)`)

// ExtractText pulls a human-readable message out of structured engine logs
// (goose JSON lines, nested OpenAI/Anthropic error objects). Returns the
// input unchanged when no structured body is found.
func ExtractText(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	// Nested JSON error bodies often sit after a short prefix
	// ("Provider request failed … Payload: {…}").
	if i := strings.Index(raw, "{"); i >= 0 && i < len(raw)-1 {
		if extracted := extractJSONMessage(raw[i:]); extracted != "" {
			return extracted
		}
	}
	if strings.HasPrefix(raw, "{") {
		if extracted := extractJSONMessage(raw); extracted != "" {
			return extracted
		}
	}
	// Goose/Rust Debug payloads: String("Weekly usage limit reached…") when
	// the embedded body is not valid JSON (Some(Object {…})).
	if m := reRustDebugString.FindStringSubmatch(raw); m != nil {
		if unquoted := unquoteRustString(m[1]); unquoted != "" && isUsefulProse(unquoted) {
			return unquoted
		}
	}
	return raw
}

// reRustDebugString pulls the first Debug-format String("…") body. Goose
// logs 429 payloads as Rust Debug, not JSON, so nested error.message is only
// reachable this way.
var reRustDebugString = regexp.MustCompile(`String\("((?:\\.|[^"\\])*)"\)`)

func unquoteRustString(s string) string {
	// Minimal unescape for the common \" and \\ sequences in Debug output.
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case '"', '\\':
				b.WriteByte(s[i+1])
				i++
				continue
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return strings.TrimSpace(b.String())
}

func extractJSONMessage(js string) string {
	var top map[string]any
	if err := json.Unmarshal([]byte(js), &top); err != nil {
		return ""
	}
	// goose structured log: {"fields":{"message":"…"}, …}
	if fields, ok := top["fields"].(map[string]any); ok {
		if m, ok := fields["message"].(string); ok && strings.TrimSpace(m) != "" {
			// Recurse: the message itself may embed a Payload JSON object.
			// Prefer the outer message when the nested extract is weaker
			// (placeholder "..." bodies) so codes like insufficient_quota
			// on the outer object still classify.
			outer := strings.TrimSpace(m)
			if nested := extractJSONMessageFromPayload(outer); nested != "" && isUsefulProse(nested) {
				return nested
			}
			if nested := ExtractText(outer); nested != outer && isUsefulProse(nested) {
				return nested
			}
			return outer
		}
	}
	// OpenAI / OpenCode / Anthropic shapes — keep code/type alongside the
	// human message so classification still sees insufficient_quota when
	// the message body is a placeholder.
	if errObj, ok := top["error"].(map[string]any); ok {
		var parts []string
		if m, ok := errObj["message"].(string); ok {
			if t := strings.TrimSpace(m); t != "" && t != "..." {
				parts = append(parts, t)
			}
		}
		if c, ok := errObj["code"].(string); ok {
			if t := strings.TrimSpace(c); t != "" {
				parts = append(parts, t)
			}
		}
		if typ, ok := errObj["type"].(string); ok {
			if t := strings.TrimSpace(typ); t != "" {
				parts = append(parts, t)
			}
		}
		// JSON-RPC error frames put the real cause in `data`, not `message`
		// — grok's 401 is {"error":{"code":-32603,"message":"Internal
		// error","data":"Unauthorized (401) from …"}} (captured live, MADR
		// 0073 follow-up). Without this the classifier only ever saw the
		// generic wrapper.
		if d := jsonDataText(errObj["data"]); d != "" {
			parts = append(parts, d)
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}
	if m, ok := top["message"].(string); ok && strings.TrimSpace(m) != "" {
		msg := strings.TrimSpace(m)
		// Bare JSON-RPC error object (the acp-go-sdk RequestError.Error()
		// rendering: {"code":…,"message":…,"data":…}): same rule as above —
		// `data` carries the cause, `message` is usually the generic
		// "Internal error" wrapper.
		if d := jsonDataText(top["data"]); d != "" {
			return msg + " " + d
		}
		return msg
	}
	return ""
}

// jsonDataText renders a JSON-RPC error `data` value as prose: strings pass
// through, objects are mined for a nested message (recursively re-using the
// same extractors), anything else is dropped.
func jsonDataText(v any) string {
	switch d := v.(type) {
	case string:
		return strings.TrimSpace(d)
	case map[string]any:
		if b, err := json.Marshal(d); err == nil {
			if nested := extractJSONMessage(string(b)); nested != "" {
				return nested
			}
		}
	}
	return ""
}

// extractJSONMessageFromPayload finds a JSON object embedded after a prose
// prefix ("… Payload: {…}") and pulls the error message from it.
func extractJSONMessageFromPayload(raw string) string {
	i := strings.Index(raw, "{")
	if i < 0 {
		return ""
	}
	return extractJSONMessage(raw[i:])
}

func formatLimit(kind Kind, reset time.Time, raw string, now time.Time) string {
	cleaned := cleanRaw(raw)
	// Prefer a provider sentence that already explains itself when it is
	// short and not a pure status token. For server errors, a bare label
	// ("Internal error", "bad gateway") clears the generic prose bar but is
	// exactly the opaque wrapper this classification exists to improve —
	// demand a real sentence before trusting it over the canonical copy.
	useful := isUsefulProse(cleaned)
	if useful && kind == KindServer && len(cleaned) < 40 {
		useful = false
	}
	if useful {
		if !reset.IsZero() && !containsResetHint(cleaned) {
			return cleaned + " " + resetSuffix(reset, now)
		}
		return cleaned
	}
	var b strings.Builder
	switch kind {
	case KindQuota:
		b.WriteString("The model provider hit a usage or billing limit.")
	case KindRateLimit:
		if reHTTPLimit.MatchString(raw) {
			code := reHTTPLimit.FindString(raw)
			switch code {
			case "529":
				b.WriteString("The model provider is overloaded (HTTP 529).")
			case "503":
				b.WriteString("The model provider is temporarily unavailable (HTTP 503).")
			default:
				b.WriteString("Too many requests to the model provider (HTTP 429).")
			}
		} else {
			b.WriteString("The model provider is rate-limiting requests.")
		}
	case KindServer:
		if code := reHTTPServer.FindString(raw); code != "" {
			b.WriteString("The model provider had a server error (HTTP " + code + ").")
		} else {
			b.WriteString("The model provider had a server error.")
		}
	default:
		b.WriteString(cleaned)
	}
	if !reset.IsZero() {
		b.WriteByte(' ')
		b.WriteString(resetSuffix(reset, now))
	} else if kind == KindQuota {
		b.WriteString(" Wait for the limit to reset, or switch models.")
	} else if kind == KindRateLimit {
		b.WriteString(" Wait a moment and try again, or switch models.")
	} else if kind == KindServer {
		b.WriteString(" This is usually temporary — try again in a moment, or switch models.")
	}
	return b.String()
}

// formatAuth builds the copy for a credentials rejection. Unlike limits, the
// provider's own prose here tends to leak internal parameters (grok's 401
// carries "auth_kind=none, x_xai_token_auth=…"), so the canonical sentence
// leads and only a short, clean provider clause is kept as context.
func formatAuth(raw string) string {
	code := reHTTPAuth.FindString(raw)
	var b strings.Builder
	if code != "" {
		b.WriteString("The model provider rejected the agent's credentials (HTTP " + code + ").")
	} else {
		b.WriteString("The model provider rejected the agent's credentials.")
	}
	b.WriteString(" Sign in to the agent CLI on the host again, then retry.")
	return b.String()
}

func formatPermission(raw string) string {
	cleaned := cleanRaw(raw)
	if isUsefulProse(cleaned) {
		return cleaned
	}
	return "Blocked by OS or sandbox permissions."
}

func resetSuffix(reset, now time.Time) string {
	d := reset.Sub(now)
	if d < 0 {
		return "The limit should have already reset — try again."
	}
	switch {
	case d < time.Minute:
		return "Try again in about " + formatDur(d) + "."
	case d < 48*time.Hour:
		return "Resets in about " + formatDur(d) + "."
	default:
		return "Resets around " + reset.Local().Format("Jan 2") + " (about " + formatDur(d) + ")."
	}
}

func formatDur(d time.Duration) string {
	if d < time.Second {
		return "a second"
	}
	if d < time.Minute {
		s := int(d.Round(time.Second) / time.Second)
		if s <= 1 {
			return "1 second"
		}
		return strconv.Itoa(s) + " seconds"
	}
	if d < time.Hour {
		m := int(d.Round(time.Minute) / time.Minute)
		if m <= 1 {
			return "1 minute"
		}
		return strconv.Itoa(m) + " minutes"
	}
	if d < 48*time.Hour {
		h := int(d.Round(time.Hour) / time.Hour)
		if h <= 1 {
			return "1 hour"
		}
		return strconv.Itoa(h) + " hours"
	}
	days := int(d.Round(24*time.Hour) / (24 * time.Hour))
	if days <= 1 {
		return "1 day"
	}
	return strconv.Itoa(days) + " days"
}

func containsResetHint(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "reset") ||
		strings.Contains(l, "try again") ||
		strings.Contains(l, "retry") ||
		strings.Contains(l, "backing off")
}

func isUsefulProse(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 12 {
		return false
	}
	// Pure status / code tokens are not useful alone.
	if reHTTPLimit.MatchString(s) && len(s) < 40 && !strings.Contains(s, " ") {
		return false
	}
	// Heavy JSON dumps are not prose.
	if strings.Count(s, "{") >= 2 || strings.Count(s, `"`) > 8 {
		return false
	}
	// Need at least a few letters.
	letters := 0
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			letters++
		}
	}
	return letters >= 8
}

// reToolSchemaUnion matches the upstream rejection of a tool whose JSON Schema
// uses a union at the top level. Anthropic's Messages API forbids
// oneOf/allOf/anyOf there, and an engine forwards a tool's schema verbatim —
// so a single bad tool fails every turn on any Anthropic-routed model while
// leaving other vendors working, which reads to a user as "this model is
// broken". The capture is the tool's position in the request's tool array.
var reToolSchemaUnion = regexp.MustCompile(
	`tools\.(\d+)\.[\w.]*input_schema[^:]*:\s*input_schema does not support (?:oneOf, allOf, or anyOf|anyOf, allOf, or oneOf)`)

// formatToolSchema rewrites a tool-schema rejection into something the user can
// act on, or returns "" when msg is not one.
//
// It stays KindNone deliberately: this is not a quota, an auth failure or a
// transient server error, and nothing should retry it. Only the copy changes,
// because the raw upstream string names an array index the user has no way to
// resolve to a tool.
func formatToolSchema(msg string) string {
	m := reToolSchemaUnion.FindStringSubmatch(msg)
	if m == nil {
		return ""
	}
	return "A tool's input schema uses oneOf/allOf/anyOf at the top level, which " +
		"models on the Anthropic API reject — so every turn on this model fails " +
		"before it starts (tool #" + m[1] + " in this request). This is almost " +
		"always an MCP server's tool definition, not the agent's own: disable " +
		"that server for this session, switch to a non-Anthropic model, or fix " +
		"the tool's schema to be a single top-level object."
}

func cleanRaw(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s
	}
	// First line only — multi-line stack dumps / JSON pretty-print.
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = strings.TrimSpace(s[:i])
	}
	// Collapse whitespace.
	s = strings.Join(strings.Fields(s), " ")
	// Cap length (rune-safe enough for ASCII-heavy provider text).
	const max = 400
	if len(s) > max {
		// Walk back to a rune start.
		cut := max
		for cut > 0 && s[cut]&0xc0 == 0x80 {
			cut--
		}
		s = s[:cut] + "…"
	}
	return s
}

// isPermissionMsg matches OS permission denials in forwarded agent error
// text (MADR 0069 D4). Deliberately narrow: "operation not permitted"
// (EPERM's strerror — the macOS TCC/Seatbelt shape) and errno tokens are
// unambiguous; the bare phrase "permission denied" (EACCES) is only
// accepted alongside a path-ish "/" so a provider's prose about a *user*
// denying a tool-approval request cannot misclassify as an OS error.
func isPermissionMsg(m string) bool {
	if strings.Contains(m, "operation not permitted") ||
		strings.Contains(m, "eperm") ||
		strings.Contains(m, "eacces") {
		return true
	}
	return strings.Contains(m, "permission denied") && strings.Contains(m, "/")
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

// Goose / provider retry loops log "Backing off for 3600s before retry"
// without any other reset phrase — treat that as the retry-at hint.
var reBackoff = regexp.MustCompile(`(?i)backing off for\s+(\d+(?:\.\d+)?)\s*(ms|s|secs?|seconds?|m|mins?|minutes?|h|hrs?|hours?)?\b`)

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
	if m := reRetryDelaySome.FindStringSubmatch(msg); m != nil {
		if d := unitDuration(m[1], "s"); d > 0 {
			return now.Add(d)
		}
	}
	if m := reBackoff.FindStringSubmatch(msg); m != nil {
		unit := m[2]
		if unit == "" {
			unit = "s"
		}
		if d := unitDuration(m[1], unit); d > 0 {
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
