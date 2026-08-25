// Package provider defines the agent provider adapter interfaces.
package provider

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
)

// ErrNotImplemented indicates a provider is registered but not ready.
var ErrNotImplemented = errors.New("provider not implemented")

// ErrTurnBusy indicates a prompt was refused because a turn is already active
// (MADR 0020). Mapped to protocol error code turn_busy on the WebSocket.
var ErrTurnBusy = errors.New("turn busy")

// ErrInvalidAgent indicates a requested agent cannot accept a top-level user
// turn. It is distinct from an engine failure so the wire can return a stable,
// actionable bad_agent error.
var ErrInvalidAgent = errors.New("invalid agent")

// ErrThinkingLevelFixed means the session's thinking level is locked at spawn
// and cannot change mid-session. Grok is the case: --reasoning-effort is a
// process flag, and session/set_model silently ignores a reasoning field
// (MADR 0052 §2.2). The command layer renders this as "applies to new sessions".
var ErrThinkingLevelFixed = errors.New("thinking level is fixed for this session; start a new session to change it")

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
	// IDGoose is the Goose ACP-over-HTTP provider.
	IDGoose ID = "goose"
	// IDCodex is the Codex app-server JSON-RPC provider.
	IDCodex ID = "codex"
	// IDKilo is the Kilo CLI provider (shared `kilo serve` engine, MADR 0075).
	IDKilo ID = "kilo"
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
	// ThinkingLevel is the per-session reasoning/thinking rung (e.g. "low",
	// "high"). Empty means "provider default": codex omits turn/start.effort;
	// grok falls through to Config.ReasoningEffort then session/new|load
	// `_meta.reasoningEffort` (MADR 0106). Mid-session grok /thinking is
	// Phase C of that pair. Codex can change it mid-session.
	ThinkingLevel string
	// ModeID persists the autonomy/permission selection. Empty means the
	// provider default. Additive (MADR 0080 D7).
	ModeID string
	// CollaborationModeID is the Codex collaboration-mode id (`plan` /
	// `default`). Empty means default. Additive (MADR 0080 D7).
	CollaborationModeID string
	// ServiceTier is the Fast override wire id. Empty means off.
	ServiceTier string
	// Personality is the generated personality enum. Empty means provider
	// default. `/personality none` is the enum value "none", not empty.
	Personality string
}

// ErrCollaborationUnsupported means the live session has no collaboration-mode
// capability (MADR 0080 D2/D3).
var ErrCollaborationUnsupported = errors.New("collaboration mode unsupported")

// ErrCollaborationInvalid means the requested collaboration-mode id is not in
// the current catalog.
var ErrCollaborationInvalid = errors.New("collaboration mode invalid")

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
	// deviceID identifies which paired device resolved it (MADR 0077 §1) —
	// empty when the resolution wasn't a single human's fresh tap (e.g. an
	// auto-mode-arm sweep answering previously pending permissions in bulk).
	RespondPermission(ctx context.Context, permissionID, optionID string, cancelled bool, deviceID string) error
}

// QuestionSession can resolve remote multi-question forms (OpenCode questions).
// answers[i] is the selected label list for questions[i] on the matching
// question_request. cancelled rejects the whole form.
type QuestionSession interface {
	Session
	RespondQuestion(ctx context.Context, questionID string, answers QuestionAnswers, cancelled bool) error
}

// QuestionAnswers keys selected labels by the upstream question field id.
// Numeric keys preserve compatibility with providers whose native protocol is
// an ordered list rather than an object keyed by field id.
type QuestionAnswers map[string][]string

// LogValue makes question values write-only at every structured logging site.
func (a QuestionAnswers) LogValue() slog.Value {
	return slog.GroupValue(slog.Int("field_count", len(a)), slog.String("values", "<redacted>"))
}

// Clear overwrites and releases every answer value.
func (a QuestionAnswers) Clear() {
	for id, values := range a {
		for i := range values {
			values[i] = ""
		}
		a[id] = nil
	}
}

// OrderedQuestionAnswers converts keyed answers for legacy ordered provider
// dialects. Numeric ids are placed at their exact indexes; non-numeric ids are
// appended in bytewise order so conversion is deterministic.
func OrderedQuestionAnswers(answers QuestionAnswers) [][]string {
	if len(answers) == 0 {
		return [][]string{}
	}
	max := -1
	var rest []string
	for id := range answers {
		i, err := strconv.Atoi(id)
		if err == nil && i >= 0 {
			if i > max {
				max = i
			}
			continue
		}
		rest = append(rest, id)
	}
	out := make([][]string, max+1, max+1+len(rest))
	for id, values := range answers {
		if i, err := strconv.Atoi(id); err == nil && i >= 0 {
			out[i] = values
		}
	}
	sort.Strings(rest)
	for _, id := range rest {
		out = append(out, answers[id])
	}
	return out
}

// ModeSession is optionally implemented by sessions that expose switchable
// operating modes (ACP session modes). The available modes and current mode are
// reported via event.TypeMode; SetMode changes the active one.
type ModeSession interface {
	Session
	SetMode(ctx context.Context, modeID string) error
}

// CollaborationMode is one Codex (or compatible) collaboration-mode preset.
type CollaborationMode struct {
	ID          string
	Name        string
	Description string
}

// CollaborationModeSession is the independent Plan/Default axis (MADR 0080 D3).
// It must not share identifiers with [ModeSession].
type CollaborationModeSession interface {
	Session
	CollaborationModes() ([]CollaborationMode, string, error)
	SetCollaborationMode(context.Context, string) error
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

// ForkOptions is the structured fork request (MADR 0080 D20).
type ForkOptions struct {
	LastTurnID            string
	DeferGoalContinuation bool
}

// ForkResult is the structured fork response.
type ForkResult struct {
	AgentSessionID string
	ForkedFromID   string
}

// ErrForkNothing is the never-materialized Codex thread ("no rollout found").
var ErrForkNothing = errors.New("nothing to fork yet")

// ErrDiffUnavailable means no working-tree or turn-diff fallback is available.
var ErrDiffUnavailable = errors.New("working-tree diff is unavailable")

// ErrAppliesNextTurn means the setting was accepted locally and will be sent
// on the next turn/start because immediate settings update is unavailable.
var ErrAppliesNextTurn = errors.New("applies next turn")

// ErrServiceTierUnsupported means the active model has no Fast tier.
var ErrServiceTierUnsupported = errors.New("fast service tier unsupported")

// ErrPersonalityUnsupported means the active model has no personality setting.
var ErrPersonalityUnsupported = errors.New("personality unsupported")

// ErrPersonalityInvalid means the value is not a generated personality enum.
var ErrPersonalityInvalid = errors.New("personality invalid")

// ServiceTierSession exposes catalog-driven Fast (MADR 0080 D17).
type ServiceTierSession interface {
	Session
	HasFast() bool
	ServiceTier() string
	SetServiceTier(ctx context.Context, on bool) error
}

// PersonalitySession exposes model-gated personality (MADR 0080 D18).
type PersonalitySession interface {
	Session
	PersonalitySupported() bool
	Personality() string
	SetPersonality(ctx context.Context, value string) error
}

// Goal is the bounded Codex thread goal (MADR 0080 D16).
type Goal struct {
	Objective   string
	Status      string
	TokenBudget int
	TokenUsage  int
}

// GoalKind is a user-facing goal mutation.
type GoalKind string

const (
	// GoalView reads the current goal.
	GoalView GoalKind = "view"
	// GoalReplace creates or replaces an active goal.
	GoalReplace GoalKind = "replace"
	// GoalEdit changes the objective without changing status.
	GoalEdit GoalKind = "edit"
	// GoalPause pauses an active goal.
	GoalPause GoalKind = "pause"
	// GoalResume resumes a paused goal.
	GoalResume GoalKind = "resume"
	// GoalClear removes the goal.
	GoalClear GoalKind = "clear"
)

// GoalMutation is a parsed /goal action.
type GoalMutation struct {
	Kind      GoalKind
	Objective string
}

// Goal statuses the user may request. Engine-only statuses are never set here.
const (
	GoalStatusActive = "active"
	GoalStatusPaused = "paused"
)

// ErrGoalBusy means a goal mutation was refused because a turn is active.
var ErrGoalBusy = ErrTurnBusy

// ErrGoalPlanConflict means Plan and an active goal cannot coexist.
var ErrGoalPlanConflict = errors.New("goal conflicts with plan")

// ErrGoalInvalid means the mutation is empty, too long, or not allowed.
var ErrGoalInvalid = errors.New("goal invalid")

// GoalSession exposes Codex thread goals.
type GoalSession interface {
	Session
	CurrentGoal() (Goal, bool)
	ApplyGoal(ctx context.Context, mut GoalMutation) (Goal, error)
	HydrateGoal(ctx context.Context) error
}

// GoalIsActive reports whether a present goal blocks Plan / requires fork defer.
func GoalIsActive(g Goal, ok bool) bool {
	if !ok || g.Objective == "" && g.Status == "" {
		return false
	}
	switch g.Status {
	case GoalStatusPaused, "complete":
		return false
	default:
		return true
	}
}

// ForkSession can fork the provider-native conversation into a new agent
// session (OpenCode POST /session/{id}/fork). LastTurnID is optional (engine
// default when empty).
type ForkSession interface {
	Session
	Fork(ctx context.Context, opts ForkOptions) (ForkResult, error)
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

// DiffResult is a bounded file-change report (MADR 0080 D15).
type DiffResult struct {
	Summary   string
	BaseSHA   string
	Scope     string
	Truncated bool
}

// DiffSession can fetch file diffs for the session (OpenCode GET …/diff).
// messageID optional. Results are typically also pushed as notices via SSE
// session.diff; this is the pull path.
type DiffSession interface {
	Session
	Diff(ctx context.Context, messageID string) (DiffResult, error)
}

// RenameSession optionally changes the user-visible title of a provider-native
// session. The manager persists its own name only after this operation succeeds.
type RenameSession interface {
	Session
	Rename(ctx context.Context, title string) error
}

// Diagnostics is a deliberately small, read-only session/project snapshot.
// It must never contain paths, file contents, URLs, headers, credentials, or
// arbitrary provider configuration.
type Diagnostics struct {
	Branch        string            `json:"branch,omitempty"`
	DefaultBranch string            `json:"default_branch,omitempty"`
	VCS           *VCSStatusSummary `json:"vcs,omitempty"`
	MCP           []MCPServerStatus `json:"mcp,omitempty"`
}

// VCSStatusSummary aggregates a provider's working-tree state without
// revealing individual repository paths.
type VCSStatusSummary struct {
	Added     int `json:"added,omitempty"`
	Modified  int `json:"modified,omitempty"`
	Deleted   int `json:"deleted,omitempty"`
	Additions int `json:"additions,omitempty"`
	Deletions int `json:"deletions,omitempty"`
}

// MCPServerStatus is a redacted MCP connection state.
type MCPServerStatus struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

// DiagnosticsSession optionally exposes bounded read-only project metadata.
type DiagnosticsSession interface {
	Session
	Diagnostics(ctx context.Context) (Diagnostics, error)
}

// CompactSession can summarise its own conversation in place to reclaim
// context (OpenCode POST /session/{id}/summarize). The summary reaches clients
// through the provider's normal event stream, so Compact only reports whether
// the request was accepted.
type CompactSession interface {
	Session
	Compact(ctx context.Context) error
}

// ModelSession can switch the session's model without restarting the agent
// (OpenCode POST /api/session/{id}/model). Sessions without it are relaunched
// by the daemon instead, which costs the agent its context.
type ModelSession interface {
	Session
	SetModel(ctx context.Context, model string) error
}

// ThinkingSession accepts a thinking/reasoning level. Absence is the honest
// answer for opencode and goose, which expose no per-session effort control
// (MADR 0052 D6). Codex applies the level on the next turn/start; grok 1.0.5
// applies it on session/new|load `_meta` and mid-session via session/set_model
// `_meta.reasoningEffort` (MADR 0106).
type ThinkingSession interface {
	Session
	SetThinkingLevel(ctx context.Context, level string) error
	ThinkingLevel() string
}

// MCPStatusSession optionally exposes per-MCP-server connection state.
// The session keeps a snapshot updated from agent lifecycle notifications;
// polled by Diagnostics.
type MCPStatusSession interface {
	Session
	MCPStatus(ctx context.Context) ([]MCPServerStatus, error)
}

// UndoSession can revert the changes made by the last turn. It is separate
// from [RevertSession] because that one needs a provider-native message id,
// which the daemon never sees; an UndoSession resolves "the last turn" itself
// and returns a short description of what it undid.
type UndoSession interface {
	Session
	UndoLast(ctx context.Context) (summary string, err error)
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

// Catalog scopes for models.list (MADR 0043 D1). A request without a scope
// means [CatalogScopeModels].
const (
	// CatalogScopeModels enumerates models.
	CatalogScopeModels = "models"
	// CatalogScopeProviders enumerates the *model* providers (anthropic,
	// openai, …) whose models an agent provider can reach. This is a different
	// axis from the agent providers in providers.list (grok, opencode, …).
	CatalogScopeProviders = "providers"
)

// ModelProviderCatalog is optionally implemented by providers whose models are
// grouped under distinct model providers, so a client can offer a provider step
// before the model step (MADR 0043 D1). A provider that does not implement it
// has one implicit model provider, and clients show no provider step.
type ModelProviderCatalog interface {
	ModelCatalog
	// ListModelProviders returns a single-select catalog of model provider
	// ids. Options carry picker.MetaConnected / MetaModelCount /
	// MetaDefaultModel where the engine reports them.
	ListModelProviders(ctx context.Context) (picker.Catalog, error)
	// ListModelsFor returns the models of one model provider. An unknown id
	// yields an empty catalog rather than an error: the client may be asking
	// about a provider that has since disappeared from the engine's list.
	ListModelsFor(ctx context.Context, modelProvider string) (picker.Catalog, error)
}

// ModelCatalogSession is optionally implemented by sessions that can report a
// model catalog scoped to themselves — the models of the model provider this
// session is actually using, with its current model as the default. It is what
// makes an in-session /model picker show the right list instead of the whole
// provider-wide one (MADR 0043 D9).
//
// scope is [CatalogScopeModels] or [CatalogScopeProviders].
type ModelCatalogSession interface {
	Session
	ModelCatalog(ctx context.Context, scope string) (picker.Catalog, error)
}

// AgentCatalog is optionally implemented by providers that can advertise an
// agent-name picker catalog for agents.list (OpenCode GET /agent). When
// absent, the daemon returns an empty allow-custom catalog.
type AgentCatalog interface {
	// ListAgents returns a single-select catalog of agent names. Prefer a live
	// engine list and fall back to static. Respect ctx cancellation.
	ListAgents(ctx context.Context) (picker.Catalog, error)
}

// AgentSessionMeta is the metadata-only description of an agent-native
// session. It intentionally excludes transcript and tool content: discovery
// lets a device choose a session to load, but does not import or replay it.
type AgentSessionMeta struct {
	ID    string `json:"id"`
	CWD   string `json:"cwd,omitempty"`
	Title string `json:"title,omitempty"`
	// omitzero, not omitempty: omitempty never applies to a struct, so an
	// unknown timestamp went on the wire as "0001-01-01T00:00:00Z" and the
	// session picker rendered it as an age of about two thousand years
	// (MADR 0046 L-13).
	UpdatedAt time.Time `json:"updated_at,omitzero"`
}

// AgentSessionLister is optionally implemented by providers whose native
// protocol can enumerate resumable sessions. Callers must treat results as
// untrusted metadata and create a normal daemon session only after the user
// selects one.
type AgentSessionLister interface {
	ListAgentSessions(ctx context.Context) ([]AgentSessionMeta, error)
}
