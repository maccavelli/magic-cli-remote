package provider

import (
	"context"
	"errors"
)

// ErrAuthUnsupported is returned by an Auth implementation for an
// operation the underlying agent cannot express — for example switching the
// active upstream on codex, which has exactly one. Callers treat it as "this
// affordance does not exist here", never as a failure (MADR 0074 D3/D14).
var ErrAuthUnsupported = errors.New("provider auth: unsupported operation")

// ErrAuthBusy is returned when a credential change needs an engine restart but
// a turn is in flight (MADR 0074 D9). Restarting under a live turn would kill
// it, so the caller is told to retry rather than losing work.
var ErrAuthBusy = errors.New("provider auth: engine busy with an active turn")

// ErrAuthConfirmRequired is returned when a flow would destroy an existing
// credential before it can succeed and the caller did not confirm
// (MADR 0074 D8). Today only codex device auth is in this class.
var ErrAuthConfirmRequired = errors.New("provider auth: destructive flow needs explicit confirmation")

// Auth status values, mirroring the protocol constants. Status is advisory:
// AuthConfigured means a credential exists on the host, not that the next turn
// will succeed — a key can be present and revoked.
const (
	AuthConfigured = "configured"
	AuthMissing    = "missing"
	AuthError      = "error"
	AuthQuota      = "quota"
)

// Auth method types (MADR 0074 D5).
const (
	AuthMethodAPIKey       = "api_key"
	AuthMethodOAuthDevice  = "oauth_device"
	AuthMethodOAuthBrowser = "oauth_browser"
)

// Auth input widget types (MADR 0074 D5).
const (
	AuthInputText   = "text"
	AuthInputSelect = "select"
)

// AuthInputOption is one choice in a select input. Label and Value differ
// ("Resource name" vs "resourceName"), so the two cannot be collapsed.
type AuthInputOption struct {
	Value string
	Label string
	Hint  string
}

// AuthInputCondition hides an input until another input holds a given value —
// Azure's resourceName applies only when endpointType is resourceName. Op is
// the agent's comparison operator ("eq" is the only one kilo 7.4.20 emits);
// an unrecognised op means the client shows the field unconditionally rather
// than hiding something the user may need.
type AuthInputCondition struct {
	Key   string
	Op    string
	Value string
}

// AuthInput is one field a method needs before it can run. Kilo's live catalog
// declares these for 8 of its 13 upstreams (GitHub Copilot's deployment type,
// GitLab's instance URL, Azure's resource name, …), which is why an auth
// method is not reducible to a label plus a key field (MADR 0074 D5).
type AuthInput struct {
	Key         string
	Type        string
	Message     string
	Options     []AuthInputOption
	Placeholder string
	Required    bool
	When        *AuthInputCondition
}

// AuthMethod is one way to authenticate an upstream.
type AuthMethod struct {
	ID     string
	Type   string
	Label  string
	Inputs []AuthInput

	// Unavailable marks a method the daemon cannot drive on this host, with
	// Reason as the wire code (MADR 0083 D4) — e.g. goose's api-key methods
	// on a keyring-managed host. The transport-generic reasons (browser_only,
	// device_unsupported) are annotated centrally at payload time; only
	// provider-specific knowledge belongs here.
	Unavailable bool
	Reason      string

	// Configured reports presence only: this specific method currently has a
	// credential mcremote can see and remove (MADR 0074 P18 step 12).
	//
	// It is per-method rather than derived from the upstream's aggregate
	// status, because the two disagree in ways that matter. Grok can hold an
	// API key in config.toml and an OAuth session in auth.json at the same
	// time, and Codex keeps one native credential whose auth mode says which
	// method wrote it. A method configured only through an environment
	// variable the daemon does not own must stay false: it may make the
	// upstream configured, but this daemon cannot remove it.
	Configured bool
}

// DeviceAuthCapable is optionally implemented by providers whose transport
// declares StartDeviceAuth unconditionally (httpagent, acpagent) to report
// whether the flow is actually wired for this agent (MADR 0083 D4): the type
// assertion alone would promise device flows that return ErrAuthUnsupported.
type DeviceAuthCapable interface {
	SupportsDeviceAuth() bool
}

// UpstreamAuth is one model vendor reachable through an agent.
type UpstreamAuth struct {
	ID      string
	Label   string
	Status  string
	Methods []AuthMethod
}

// AuthState is an agent's whole credential picture.
type AuthState struct {
	Status         string
	ActiveUpstream string
	Upstreams      []UpstreamAuth

	// BackupState and RecoveryAvailable are the additive, non-secret
	// projection of credential recovery state (MADR 0074 D24, P19 step 5).
	// They carry no path, hash, generation id, or token metadata.
	//
	// Empty BackupState means the provider has no credential coordinator, so
	// an older daemon and an unmanaged provider read identically.
	BackupState       string
	RecoveryAvailable bool
}

// Backup states surfaced through AuthState.BackupState. The set is fixed so a
// client can switch on it exhaustively (MADR 0074 P19 step 5).
const (
	// BackupUnmanaged means no credential is under coordinator management.
	BackupUnmanaged = "unmanaged"
	// BackupCurrent means a validated committed generation matches LIVE.
	BackupCurrent = "current"
	// BackupPending means a credential transaction is in flight.
	BackupPending = "pending"
	// BackupLoggedOut means an explicit logout removed the credential.
	BackupLoggedOut = "logged_out"
	// BackupRecoveryRequired means a restorable generation exists and an
	// operator decision is needed.
	BackupRecoveryRequired = "recovery_required"
	// BackupReauthRequired means every surviving generation was revoked by a
	// coordinator action, so no restore can succeed and only a fresh sign-in
	// will work (MADR 0074 D24/F14).
	BackupReauthRequired = "reauth_required"
	// BackupUnsupported means the provider's credential store cannot be
	// observed or protected.
	BackupUnsupported = "unsupported"
)

// DeviceFlow describes a started RFC 8628 flow the phone must display.
type DeviceFlow struct {
	VerificationURI string
	UserCode        string
	ExpiresIn       int
	Interval        int
}

// Auth is optionally implemented by providers that can report or
// modify upstream credentials (MADR 0074 D3). A provider that does not
// implement it contributes no auth block to providers.list, and the phone
// shows it exactly as it does today.
//
// Implementations must never return key material in AuthState: presence and
// metadata only (D2).
type Auth interface {
	// AuthStatus reports credential presence per upstream. It is called on the
	// providers.list path, so it must respect ctx and fail fast: a degraded
	// AuthError entry beats blocking the whole listing.
	AuthStatus(ctx context.Context) (AuthState, error)
}

// AuthCatalogSource records where a catalog came from, so the phone can tell a
// live read from a table pinned to a CLI version (MADR 0074 D16).
const (
	AuthCatalogSourceEngine = "engine"
	AuthCatalogSourceStatic = "static"
)

// AuthCatalog is a full list of the upstreams an agent can be authenticated
// against, plus where the list came from.
type AuthCatalog struct {
	Upstreams []UpstreamAuth
	Source    string
}

// AuthCataloger is optionally implemented by agents that can enumerate every
// upstream they support, not merely the ones already configured (MADR 0074
// D16).
//
// The split from AuthStatus is deliberate and is the whole point of D16.
// AuthStatus answers "what is configured here" and rides on every
// providers.list; a catalog answers "what could be configured" and is ~185
// vendors for the OpenCode-family agents. Sending the second one on the first
// one's schedule would put tens of kilobytes on the wire every time a chip
// changes colour.
type AuthCataloger interface {
	// AuthCatalogList returns every upstream this agent can authenticate.
	// Status on each entry is filled in by the caller from AuthStatus, so an
	// implementation may leave it empty.
	AuthCatalogList(ctx context.Context) (AuthCatalog, error)
}

// ErrAuthMethodUnsupported is returned by a SetCredential/StartDeviceAuth
// given a method the provider cannot drive from the daemon (MADR 0083 D2):
// refusing beats writing a wrong-shaped credential that looks like success.
var ErrAuthMethodUnsupported = errors.New("auth method not supported for this provider")

// ErrCredentialNotAccepted is returned when the agent's native write
// succeeded but a follow-up read shows the agent is not using the
// credential (MADR 0086 D1).
var ErrCredentialNotAccepted = errors.New("agent stored the credential but is not using it")

// AuthWriter is optionally implemented by providers whose credentials
// mcremote can change (MADR 0074 D1). Split from Auth so a read-only
// integration is a valid, complete implementation.
type AuthWriter interface {
	// SetCredential stores secret for upstreamID using methodID. inputs carries
	// the method's declared AuthInput values (D5). Implementations write the
	// agent's native store and never a store of mcremote's own (D2).
	SetCredential(ctx context.Context, upstreamID, methodID, secret string, inputs map[string]string) error
	// ClearCredential removes the stored credential for upstreamID.
	ClearCredential(ctx context.Context, upstreamID string) error
}

// UpstreamSwitcher is optionally implemented by agents that can be
// repointed at another already-configured upstream without re-authenticating
// (MADR 0074 D14) — the operational fix for the MADR 0073 quota hang.
type UpstreamSwitcher interface {
	SetActiveUpstream(ctx context.Context, upstreamID string) error
}

// DeviceAuth is optionally implemented by agents that can run an
// RFC 8628 device flow headlessly (MADR 0074 Strategy A).
type DeviceAuth interface {
	// StartDeviceAuth begins a flow and returns what the phone must display.
	// The returned wait func blocks until the flow completes, fails, or ctx is
	// cancelled; callers run it off the request goroutine.
	//
	// confirmDestructive gates flows that destroy the existing credential
	// before they can succeed; an implementation whose flow is destructive
	// must return ErrAuthConfirmRequired when it is false (D8).
	StartDeviceAuth(ctx context.Context, upstreamID, methodID string, inputs map[string]string, confirmDestructive bool) (flow DeviceFlow, wait func(context.Context) error, err error)
}

// DeviceAuthState is a non-terminal update a flow may publish while it runs.
type DeviceAuthState string

const (
	// DeviceAuthReadyToActivate means the OAuth exchange succeeded and the
	// candidate validated, but the provider is busy with live work so
	// publication is deferred. It is neither a failure nor a completion, and
	// the phone must say so (MADR 0074 D28).
	DeviceAuthReadyToActivate DeviceAuthState = "ready_to_activate"
)

// DeviceAuthHandle is an owned device flow: one object that owns the child
// process, the credential transaction, the activation timer, and exactly one
// cleanup path (MADR 0074 D27).
//
// The bare wait closure it replaces could be dropped by a caller that returned
// early, orphaning a running CLI and the only record of how to undo it. A
// handle cannot be dropped silently: whoever holds it must Wait or Cancel, and
// both resolve the same transaction.
type DeviceAuthHandle interface {
	// Flow is the immutable display payload. It never changes after start, so
	// callers may read it without synchronization.
	Flow() DeviceFlow

	// Wait blocks until the flow reaches a terminal outcome. It is safe to
	// call concurrently and repeatedly; every caller observes the same result.
	Wait(ctx context.Context) error

	// Cancel terminates the child and its process group, waits for it, and
	// aborts the credential transaction so LIVE is left byte-identical. It is
	// idempotent and may be called before or after Wait; both share one
	// internal result, so cancelling never converts a completed flow into a
	// failure.
	Cancel()
}

// DeviceAuthUpdateSource is optionally implemented by a DeviceAuthHandle that
// publishes non-terminal state changes. The channel is closed when the flow
// reaches its terminal outcome; a caller that never reads it must still be able
// to Wait or Cancel without blocking the flow, so implementations must not
// depend on delivery.
type DeviceAuthUpdateSource interface {
	Updates() <-chan DeviceAuthState
}

// OwnedDeviceAuth is optionally implemented by agents whose device flow runs
// inside a credential transaction. It is deliberately separate from DeviceAuth
// so providers that have not adopted the transaction contract keep working
// unchanged (MADR 0074 D20/D27).
//
// There is no confirmDestructive parameter: an owned flow runs against an
// isolated, empty pending home, so starting one cannot sign the host out and
// there is nothing for the user to confirm (D22/F14).
type OwnedDeviceAuth interface {
	StartOwnedDeviceAuth(ctx context.Context, upstreamID, methodID string, inputs map[string]string) (DeviceAuthHandle, error)
}

// AuthMethodClearer is optionally implemented by providers whose auth methods
// clear independently. Grok's API key lives in config.toml and its OAuth
// session in auth.json, so clearing one must not disturb the other; Codex
// keeps one native credential, so both of its method ids are aliases for it.
//
// This is deliberately not folded into AuthWriter, which would force every
// unrelated provider to implement a semantic it does not have (P18 step 10).
type AuthMethodClearer interface {
	ClearCredentialMethod(ctx context.Context, upstreamID, methodID string) error
}
