// Package event defines the daemon-owned event model for remote clients.
package event

import "time"

// Type discriminates event kinds in the mcremote.v1 control plane.
type Type string

const (
	TypeSessionStatus   Type = "session_status"
	TypeUserMessage     Type = "user_message"
	TypeAssistantChunk  Type = "assistant_message_chunk"
	TypeThoughtChunk    Type = "thought_chunk"
	TypeToolCall        Type = "tool_call"
	TypeToolUpdate      Type = "tool_call_update"
	TypePermission      Type = "permission_request"
	TypeTurnComplete    Type = "turn_complete"
	TypeError           Type = "error"
)

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
}
