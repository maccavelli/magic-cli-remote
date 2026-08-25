// Package event defines the daemon-owned event model for remote clients.
package event

import "time"

// Type discriminates event kinds in the mcremote.v1 control plane.
type Type string

// Domain event type strings for the mcremote.v1 control plane.
const (
	// TypeSessionStatus reports idle/running/error/disconnected session state.
	TypeSessionStatus Type = "session_status"
	// TypeUserMessage is the user's prompt text as recorded for the transcript.
	TypeUserMessage Type = "user_message"
	// TypeAssistantChunk is a streaming assistant text fragment.
	TypeAssistantChunk Type = "assistant_message_chunk"
	// TypeThoughtChunk is a streaming model "thought" / reasoning fragment.
	TypeThoughtChunk Type = "thought_chunk"
	// TypeToolCall announces a new tool invocation.
	TypeToolCall Type = "tool_call"
	// TypeToolUpdate updates progress or completion of a tool call.
	TypeToolUpdate Type = "tool_call_update"
	// TypePermission is a remote permission request awaiting the phone.
	TypePermission Type = "permission_request"
	// TypePermissionResolved ends a permission request (resolved or cancelled).
	TypePermissionResolved Type = "permission_resolved"
	// TypeQuestion is a multi-question form awaiting the phone (OpenCode questions).
	TypeQuestion Type = "question_request"
	// TypeQuestionResolved ends a question request (resolved or cancelled).
	TypeQuestionResolved Type = "question_resolved"
	// TypeTurnComplete marks the end of an agent turn.
	TypeTurnComplete Type = "turn_complete"
	// TypeError is a user-visible error for the session transcript.
	TypeError Type = "error"
	// TypeAvailableCommands advertises agent slash commands.
	TypeAvailableCommands Type = "available_commands"
	// TypePlan carries an ACP plan update.
	TypePlan Type = "plan"
	// TypeNotice is a daemon-originated informational line (e.g. the output of a
	// built-in slash command like /model or /help). Rendered as a system message,
	// distinct from TypeError so it is not styled as a failure.
	TypeNotice Type = "notice"
	// TypeUsage carries a token/context usage report (ACP usage_update). Advisory
	// telemetry for a context-window indicator; droppable under back-pressure.
	TypeUsage Type = "usage_update"
	// TypeMode carries the session's operating modes (ACP session modes): the
	// available set and/or the currently active mode. Emitted on session
	// create/load (full list + current) and on current_mode_update (current only).
	TypeMode Type = "session_mode"
	// TypeSessionConfig carries the session's agent-defined config options (ACP
	// session config options): each option's id, kind, current value, and — for
	// selects — the allowed values. Emitted on session create/load.
	TypeSessionConfig Type = "session_config"
	// TypeSessionCapabilities carries the agent's negotiated capabilities (ACP
	// initialize): whether it accepts image/audio prompt content and supports
	// session/load. Emitted once at session create/load so a client can gate its
	// UI (e.g. hide the image-attach button when the agent can't accept images).
	TypeSessionCapabilities Type = "session_capabilities"
	// TypeSessionTitle carries a session title/metadata update (ACP sessionInfoUpdate).
	TypeSessionTitle Type = "session_title"
	// TypeDiagnosticsChanged is a bounded marker telling a client its cached
	// diagnostics are stale. It carries no payload at all: the engine's global
	// events name servers and carry errors, and forwarding any of that would
	// leak exactly what the sanitized diagnostics report exists to withhold
	// (MADR 0112 A6).
	TypeDiagnosticsChanged Type = "diagnostics_changed"
	// TypeArtifact carries a file the agent produced — an assistant FilePart or
	// a completed tool's attachment. It uses the same native identity as every
	// other transcript row, so replay deduplicates it and a tombstone removes
	// it (MADR 0112 A3).
	TypeArtifact Type = "artifact"
	// TypeTranscriptRemove deletes already-delivered transcript content by
	// native identity: NativeMessageID alone removes the whole message,
	// plus NativePartID removes only that part.
	//
	// It exists because an agent can retract content it already streamed —
	// OpenCode's message.removed / message.part.removed — and a client that can
	// only append would keep showing text the agent has withdrawn. Unknown ids
	// are idempotent no-ops, so a late or duplicated tombstone is harmless
	// (MADR 0112 A3).
	TypeTranscriptRemove Type = "transcript_remove"
	// TypeRemoteCommands carries the canonical slash commands the daemon offers
	// in this session, each marked available or not with a reason (MADR 0023).
	// Emitted at session create and again whenever the answer changes (the agent
	// advertises commands, modes arrive, the first usage report lands), so a
	// client can render exactly what will work instead of guessing.
	TypeRemoteCommands Type = "remote_commands"
	// TypeApprovalSummary is a collapsing summary of the permissions
	// auto-approved in the current turn. The daemon re-emits the full list on
	// every approval; clients upsert on ApprovalGroupID rather than appending,
	// so an auto-armed session shows one card per turn instead of one line per
	// approval (MADR 0051 Part I).
	TypeApprovalSummary Type = "approval_summary"
	// TypeSubagents carries the session's currently-known sub-agents with
	// replace semantics, exactly like TypePlan: an event with entries replaces
	// the set, one with none clears it. Status only — sub-agent *output* never
	// reaches the transcript (MADR 0051 D6/D8).
	TypeSubagents Type = "subagents"
	// TypeCollaboration carries the independent Codex collaboration-mode
	// catalog and/or current id (MADR 0080 D9). Merge like session_mode:
	// a full list replaces; a current-only event keeps the stored list.
	TypeCollaboration Type = "collaboration_mode"
	// TypeGoal carries the current Codex thread goal, or a clear (Goal==nil).
	TypeGoal Type = "session_goal"
	// Codex activity events are bounded, typed projections of app-server
	// notifications. They never carry raw notification or item JSON.
	TypeCodexProgress            Type = "codex_progress"
	TypeCodexWarning             Type = "codex_warning"
	TypeCodexModelReroute        Type = "codex_model_reroute"
	TypeCodexModelVerification   Type = "codex_model_verification"
	TypeCodexTerminalInteraction Type = "codex_terminal_interaction"
	TypeCodexUnsupportedItem     Type = "codex_unsupported_item"
)

// Types returns every event type the daemon can emit, in declaration order.
//
// This is the enumeration clients are specified against, and
// TestEventTypesAreDocumented asserts each entry appears in the event-type list
// in docs/protocol-v1.md (MADR 0036 D6). A new type therefore fails the build
// until it is documented — the guard for `session_title`, which shipped as a
// live control event with zero mentions in the spec.
//
// Callers must not mutate the result.
func Types() []Type {
	return []Type{
		TypeSessionStatus,
		TypeUserMessage,
		TypeAssistantChunk,
		TypeThoughtChunk,
		TypeToolCall,
		TypeToolUpdate,
		TypePermission,
		TypePermissionResolved,
		TypeQuestion,
		TypeQuestionResolved,
		TypeTurnComplete,
		TypeError,
		TypeAvailableCommands,
		TypePlan,
		TypeNotice,
		TypeUsage,
		TypeMode,
		TypeSessionConfig,
		TypeSessionCapabilities,
		TypeSessionTitle,
		TypeArtifact,
		TypeDiagnosticsChanged,
		TypeTranscriptRemove,
		TypeRemoteCommands,
		TypeApprovalSummary,
		TypeSubagents,
		TypeCollaboration,
		TypeGoal,
		TypeCodexProgress,
		TypeCodexWarning,
		TypeCodexModelReroute,
		TypeCodexModelVerification,
		TypeCodexTerminalInteraction,
		TypeCodexUnsupportedItem,
	}
}

// IsControl reports event types that must not be dropped under back-pressure
// (permissions, status, turn lifecycle, tool state). Streaming chunks may still
// drop when a consumer is slow; control events block until delivered. This is
// the single source of truth shared by every provider transport (ACP, HTTP, and
// the fake) so their delivery guarantees cannot drift apart.
//
// Notices are control: they carry the reason for a state change (e.g. why a
// permission auto-cancelled), which matters most exactly when the client is slow
// enough to cause drops. Tool events are control because they drive a client
// state machine — dropping a terminal tool_call_update leaves a spinner running
// forever, which reads as a hang; their rate is bounded by tool executions, so
// blocking delivery is safe.
func IsControl(t Type) bool {
	switch t {
	case TypeSessionStatus,
		TypePermission,
		TypePermissionResolved,
		TypeQuestion,
		TypeQuestionResolved,
		TypeTurnComplete,
		TypeError,
		TypeNotice,
		TypeToolCall,
		TypeToolUpdate,
		TypeUserMessage,
		// Mode and config carry session state a client must not miss: a dropped
		// mode change leaves the UI showing the wrong active mode. Usage is
		// pure telemetry (a stale token count self-corrects on the next report)
		// and stays droppable.
		TypeMode,
		TypeSessionConfig,
		// Capabilities gate client UI (e.g. the image-attach button); a drop
		// would leave that UI wrong until the next session load.
		TypeSessionCapabilities,
		// The command list is low-rate and drives autocomplete; dropping one
		// leaves the composer offering commands that no longer work (or hiding
		// ones that do) until the next session load.
		TypeRemoteCommands,
		// Plan/todo strips are low-rate replace snapshots; dropping one leaves
		// multi-step work looking stuck or incomplete (MADR 0020 Sprint 2).
		TypePlan,
		// An artifact creates a transcript row; dropping one loses a file the
		// agent produced, with nothing later to re-announce it. Their rate is
		// bounded by actual file output.
		TypeArtifact,
		// A dropped tombstone is unrecoverable: the client would keep rendering
		// content the agent withdrew, with nothing later to correct it. Their
		// rate is bounded by actual retractions, so blocking delivery is safe.
		TypeTranscriptRemove,
		// Session title updates are low-rate metadata; dropping one leaves the
		// UI showing a stale title until the next update.
		TypeSessionTitle,
		// Approval summaries and the sub-agent set are low-rate replace
		// snapshots. Dropping the last one leaves a stale card (a short
		// approval list) or a panel showing sub-agents that already finished
		// (MADR 0051).
		TypeApprovalSummary,
		TypeSubagents,
		TypeCollaboration,
		TypeGoal,
		TypeCodexProgress,
		TypeCodexWarning,
		TypeCodexModelReroute,
		TypeCodexModelVerification,
		TypeCodexTerminalInteraction,
		TypeCodexUnsupportedItem:
		return true
	default:
		return false
	}
}

// IsInPlaceUpdate reports control events that mutate an existing transcript
// item rather than creating one. They keep the delivery guarantee of
// [IsControl] but carry no ordering constraint against streaming text: the
// item they update was positioned by an earlier event, so a client applying
// them out of order renders the same transcript.
//
// Identified by payload, not type alone: an update with no tool id cannot be
// matched to an existing item, so clients fall back to "the most recent tool
// card" — which *is* order-dependent. Those keep boundary semantics.
//
// Only tool updates qualify. approval_summary and subagents carry their own
// client-side replace keys (ApprovalGroupID; the type itself), but they are NOT
// in-place updates for transport purposes: chunkbuf's tool lane files every
// IsInPlaceUpdate event under ev.ToolID (chunkbuf.go:147, holdTool), so a
// keyless event would collide on "" and be mergeTool'd with an unrelated tool
// card — copying its name, kind, status and text across. See MADR 0051 §4.2.
func IsInPlaceUpdate(ev Event) bool {
	return ev.Type == TypeToolUpdate && ev.ToolID != ""
}

// Tool statuses carried on tool_call / tool_call_update events. The closed
// vocabulary is specified in docs/protocol-v1.md under `tool_status`.
const (
	ToolStatusPending   = "pending"
	ToolStatusRunning   = "running"
	ToolStatusCompleted = "completed"
	ToolStatusFailed    = "failed"
)

// IsTerminalToolStatus reports whether s is a final state for a tool call: no
// further update for that tool id can follow it.
//
// A provider may pass an unrecognised native status through (MADR 0036 D5).
// Those read as non-terminal, which is the safe default everywhere this is
// used: a coalescer may hold them briefly, but never past a boundary.
func IsTerminalToolStatus(s string) bool {
	return s == ToolStatusCompleted || s == ToolStatusFailed
}

// Plan entry statuses carried on plan events (ACP PlanEntryStatus values).
const (
	PlanStatusPending    = "pending"
	PlanStatusInProgress = "in_progress"
	PlanStatusCompleted  = "completed"
)

// Plan entry priorities carried on plan events (ACP PlanEntryPriority values).
const (
	PlanPriorityHigh   = "high"
	PlanPriorityMedium = "medium"
	PlanPriorityLow    = "low"
)

// Permission resolution statuses carried on permission_resolved events.
const (
	// PermissionStatusResolved means the client's decision was applied.
	PermissionStatusResolved = "resolved"
	// PermissionStatusCancelled means the request was abandoned (context
	// cancelled or session closed) and no decision will ever be applied.
	PermissionStatusCancelled = "cancelled"
)

// PermissionOption is a selectable choice on a permission_request event.
type PermissionOption struct {
	OptionID    string `json:"option_id"`
	Name        string `json:"name"`
	Kind        string `json:"kind,omitempty"`
	Description string `json:"description,omitempty"`
}

// QuestionItem is one prompt inside a question_request (multi-question form).
// Options use option_id == label for OpenCode reply wire format.
type QuestionItem struct {
	ID       string             `json:"id,omitempty"`
	Header   string             `json:"header,omitempty"`
	Text     string             `json:"text,omitempty"`
	Multiple bool               `json:"multiple,omitempty"`
	Custom   bool               `json:"custom,omitempty"`
	Secret   bool               `json:"secret,omitempty"`
	Options  []PermissionOption `json:"options,omitempty"`
}

// AvailableCommand is an agent slash command (ACP available_commands_update).
// Clients invoke these by sending a normal prompt starting with "/name …".
type AvailableCommand struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Hint        string `json:"hint,omitempty"`
}

// RemoteCommand is one canonical slash command as resolved for a session,
// carried on remote_commands events. Unavailable commands are still sent, with
// Reason, so a client can explain instead of silently omitting them.
type RemoteCommand struct {
	Name string `json:"name"`
	// Hint is the usage suffix, e.g. "[off]".
	Hint        string `json:"hint,omitempty"`
	Description string `json:"description,omitempty"`
	// Available is false when this session cannot run the command.
	Available bool `json:"available"`
	// Reason explains an unavailable command in words a user reads.
	Reason string `json:"reason,omitempty"`
}

// PlanEntry is a single task in an agent execution plan (ACP PlanEntry).
// Status is one of PlanStatus*; Priority is one of PlanPriority*.
type PlanEntry struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

// ApprovalItem is one auto-approved permission inside an approval_summary
// event. Clients render these as rows in a collapsible card.
type ApprovalItem struct {
	ToolName string    `json:"tool_name"` // "bash", "file", "shell", "mcp", …
	Detail   string    `json:"detail"`    // "git status", "header.html", …
	Time     time.Time `json:"time"`      // when the approval happened
}

// Approval-summary statuses carried on approval_summary events.
const (
	// ApprovalStatusRunning means the turn is still live and more approvals
	// may join the list.
	ApprovalStatusRunning = "running"
	// ApprovalStatusCompleted is the final summary for a turn (or for the
	// stretch of a turn that ran under auto-approve).
	ApprovalStatusCompleted = "completed"
)

// SubagentInfo is one sub-agent the provider has told us about, carried on
// subagents events. Status is one of SubagentStatus*.
//
// Status only: a sub-agent's own output never reaches the transcript, because
// it reports to the main agent over the engine's own channel and the parent's
// reply carries the conclusion (MADR 0051 D6).
type SubagentInfo struct {
	// ID is provider-scoped: an OpenCode child session id, a grok subagent_id,
	// or a codex agent thread id.
	ID string `json:"id"`
	// Name is the agent's role/kind — OpenCode's session info.agent, grok's
	// subagent_type, codex's agentPath basename.
	Name string `json:"name"`
	// Task is what it was asked to do — OpenCode's session title, grok's
	// description, codex's collab prompt.
	Task   string `json:"task,omitempty"`
	Status string `json:"status"`
}

// Sub-agent statuses carried on subagents events.
const (
	SubagentStatusRunning   = "running"
	SubagentStatusCompleted = "completed"
	SubagentStatusFailed    = "failed"
)

// Usage is a token/context report (ACP usage_update) carried on usage_update
// events: Used tokens currently in context out of a Size-token window.
type Usage struct {
	Used int `json:"used"`
	Size int `json:"size"`

	// The fields below are additive latest-turn accounting (MADR 0112 A4).
	// They describe the most recent assistant message, not a cumulative
	// session total: labelling a per-turn figure as a session total is the
	// specific error this split exists to avoid. Native aggregate session
	// accounting travels separately on AgentSessionMeta.
	//
	// Counts are non-negative; a provider reporting a negative or non-finite
	// value has them omitted rather than clamped, because a wrong number
	// presented confidently is worse than an absent one.
	Input      int64 `json:"input,omitempty"`
	Output     int64 `json:"output,omitempty"`
	Reasoning  int64 `json:"reasoning,omitempty"`
	CacheRead  int64 `json:"cache_read,omitempty"`
	CacheWrite int64 `json:"cache_write,omitempty"`

	// CostUSD is nil when the agent reported no cost at all. A present zero is
	// a real value — a known-free turn — and the two must stay distinguishable.
	CostUSD *float64 `json:"cost_usd,omitempty"`
}

// Artifact is a file the agent produced, surfaced to the transcript.
//
// Exactly one of URL or Data carries content, and either may be absent: an
// artifact the daemon cannot safely represent still reports its metadata rather
// than disappearing. The daemon never fetches URL — it only validates and
// forwards it, so a hostile link cannot turn the daemon into a fetcher
// (MADR 0112 A3, PLAN P5).
type Artifact struct {
	// Filename and MIME are bounded display metadata.
	Filename string `json:"filename,omitempty"`
	MIME     string `json:"mime,omitempty"`
	// Bytes is the decoded size when known, 0 otherwise.
	Bytes int64 `json:"bytes,omitempty"`
	// URL is a validated https URL with no userinfo. Never file:, http:, or
	// any other scheme.
	URL string `json:"url,omitempty"`
	// Data is bounded inline content, base64-encoded, at most
	// MaxArtifactInlineBytes decoded.
	Data string `json:"data,omitempty"`
	// Truncated marks an artifact whose content was withheld — too large,
	// malformed, or carried by a scheme the daemon will not forward. The
	// metadata is still accurate; only the payload is missing.
	Truncated bool `json:"truncated,omitempty"`
}

// MaxArtifactInlineBytes bounds inline artifact payloads after decoding.
const MaxArtifactInlineBytes = 524288

// CollaborationMode is one selectable collaboration preset (MADR 0080).
// Distinct from SessionMode: it is not an autonomy/permission mode.
type CollaborationMode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SessionMode is one selectable agent operating mode (ACP SessionMode).
type SessionMode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Dangerous marks a mode that removes a safety control the user would
	// otherwise have — today, one that answers permission requests without
	// them. Clients may style it distinctly and confirm before switching to it.
	//
	// The provider declares this rather than the client inferring it from the
	// id, because only the provider knows what a mode costs: goose has shipped
	// an `auto` mode for a while and it is goose's *default*, so id-matching
	// would alarm on a normal state (MADR 0044 D1 / plan §5.0).
	//
	// Optional: omitted means "no special treatment", which is what every
	// provider predating this field wants.
	Dangerous bool `json:"dangerous,omitempty"`
}

// ConfigOption is one agent-defined session config option (ACP
// SessionConfigOption). Kind is "select" or "boolean". For a select,
// CurrentValue is the chosen value id and Values lists the choices; for a
// boolean, BoolValue holds the state and Values is empty.
type ConfigOption struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Description  string              `json:"description,omitempty"`
	Kind         string              `json:"kind"`
	CurrentValue string              `json:"current_value,omitempty"`
	BoolValue    bool                `json:"bool_value,omitempty"`
	Values       []ConfigOptionValue `json:"values,omitempty"`
}

// ConfigOptionValue is one choice in a select-kind ConfigOption.
type ConfigOptionValue struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AttachmentInfo describes a non-text block sent with a prompt, carried on the
// user_message event so clients can show that the turn included an image/audio.
// The payload bytes are NOT carried back — only kind and media type.
type AttachmentInfo struct {
	Kind     string `json:"kind"`
	MimeType string `json:"mime_type,omitempty"`
}

// Capabilities are the agent's negotiated ACP capabilities relevant to a
// client's UI, carried on session_capabilities events.
type Capabilities struct {
	// Image/Audio report whether the agent accepts image/audio prompt content
	// (ACP promptCapabilities). A client should hide the corresponding attach
	// affordance when false — the daemon drops unsupported content.
	Image bool `json:"image"`
	Audio bool `json:"audio"`
	// LoadSession reports whether the agent supports resuming a prior session.
	LoadSession     bool `json:"load_session"`
	EmbeddedContext bool `json:"embedded_context"`
	ListSessions    bool `json:"list_sessions"`
	CloseSession    bool `json:"close_session"`
	MCPHTTP         bool `json:"mcp_http"`
	MCPSSE          bool `json:"mcp_sse"`
	MCPACP          bool `json:"mcp_acp"`

	// WorkspaceRead reports that the session can inspect its own working
	// directory read-only. False for every provider without the optional
	// interface, and the phone renders no workspace affordance when false
	// (MADR 0112 A5).
	WorkspaceRead bool `json:"workspace_read,omitempty"`

	// SkillRefresh reports that the session can recycle its idle engine
	// instance so newly authored skills become discoverable (MADR 0112 A10).
	SkillRefresh bool `json:"skill_refresh,omitempty"`

	// ShareState reports that the session's publication state can be read.
	// Share reports that the operator additionally permits changing it. They
	// are separate because an existing public link must stay visible even where
	// mutation is forbidden (MADR 0112 A8).
	ShareState bool `json:"share_state,omitempty"`
	Share      bool `json:"share,omitempty"`

	// Shell reports that the operator permits running a command directly in
	// the session's working directory. There is no read-only half: a command
	// either may be run or may not (MADR 0112 A9).
	Shell bool `json:"shell,omitempty"`
}

// Event is a single stream item for a session.
type Event struct {
	Type      Type      `json:"type"`
	SessionID string    `json:"session_id"`
	Timestamp time.Time `json:"timestamp"`

	// Seq is a per-session monotonic sequence number stamped by the session
	// manager as the event enters the history ring. Clients can use it to
	// dedupe the live-broadcast/history-replay overlap on reconnect and to
	// detect ordering. 0 means unstamped (event for an untracked session).
	Seq uint64 `json:"seq,omitempty"`

	// Replay marks an event the agent re-emitted while loading an existing
	// conversation (ACP session/load replays the whole history as ordinary
	// updates). Replayed events populate the daemon's history ring for cold
	// clients but are never broadcast live — a client that resumed a session
	// it already displays would append the entire conversation again.
	Replay bool `json:"replay,omitempty"`

	Status   string `json:"status,omitempty"`
	Text     string `json:"text,omitempty"`
	ToolID   string `json:"tool_id,omitempty"`
	ToolName string `json:"tool_name,omitempty"`

	// ToolKind classifies a tool_call / tool_call_update using the ACP tool
	// kind vocabulary (read, edit, delete, move, search, execute, think,
	// fetch, switch_mode, other). Clients use it to group actions ("Ran N
	// commands", "Edited N files"); empty when the agent did not say.
	ToolKind string `json:"tool_kind,omitempty"`

	Error string `json:"error,omitempty"`

	// Title is set on session_title events (ACP sessionInfoUpdate).
	Title string `json:"title,omitempty"`
	// TimedOut is set on permission_resolved events when the permission timed out
	// (the user did not respond in time).
	TimedOut bool `json:"timed_out,omitempty"`

	// ErrorKind classifies error events so clients can render actionable
	// cards instead of raw provider dumps: "quota" (hard usage/credit
	// limit), "rate_limit" (transient throttling, incl. 429/529/503),
	// "auth" (credentials rejected, 401/403 — re-authenticate the agent CLI
	// on the host), "server" (provider-side 500/502/504, usually
	// transient), "permission" (OS/sandbox denial, MADR 0069). Empty for
	// generic errors. The authoritative vocabulary is
	// internal/agenterr.Kind; docs/protocol-v1.md documents the wire
	// contract.
	ErrorKind string `json:"error_kind,omitempty"`

	// RetryAt is when a quota/rate limit is expected to lift, when the
	// provider's error message said so. Zero (omitted) when unknown.
	RetryAt time.Time `json:"retry_at,omitzero"`

	// Permission fields (type=permission_request, type=permission_resolved).
	PermissionID string             `json:"permission_id,omitempty"`
	Options      []PermissionOption `json:"options,omitempty"`
	// DeviceID and OptionID (type=permission_resolved only) record which
	// paired device resolved this permission and which option it chose
	// (MADR 0077 §1 — previously untracked). Both empty when the resolution
	// wasn't a single human's fresh tap: an auto-mode-arm sweep answering
	// previously pending permissions in bulk, a timeout auto-cancel, or the
	// engine self-reporting a decision made outside mcremote entirely (e.g.
	// via the provider's own CLI/TUI, caught on resync).
	DeviceID string `json:"device_id,omitempty"`
	OptionID string `json:"option_id,omitempty"`

	// Question fields (type=question_request, type=question_resolved).
	// QuestionID is the engine request id. Questions holds the multi-item form.
	QuestionID string         `json:"question_id,omitempty"`
	Questions  []QuestionItem `json:"questions,omitempty"`

	// Commands is set on available_commands events.
	Commands []AvailableCommand `json:"commands,omitempty"`

	// Entries is the full current plan on plan events (replace-semantics).
	// A plan clear (PlanRemoved) has no entries at all: omitempty drops any
	// zero-length slice, so the key is absent from the wire rather than
	// present and empty. Clients must read an absent Entries on a plan event
	// as "clear the plan", which is the opposite of the merge rule
	// session_mode uses for its absent lists (MADR 0046 I-1).
	Entries []PlanEntry `json:"entries,omitempty"`

	// ApprovalGroupID is the stable client-side upsert key for approval_summary
	// events: events sharing a group id replace one another. The key carries the
	// rendering contract; the type is deliberately NOT in IsInPlaceUpdate
	// (MADR 0051 §4.2).
	ApprovalGroupID string `json:"approval_group_id,omitempty"`

	// Approvals is the full chronological list of auto-approved requests on an
	// approval_summary event. Replace-semantics, like Entries on plan.
	Approvals []ApprovalItem `json:"approvals,omitempty"`

	// Subagents is the full current sub-agent set on subagents events. As with
	// Entries on plan, omitempty drops a zero-length slice, so an absent key
	// means "clear the set" — the opposite of the merge rule session_mode uses
	// for its absent lists (MADR 0046 I-1).
	Subagents []SubagentInfo `json:"subagents,omitempty"`

	// AgentSessionID is the provider-native session id (e.g. ACP sessionId).
	AgentSessionID string `json:"agent_session_id,omitempty"`

	// StopReason is set on turn_complete when known.
	StopReason string `json:"stop_reason,omitempty"`

	// Usage is set on usage_update events (token/context report).
	Usage *Usage `json:"usage,omitempty"`

	// Modes is the full available mode list on session_mode events emitted at
	// session create/load. Nil on a current_mode_update (only CurrentModeID
	// changes) so a client keeps the list it already has.
	Modes []SessionMode `json:"modes,omitempty"`

	// CurrentModeID is the active mode id on session_mode events.
	CurrentModeID string `json:"current_mode_id,omitempty"`
	// ApprovalsReviewer is the independent Codex reviewer axis when known.
	ApprovalsReviewer string `json:"approvals_reviewer,omitempty"`

	// CollaborationModes is the full collaboration catalog on
	// collaboration_mode events. Nil on a current-only update.
	CollaborationModes []CollaborationMode `json:"collaboration_modes,omitempty"`
	// CurrentCollaborationModeID is the active collaboration-mode id.
	CurrentCollaborationModeID string `json:"current_collaboration_mode_id,omitempty"`

	// ConfigOptions is the full option set on session_config events.
	ConfigOptions []ConfigOption `json:"config_options,omitempty"`

	// Capabilities is the agent's negotiated capability set on
	// session_capabilities events.
	Capabilities *Capabilities `json:"capabilities,omitempty"`

	// Attachments describes non-text content sent with a prompt, on
	// user_message events. Descriptors only — never the payload bytes.
	Attachments []AttachmentInfo `json:"attachments,omitempty"`

	// RemoteCommands is the canonical command list on remote_commands events.
	RemoteCommands []RemoteCommand `json:"remote_commands,omitempty"`

	// Goal is the current thread goal on session_goal events. Nil means
	// cleared / absent (MADR 0080 D16).
	Goal *Goal `json:"goal,omitempty"`

	// Artifact is the bounded typed body for artifact events.
	Artifact *Artifact `json:"artifact,omitempty"`

	// Codex is the bounded typed body for codex_* events.
	Codex *CodexPayload `json:"codex,omitempty"`

	// NativeMessageID and NativePartID carry the agent's own identity for a
	// transcript-bearing event, so live streaming and replay describe the same
	// row instead of two rows that merely look alike (MADR 0112 A3).
	//
	// Empty means the provider does not supply one. Such rows keep the previous
	// append-only behaviour and are never matched, replaced or removed by
	// identity — guessing an id for a row that has none would let one retraction
	// delete unrelated content.
	NativeMessageID string `json:"native_message_id,omitempty"`
	NativePartID    string `json:"native_part_id,omitempty"`

	// Replace marks an authoritative full snapshot of the identified part:
	// consumers discard what they already hold for that identity and take this
	// instead. False is an append delta.
	//
	// This is the difference between resume rebuilding a transcript and resume
	// doubling it: replayed parts and full `message.part.updated` frames repeat
	// text already streamed, so appending them duplicates the conversation.
	Replace bool `json:"replace,omitempty"`
}

// Goal is the bounded Codex thread-goal snapshot.
type Goal struct {
	Objective   string `json:"objective,omitempty"`
	Status      string `json:"status,omitempty"`
	TokenBudget int    `json:"token_budget,omitempty"`
	TokenUsage  int    `json:"token_usage,omitempty"`
}
