package protocol

// Error codes carried in an `error`, `auth_error` or `pair_error` frame's
// `code` field (MADR 0036 D4).
//
// This block is the registry: every code the daemon can put on the wire is
// named here, and TestErrorCodesAreDocumented asserts each one appears in
// docs/protocol-v1.md. TestWSErrorCodesAreRegistered walks the internal/ws
// package with go/ast and asserts the converse — that no emit site invents a
// code this list does not know about. Adding a code therefore fails the build
// until it is both registered and documented.
//
// Codes are a stable client-facing contract: clients branch on them (a
// permanent `client_key_mismatch` means re-pair, a transient `rate_limited`
// means back off). Rename one only with a protocol version bump.
const (
	// --- envelope / routing, any request ---

	// ErrBadJSON is a frame that is not a decodable JSON envelope. It is the
	// one code returned with an empty request id, because the id could not be
	// parsed either.
	ErrBadJSON = "bad_json"
	// ErrBadVersion is an envelope whose `v` is not the supported version.
	ErrBadVersion = "bad_version"
	// ErrUnauthorized is any request other than auth/pair.claim before the
	// socket has authenticated.
	ErrUnauthorized = "unauthorized"
	// ErrUnknownType is an envelope `type` the daemon does not handle.
	ErrUnknownType = "unknown_type"
	// ErrBadPayload is an undecodable or invalid payload (also used for
	// oversize fields and missing required ids).
	ErrBadPayload = "bad_payload"
	// ErrRateLimited is a transient throttle: too many failed auth or pair
	// attempts, or too many concurrent async requests. Retry after a delay.
	ErrRateLimited = "rate_limited"
	// ErrDeadlineExceeded means an async operation hit its server-side deadline
	// (MADR 0056 H-2). The work was cancelled; the client may reconcile via
	// session.list / history rather than assuming success.
	ErrDeadlineExceeded = "deadline_exceeded"

	// --- provider / catalog lookups ---

	// ErrUnknownProvider is a provider id the daemon has not registered.
	ErrUnknownProvider = "unknown_provider"
	// ErrProviderUnavailable is a registered provider whose engine is not ready.
	ErrProviderUnavailable = "provider_unavailable"
	// ErrUnsupported is a request the provider cannot serve (e.g. native
	// session discovery on a provider without it).
	ErrUnsupported = "unsupported"
	// ErrAgentSessionsListFailed is a failed provider-native session listing.
	ErrAgentSessionsListFailed = "agent_sessions_list_failed"

	// --- session ownership and lifecycle, override any per-op code below ---

	// ErrSessionForbidden means another device owns the session.
	ErrSessionForbidden = "session_forbidden"
	// ErrSessionNotLive means the session is missing or no longer live; the
	// client must re-create it before interacting.
	ErrSessionNotLive = "session_not_live"
	// ErrSessionNotReleased means session.claim targeted a session whose owner
	// has not released it (MADR 0078). Not claimable until released.
	ErrSessionNotReleased = "session_not_released"
	// ErrSessionLimit means the per-daemon live-session cap is reached.
	ErrSessionLimit = "session_limit"
	// ErrShuttingDown means the daemon is stopping and accepted no new work.
	ErrShuttingDown = "shutting_down"
	// ErrTurnBusy means a turn is already active on this session (MADR 0020);
	// it is not a generic failure and the client should not retry blindly.
	ErrTurnBusy = "turn_busy"
	// ErrBadAgent is an agent name the provider rejects (unknown, hidden, or a
	// subagent that cannot be started top-level).
	ErrBadAgent = "bad_agent"
	// ErrPermissionDenied is an OS-level permission denial (EPERM/EACCES):
	// file modes, a provider sandbox policy, or macOS privacy protection
	// (TCC) blocking the daemon or an agent process (MADR 0069 D4). Not
	// retryable without operator action — a mode change, chmod, or a
	// privacy grant.
	ErrPermissionDenied = "permission_denied"
	// ErrPersistFailed means a security-critical durable write failed (session
	// create owner stamp or first-touch ownership claim). Fail closed; retry
	// after fixing disk, do not treat the mutation as successful (MADR 0056 H-4).
	ErrPersistFailed = "persist_failed"
	// ErrSessionListFailed means the durable session store could not be
	// enumerated. The list is not complete; clients must not prune local state.
	ErrSessionListFailed = "session_list_failed"

	// --- per-operation failures (fallback when none of the above applies) ---

	ErrSessionCreateFailed          = "session_create_failed"
	ErrSessionCloseFailed           = "session_close_failed"
	ErrSessionDeleteFailed          = "session_delete_failed"
	ErrSessionReleaseFailed         = "session_release_failed"
	ErrSessionClaimFailed           = "session_claim_failed"
	ErrSessionPromptFailed          = "session_prompt_failed"
	ErrSessionCancelFailed          = "session_cancel_failed"
	ErrSessionHistoryFailed         = "session_history_failed"
	ErrSessionSetModeFailed         = "session_set_mode_failed"
	ErrCollaborationModeUnsupported = "collaboration_mode_unsupported"
	ErrCollaborationModeInvalid     = "collaboration_mode_invalid"
	ErrSetCollaborationModeFailed   = "set_collaboration_mode_failed"
	ErrSessionSetConfigFailed       = "session_set_config_failed"
	ErrSessionForkFailed            = "session_fork_failed"
	ErrSessionRevertFailed          = "session_revert_failed"
	ErrSessionUnrevertFailed        = "session_unrevert_failed"
	ErrSessionDiffFailed            = "session_diff_failed"
	ErrSessionRenameFailed          = "session_rename_failed"
	ErrSessionDiagnosticsFailed     = "session_diagnostics_failed"
	ErrPermissionFailed             = "permission_failed"
	ErrQuestionFailed               = "question_failed"
	ErrReceiptsListFailed           = "receipts_list_failed"
	ErrDevicesListFailed            = "devices_list_failed"

	// --- remote provider auth (MADR 0074) ---

	// ErrProviderBusy means a credential change needs an engine restart but a
	// turn is in flight (D9). Transient by construction: retry after the turn.
	ErrProviderBusy = "provider_busy"
	// ErrConfirmRequired means the requested flow destroys an existing
	// credential before it can succeed and the client did not confirm (D8).
	// Today only codex device auth, which deletes ~/.codex/auth.json at start.
	ErrConfirmRequired = "confirm_required"
	// ErrCredentialFailed is a failed credential write or clear. Distinct from
	// ErrAuthFailed, which is about the phone's own device auth — conflating
	// them would make a bad upstream key look like a pairing problem.
	ErrCredentialFailed = "credential_failed"
	// ErrKeyringManaged means the agent keeps its secrets in the host's OS
	// keyring, which the daemon deliberately does not write (MADR 0074 D18,
	// 0083 D5). Actionable on the host, never by retrying from the phone.
	ErrKeyringManaged = "keyring_managed"
	// ErrMethodUnsupported means the chosen auth method cannot be driven from
	// the phone for this agent (MADR 0083 D2/D5) — pick another method.
	ErrMethodUnsupported = "method_unsupported"
	// ErrInvalidKey means the submitted credential failed validation before
	// any write happened (empty, oversized).
	ErrInvalidKey = "invalid_key"
	// ErrEngineUnavailable means the agent's engine did not answer the
	// credential operation in time — is it running on the host?
	ErrEngineUnavailable = "engine_unavailable"
	// ErrCredentialNotAccepted means the native write returned success but
	// the agent is not using the credential (MADR 0086 D1) — pick another
	// sign-in method rather than retrying the same key.
	ErrCredentialNotAccepted = "credential_not_accepted"

	// --- prewarm control (MADR 0089 D7) ---

	// ErrConfigWriteFailed means the surgical `providers.<id>.prewarm` write
	// did not land: the daemon has no config file path, the file is missing or
	// empty, or the rename failed. Nothing was applied — the engine still
	// reflects the old value, so the client must not update its switch.
	ErrConfigWriteFailed = "config_write_failed"
	// ErrProviderNotReady means the flag was persisted but the engine could not
	// be started: the agent is not enabled on this daemon, so there is nothing
	// to pre-warm. Distinct from ErrProviderUnavailable, which is a registered
	// engine that failed to boot for a session request; this one is about the
	// prewarm toggle finding no engine to act on at all.
	ErrProviderNotReady = "provider_not_ready"

	// --- auth_error frames ---

	// ErrAuthFailed is the generic auth outcome; the detail is deliberately
	// withheld from the peer and logged instead.
	ErrAuthFailed = "auth_failed"
	// ErrInvalidToken is an unknown or revoked device token.
	ErrInvalidToken = "invalid_token"
	// ErrClientKeyRequired means enforcement is on and the socket presented no
	// client certificate. Permanent: re-pair, never retry.
	ErrClientKeyRequired = "client_key_required"
	// ErrClientKeyMismatch means the presented key is not the one enrolled for
	// this device. Permanent: re-pair, never retry.
	ErrClientKeyMismatch = "client_key_mismatch"
	// ErrAlreadyAuthed means the socket (or device) is already authenticated.
	// Emitted on both auth_error and pair_error.
	ErrAlreadyAuthed = "already_authed"

	// --- pair_error frames ---

	// ErrInvalidCode is a missing or unrecognised pair code.
	ErrInvalidCode = "invalid_code"
	// ErrExpired is a pair code past its TTL.
	ErrExpired = "expired"
	// ErrUnavailable means pairing is not configured on this daemon.
	ErrUnavailable = "unavailable"
	// ErrCreateFailed means the device store write failed during pairing.
	ErrCreateFailed = "create_failed"
)

// ErrorCodes returns every registered protocol error code.
//
// Order is stable (grouped as declared above) so test failures read
// predictably. Callers must not mutate the result.
func ErrorCodes() []string {
	return []string{
		ErrBadJSON,
		ErrBadVersion,
		ErrUnauthorized,
		ErrUnknownType,
		ErrBadPayload,
		ErrRateLimited,
		ErrDeadlineExceeded,

		ErrUnknownProvider,
		ErrProviderUnavailable,
		ErrUnsupported,
		ErrAgentSessionsListFailed,

		ErrSessionForbidden,
		ErrSessionNotLive,
		ErrSessionNotReleased,
		ErrSessionLimit,
		ErrShuttingDown,
		ErrTurnBusy,
		ErrBadAgent,
		ErrPermissionDenied,
		ErrPersistFailed,
		ErrSessionListFailed,

		ErrSessionCreateFailed,
		ErrSessionCloseFailed,
		ErrSessionDeleteFailed,
		ErrSessionReleaseFailed,
		ErrSessionClaimFailed,
		ErrSessionPromptFailed,
		ErrSessionCancelFailed,
		ErrSessionHistoryFailed,
		ErrSessionSetModeFailed,
		ErrCollaborationModeUnsupported,
		ErrCollaborationModeInvalid,
		ErrSetCollaborationModeFailed,
		ErrSessionSetConfigFailed,
		ErrSessionForkFailed,
		ErrSessionRevertFailed,
		ErrSessionUnrevertFailed,
		ErrSessionDiffFailed,
		ErrSessionRenameFailed,
		ErrSessionDiagnosticsFailed,
		ErrPermissionFailed,
		ErrQuestionFailed,
		ErrReceiptsListFailed,
		ErrDevicesListFailed,

		ErrProviderBusy,
		ErrConfirmRequired,
		ErrCredentialFailed,
		ErrKeyringManaged,
		ErrMethodUnsupported,
		ErrInvalidKey,
		ErrEngineUnavailable,
		ErrCredentialNotAccepted,

		ErrConfigWriteFailed,
		ErrProviderNotReady,

		ErrAuthFailed,
		ErrInvalidToken,
		ErrClientKeyRequired,
		ErrClientKeyMismatch,
		ErrAlreadyAuthed,

		ErrInvalidCode,
		ErrExpired,
		ErrUnavailable,
		ErrCreateFailed,
	}
}
