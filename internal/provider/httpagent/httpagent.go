// Package httpagent is the HTTP + SSE counterpart of acpagent: a provider
// transport for CLI agents that expose a local HTTP server, driven through a
// single long-lived engine instead of per-session stdio subprocesses.
//
// The package owns everything transport-generic — engine supervision (spawn,
// health poll, death monitor, respawn, shutdown), the SSE pump with
// reconnect, the session registry and event demux, the JSON REST helper,
// event-channel delivery guarantees, turn and permission bookkeeping, the
// stall watchdog, and permission expiry. Everything agent-specific — REST
// paths and body shapes, the SSE event vocabulary, model conventions — lives
// behind [Dialect] and [DialectSession], mirroring how acpagent.Spec keeps
// agent knowledge out of the shared ACP machinery.
//
// OpenCode (`opencode serve`) is the first dialect, implemented in
// internal/provider/opencode. See docs/0011-opencode-provider-plan.md,
// "Performance addendum", for why a shared HTTP engine beats per-session
// subprocesses.
package httpagent

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/provider/acpagent"
)

// Config reuses the shared agent config shape (Bin, DefaultCWD, Model,
// AlwaysApprove, PermissionTimeout, TurnStallNotice are honoured; Args and
// Prewarm are ACP-specific and ignored here).
type Config = acpagent.Config

// API performs one JSON request against the engine. The path is appended to
// the engine base URL; a non-2xx response is returned as an error carrying a
// clipped body. Instances handed to dialects are already bound to the
// current (or just-booted) engine.
type API func(ctx context.Context, method, path string, body, out any) error

// Dialect is the agent-specific half of the transport at engine scope: how
// to launch the server, where its health and event streams live, and how to
// parse its SSE frames. One Dialect instance backs one Provider and may hold
// engine-level state (e.g. a default model resolved at boot).
type Dialect interface {
	// ID is the provider identity this dialect implements.
	ID() provider.ID
	// DefaultBin is the executable name used when Config.Bin is empty.
	DefaultBin() string
	// ServeArgs builds the argv (after the binary) that starts the engine
	// bound to 127.0.0.1:port.
	ServeArgs(port int) []string
	// HealthPath is polled with GET until it returns 200 after spawn.
	HealthPath() string
	// EventsPath is the SSE stream carrying every session's events.
	EventsPath() string
	// AfterBoot runs once per engine (re)start with an API bound to the new
	// engine — e.g. to resolve a usable default model from its catalog.
	// Best-effort: failures must be handled (logged) by the dialect.
	AfterBoot(ctx context.Context, api API)
	// DecodeFrame parses one SSE `data:` payload into an event type, its
	// properties, and the agent-side session id it belongs to. ok=false (or
	// an empty session id) skips the frame.
	DecodeFrame(data []byte) (typ string, props json.RawMessage, agentSessionID string, ok bool)
	// NewSession creates the per-session protocol adapter. Called by Start
	// before Create/Resume, so the host's AgentSessionID is not yet set.
	NewSession(h Host) DialectSession
}

// DialectSession is the agent-specific half of one session: its REST
// operations and the translation of its SSE events into daemon events.
// Contexts passed in already carry transport-owned timeouts.
type DialectSession interface {
	// Create makes a new server-side session and returns its agent id.
	Create(ctx context.Context, opts provider.StartOptions) (agentSessionID string, err error)
	// Resume verifies an existing server-side session and returns its
	// (possibly normalized) agent id.
	Resume(ctx context.Context, agentSessionID string) (string, error)
	// Replay re-emits the session's recorded conversation via
	// [Host.EmitReplay] to rebuild the daemon history ring after a resume.
	// Best-effort; called before the manager pump attaches.
	Replay(ctx context.Context)
	// Prompt submits one user turn. It must return once the turn is enqueued
	// (async): the turn itself streams back over SSE. The host emits the
	// user_message/running events on success.
	Prompt(ctx context.Context, parts []provider.Content) error
	// Abort cancels the in-flight turn server-side.
	Abort(ctx context.Context) error
	// RespondPermission answers a permission request server-side. cancelled
	// means reject regardless of optionID (also used by the expiry
	// fail-safe). Pending-set bookkeeping is the host's job.
	RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool) error
	// HandleEvent translates one SSE event for this session into daemon
	// events via [Host.Emit] and the host's turn/permission hooks.
	HandleEvent(typ string, props json.RawMessage)
}

// Host is the transport-generic half of a session as seen by its
// DialectSession: identity, config, the engine API, event emission with
// delivery guarantees, and turn/permission bookkeeping.
type Host interface {
	// ID is the daemon-local session id.
	ID() string
	// AgentSessionID is the server-side session id (set once Create/Resume
	// returns).
	AgentSessionID() string
	// CWD is the session working directory (absolute, validated).
	CWD() string
	// Model is the requested model string (per-session override or config
	// default), empty when unset. Interpretation is dialect business.
	Model() string
	Config() Config
	Log() *slog.Logger
	// API is bound to the provider's current engine base URL.
	API() API
	// Emit delivers a daemon event. SessionID and Timestamp are filled in
	// when zero. Control events block until consumed or the session closes;
	// stream chunks may be dropped under backpressure.
	Emit(ev event.Event)
	// EmitReplay delivers a history-rebuild event (Replay flag set) with
	// pre-attach drop-oldest semantics: never blocks, keeps the most recent
	// conversation when the buffer fills.
	EmitReplay(ev event.Event)
	// EndTurn marks the active turn finished and reports whether one was
	// active — dialects call it on their turn-end/error events and emit
	// turn_complete only when it returns true (idle events can arrive for
	// turns this daemon never started).
	EndTurn() bool
	// TrackPermission records a pending permission request and arms the
	// expiry fail-safe (Config.PermissionTimeout).
	TrackPermission(id string)
	// TakePending atomically claims a pending permission id, reporting
	// whether it was outstanding — dialects call it on their
	// permission-resolved events to dedupe against local answers.
	TakePending(id string) bool
}
