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
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// Config configures an HTTP+SSE agent transport. Bin, DefaultCWD, Model,
// AlwaysApprove, PermissionTimeout and TurnStallNotice are honoured. Args and
// FSRoots from the ACP shape are not: the engine's argv is fixed and it does
// its own file I/O. Prewarm is a daemon-level decision (whether to call
// EnsureServer at boot), not something this transport reads.
type Config struct {
	Bin               string
	AlwaysApprove     bool
	DefaultCWD        string
	Model             string
	PermissionTimeout time.Duration
	TurnStallNotice   time.Duration
	// SessionTree enables multi-agent demux (MADR 0020 KD11). nil means true
	// (default after Sprint 1). Explicit false is the full pre-0020 kill
	// switch: no childAliases, parent-only EndTurn, no child fan-in.
	SessionTree *bool
}

// TreeEnabled reports whether session-tree demux is on (default true when
// SessionTree is nil — MADR 0020 KD11).
func (c Config) TreeEnabled() bool {
	if c.SessionTree == nil {
		return true
	}
	return *c.SessionTree
}

// treeEnabled is the unexported alias used inside this package.
func (c Config) treeEnabled() bool { return c.TreeEnabled() }

// Bool returns a *bool for Config.SessionTree literals.
func Bool(v bool) *bool { return &v }

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

// HealthyHook is optionally implemented by a [Dialect] that wants the HTTP
// body of a successful health probe (e.g. to parse OpenCode's version field
// for MADR 0020 KD10). Errors fail engine startup.
type HealthyHook interface {
	OnHealthy(body []byte) error
}

// VersionGate is optionally implemented by a [Dialect] that enforces a minimum
// engine version when session-tree features are enabled.
type VersionGate interface {
	// CheckMinVersion returns an error when the engine is too old for the
	// given config (e.g. session_tree true and OpenCode < 1.18). Empty
	// version (not yet observed) must return nil so a race with the health
	// probe cannot block Start.
	CheckMinVersion(cfg Config) error
}

// ChildFrame is optionally implemented by a [Dialect] that can extract a
// parent agent-session id from an SSE properties blob (e.g. session.created
// with info.parentID). Used by the transport to bootstrap child aliases when
// the frame's own sid is not yet registered (MADR 0020).
type ChildFrame interface {
	// ParentIDFromProps returns the parent agent session id for a frame that
	// introduces a child, or "" when the frame is not a child-create shape.
	ParentIDFromProps(props json.RawMessage) string
}

// TreeIdleConfirmer is optionally implemented by a [DialectSession] to confirm
// via engine REST that the session tree is idle before EndTurn (MADR 0020
// idle-confirm). When absent, [Host.TryEndTurnIfTreeIdle] uses only locally
// known node status (parent-only dialects behave as bare EndTurn).
type TreeIdleConfirmer interface {
	// ConfirmTreeIdle probes the engine for liveness of parentID and known
	// tree members. stillBusy are ids that must remain non-idle; discovered
	// are child ids the engine lists that the transport should bind.
	// Errors are treated as "cannot confirm idle" (do not EndTurn).
	ConfirmTreeIdle(ctx context.Context, parentID string, knownTreeIDs []string) (stillBusy, discovered []string, err error)
}

// NodeStatus is the liveness of one agent session in a turn's session tree
// (parent or child). Used by [Host.NoteNodeStatus] / [Host.TryEndTurnIfTreeIdle].
type NodeStatus string

const (
	// NodeIdle means the agent session is not running a turn.
	NodeIdle NodeStatus = "idle"
	// NodeBusy means the agent session is actively working.
	NodeBusy NodeStatus = "busy"
	// NodeRetry means the agent session is in a retry backoff; counts as busy
	// for tree EndTurn purposes.
	NodeRetry NodeStatus = "retry"
)

// NodeBusyForEndTurn reports whether status blocks tree-idle EndTurn.
func NodeBusyForEndTurn(s NodeStatus) bool {
	return s == NodeBusy || s == NodeRetry
}

// ModelLister is optionally implemented by a [Dialect] that can advertise a
// model picker catalog. [Provider.ListModels] prefers a live fetch when the
// engine is (or can be) running, and always has a static fallback.
type ModelLister interface {
	// StaticModels is the offline catalog (never blocks on the engine).
	StaticModels(cfg Config) picker.Catalog
	// ListModelsLive fetches from a healthy engine. Failures fall back to
	// StaticModels; the call must honor ctx.
	ListModelsLive(ctx context.Context, api API) (picker.Catalog, error)
}

// AgentLister is optionally implemented by a [Dialect] that can advertise an
// agent-name picker catalog (OpenCode GET /agent). [Provider.ListAgents]
// prefers a live fetch when the engine is up.
type AgentLister interface {
	// StaticAgents is the offline catalog (never blocks on the engine).
	StaticAgents(cfg Config) picker.Catalog
	// ListAgentsLive fetches from a healthy engine. Failures fall back to
	// StaticAgents; the call must honor ctx.
	ListAgentsLive(ctx context.Context, api API) (picker.Catalog, error)
}

// CommandLister is optionally implemented by a [Dialect] that can advertise a
// slash-command catalog (OpenCode GET /command). [Provider.ListCommands]
// prefers a live fetch when the engine is up.
type CommandLister interface {
	StaticCommands(cfg Config) picker.Catalog
	ListCommandsLive(ctx context.Context, api API) (picker.Catalog, error)
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
	// RespondQuestion answers a multi-question form server-side. cancelled
	// rejects the whole form (timeout / user cancel). answers is label lists
	// in question order. Dialects that never emit questions may no-op.
	RespondQuestion(ctx context.Context, questionID string, answers [][]string, cancelled bool) error
	// Delete removes the server-side session permanently (daemon session.delete).
	// Close must not call this — resume relies on the engine keeping state.
	Delete(ctx context.Context) error
	// Resync reconciles this session against authoritative engine state after
	// a window where SSE frames may have been missed (stream reconnect, stall
	// watchdog). The transport calls it only while a turn is active and never
	// while the prompt submit is still in flight. turnStartedAt is when the
	// local turn began: engine evidence of a turn that finished before it
	// belongs to a previous turn and must be ignored. If the engine shows the
	// current turn already finished, the dialect emits any missed final text,
	// then calls [Host.EndTurn] and emits the turn-end events the stream would
	// have carried. If the turn is still live engine-side it must do nothing —
	// the in-stream snapshot events heal text gaps for live turns, and acting
	// on a moving turn risks duplicating output. Best-effort: errors are
	// logged, not returned.
	Resync(ctx context.Context, turnStartedAt time.Time)
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
	// Agent is the optional OpenCode agent name for prompt_async (e.g. "build",
	// "plan"). Empty uses the engine default. MADR 0020 Sprint 3.
	Agent() string
	// SetAgent replaces the agent used by subsequent prompts. The engine binds
	// the agent per message, so a switch takes effect on the next turn — this is
	// how a mode change reaches an OpenCode session (MADR 0022).
	SetAgent(name string)
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
	// turns this daemon never started). Prefer [TryEndTurnIfTreeIdle] for
	// multi-agent (session-tree) engines so a parent idle does not clear the
	// turn while children are still busy (MADR 0020).
	EndTurn() bool
	// BindChildAlias routes SSE for childAgentID to this host's session.
	// Dialects call this when they observe session.created/updated with a
	// parentID matching [AgentSessionID], or when resync lists children.
	// Idempotent. Also records the child as busy in the tree until a later
	// [NoteNodeStatus] says otherwise (safe default for just-spawned agents).
	BindChildAlias(childAgentID string)
	// UnbindChildAlias drops a child route (session.deleted or tree cleanup).
	UnbindChildAlias(childAgentID string)
	// NoteNodeStatus records busy/idle/retry for agentSessionID (parent or
	// child) for tree-idle EndTurn. Empty agentSessionID means the parent
	// ([AgentSessionID]).
	NoteNodeStatus(agentSessionID string, status NodeStatus)
	// TryEndTurnIfTreeIdle ends the turn iff every *known* tree node is idle
	// and the turn is still active. Optionally consults [TreeIdleConfirmer]
	// before ending when all local nodes look idle. Returns whether EndTurn
	// fired. Dialects should call this from session.idle / session.status
	// instead of bare [EndTurn] when children may exist.
	//
	// Without any [BindChildAlias] / child [NoteNodeStatus] calls this is
	// equivalent to EndTurn when the parent is idle (single-session engines).
	TryEndTurnIfTreeIdle() bool
	// EventAgentSessionID is the agent-side session id of the SSE frame
	// currently being handled (set by the transport before HandleEvent).
	// Empty outside of HandleEvent. Dialects use it when properties omit sid.
	EventAgentSessionID() string
	// TrackPermission records a pending permission request and arms the
	// expiry fail-safe (Config.PermissionTimeout).
	TrackPermission(id string)
	// TrackPermissionOrigin records which agent session owns a permission id
	// so answers can target the correct REST path (child vs parent). Empty
	// agentSessionID means the parent. No-op bookkeeping until the dialect
	// uses origin on reply (MADR 0020 PR3).
	TrackPermissionOrigin(permissionID, agentSessionID string)
	// PermissionOrigin returns the agent session id recorded for a permission
	// (or the parent agent id when unknown).
	PermissionOrigin(permissionID string) string
	// TakePending atomically claims a pending permission id, reporting
	// whether it was outstanding — dialects call it on their
	// permission-resolved events to dedupe against local answers.
	TakePending(id string) bool
	// TrackQuestion records a pending question form and arms the same timeout
	// as permissions (MADR 0020 Sprint 1b).
	TrackQuestion(id string)
	// TakeQuestionPending claims a pending question id.
	TakeQuestionPending(id string) bool
}
