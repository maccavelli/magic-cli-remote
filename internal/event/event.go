// Package event defines the daemon-owned event model for remote clients.
package event

import "time"

// Type discriminates event kinds in the mcremote.v1 control plane.
type Type string

const (
	TypeSessionStatus      Type = "session_status"
	TypeUserMessage        Type = "user_message"
	TypeAssistantChunk     Type = "assistant_message_chunk"
	TypeThoughtChunk       Type = "thought_chunk"
	TypeToolCall           Type = "tool_call"
	TypeToolUpdate         Type = "tool_call_update"
	TypePermission         Type = "permission_request"
	TypePermissionResolved Type = "permission_resolved"
	TypeTurnComplete       Type = "turn_complete"
	TypeError              Type = "error"
	TypeAvailableCommands  Type = "available_commands"
	TypePlan               Type = "plan"
	// TypeNotice is a daemon-originated informational line (e.g. the output of a
	// built-in slash command like /model or /help). Rendered as a system message,
	// distinct from TypeError so it is not styled as a failure.
	TypeNotice Type = "notice"
)

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

// AvailableCommand is an agent slash command (ACP available_commands_update).
// Clients invoke these by sending a normal prompt starting with "/name …".
type AvailableCommand struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Hint        string `json:"hint,omitempty"`
}

// PlanEntry is a single task in an agent execution plan (ACP PlanEntry).
// Status is one of PlanStatus*; Priority is one of PlanPriority*.
type PlanEntry struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
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

	// Commands is set on available_commands events.
	Commands []AvailableCommand `json:"commands,omitempty"`

	// Entries is the full current plan on plan events (replace-semantics).
	// A plan clear (PlanRemoved) carries an empty, non-nil list.
	Entries []PlanEntry `json:"entries,omitempty"`

	// AgentSessionID is the provider-native session id (e.g. ACP sessionId).
	AgentSessionID string `json:"agent_session_id,omitempty"`

	// StopReason is set on turn_complete when known.
	StopReason string `json:"stop_reason,omitempty"`
}
