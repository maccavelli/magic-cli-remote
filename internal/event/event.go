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

// Event is a single stream item for a session.
type Event struct {
	Type      Type      `json:"type"`
	SessionID string    `json:"session_id"`
	Timestamp time.Time `json:"timestamp"`

	Status   string `json:"status,omitempty"`
	Text     string `json:"text,omitempty"`
	ToolID   string `json:"tool_id,omitempty"`
	ToolName string `json:"tool_name,omitempty"`
	Error    string `json:"error,omitempty"`

	// Permission fields (type=permission_request, type=permission_resolved).
	PermissionID string             `json:"permission_id,omitempty"`
	Options      []PermissionOption `json:"options,omitempty"`

	// Commands is set on available_commands events.
	Commands []AvailableCommand `json:"commands,omitempty"`

	// AgentSessionID is the provider-native session id (e.g. ACP sessionId).
	AgentSessionID string `json:"agent_session_id,omitempty"`

	// StopReason is set on turn_complete when known.
	StopReason string `json:"stop_reason,omitempty"`
}
