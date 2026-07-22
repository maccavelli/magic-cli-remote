// Package protocol defines the mcremote.v1 WebSocket JSON envelope.
package protocol

import (
	"encoding/json"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// Version is the protocol version carried on every message.
const Version = 1

// Message types (client ↔ server).
const (
	TypeAuth                 = "auth"
	TypeAuthOK               = "auth_ok"
	TypeAuthError            = "auth_error"
	TypePairClaim            = "pair.claim"
	TypePairOK               = "pair_ok"
	TypePairError            = "pair_error"
	TypeSessionCreate        = "session.create"
	TypeSessionCreated       = "session.created"
	TypeSessionList          = "session.list"
	TypeSessionListResult    = "session.list_result"
	TypeSessionClose         = "session.close"
	TypeSessionDelete        = "session.delete"
	TypeSessionPrompt        = "session.prompt"
	TypeSessionCancel        = "session.cancel"
	TypeSessionHistory       = "session.history"
	TypeSessionHistoryResult = "session.history_result"
	TypeOK                   = "ok"
	TypeError                = "error"
	TypeEvent                = "event"
	TypePing                 = "ping"
	TypePong                 = "pong"
	TypeProvidersList        = "providers.list"
	TypeProvidersResult      = "providers.list_result"
	TypePermissionRespond    = "permission.respond"
)

// Envelope is the common WS message wrapper.
type Envelope struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	// Token is only used on auth messages for convenience (also accepted in payload).
	Token string `json:"token,omitempty"`
}

// AuthPayload is the body of an auth request.
type AuthPayload struct {
	Token string `json:"token"`
}

// AuthOKPayload is returned on successful auth.
type AuthOKPayload struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
}

// PairClaimPayload exchanges a short-lived pair code for a durable device token.
type PairClaimPayload struct {
	Code string `json:"code"`
	// Name optionally overrides the device label from the pending code.
	Name string `json:"name,omitempty"`
}

// PairOKPayload returns the one-shot durable token after a successful claim.
type PairOKPayload struct {
	Token      string `json:"token"`
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
}

// ErrorPayload is a generic error body.
type ErrorPayload struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// SessionCreatePayload requests a new session.
type SessionCreatePayload struct {
	Provider string `json:"provider"`
	Name     string `json:"name,omitempty"`
	CWD      string `json:"cwd,omitempty"`
	// Model optionally selects the agent model for this session (provider
	// semantics: grok passes a -m flag; opencode sets the ACP "model" session
	// config option). Empty uses the provider/agent default.
	Model string `json:"model,omitempty"`
	// AgentSessionID resumes a provider-native session (e.g. ACP session/load).
	AgentSessionID string `json:"agent_session_id,omitempty"`
	// SessionID optionally forces the mcremote session id (used when reconnecting a persisted record).
	SessionID string `json:"session_id,omitempty"`
}

// SessionIDPayload identifies a session.
type SessionIDPayload struct {
	SessionID string `json:"session_id"`
}

// SessionHistoryPayload requests buffered event replay for a session.
// SinceSeq / Limit enable paging (Phase 3.5); omitted fields mean "from the
// start" / server default page size. Older clients that only send session_id
// keep working.
type SessionHistoryPayload struct {
	SessionID string `json:"session_id"`
	// SinceSeq is exclusive: only events with Seq > SinceSeq are returned.
	SinceSeq uint64 `json:"since_seq,omitempty"`
	// Limit caps events in this response (server clamps; 0 = default).
	Limit int `json:"limit,omitempty"`
}

// SessionPromptPayload sends a user prompt.
type SessionPromptPayload struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
}

// SessionListResultPayload lists sessions.
type SessionListResultPayload struct {
	Sessions []session.Meta `json:"sessions"`
}

// EventPayload wraps a domain event for push.
type EventPayload struct {
	Event event.Event `json:"event"`
}

// SessionHistoryResultPayload replays a session's buffered events. Each element
// of Events is the identical JSON shape as the Event field of a live EventPayload
// — clients feed them straight back through the same reducer. An unknown or
// never-active session yields an empty Events list, not an error.
//
// Truncated + NextSinceSeq support paging: when Truncated is true, request
// again with since_seq=NextSinceSeq until Truncated is false.
type SessionHistoryResultPayload struct {
	SessionID    string        `json:"session_id"`
	Events       []event.Event `json:"events"`
	Truncated    bool          `json:"truncated,omitempty"`
	NextSinceSeq uint64        `json:"next_since_seq,omitempty"`
}

// ProviderInfoPayload is one entry in providers.list_result (Phase 4.7).
type ProviderInfoPayload struct {
	ID    string `json:"id"`
	Ready bool   `json:"ready"`
}

// ProvidersResultPayload is the typed body of providers.list_result.
type ProvidersResultPayload struct {
	Providers []ProviderInfoPayload `json:"providers"`
}

// PermissionRespondPayload answers a permission_request event.
type PermissionRespondPayload struct {
	SessionID    string `json:"session_id"`
	PermissionID string `json:"permission_id"`
	// OptionID is the selected permission option (required unless Cancelled).
	OptionID  string `json:"option_id,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

// NewEnvelope builds a versioned envelope.
func NewEnvelope(typ, id string, payload any) (Envelope, error) {
	env := Envelope{V: Version, Type: typ, ID: id}
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return Envelope{}, err
		}
		env.Payload = b
	}
	return env, nil
}

// DecodePayload unmarshals env.Payload into dest.
func DecodePayload(env Envelope, dest any) error {
	if len(env.Payload) == 0 {
		return nil
	}
	return json.Unmarshal(env.Payload, dest)
}
