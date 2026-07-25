// Package provider defines the agent provider adapter interfaces.
package provider

import (
	"context"
	"errors"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
)

// ErrNotImplemented indicates a provider is registered but not ready.
var ErrNotImplemented = errors.New("provider not implemented")

// ErrTurnBusy indicates a prompt was refused because a turn is already active
// (MADR 0020). Mapped to protocol error code turn_busy on the WebSocket.
var ErrTurnBusy = errors.New("turn busy")

// ID identifies a provider implementation.
type ID string

// Well-known provider IDs registered by the daemon.
const (
	// IDFake is the deterministic test/smoke provider.
	IDFake ID = "fake"
	// IDGrok is the Grok Build ACP provider.
	IDGrok ID = "grok"
	// IDOpencode is the OpenCode provider (shared `opencode serve` engine).
	IDOpencode ID = "opencode"
)

// StartOptions configure a new agent session.
type StartOptions struct {
	CWD   string
	Model string
	Name  string
	// Agent is an optional OpenCode agent name (e.g. "build", "plan") sent on
	// prompt_async. Empty uses the engine default. Ignored by non-OpenCode
	// providers. Prefer values from agents.list (MADR 0020 Sprint 3).
	Agent string
	// AgentSessionID, when set, asks the provider to resume/load an existing agent session.
	AgentSessionID string
	// LocalSessionID is the mcremote session id (optional; provider may generate if empty).
	LocalSessionID string
}

// Content is a prompt content block. Type is "text" (default), "image", or
// "audio". For image/audio, Data is the base64-encoded payload and MimeType its
// media type (e.g. "image/png"); providers that or agents that do not advertise
// the matching capability drop non-text blocks.
type Content struct {
	Type     string
	Text     string
	MimeType string
	Data     string
}

// Session is a running agent conversation.
type Session interface {
	ID() string
	ProviderID() ID
	// AgentSessionID returns the provider-native session id when available.
	AgentSessionID() string
	Prompt(ctx context.Context, parts []Content) error
	Cancel(ctx context.Context) error
	Events() <-chan event.Event
	Close(ctx context.Context) error
}

// CWDSession is optionally implemented by sessions that resolve a concrete
// working directory (defaults, home-dir fallback). The manager prefers this
// over the caller-supplied path when populating session metadata, so clients
// see where the agent actually runs rather than an empty field.
type CWDSession interface {
	Session
	CWD() string
}

// PermissionSession can resolve remote permission prompts.
type PermissionSession interface {
	Session
	// RespondPermission selects an option for a pending permission_request.
	// If cancelled is true, the permission is rejected as cancelled.
	RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool) error
}

// QuestionSession can resolve remote multi-question forms (OpenCode questions).
// answers[i] is the selected label list for questions[i] on the matching
// question_request. cancelled rejects the whole form.
type QuestionSession interface {
	Session
	RespondQuestion(ctx context.Context, questionID string, answers [][]string, cancelled bool) error
}

// ModeSession is optionally implemented by sessions that expose switchable
// operating modes (ACP session modes). The available modes and current mode are
// reported via event.TypeMode; SetMode changes the active one.
type ModeSession interface {
	Session
	SetMode(ctx context.Context, modeID string) error
}

// ConfigSession is optionally implemented by sessions that expose agent-defined
// config options (ACP session config options). Options are reported via
// event.TypeSessionConfig; SetConfigOption changes one. kind is "select" or
// "boolean"; for boolean, value is "true"/"false"; for select, value is the
// chosen value id.
type ConfigSession interface {
	Session
	SetConfigOption(ctx context.Context, optionID, kind, value string) error
}

// PurgeSession optionally owns durable provider-side state that should be
// removed when the daemon hard-deletes a session (session.delete). Soft close
// must not call Purge — resume relies on that state remaining.
type PurgeSession interface {
	Session
	Purge(ctx context.Context) error
}

// ForkSession can fork the provider-native conversation into a new agent
// session (OpenCode POST /session/{id}/fork). messageID is optional (engine
// default when empty). Returns the new agent session id.
type ForkSession interface {
	Session
	Fork(ctx context.Context, messageID string) (newAgentSessionID string, err error)
}

// RevertSession can undo or restore messages in the provider-native session
// (OpenCode revert / unrevert).
type RevertSession interface {
	Session
	// Revert undoes messageID (and optionally a part). Empty partID reverts
	// the whole message.
	Revert(ctx context.Context, messageID, partID string) error
	// Unrevert restores previously reverted messages.
	Unrevert(ctx context.Context) error
}

// DiffSession can fetch file diffs for the session (OpenCode GET …/diff).
// messageID optional. Results are typically also pushed as notices via SSE
// session.diff; this is the pull path.
type DiffSession interface {
	Session
	// Diff returns a short multi-line summary of file changes (paths + +/−).
	Diff(ctx context.Context, messageID string) (summary string, err error)
}

// CommandCatalog is optionally implemented by providers that advertise a
// slash-command picker (OpenCode GET /command → commands.list).
type CommandCatalog interface {
	ListCommands(ctx context.Context) (picker.Catalog, error)
}

// Provider starts sessions for a given agent backend.
type Provider interface {
	ID() ID
	// Ready reports whether Start is expected to succeed (binary present, etc.).
	Ready() bool
	Start(ctx context.Context, opts StartOptions) (Session, error)
}

// ModelCatalog is optionally implemented by providers that can advertise a
// model picker catalog for models.list. When absent, the daemon returns an
// empty allow-custom catalog so clients can still free-type a model id.
type ModelCatalog interface {
	// ListModels returns a single- or multi-select catalog. Implementations
	// should prefer a live engine catalog and fall back to a static list
	// (picker.SourceMerged / SourceStatic). The call may start a shared
	// engine if needed; it must respect ctx cancellation.
	ListModels(ctx context.Context) (picker.Catalog, error)
}

// AgentCatalog is optionally implemented by providers that can advertise an
// agent-name picker catalog for agents.list (OpenCode GET /agent). When
// absent, the daemon returns an empty allow-custom catalog.
type AgentCatalog interface {
	// ListAgents returns a single-select catalog of agent names. Prefer a live
	// engine list and fall back to static. Respect ctx cancellation.
	ListAgents(ctx context.Context) (picker.Catalog, error)
}
