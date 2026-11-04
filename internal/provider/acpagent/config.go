package acpagent

import "time"

// McpTransport identifies how an MCP server is reached.
type McpTransport string

const (
	// McpHTTP is the streamable-HTTP MCP transport (agent mcpCapabilities.http).
	McpHTTP McpTransport = "http"
	// McpSSE is the server-sent-events MCP transport (agent mcpCapabilities.sse).
	McpSSE McpTransport = "sse"
)

// McpServer describes one MCP server to advertise to the agent. Transport
// selects http or sse; URL is the endpoint; Name labels it; Headers are
// optional request headers (e.g. auth tokens) sent as "Key: Value" strings.
type McpServer struct {
	Name      string
	Transport McpTransport
	URL       string
	Headers   map[string]string
}

// Config configures an ACP CLI agent provider.
//
// Prewarm keeps one spare agent process spawned and ACP-initialized so the
// next session create/resume/relaunch skips the engine cold start when the
// session cwd matches the spare's process directory (DefaultCWD or $HOME).
// Project sessions with a different cwd always cold-start so the agent process
// OS cwd — and any stdio MCP children that inherit it — match the repo.
// Only used by specs whose default argv is model-independent.
type Config struct {
	// Bin is the agent executable (default comes from the Spec).
	Bin string
	// Args are appended after the binary. Default comes from Spec.DefaultArgs.
	Args []string
	// AlwaysApprove auto-selects the first "allow"-style permission option
	// without remoting to the client. Prefer remote permissions for phones.
	AlwaysApprove bool
	// DefaultCWD is used when StartOptions.CWD is empty.
	DefaultCWD string
	// Model is the default model passed to the agent when non-empty (how it is
	// applied is Spec-specific, e.g. a CLI flag).
	Model string
	// ReasoningEffort is passed to agents supporting a reasoning effort setting
	// (a launch flag, for a Spec that has one). Passed as an engine-level setting.
	ReasoningEffort string
	// PermissionMode is the agent's permission mode, for a Spec whose agent
	// takes one at launch (MADR 0050).
	PermissionMode string
	// Sandbox is the agent's OS-level sandbox profile, for a Spec whose agent
	// takes one at launch. Empty leaves the agent's own default (MADR 0050 D4).
	Sandbox string
	// AllowedTools whitelists built-in tools.
	AllowedTools []string
	// DisallowedTools blacklists built-in tools.
	DisallowedTools []string
	// AllowRules adds persistent permission allow rules.
	AllowRules []string
	// DenyRules adds persistent permission deny rules.
	DenyRules []string
	// NoSubagents disables subagent spawning when true.
	NoSubagents bool
	// DisableWebSearch disables built-in web search when true.
	DisableWebSearch bool
	// PermissionTimeout bounds how long a remote permission request waits for a
	// client decision before the agent stops waiting and the action is treated
	// as cancelled. Zero disables the timeout (wait indefinitely). Prevents a
	// missed notification from hanging the agent forever.
	PermissionTimeout time.Duration
	// Prewarm keeps one spare initialized agent process ready (see package
	// doc above). The daemon must call Provider.EnsureWarm to arm it.
	Prewarm bool
	// AllowRemoteShell permits a phone to run a command in the session's
	// working directory, for a Spec with Ops.Shell. Default false: it is remote
	// command execution on the daemon host, so the operator turns it on, as
	// for httpagent (MADR 0112 A9; 0214 MADR, added at P6e).
	AllowRemoteShell bool
	// TurnStallNotice emits a notice event when a running turn has produced no
	// agent output for this long — the "is it stuck?" signal that lets a user
	// decide to stop/reset instead of staring at a frozen spinner. Zero
	// disables it.
	TurnStallNotice time.Duration
	// McpServers are MCP servers to hand the agent at session/new and
	// session/load (ACP MCP capability). Each extends the agent with extra
	// tools/context. Only forwarded when the agent advertises the matching
	// mcpCapabilities transport (http/sse); unsupported entries are dropped
	// with a warning rather than failing session creation.
	McpServers []McpServer
	// AuthMethodID, when set, is an optional pin of the ACP authenticate
	// method. It is used only when advertised and headless-safe
	// (Spec.SafeAuthMethodIDs). Empty lets D2 auto-select (MADR 0085). A
	// method that opens a browser is never safe: it hangs a headless host.
	AuthMethodID string
	// FSRoots optionally confines the agent's fs/read_text_file and
	// fs/write_text_file callbacks. Empty (the default) leaves them
	// unrestricted. When non-empty, a callback path must resolve — after
	// symlink evaluation — within one of these roots or the session cwd, else it
	// is refused. This is defense-in-depth and an audit surface, NOT a sandbox:
	// the agent runs as the same user and has terminal access, so it can reach
	// any path the daemon user can regardless. Every fs callback is emitted as a
	// tool event whether or not confinement is set.
	FSRoots []string
	// StreamCoalesce is how long assistant/thought text is held so it can be
	// emitted as one event instead of one per model token (MADR 0024 / 0057).
	// The first chunk of a run and the tail before any control event are never
	// delayed. nil uses defaultStreamCoalesce; an explicit 0 disables coalescing
	// entirely (one event per chunk).
	StreamCoalesce *time.Duration
	// ClientVersion is the daemon's own version, sent to the agent as
	// clientInfo.version at initialize (MADR 0184 D4). Empty sends "dev".
	ClientVersion string
}

// StreamCoalesceWindow reports the streaming-text coalescing window, defaulting
// to defaultStreamCoalesce when unset. Zero means coalescing is off (MADR 0057).
func (c Config) StreamCoalesceWindow() time.Duration {
	if c.StreamCoalesce == nil {
		return defaultStreamCoalesce
	}
	return *c.StreamCoalesce
}
