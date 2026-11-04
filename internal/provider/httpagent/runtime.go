package httpagent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

var _ provider.RuntimeSession = (*session)(nil)

// Runtime status and usage for the HTTP-engine transports (MADR 0138 Phase 11),
// each the engine's own answer, passed through unchanged (MADR 0208 D12).
//
// The usage half is shared: the session object, `GET /session/{id}`, carries
// the session's cost (a JSON number in USD) and token totals.
//
// The status half is not shared, because what an engine can say about an
// account differs. That goes through [RuntimeDialect], following the
// AuthDialect pattern above it.

// runtimeProbeTimeout bounds a status or usage read.
//
// These run from a slash command with a person waiting, and the engine is on
// loopback, so this is generous rather than tuned.
const runtimeProbeTimeout = 5 * time.Second

// RuntimeDialect is optionally implemented by a [Dialect] whose engine can
// report account-level plan or quota state.
//
// Optional, and not a required part of Dialect, because opencode genuinely has
// nothing to report: its published API carries no plan usage, billing or quota
// path. A dialect that does not implement this still gets a working `/status` —
// it just says what the engine does and does not publish.
type RuntimeDialect interface {
	// RuntimeStatus returns the engine's answer about the account behind it:
	// the response body as the engine sent it, and the request that got it
	// (MADR 0208 D12). An error is shown as text by the caller, never
	// returned: internal/session's cmdRuntime propagates an error instead of
	// showing it, so an error here would make /status say nothing.
	RuntimeStatus(ctx context.Context, api API) (provider.Result, error)
}

// RuntimeStatus implements [provider.RuntimeSession].
func (s *session) RuntimeStatus(ctx context.Context) (provider.Result, error) {
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
func runtimeStatus(ctx context.Context, d Dialect, api API, engineLine string) provider.Result {
	rd, ok := d.(RuntimeDialect)
	if !ok {
		// Not a failure: an engine with no account usage publishes none, and
		// saying so is different from being broken.
		return provider.Result{Text: engineLine + " · this engine publishes no plan usage"}
	}
	res, err := rd.RuntimeStatus(ctx, api)
	if err != nil {
		return provider.Result{Text: engineLine + " · status unavailable: " + clipErr(err, 160)}
	}
	if len(res.Body) == 0 && strings.TrimSpace(res.Text) == "" {
		return provider.Result{Text: engineLine + " · this engine publishes no plan usage"}
	}
	return res
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

// RuntimeUsage implements [provider.RuntimeSession]: the engine's session
// object, as the engine sent it, for the phone to show as JSON (MADR 0208 D12;
// PLAN P5 Deviation 5). It carries the session's cost and token totals.
func (s *session) RuntimeUsage(ctx context.Context) (provider.Result, error) {
	callCtx, cancel := context.WithTimeout(ctx, runtimeProbeTimeout)
	defer cancel()
	return runtimeUsage(callCtx, s.p.api, s.AgentSessionID()), nil
}

// runtimeUsage is RuntimeUsage's engine half, split out as runtimeStatus is so
// a test can drive it with a stub API. Like runtimeStatus it returns no error:
// a failure comes back as text, or /usage would show nothing.
func runtimeUsage(ctx context.Context, api API, agentID string) provider.Result {
	if agentID == "" {
		return provider.Result{Text: "Usage is unavailable: this session has no engine session id."}
	}
	path := sessionUsagePath(agentID)
	var body json.RawMessage
	if err := api(ctx, "GET", path, nil, &body); err != nil {
		return provider.Result{Text: "Usage is unavailable: " + clipErr(err, 160)}
	}
	return provider.Result{Method: "GET " + path, Body: body}
}

// sessionUsagePath is the one endpoint these totals come from.
//
// kilo also publishes /session/{id}/model-usage with the same totals plus a
// per-model breakdown. It is deliberately unused: the session object already
// carries the numbers, and a second source for one figure is how the two come
// to disagree. Named here so a change of mind has to edit something explicit.
func sessionUsagePath(agentID string) string { return "/session/" + agentID }
