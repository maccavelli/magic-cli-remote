package httpagent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

var _ provider.RuntimeSession = (*session)(nil)

// Runtime status and usage for the HTTP-engine transports (MADR 0138 Phase 11).
//
// The usage half is shared. kilo 7.5.6 and opencode publish byte-identical
// schemas for the session object's cost and token totals — verified against
// each engine's own OpenAPI document, read live rather than assumed — so one
// read serves both dialects:
//
//	GET /session/{id} -> {"cost": number,
//	                      "tokens": {"input","output","reasoning",
//	                                 "cache": {"read","write"}}}
//
// The status half is not shared, because what an engine can say about an
// account differs: kilo publishes plan usage and opencode publishes none. That
// goes through [RuntimeDialect], following the AuthDialect pattern above it.

// runtimeProbeTimeout bounds a status or usage read.
//
// These run from a slash command with a person waiting, and both engines are on
// loopback, so this is generous rather than tuned.
const runtimeProbeTimeout = 5 * time.Second

// sessionTotals is the cost and token accounting an engine keeps on the session
// object itself.
//
// `cost` is a JSON **number in USD** — not cents, and not grok's 1e10 ticks.
// Three transports in this repository, three money units; each is named at its
// decode site so no one has to remember which is which.
type sessionTotals struct {
	Cost   float64      `json:"cost"`
	Tokens sessionTokid `json:"tokens"`
}

type sessionTokid struct {
	Input     float64 `json:"input"`
	Output    float64 `json:"output"`
	Reasoning float64 `json:"reasoning"`
	Cache     struct {
		Read  float64 `json:"read"`
		Write float64 `json:"write"`
	} `json:"cache"`
}

// RuntimeDialect is optionally implemented by a [Dialect] whose engine can
// report account-level plan or quota state.
//
// Optional, and not a required part of Dialect, because opencode genuinely has
// nothing to report: its published API carries no plan usage, billing or quota
// path. A dialect that does not implement this still gets a working `/status` —
// it just says what the engine does and does not publish.
type RuntimeDialect interface {
	// RuntimeStatus returns one line about the engine and the account behind
	// it. Implementations report their own unavailability as text with a nil
	// error: internal/session's cmdRuntime propagates an error to its caller
	// instead of showing it, so an error here makes /status say nothing.
	RuntimeStatus(ctx context.Context, api API) (string, error)
}

// RuntimeStatus implements [provider.RuntimeSession].
func (s *session) RuntimeStatus(ctx context.Context) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, runtimeProbeTimeout)
	defer cancel()
	return runtimeStatus(callCtx, s.p.dialect, s.p.api, s.engineLine()), nil
}

// runtimeStatus is the dialect-dispatch half, split out from the method so it
// can be driven with a stub dialect and a stub API. p.api is a method, so a
// test cannot otherwise reach either branch (the seam Phase 9 needed for the
// same reason).
//
// It returns no error by design. internal/session's cmdRuntime propagates a
// returned error to its caller instead of showing it, so every failure here has
// to come back as text or `/status` silently does nothing.
func runtimeStatus(ctx context.Context, d Dialect, api API, engineLine string) string {
	rd, ok := d.(RuntimeDialect)
	if !ok {
		// Not a failure: opencode-shaped engines genuinely publish no account
		// usage, and saying so is different from being broken.
		return engineLine + " · this engine publishes no plan usage"
	}
	msg, err := rd.RuntimeStatus(ctx, api)
	if err != nil {
		return engineLine + " · status unavailable: " + clipErr(err, 160)
	}
	if strings.TrimSpace(msg) == "" {
		return engineLine + " · this engine publishes no plan usage"
	}
	return msg
}

// engineLine describes the session's own runtime, which every dialect can
// answer without asking the engine anything.
func (s *session) engineLine() string {
	parts := []string{titleID(s.p.dialect.ID())}
	if m := s.Model(); m != "" {
		parts = append(parts, "model "+m)
	}
	if a := s.Agent(); a != "" {
		parts = append(parts, "agent "+a)
	}
	return strings.Join(parts, " · ")
}

// titleID capitalises a provider id for display. strings.Title is deprecated
// and the ids are ASCII single words, so this is the whole job.
func titleID(id provider.ID) string {
	s := string(id)
	if s == "" {
		return "Engine"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// RuntimeUsage implements [provider.RuntimeSession] from the session object's
// own totals.
func (s *session) RuntimeUsage(ctx context.Context) (string, error) {
	agentID := s.AgentSessionID()
	if agentID == "" {
		return "Usage is unavailable: this session has no engine session id.", nil
	}
	callCtx, cancel := context.WithTimeout(ctx, runtimeProbeTimeout)
	defer cancel()

	totals, err := fetchSessionTotals(callCtx, s.p.api, agentID)
	if err != nil {
		return "Usage is unavailable: " + clipErr(err, 160), nil
	}
	return formatSessionTotals(totals), nil
}

// sessionUsagePath is the one endpoint these totals come from.
//
// kilo also publishes /session/{id}/model-usage with the same totals plus a
// per-model breakdown. It is deliberately unused: the session object already
// carries the numbers, and a second source for one figure is how the two come
// to disagree. Named here so a change of mind has to edit something explicit.
func sessionUsagePath(agentID string) string { return "/session/" + agentID }

func fetchSessionTotals(ctx context.Context, api API, agentID string) (sessionTotals, error) {
	var totals sessionTotals
	if err := api(ctx, "GET", sessionUsagePath(agentID), nil, &totals); err != nil {
		return sessionTotals{}, err
	}
	return totals, nil
}

// formatSessionTotals renders the engine's cumulative accounting.
func formatSessionTotals(t sessionTotals) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Usage: %.0f input + %.0f output tokens", t.Tokens.Input, t.Tokens.Output)
	if t.Tokens.Cache.Read > 0 {
		fmt.Fprintf(&b, " (%.0f cached)", t.Tokens.Cache.Read)
	}
	if t.Tokens.Reasoning > 0 {
		fmt.Fprintf(&b, " · %.0f reasoning", t.Tokens.Reasoning)
	}
	// Cost is USD from the engine. Four decimals under a dollar for the same
	// reason as grok's: a real session commonly costs fractions of a cent, and
	// two decimals would render it as $0.00.
	switch {
	case t.Cost >= 1:
		fmt.Fprintf(&b, " · $%.2f", t.Cost)
	case t.Cost > 0:
		fmt.Fprintf(&b, " · $%.4f", t.Cost)
	default:
		// Zero here is a reported zero, not an absent field: both engines make
		// `cost` required in their schemas.
		b.WriteString(" · $0.00")
	}
	return b.String()
}
