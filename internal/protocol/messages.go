// Package protocol defines the mcremote.v1 WebSocket JSON envelope.
package protocol

import (
	"encoding/json"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
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
	TypeSessionSetMode       = "session.set_mode"
	TypeSessionSetConfig     = "session.set_config_option"
	TypeSessionHistory       = "session.history"
	TypeSessionHistoryResult = "session.history_result"
	TypeOK                   = "ok"
	TypeError                = "error"
	TypeEvent                = "event"
	TypePing                 = "ping"
	TypePong                 = "pong"
	TypeProvidersList        = "providers.list"
	TypeProvidersResult      = "providers.list_result"
	TypeModelsList           = "models.list"
	TypeModelsResult         = "models.list_result"
	TypeAgentsList           = "agents.list"
	TypeAgentsResult         = "agents.list_result"
	TypeCommandsList         = "commands.list"
	TypeCommandsResult       = "commands.list_result"
	TypeSessionFork          = "session.fork"
	TypeSessionRevert        = "session.revert"
	TypeSessionUnrevert      = "session.unrevert"
	TypeSessionDiff          = "session.diff"
	TypeSessionDiffResult    = "session.diff_result"
	TypePermissionRespond    = "permission.respond"
	TypeQuestionRespond      = "question.respond"
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
	// HomeDir is the daemon user's home directory — the default working
	// directory for new sessions. Lets clients pre-populate path inputs.
	HomeDir string `json:"home_dir,omitempty"`
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
	// semantics: grok passes a -m flag; opencode pins {providerID, id} on the
	// engine session at create). Empty uses the provider/agent default.
	Model string `json:"model,omitempty"`
	// Agent optionally selects the OpenCode agent name (e.g. "build", "plan")
	// sent on each prompt_async. Empty uses the engine default. Prefer values
	// from agents.list. Ignored by non-OpenCode providers (MADR 0020 Sprint 3).
	Agent string `json:"agent,omitempty"`
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
	// Attachments are optional non-text content blocks (image/audio) sent
	// alongside Text. Only forwarded to agents advertising the matching ACP
	// promptCapability; unsupported ones are dropped by the provider.
	Attachments []PromptAttachment `json:"attachments,omitempty"`
}

// PromptAttachment is a non-text prompt content block. Kind is "image" or
// "audio"; Data is base64-encoded; MimeType is the media type (e.g. "image/png").
type PromptAttachment struct {
	Kind     string `json:"kind"`
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}

// SessionSetModePayload switches the active session mode (session.set_mode).
type SessionSetModePayload struct {
	SessionID string `json:"session_id"`
	ModeID    string `json:"mode_id"`
}

// SessionSetConfigPayload changes one session config option
// (session.set_config_option). Kind is "select" or "boolean"; for boolean,
// Value is "true"/"false"; for select, Value is the chosen value id.
type SessionSetConfigPayload struct {
	SessionID string `json:"session_id"`
	OptionID  string `json:"option_id"`
	Kind      string `json:"kind"`
	Value     string `json:"value"`
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

// ModelsListPayload requests a model picker catalog for one provider.
type ModelsListPayload struct {
	// Provider is a registered provider id (grok, opencode, fake, …).
	Provider string `json:"provider"`
}

// ModelsResultPayload is the typed body of models.list_result. The catalog
// fields reuse the shared picker schema so multi-select surfaces can share
// the same client widget later.
type ModelsResultPayload struct {
	Provider string `json:"provider"`
	// Kind is "single" or "multi" (models are single-select today).
	Kind string `json:"kind"`
	// Source is live | static | merged.
	Source string `json:"source,omitempty"`
	// Options is the ordered picker rows.
	Options []picker.Option `json:"options"`
	// DefaultIDs are suggested selections (first used for single-select).
	DefaultIDs []string `json:"default_ids,omitempty"`
	// AllowCustom permits free-text model ids not in Options.
	AllowCustom bool `json:"allow_custom,omitempty"`
	MinSelect   int  `json:"min_select,omitempty"`
	MaxSelect   int  `json:"max_select,omitempty"`
}

// ModelsResultFromCatalog builds a models.list_result body.
func ModelsResultFromCatalog(provider string, cat picker.Catalog) ModelsResultPayload {
	cat = cat.Normalize()
	return ModelsResultPayload{
		Provider:    provider,
		Kind:        string(cat.Kind),
		Source:      string(cat.Source),
		Options:     cat.Options,
		DefaultIDs:  cat.DefaultIDs,
		AllowCustom: cat.AllowCustom,
		MinSelect:   cat.MinSelect,
		MaxSelect:   cat.MaxSelect,
	}
}

// AgentsListPayload requests an agent-name picker catalog for one provider.
type AgentsListPayload struct {
	// Provider is a registered provider id (opencode, …).
	Provider string `json:"provider"`
}

// AgentsResultPayload is the typed body of agents.list_result. Same catalog
// schema as models.list_result (shared picker widget).
type AgentsResultPayload = ModelsResultPayload

// AgentsResultFromCatalog builds an agents.list_result body.
func AgentsResultFromCatalog(provider string, cat picker.Catalog) AgentsResultPayload {
	return ModelsResultFromCatalog(provider, cat)
}

// CommandsListPayload requests a slash-command catalog for one provider.
type CommandsListPayload struct {
	Provider string `json:"provider"`
	// SessionID is optional. With a live session the canonical commands in the
	// catalog reflect what that session can actually run; without it, only the
	// ones that work on every session of the provider are enabled.
	SessionID string `json:"session_id,omitempty"`
}

// CommandsResultPayload is commands.list_result (same catalog schema as models).
type CommandsResultPayload = ModelsResultPayload

// CommandsResultFromCatalog builds a commands.list_result body.
func CommandsResultFromCatalog(provider string, cat picker.Catalog) CommandsResultPayload {
	return ModelsResultFromCatalog(provider, cat)
}

// SessionForkPayload forks a session (OpenCode POST …/fork).
type SessionForkPayload struct {
	SessionID string `json:"session_id"`
	// MessageID is optional; empty forks at the engine default point.
	MessageID string `json:"message_id,omitempty"`
}

// SessionRevertPayload reverts a message (OpenCode POST …/revert).
type SessionRevertPayload struct {
	SessionID string `json:"session_id"`
	MessageID string `json:"message_id"`
	PartID    string `json:"part_id,omitempty"`
}

// SessionUnrevertPayload restores reverted messages.
type SessionUnrevertPayload struct {
	SessionID string `json:"session_id"`
}

// SessionDiffPayload requests a file-change summary (OpenCode GET …/diff).
type SessionDiffPayload struct {
	SessionID string `json:"session_id"`
	MessageID string `json:"message_id,omitempty"`
}

// SessionDiffResultPayload is the body of session.diff_result.
type SessionDiffResultPayload struct {
	SessionID string `json:"session_id"`
	// Summary is a multi-line human-readable strip (also emitted as notice).
	Summary string `json:"summary"`
}

// PermissionRespondPayload answers a permission_request event.
type PermissionRespondPayload struct {
	SessionID    string `json:"session_id"`
	PermissionID string `json:"permission_id"`
	// OptionID is the selected permission option (required unless Cancelled).
	OptionID  string `json:"option_id,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

// QuestionRespondPayload answers a question_request event (MADR 0020 Sprint 1b).
// answers[i] is the selected label list for questions[i]; cancelled rejects.
type QuestionRespondPayload struct {
	SessionID  string     `json:"session_id"`
	QuestionID string     `json:"question_id"`
	Answers    [][]string `json:"answers,omitempty"`
	Cancelled  bool       `json:"cancelled,omitempty"`
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
