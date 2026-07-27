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
	// TypeRemoteCommands carries the canonical slash commands the daemon offers
	// in this session, each marked available or not with a reason (MADR 0023).
	// Emitted at session create and again whenever the answer changes (the agent
	// advertises commands, modes arrive, the first usage report lands), so a
	// client can render exactly what will work instead of guessing.
	TypeRemoteCommands Type = "remote_commands"
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
		TypeRemoteCommands,
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
		// Session title updates are low-rate metadata; dropping one leaves the
		// UI showing a stale title until the next update.
		TypeSessionTitle:
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
func IsInPlaceUpdate(ev Event) bool {
	return ev.Type == TypeToolUpdate && ev.ToolID != ""
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
	OptionID string `json:"option_id"`
	Name     string `json:"name"`
	Kind     string `json:"kind,omitempty"`
}

// QuestionItem is one prompt inside a question_request (multi-question form).
// Options use option_id == label for OpenCode reply wire format.
type QuestionItem struct {
	Header   string             `json:"header,omitempty"`
	Text     string             `json:"text,omitempty"`
	Multiple bool               `json:"multiple,omitempty"`
	Custom   bool               `json:"custom,omitempty"`
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

// Usage is a token/context report (ACP usage_update) carried on usage_update
// events: Used tokens currently in context out of a Size-token window.
type Usage struct {
	Used int `json:"used"`
	Size int `json:"size"`
}

// SessionMode is one selectable agent operating mode (ACP SessionMode).
type SessionMode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
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
	// cards instead of raw provider dumps: "quota" (hard usage/credit limit),
	// "rate_limit" (transient throttling). Empty for generic errors.
	ErrorKind string `json:"error_kind,omitempty"`

	// RetryAt is when a quota/rate limit is expected to lift, when the
	// provider's error message said so. Zero (omitted) when unknown.
	RetryAt time.Time `json:"retry_at,omitzero"`

	// Permission fields (type=permission_request, type=permission_resolved).
	PermissionID string             `json:"permission_id,omitempty"`
	Options      []PermissionOption `json:"options,omitempty"`

	// Question fields (type=question_request, type=question_resolved).
	// QuestionID is the engine request id. Questions holds the multi-item form.
	QuestionID string         `json:"question_id,omitempty"`
	Questions  []QuestionItem `json:"questions,omitempty"`

	// Commands is set on available_commands events.
	Commands []AvailableCommand `json:"commands,omitempty"`

	// Entries is the full current plan on plan events (replace-semantics).
	// A plan clear (PlanRemoved) carries an empty, non-nil list.
	Entries []PlanEntry `json:"entries,omitempty"`

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
}
