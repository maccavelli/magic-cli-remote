// Package protocol defines the mcremote WebSocket JSON envelope (v1, and
// the negotiated v2 extensions of MADR 0068).
package protocol

import (
	"encoding/json"

	"github.com/maccavelli/magic-cli-remote/internal/event"
	"github.com/maccavelli/magic-cli-remote/internal/picker"
	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/session"
)

// Protocol versions (MADR 0068 D1). V2 is v1 plus negotiated capabilities;
// the envelope format itself is unchanged between them.
const (
	V1 = 1
	V2 = 2
)

// Version is the default (pre-negotiation) protocol version carried on
// every message. Connections speak V1 until an auth/pair.claim negotiates
// higher; envelopes are then accepted for any version up to the negotiated
// one (fan-out event frames may still carry V1 — see protocol-v2.md).
const Version = V1

// SupportedVersions lists the versions this build speaks, ascending.
var SupportedVersions = []int{V1, V2}

// CloseReplaced is the WebSocket close code (RFC 6455 application range)
// sent to a device's older connections when a newer one authenticates
// (MADR 0068 D3): one live socket per device. A client receiving it must
// not auto-reconnect — a newer connection of the same device exists, and
// reconnecting would fight it.
const CloseReplaced = 4001

// CloseReplacedReason is the close frame's reason string.
const CloseReplacedReason = "replaced"

// NegotiateVersion picks the highest mutually supported version from a
// client's offer. An absent/empty offer means a v1 client. Returns 0 when
// the offer is non-empty but has no mutual version (caller rejects with
// ErrBadVersion).
func NegotiateVersion(offered []int) int {
	if len(offered) == 0 {
		return V1
	}
	best := 0
	for _, v := range offered {
		for _, s := range SupportedVersions {
			if v == s && v > best {
				best = v
			}
		}
	}
	return best
}

// Message types (client ↔ server).
const (
	TypeAuth                     = "auth"
	TypeAuthOK                   = "auth_ok"
	TypeAuthError                = "auth_error"
	TypePairClaim                = "pair.claim"
	TypePairOK                   = "pair_ok"
	TypePairError                = "pair_error"
	TypeSessionCreate            = "session.create"
	TypeSessionCreated           = "session.created"
	TypeSessionList              = "session.list"
	TypeSessionListResult        = "session.list_result"
	TypeSessionClose             = "session.close"
	TypeSessionDelete            = "session.delete"
	TypeSessionRelease           = "session.release" // MADR 0078: hand off to another device
	TypeSessionClaim             = "session.claim"   // MADR 0078: take a released session
	TypeSessionPrompt            = "session.prompt"
	TypeSessionCancel            = "session.cancel"
	TypeSessionSetMode           = "session.set_mode"
	TypeSessionSetConfig         = "session.set_config_option"
	TypeSessionHistory           = "session.history"
	TypeSessionHistoryResult     = "session.history_result"
	TypeSessionPendingAsks       = "session.pending_asks"
	TypeSessionPendingAsksResult = "session.pending_asks_result"
	TypeOK                       = "ok"
	TypeError                    = "error"
	TypeEvent                    = "event"
	TypePing                     = "ping"
	TypePong                     = "pong"
	TypeProvidersList            = "providers.list"
	TypeProvidersResult          = "providers.list_result"
	TypeModelsList               = "models.list"
	TypeModelsResult             = "models.list_result"
	TypeAgentsList               = "agents.list"
	TypeAgentsResult             = "agents.list_result"
	TypeAgentSessionsList        = "agent_sessions.list"
	TypeAgentSessionsResult      = "agent_sessions.list_result"
	TypeCommandsList             = "commands.list"
	TypeCommandsResult           = "commands.list_result"
	TypeSessionFork              = "session.fork"
	TypeSessionRevert            = "session.revert"
	TypeSessionUnrevert          = "session.unrevert"
	TypeSessionDiff              = "session.diff"
	TypeSessionDiffResult        = "session.diff_result"
	TypeSessionRename            = "session.rename"
	TypeSessionRenameResult      = "session.rename_result"
	TypeSessionDiagnostics       = "session.diagnostics"
	TypeSessionDiagnosticsResult = "session.diagnostics_result"
	TypePermissionRespond        = "permission.respond"
	TypeQuestionRespond          = "question.respond"
	// Signed-receipt read surface (MADR 0078 D8): a device reads its OWN chain.
	TypeReceiptsList       = "receipts.list"
	TypeReceiptsListResult = "receipts.list_result"
	// Paired-device roster (MADR 0078): lets a device pick a handoff target.
	TypeDevicesList       = "devices.list"
	TypeDevicesListResult = "devices.list_result"
	// Signed receipts for permission decisions (MADR 0077 P7).
	TypePermissionReceiptRequest = "permission.receipt_request" // daemon -> phone
	TypePermissionReceipt        = "permission.receipt"         // phone -> daemon
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

// AuthResumePayload rides on a v2 auth to resume within the token window
// (MADR 0068 D4, shape R1: piggybacked on auth to save one round trip).
type AuthResumePayload struct {
	// Token is the resume_token from the previous connection's auth_ok.
	Token string `json:"token"`
	// Sessions maps session id → the client's last handled seq.
	Sessions map[string]uint64 `json:"sessions,omitempty"`
}

// AuthPayload is the body of an auth request.
type AuthPayload struct {
	Token string `json:"token"`
	// Protocols is the client's version offer (MADR 0068 D1). Absent means
	// a v1 client; the server picks the highest mutual version.
	Protocols []int `json:"protocols,omitempty"`
	// Resume optionally attempts the fast path (MADR 0068 D4). Resume
	// failure is not an auth failure.
	Resume *AuthResumePayload `json:"resume,omitempty"`
	// ResumeWindowMS optionally requests a shorter resume-token validity
	// than the server default. Never widens (MADR 0068 Q1).
	ResumeWindowMS int64 `json:"resume_window_ms,omitempty"`
}

// ResumeCaps advertises connection-resume support (populated by 0068 P4;
// absent means resume is not offered).
type ResumeCaps struct {
	WindowMS int64 `json:"window_ms"`
}

// Caps is the v2 capability/limit block carried in auth_ok (MADR 0068 D1).
// Advertised values MUST equal enforced values — both are built from the
// same LivenessSpec (internal/ws/liveness.go), by construction.
type Caps struct {
	Protocol             int         `json:"protocol"`
	ReadDeadlineMS       int64       `json:"read_deadline_ms"`
	PingIntervalMS       int64       `json:"ping_interval_ms"`
	WSPingResetsDeadline bool        `json:"ws_ping_resets_deadline"`
	Resume               *ResumeCaps `json:"resume,omitempty"`
	HistoryRing          int         `json:"history_ring"`
	MaxFrameBytes        int         `json:"max_frame_bytes"`
	// TLSResumed reports whether this connection's TLS handshake resumed a
	// prior session — the client-verifiable signal for the phone's
	// SecurityContext cache (0068 Q3/P5).
	TLSResumed bool `json:"tls_resumed"`
	// Epoch is the daemon's seq-lineage id (MADR 0068 P3); empty when the
	// daemon runs without a session store.
	Epoch string `json:"epoch,omitempty"`
	// Receipts reports whether the daemon keeps signed receipts (MADR 0078
	// D7): the phone shows its receipt UI only when true. Additive; absent
	// (false) for daemons without receipts configured, and v1 clients ignore
	// the whole Caps block.
	Receipts bool `json:"receipts,omitempty"`
}

// AuthOKPayload is returned on successful auth.
type AuthOKPayload struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	// HomeDir is the daemon user's home directory — the default working
	// directory for new sessions. Lets clients pre-populate path inputs.
	HomeDir string `json:"home_dir,omitempty"`
	// Protocol is the negotiated version; omitted for v1 clients so the v1
	// auth_ok stays byte-identical (0068 U1).
	Protocol int `json:"protocol,omitempty"`
	// Caps is present iff Protocol >= V2.
	Caps *Caps `json:"caps,omitempty"`
	// ResumeToken is the opaque token for the *next* connection's resume
	// attempt (MADR 0068 D4); rotated on every v2 auth. Its validity
	// window is Caps.Resume.WindowMS.
	ResumeToken string `json:"resume_token,omitempty"`
	// Resumed reports the per-session retained-seq windows when the auth's
	// resume attempt succeeded; the client fetches only real gaps.
	Resumed *ResumedPayload `json:"resumed,omitempty"`
	// ResumeFailed is set when a resume attempt was made and rejected
	// (expired/unknown token). Auth itself still succeeded; the client
	// falls back to the ordinary full reconcile.
	ResumeFailed bool `json:"resume_failed,omitempty"`
}

// ResumedPayload answers a successful resume attempt (MADR 0068 D4).
type ResumedPayload struct {
	// Sessions maps session id → retained-seq window. Only sessions the
	// daemon knows and the device may access appear; a session the client
	// asked about that is absent here must be reconciled the ordinary way.
	Sessions map[string]SeqBoundsPayload `json:"sessions"`
}

// PairClaimPayload exchanges a short-lived pair code for a durable device token.
type PairClaimPayload struct {
	Code string `json:"code"`
	// Name optionally overrides the device label from the pending code.
	Name string `json:"name,omitempty"`
	// Protocols is the client's version offer (MADR 0068 D1), as on auth.
	Protocols []int `json:"protocols,omitempty"`
}

// PairOKPayload returns the one-shot durable token after a successful claim.
type PairOKPayload struct {
	Token      string `json:"token"`
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	// Protocol is the negotiated version; omitted for v1 clients (0068 D1).
	Protocol int `json:"protocol,omitempty"`
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
	// ThinkingLevel optionally selects the reasoning/thinking effort for this
	// session (e.g. "low", "high"). Empty means the provider default: codex
	// omits turn/start.effort; grok falls through to config then omits the
	// spawn flag (MADR 0052). Prefer values from models.list thinking_levels.
	ThinkingLevel string `json:"thinking_level,omitempty"`
	// Agent optionally selects the OpenCode agent name (e.g. "build", "plan")
	// sent on each prompt_async. Empty uses the engine default. Prefer values
	// from agents.list. Ignored by non-OpenCode providers (MADR 0020 Sprint 3).
	Agent string `json:"agent,omitempty"`
	// AgentSessionID resumes a provider-native session (e.g. ACP session/load).
	AgentSessionID string `json:"agent_session_id,omitempty"`
	// SessionID optionally forces the mcremote session id (used when reconnecting a persisted record).
	SessionID string `json:"session_id,omitempty"`
}

// SessionIDPayload identifies a session. Reused by session.claim (MADR 0078):
// the claimer is the connection's own device, so no extra field is needed.
type SessionIDPayload struct {
	SessionID string `json:"session_id"`
}

// SessionReleasePayload hands a session off for another device to claim
// (MADR 0078 D1/D2). ToDeviceID scopes the release to one target device;
// empty means an open release any paired device may claim.
type SessionReleasePayload struct {
	SessionID  string `json:"session_id"`
	ToDeviceID string `json:"to_device_id,omitempty"`
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

// SeqBoundsPayload is the retained-seq window for one session
// (MADR 0068 P3): a client whose cached seq is below FirstSeq knows the
// ring truncated past it; one whose cached seq equals LatestSeq can skip
// the history walk entirely.
type SeqBoundsPayload struct {
	FirstSeq  uint64 `json:"first_seq"`
	LatestSeq uint64 `json:"latest_seq"`
}

// SessionListResultPayload lists sessions.
//
// Complete is true only when the durable store enumeration succeeded without
// skipping corrupt rows (MADR 0056 H-6). Clients must not treat a non-complete
// snapshot as destructive-authoritative for cache eviction.
type SessionListResultPayload struct {
	Sessions []session.Meta `json:"sessions"`
	Complete bool           `json:"complete"`
	Degraded bool           `json:"degraded,omitempty"`
	Skipped  int            `json:"skipped,omitempty"`
	// Epoch is the daemon's seq-lineage id (MADR 0068 P3); a change means
	// cached seqs are stale. Additive — v1 clients ignore it.
	Epoch string `json:"epoch,omitempty"`
	// Seqs maps session id → retained-seq window, for gap-scaled resync.
	Seqs map[string]SeqBoundsPayload `json:"seqs,omitempty"`
}

// DeviceInfo is one paired device in the handoff-target roster (MADR 0078).
// Only non-secret identity fields: never keys or fingerprints.
type DeviceInfo struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
	// Self marks the calling device's own row, so the phone can exclude it
	// from a "hand off to another device" picker without knowing its own id.
	Self bool `json:"self"`
}

// DevicesListResultPayload is the paired-device roster a device uses to pick a
// handoff target (MADR 0078). Every paired device is listed (the caller's own
// row flagged Self); it is a fleet roster, not scoped like receipts.
type DevicesListResultPayload struct {
	Devices []DeviceInfo `json:"devices"`
}

// ReceiptsListResultPayload returns the calling device's own receipt chain,
// newest first (MADR 0078 D8). Each entry carries the raw JWS compact string
// (the phone re-verifies the signature itself — D9) and the decoded
// Statement for display.
type ReceiptsListResultPayload struct {
	Entries []session.ReceiptEntry `json:"entries"`
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
	// FirstSeq/LatestSeq bound the retained ring (MADR 0068 P3): a
	// since_seq below FirstSeq was silently unservable before these fields
	// existed. Zero/absent means an empty ring (or a pre-P3 daemon).
	FirstSeq  uint64 `json:"first_seq,omitempty"`
	LatestSeq uint64 `json:"latest_seq,omitempty"`
}

// SessionPendingAsksResultPayload is an owner-scoped snapshot of unresolved
// permission/question requests. It is independent of bounded history replay.
type SessionPendingAsksResultPayload struct {
	Events []event.Event `json:"events"`
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
//
// The three optional fields scope the reply (MADR 0043 D1). They exist because
// an unscoped catalog is not merely large but unusable: OpenCode's is 5,788
// models across 172 model providers, which is both a bad picker and half the
// 1 MiB WebSocket frame budget. A request carrying only Provider keeps its
// original meaning — the provider's default catalog.
type ModelsListPayload struct {
	// Provider is a registered provider id (grok, opencode, fake, …).
	Provider string `json:"provider"`
	// Scope is "models" (default) or "providers". With "providers" the reply
	// enumerates model providers rather than models.
	Scope string `json:"scope,omitempty"`
	// ModelProvider narrows a "models" request to one model provider id
	// (e.g. "anthropic"). Empty means the provider's default set, which for a
	// provider that reports connectivity is its connected model providers.
	ModelProvider string `json:"model_provider,omitempty"`
	// SessionID scopes the catalog to a live session: the model provider that
	// session is using, with its current model as the default id. The
	// requesting device must own the session.
	SessionID string `json:"session_id,omitempty"`
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
	// ModelProvider echoes the scope the daemon actually applied, which is not
	// always the one requested (a session-scoped request resolves it from the
	// session). Clients label the picker with it.
	ModelProvider string `json:"model_provider,omitempty"`
	// Truncated reports that the daemon dropped options to stay inside the
	// frame budget. It is never silent: a catalog that quietly loses rows reads
	// to a user as "my model does not exist" (MADR 0043 D4).
	Truncated bool `json:"truncated,omitempty"`
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

// AgentSessionsListPayload requests bounded provider-native session discovery.
// Results are metadata only; importing an entry uses the existing
// session.create agent_session_id field.
type AgentSessionsListPayload struct {
	Provider string `json:"provider"`
}

// AgentSessionsResultPayload is the metadata-only result of agent_sessions.list.
type AgentSessionsResultPayload struct {
	Provider string                      `json:"provider"`
	Sessions []provider.AgentSessionMeta `json:"sessions"`
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

// SessionRenamePayload changes a session's user-visible title.
type SessionRenamePayload struct {
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
}

// SessionRenameResultPayload returns persisted metadata after the native
// provider title update has succeeded.
type SessionRenameResultPayload struct {
	Session session.Meta `json:"session"`
}

// SessionDiagnosticsResultPayload contains bounded read-only provider metadata.
// It is a direct response, never a transcript event.
type SessionDiagnosticsResultPayload struct {
	SessionID   string               `json:"session_id"`
	Diagnostics provider.Diagnostics `json:"diagnostics"`
}

// PermissionRespondPayload answers a permission_request event.
type PermissionRespondPayload struct {
	SessionID    string `json:"session_id"`
	PermissionID string `json:"permission_id"`
	// OptionID is the selected permission option (required unless Cancelled).
	OptionID  string `json:"option_id,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

// PermissionReceiptRequestPayload asks a specific device to sign a receipt
// for a permission decision it just made (MADR 0077 D2/P7). Statement is the
// unsigned in-toto-style Statement (internal/receipt.Statement) as raw JSON,
// including its already-computed chain.prev_sha256 — the daemon constructs
// this, the phone only signs it, never builds its own version (D2).
type PermissionReceiptRequestPayload struct {
	SessionID    string          `json:"session_id"`
	PermissionID string          `json:"permission_id"`
	Statement    json.RawMessage `json:"statement"`
}

// PermissionReceiptPayload is the phone's reply to a
// PermissionReceiptRequestPayload: the Statement signed as an ES256 JWS
// compact string (internal/receipt.SignES256Compact). Not a direct response
// to a specific envelope id — correlated by PermissionID instead, since the
// daemon may be waiting on a different connection than the one that
// resolved the permission if the device reconnected mid-flight.
type PermissionReceiptPayload struct {
	SessionID    string `json:"session_id"`
	PermissionID string `json:"permission_id"`
	JWS          string `json:"jws"`
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
