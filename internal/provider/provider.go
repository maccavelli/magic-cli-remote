// Package provider defines the agent provider adapter interfaces.
package provider

import (
	"context"
	"encoding/json"
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

// ErrExecutionOutcomeUnknown means a non-idempotent execution request may
// have reached the provider before the connection failed.
var ErrExecutionOutcomeUnknown = errors.New("execution outcome unknown")

// ErrNativeUnavailable means an optional native terminal catalog is absent;
// callers may still use daemon-owned terminals.
var ErrNativeUnavailable = errors.New("native terminal unavailable")

// ErrTerminalNotFound rejects unknown, stale, or foreign terminal handles.
var ErrTerminalNotFound = errors.New("terminal not found")

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
	// PermissionProfileID is the selected Codex permission profile. It is
	// independent from approval/reviewer policy and may be an opaque custom id.
	PermissionProfileID string
	// ApprovalsReviewer selects who reviews approval prompts. Empty means user.
	ApprovalsReviewer string
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

// ErrPermissionProfileInvalid means a requested profile is absent or managed-disabled.
var ErrPermissionProfileInvalid = errors.New("permission profile invalid or disallowed")

// ErrReviewerInvalid means the reviewer is not user or auto_review.
var ErrReviewerInvalid = errors.New("approvals reviewer invalid")

// ErrGuardianApprovalUnavailable means no exact current denial can be retried.
var ErrGuardianApprovalUnavailable = errors.New("guardian denial unavailable")

const (
	// ApprovalsReviewerUser routes approval requests to the user.
	ApprovalsReviewerUser = "user"
	// ApprovalsReviewerAutoReview routes eligible requests through Guardian.
	ApprovalsReviewerAutoReview = "auto_review"
)

// Content is a prompt content block. Type is "text" (default), "image", or
// "audio". For image/audio, Data is the base64-encoded payload and MimeType its
// media type (e.g. "image/png"); providers that or agents that do not advertise
// the matching capability drop non-text blocks.
type Content struct {
	Type     string
	Text     string
	MimeType string
	Data     string
	// Filename is an optional bare basename for a non-text block. It is never
	// a path: providers that name the attachment upstream must not be handed a
	// value that could traverse the host filesystem (MADR 0112 A2).
	Filename string
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

// RuntimeSession exposes bounded human-readable provider-global runtime and
// usage summaries for session slash commands.
type RuntimeSession interface {
	Session
	RuntimeStatus(context.Context) (string, error)
	RuntimeUsage(context.Context) (string, error)
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

// PermissionProfile is one Codex permission profile. Custom identifiers are
// opaque; Allowed reflects effective managed requirements.
type PermissionProfile struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	Allowed     bool   `json:"allowed"`
	Dangerous   bool   `json:"dangerous,omitempty"`
}

// PermissionProfileSession exposes the independent profile and reviewer axes.
type PermissionProfileSession interface {
	Session
	PermissionSettings() ([]PermissionProfile, string, string)
	SetPermissionProfile(context.Context, string) error
	SetApprovalsReviewer(context.Context, string) error
}

// GuardianApprovalSession retries the exact most recent Guardian-denied action once.
type GuardianApprovalSession interface {
	Session
	ApproveGuardianDenied(context.Context) error
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

	// Skills, LSP and Formatters explain what the engine can draw on and why a
	// capability is degraded (MADR 0112 A6). They are metadata only: a skill's
	// location and content, a language server's roots, and a formatter's
	// executable never cross this boundary.
	Skills     []SkillInfo     `json:"skills,omitempty"`
	LSP        []LSPStatus     `json:"lsp,omitempty"`
	Formatters []FormatterInfo `json:"formatters,omitempty"`
}

// SkillInfo is a skill the engine discovered.
//
// Name and description only. A skill's file location would disclose the daemon
// host's layout, and its content is prompt text the phone has no use for; both
// are dropped at decode rather than filtered later.
type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// LSPStatus is one language server's coarse state. Roots and executable paths
// are never forwarded.
type LSPStatus struct {
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
}

// FormatterInfo is one formatter's availability.
//
// Extensions are reported as a count rather than a list: the count answers
// "does this cover my files", while the list is configuration detail.
type FormatterInfo struct {
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled,omitempty"`
	Extensions int    `json:"extensions,omitempty"`
}

// Normalized MCP states. This is the total mapping of the closed 1.18.21
// MCPStatus union plus a degradation target, so a future upstream member
// becomes "unknown" rather than leaking a raw string to the phone
// (MADR 0112 A6, PLAN P7 step 9).
const (
	MCPStateConnected         = "connected"
	MCPStateDisabled          = "disabled"
	MCPStateFailed            = "failed"
	MCPStateNeedsAuth         = "needs_auth"
	MCPStateNeedsRegistration = "needs_registration"
	MCPStateUnknown           = "unknown"
)

// SkillRefreshSession optionally recycles an idle engine instance so newly
// written skills become discoverable.
//
// It is never called automatically: recycling is disruptive enough that the
// owner confirms it, and a busy instance refuses rather than waiting
// (MADR 0112 A10).
type SkillRefreshSession interface {
	Session
	RefreshSkills(ctx context.Context) error
}

// ErrInstanceBusy means a refresh was refused because work is in flight in the
// target project. The skill file is untouched; the caller retries when idle.
var ErrInstanceBusy = errors.New("instance busy")

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
// answer for goose, which exposes no per-session effort control (MADR 0052 D6).
// Codex applies the level on the next turn/start; grok 1.0.5 applies it on
// session/new|load `_meta` and mid-session via session/set_model
// `_meta.reasoningEffort` (MADR 0106). OpenCode applies it per request as the
// documented `variant` field, so it is settable mid-session and never returns
// ErrThinkingLevelFixed (MADR 0112 A14).
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
	ID      string `json:"id"`
	CWD     string `json:"cwd,omitempty"`
	Title   string `json:"title,omitempty"`
	Preview string `json:"preview,omitempty"`
	// omitzero, not omitempty: omitempty never applies to a struct, so an
	// unknown timestamp went on the wire as "0001-01-01T00:00:00Z" and the
	// session picker rendered it as an age of about two thousand years
	// (MADR 0046 L-13).
	UpdatedAt time.Time `json:"updated_at,omitzero"`
	CreatedAt time.Time `json:"created_at,omitzero"`

	// Native lifecycle and organization fields are additive. Providers with
	// only metadata discovery leave them empty; the Codex thread browser uses
	// them to reconcile loaded and persisted conversations without importing
	// transcript data into the generic list path.
	NativeStatus   string `json:"native_status,omitempty"`
	Archived       bool   `json:"archived,omitempty"`
	Pinned         bool   `json:"pinned,omitempty"`
	SectionID      string `json:"section_id,omitempty"`
	SectionName    string `json:"section_name,omitempty"`
	SectionIcon    string `json:"section_icon,omitempty"`
	SectionColor   string `json:"section_color,omitempty"`
	ParentThreadID string `json:"parent_thread_id,omitempty"`
	ForkedFromID   string `json:"forked_from_id,omitempty"`
	Source         string `json:"source,omitempty"`
	Loaded         bool   `json:"loaded,omitempty"`
	ProjectID      string `json:"project_id,omitempty"`

	// The fields below are additive (MADR 0112 A1). Every one is optional, and
	// a provider that cannot report one leaves it zero rather than guessing —
	// a picker row that invents a model or a cost is worse than one that omits
	// them.

	// ModelID is the full "provider/model" the session last used.
	ModelID string `json:"model_id,omitempty"`
	// ThinkingLevel is the reasoning-effort rung the session last used.
	//
	// OpenCode calls this a model "variant", but every 1.18.21 variant body is
	// purely a reasoning configuration, so the daemon carries it in its own
	// single thinking-level vocabulary rather than introducing a second,
	// parallel concept (MADR 0112 A14).
	ThinkingLevel string `json:"thinking_level,omitempty"`
	// Agent is the agent or mode name the session last ran under.
	Agent string `json:"agent,omitempty"`
	// Aggregate is whole-session accounting, or nil when the agent reported
	// none. See [AgentSessionUsage] for why nil and zero differ.
	Aggregate *AgentSessionUsage `json:"aggregate,omitempty"`
}

const (
	// ThreadSourceNative is a direct native thread/list result.
	ThreadSourceNative = "native"
	// ThreadSourceNativeSearch is an independently negotiated native search.
	ThreadSourceNativeSearch = "native_search"
	// ThreadSourceNativeTurns is independently negotiated turn pagination.
	ThreadSourceNativeTurns = "native_turns"
	// ThreadSourceNativeItems is independently negotiated item pagination.
	ThreadSourceNativeItems = "native_items"
	// ThreadSourceStableFallback labels bounded local filtering over stable
	// thread/list when the experimental search method is unavailable.
	ThreadSourceStableFallback = "stable_fallback"
)

// ThreadListOptions filters one bounded page of provider-native threads.
type ThreadListOptions struct {
	Cursor           string `json:"cursor,omitempty"`
	Limit            int    `json:"limit,omitempty"`
	Archived         bool   `json:"archived,omitempty"`
	ParentThreadID   string `json:"parent_thread_id,omitempty"`
	AncestorThreadID string `json:"ancestor_thread_id,omitempty"`
	SectionID        string `json:"section_id,omitempty"`
	ProjectID        string `json:"project_id,omitempty"`
	SearchTerm       string `json:"search_term,omitempty"`
}

// ThreadSearchOptions requests one search page.
type ThreadSearchOptions struct {
	Term     string `json:"term"`
	Cursor   string `json:"cursor,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Archived bool   `json:"archived,omitempty"`
}

// NativeThreadPage is a bounded, source-labelled page.
type NativeThreadPage struct {
	Threads         []AgentSessionMeta `json:"threads"`
	NextCursor      string             `json:"next_cursor,omitempty"`
	BackwardsCursor string             `json:"backwards_cursor,omitempty"`
	Source          string             `json:"source"`
	Limit           int                `json:"limit"`
	Truncated       bool               `json:"truncated,omitempty"`
}

// NativeThreadHistoryOptions requests one bounded native turn or item page.
// TurnID is used only for item pagination; ItemsView is used only for turns.
type NativeThreadHistoryOptions struct {
	ThreadID      string `json:"thread_id"`
	TurnID        string `json:"turn_id,omitempty"`
	Cursor        string `json:"cursor,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	SortDirection string `json:"sort_direction,omitempty"`
	ItemsView     string `json:"items_view,omitempty"`
}

// NativeThreadHistoryPage retains typed pagination metadata while keeping the
// experimental native entries opaque to provider-neutral callers.
type NativeThreadHistoryPage struct {
	Data            []json.RawMessage `json:"data"`
	NextCursor      string            `json:"next_cursor,omitempty"`
	BackwardsCursor string            `json:"backwards_cursor,omitempty"`
	Source          string            `json:"source"`
	Limit           int               `json:"limit"`
}

const (
	// ExecutionLabelSandboxed is structured command/exec under a profile or
	// sandbox policy.
	ExecutionLabelSandboxed = "SANDBOXED EXECUTION"
	// ExecutionLabelUnsandboxed is thread/shellCommand with full host access.
	ExecutionLabelUnsandboxed = "UNSANDBOXED SHELL — FULL HOST ACCESS"
	// ExecutionLabelStandalone is the default-off process/* surface.
	ExecutionLabelStandalone = "UNSANDBOXED STANDALONE PROCESS"

	// ExecutionAuditCommandExec records sandboxed argv execution. Audit
	// classes exist so a receipt cannot later be read as the wrong authority.
	ExecutionAuditCommandExec = "command_exec"
	// ExecutionAuditThreadShell records unsandboxed thread shell execution.
	ExecutionAuditThreadShell = "thread_shell"
	// ExecutionAuditProcess records a default-off standalone process.
	ExecutionAuditProcess = "standalone_process"

	// TerminalKindExec is the only sandboxed terminal kind.
	TerminalKindExec = "exec"
	// TerminalKindShell is an unsandboxed thread shell command.
	TerminalKindShell = "thread_shell"
	// TerminalKindBackground is a native Codex background terminal.
	TerminalKindBackground = "background"
	// TerminalKindProcess is a standalone unsandboxed process.
	TerminalKindProcess = "process"
)

// ExecRequest is an argv-based structured command. It never accepts shell
// text; callers use the separately labelled thread-shell operation for that.
type ExecRequest struct {
	Argv                []string           `json:"argv"`
	ThreadID            string             `json:"thread_id,omitempty"`
	CWD                 string             `json:"cwd,omitempty"`
	Env                 map[string]*string `json:"env,omitempty"`
	PermissionProfileID string             `json:"permission_profile_id,omitempty"`
	ProcessID           string             `json:"process_id,omitempty"`
	Stream              bool               `json:"stream,omitempty"`
	TTY                 bool               `json:"tty,omitempty"`
	Rows                int                `json:"rows,omitempty"`
	Cols                int                `json:"cols,omitempty"`
	OutputBytesCap      int                `json:"output_bytes_cap,omitempty"`
	Timeout             time.Duration      `json:"timeout,omitempty"`
}

// ExecResult is the bounded final structured-command result.
type ExecResult struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	Label      string `json:"label"`
	AuditClass string `json:"audit_class"`
}

// ExecutionResult labels a started asynchronous execution surface.
type ExecutionResult struct {
	Started    bool   `json:"started"`
	Label      string `json:"label"`
	AuditClass string `json:"audit_class"`
}

// TerminalInfo is the provider-neutral terminal registry projection.
type TerminalInfo struct {
	ID         string `json:"id"`
	ThreadID   string `json:"thread_id,omitempty"`
	Kind       string `json:"kind"`
	Label      string `json:"label"`
	Command    string `json:"command,omitempty"`
	CWD        string `json:"cwd,omitempty"`
	Generation int    `json:"generation,omitempty"`
	TTY        bool   `json:"tty,omitempty"`
	Running    bool   `json:"running"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	Native     bool   `json:"native,omitempty"`
	AuditClass string `json:"audit_class,omitempty"`
}

// TerminalPage is one bounded native terminal page.
type TerminalPage struct {
	Terminals  []TerminalInfo `json:"terminals"`
	NextCursor string         `json:"next_cursor,omitempty"`
	Limit      int            `json:"limit"`
}

// TerminalOutput is one sequence-numbered replayable terminal chunk.
type TerminalOutput struct {
	TerminalID string `json:"terminal_id"`
	Sequence   uint64 `json:"sequence"`
	Stream     string `json:"stream"`
	Data       []byte `json:"data"`
	CapReached bool   `json:"cap_reached,omitempty"`
}

// ExecutionEnvironment is administrator-owned configuration. ExecServerURL
// never crosses the phone boundary.
type ExecutionEnvironment struct {
	ID                    string        `json:"id"`
	ExecServerURL         string        `json:"-"`
	ConnectTimeout        time.Duration `json:"-"`
	RuntimeWorkspaceRoots []string      `json:"runtime_workspace_roots,omitempty"`
}

// EnvironmentStatus is an observational native status read.
type EnvironmentStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// EnvironmentInfo exposes only shell metadata and an optional canonical URI.
type EnvironmentInfo struct {
	ID        string `json:"id"`
	ShellName string `json:"shell_name"`
	ShellPath string `json:"shell_path"`
	CWD       string `json:"cwd,omitempty"`
}

// EnvironmentSelection is injected into turn/start.environments.
type EnvironmentSelection struct {
	EnvironmentID         string   `json:"environmentId"`
	CWD                   string   `json:"cwd"`
	RuntimeWorkspaceRoots []string `json:"runtimeWorkspaceRoots"`
}

// ProcessSpawnRequest is the default-off process/* standalone surface. The
// handle stays bound to the spawning connection and engine generation;
// ThreadID only files it under the requesting thread's terminal list so /ps
// and /stop can reach it, and is never sent upstream.
type ProcessSpawnRequest struct {
	Argv           []string           `json:"argv"`
	ThreadID       string             `json:"thread_id,omitempty"`
	CWD            string             `json:"cwd"`
	Env            map[string]*string `json:"env,omitempty"`
	TTY            bool               `json:"tty,omitempty"`
	Stream         bool               `json:"stream,omitempty"`
	Rows           int                `json:"rows,omitempty"`
	Cols           int                `json:"cols,omitempty"`
	OutputBytesCap int                `json:"output_bytes_cap,omitempty"`
	Timeout        time.Duration      `json:"timeout,omitempty"`
}

// ProcessInfo is an owned connection/generation-bound process handle.
type ProcessInfo struct {
	ID         string `json:"id"`
	Generation int    `json:"generation"`
	Label      string `json:"label"`
	AuditClass string `json:"audit_class"`
}

// ThreadSection is a provider-native user organization bucket.
type ThreadSection struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Icon  string `json:"icon,omitempty"`
	Color string `json:"color,omitempty"`
}

// ThreadSectionPage is a bounded page of sections.
type ThreadSectionPage struct {
	Sections   []ThreadSection `json:"sections"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

// ThreadSectionMutation creates or replaces a section's visible fields.
type ThreadSectionMutation struct {
	Name          string `json:"name"`
	Icon          string `json:"icon,omitempty"`
	Color         string `json:"color,omitempty"`
	AppearanceSet bool   `json:"appearance_set,omitempty"`
}

// ThreadDeletePreview reports descendant impact before permanent deletion.
type ThreadDeletePreview struct {
	DescendantIDs        []string `json:"descendant_ids"`
	HasLoadedDescendants bool     `json:"has_loaded_descendants"`
}

// ThreadDeleteResult distinguishes a direct acknowledgement from a read-back
// after an unknown write outcome.
type ThreadDeleteResult struct {
	Deleted             bool     `json:"deleted"`
	Reconciled          bool     `json:"reconciled,omitempty"`
	DescendantIDs       []string `json:"descendant_ids,omitempty"`
	FailedDescendantIDs []string `json:"failed_descendant_ids,omitempty"`
	Partial             bool     `json:"partial,omitempty"`
}

// Project is the bounded native project projection. Roots remain host paths
// and are returned only inside the explicitly opened authenticated browser.
type Project struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Roots     []string          `json:"roots"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Position  int64             `json:"position"`
	CreatedAt time.Time         `json:"created_at,omitzero"`
	UpdatedAt time.Time         `json:"updated_at,omitzero"`
}

// ProjectPage is one bounded native project page.
type ProjectPage struct {
	Projects   []Project `json:"projects"`
	NextCursor string    `json:"next_cursor,omitempty"`
	Limit      int       `json:"limit"`
	Truncated  bool      `json:"truncated,omitempty"`
}

// ProjectMutation is shared by create/import/update. IdempotencyKey is
// required for create/import and is derived from the phone envelope by the
// daemon rather than supplied as a second authority-bearing phone field.
type ProjectMutation struct {
	Name           string            `json:"name"`
	Roots          []string          `json:"roots"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	ThreadIDs      []string          `json:"thread_ids,omitempty"`
	IdempotencyKey string            `json:"-"`
}

// ProjectAssignment preserves the three upstream states: Set=false omits the
// field, Set=true with empty ID clears, and Set=true with an ID assigns.
type ProjectAssignment struct {
	ProjectID string `json:"project_id,omitempty"`
	Set       bool   `json:"set"`
}

// NativeThreadBrowser is the richer optional provider path. The generic
// AgentSessionLister remains unchanged for providers without this surface.
type NativeThreadBrowser interface {
	ListNativeThreads(context.Context, ThreadListOptions) (NativeThreadPage, error)
	SearchNativeThreads(context.Context, ThreadSearchOptions) (NativeThreadPage, error)
	ReadNativeThread(context.Context, string) (AgentSessionMeta, error)
	ListNativeThreadTurns(context.Context, NativeThreadHistoryOptions) (NativeThreadHistoryPage, error)
	ListNativeThreadItems(context.Context, NativeThreadHistoryOptions) (NativeThreadHistoryPage, error)
}

// NativeThreadLifecycleSession exposes archive and permanent-delete actions
// for the currently active provider-native thread.
type NativeThreadLifecycleSession interface {
	Session
	ArchiveNativeThread(context.Context, bool) error
	PreviewNativeDelete(context.Context) (ThreadDeletePreview, error)
	DeleteNativeThread(context.Context) (ThreadDeleteResult, error)
}

// ExecutionSession exposes the active thread's execution surfaces. Every
// method keeps its authority class distinct: RunSandboxedExec is argv-only
// under a permission profile, RunUnsandboxedShell is full host access, and
// SpawnStandaloneProcess is the default-off unsandboxed process surface. A
// caller can never reach one through another.
type ExecutionSession interface {
	Session
	RunSandboxedExec(context.Context, ExecRequest) (ExecResult, error)
	RunUnsandboxedShell(context.Context, string) (ExecutionResult, error)
	SpawnStandaloneProcess(context.Context, ProcessSpawnRequest) (ProcessInfo, error)
	WriteTerminal(context.Context, string, []byte, bool) error
	ResizeTerminal(context.Context, string, int, int) error
	ReplayTerminal(context.Context, string, uint64) ([]TerminalOutput, bool, error)
	ListTerminals(context.Context) ([]TerminalInfo, error)
	StopTerminal(context.Context, string) error
	StopAllTerminals(context.Context) (int, error)
}

// EnvironmentSession stores a validated host-configured selection for later
// turn/start injection. A nil selection explicitly disables sticky upstream
// selection; callers omit the operation entirely to preserve it.
type EnvironmentSession interface {
	Session
	SetExecutionEnvironment(context.Context, *EnvironmentSelection) error
}

// AgentSessionUsage is aggregate accounting for one agent-native session, as
// the agent itself reports it. It is a whole-session total, not a per-turn
// figure: [event.Usage] carries the latest turn (MADR 0112 A4).
//
// A nil *AgentSessionUsage means the agent reported no accounting at all. A
// present value whose fields are zero means a known-free or empty session —
// the distinction matters, because "unknown cost" and "no cost" read very
// differently next to a session in a picker.
type AgentSessionUsage struct {
	Input      int64 `json:"input,omitempty"`
	Output     int64 `json:"output,omitempty"`
	Reasoning  int64 `json:"reasoning,omitempty"`
	CacheRead  int64 `json:"cache_read,omitempty"`
	CacheWrite int64 `json:"cache_write,omitempty"`
	// CostUSD is nil when the agent reported no cost. Zero is a real value.
	CostUSD *float64 `json:"cost_usd,omitempty"`
}

// AgentSessionLister is optionally implemented by providers whose native
// protocol can enumerate resumable sessions. Callers must treat results as
// untrusted metadata and create a normal daemon session only after the user
// selects one.
type AgentSessionLister interface {
	ListAgentSessions(ctx context.Context) ([]AgentSessionMeta, error)
}

// ProjectMeta is one engine-known project or worktree a new session can be
// rooted at. It exists so a phone can pick a directory the agent already knows
// about instead of typing an absolute path it cannot browse.
type ProjectMeta struct {
	// ID is the agent's own project identifier.
	ID string `json:"id"`
	// Name is a display label. Providers that do not supply one derive it from
	// the worktree's base name — never from the full path, which is what the
	// phone is trying to avoid typing.
	Name string `json:"name,omitempty"`
	// Worktree is the absolute root directory. It is the value the client
	// copies into the ordinary CWD field, so it still passes every existing
	// pinned-CWD validation; discovery does not bypass that check.
	Worktree string `json:"worktree"`
}

// Workspace search kinds. A closed set: symbol search is deliberately absent
// because the 1.18.21 handler returns a hard-coded empty array, and advertising
// a search that can never match is worse than not offering it (MADR 0112 A5).
const (
	WorkspaceSearchText = "text"
	WorkspaceSearchFile = "file"
)

// WorkspaceEntry is one row of a directory listing.
//
// Path is always a normalized relative path inside the session's approved
// working directory. Upstream absolute paths are stripped rather than
// forwarded: the phone has no use for the daemon host's filesystem layout, and
// echoing it leaks the host's directory structure.
type WorkspaceEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Dir  bool   `json:"dir,omitempty"`
	// Ignored marks an entry the engine's ignore rules exclude. It is display
	// metadata; the daemon does not filter on it.
	Ignored bool `json:"ignored,omitempty"`
}

// WorkspaceContent is a bounded text view of one file.
//
// It is a viewer, not a byte-exact file API: 1.18.21 returns `.trim()`ed text,
// so trailing whitespace does not survive the round trip. Callers must not use
// it as a transport for content they intend to write back.
type WorkspaceContent struct {
	Path  string `json:"path"`
	Text  string `json:"text"`
	Bytes int    `json:"bytes"`
}

// WorkspaceMatch is one search hit. Line and Column are 1-based when known.
type WorkspaceMatch struct {
	Path   string `json:"path"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
	Text   string `json:"text,omitempty"`
}

// WorkspaceSearch is a bounded search result.
//
// Cap records the limit that actually applied, which differs by kind: file
// search accepts a request limit, while 1.18.21 hard-codes text search at ten
// matches with no parameter to raise it. Reporting the real cap keeps a client
// from implying the larger row budget applied (MADR 0112 A5).
type WorkspaceSearch struct {
	Kind      string           `json:"kind"`
	Matches   []WorkspaceMatch `json:"matches"`
	Cap       int              `json:"cap"`
	Truncated bool             `json:"truncated,omitempty"`
}

// WorkspaceSession is optionally implemented by sessions that can inspect their
// own working directory read-only.
//
// There is no write, apply, or execute method by construction: the surface is a
// viewer, and adding a mutation here would be a new decision rather than a new
// method (MADR 0112 A5/A12). Every path is relative to the session's already
// approved CWD; the implementation re-validates rather than trusting a caller.
type WorkspaceSession interface {
	Session
	ListWorkspace(ctx context.Context, path string) ([]WorkspaceEntry, error)
	ReadWorkspace(ctx context.Context, path string) (WorkspaceContent, error)
	SearchWorkspace(ctx context.Context, kind, query string) (WorkspaceSearch, error)
}

// ProjectCatalog is optionally implemented by providers that can enumerate the
// projects or worktrees their engine already knows. Providers that cannot are
// unaffected: the daemon returns the existing unsupported-operation error and
// clients keep their manual directory entry.
type ProjectCatalog interface {
	ListProjects(ctx context.Context) ([]ProjectMeta, error)
}
