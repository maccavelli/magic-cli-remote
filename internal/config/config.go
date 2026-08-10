// Package config defines typed daemon configuration and defaults.
package config

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/tailnet"
)

// Config is the full daemon configuration after load and validation.
type Config struct {
	Listen    ListenConfig    `mapstructure:"listen"`
	TLS       TLSConfig       `mapstructure:"tls"`
	Log       LogConfig       `mapstructure:"log"`
	DataDir   string          `mapstructure:"data_dir"`
	Auth      AuthConfig      `mapstructure:"auth"`
	Providers ProvidersConfig `mapstructure:"providers"`
	Headscale HeadscaleConfig `mapstructure:"headscale"`
	Limits    LimitsConfig    `mapstructure:"limits"`
	// Relay is optional outbound registration to mcrelay (MADR 0015 Phase E2).
	// Empty URL disables the client.
	Relay RelayConfig `mapstructure:"relay"`
	// Pair controls what `mcremote pair` advertises to phones.
	Pair PairConfig `mapstructure:"pair"`
	// Receipts controls signed receipts for permission decisions (MADR
	// 0077). Sibling to Providers, not nested under it: receipts are
	// cross-provider by design (D4).
	Receipts ReceiptsConfig `mapstructure:"receipts"`

	// ConfigFile is the absolute path of the loaded config file, if any.
	ConfigFile string `mapstructure:"-"`
	// Paths is the resolved XDG layout for this effective DataDir.
	Paths appdirs.Paths `mapstructure:"-"`
	// Diagnostics are non-fatal path resolution notes (e.g. relative XDG ignored).
	Diagnostics []appdirs.Diagnostic `mapstructure:"-"`
}

// PairConfig controls how the daemon presents itself to phones during pairing.
type PairConfig struct {
	// AdvertiseHost pins the host (or host:port) printed into the pair QR/URI,
	// overriding the dynamic detection (Tailscale IPv4 → loopback) the daemon
	// would otherwise use. A bare host inherits listen.port. Ignored in
	// letsencrypt mode, where the certificate's primary domain must be
	// advertised so the phone's hostname verification passes; the per-run
	// `mcremote pair --host` flag still overrides everything.
	AdvertiseHost string `mapstructure:"advertise_host"`
}

// ReceiptsConfig gates signed receipts for permission decisions (MADR 0077
// D4): opt-in, off by default. AllowPatterns/DenyPatterns are shell-glob-
// style patterns (`*`, `?`, `[set]`) evaluated against "<tool_name> <detail>"
// by internal/receipt.ShouldReceipt (D10) — a new, small, mcremote-owned
// matcher (translated to a regexp internally, not Go stdlib path.Match:
// path.Match's `*` refuses to cross `/`, which would silently break on the
// file paths detail commonly contains — see match.go's doc comment). This is
// a different config surface from providers.*.allow_rules/deny_rules (D10
// corrects an earlier "reuse" assumption: those are forwarded verbatim as
// CLI flags to the provider's own binary, never evaluated by mcremote).
type ReceiptsConfig struct {
	Enabled       bool     `mapstructure:"enabled"`
	AllowPatterns []string `mapstructure:"allow_patterns"`
	DenyPatterns  []string `mapstructure:"deny_patterns"`
	// Handoffs records a signed receipt for each device-to-device session
	// handoff (release + claim) when receipts are enabled (MADR 0078 D6).
	// Default true: the handoff feature itself is always available, but its
	// attestation follows the same opt-in as permission receipts — only
	// consulted when Enabled. Set false to keep permission receipts while
	// leaving handoffs unattested.
	Handoffs bool `mapstructure:"handoffs"`
}

// RelayConfig configures the mcremote → mcrelay host registration path.
//
// URL + HostID advertise the join path on pair QR/URI (phone-side routing).
// Secret is required only for daemon registration with mcrelay (serve), and is
// preferably supplied via MCREMOTE_RELAY_SECRET so pair can load url/host_id
// from YAML without the secret in the shell environment.
type RelayConfig struct {
	// URL is the mcrelay base, e.g. "wss://relay.example.com" or
	// "wss://relay.example.com:8443". Paths /v1/host and /v1/tunnel are appended.
	URL string `mapstructure:"url"`
	// HostID is the public registration id (pair URI hid=). URL-safe token.
	HostID string `mapstructure:"host_id"`
	// Secret is the registration secret shared with mcrelay --allow / hosts.
	// Prefer env MCREMOTE_RELAY_SECRET over committing to YAML.
	Secret string `mapstructure:"secret"`
	// InsecureSkipVerify skips TLS verification of the *relay* certificate
	// (dev/tests only). Does not affect mcremote's own TLS identity.
	InsecureSkipVerify bool `mapstructure:"insecure_skip_verify"`
}

// Enabled reports whether pair URIs should include relay routing (url set).
func (r RelayConfig) Enabled() bool {
	return strings.TrimSpace(r.URL) != ""
}

// CanRegister reports whether the daemon has enough credentials to register
// with mcrelay (url, host_id, and secret all present).
func (r RelayConfig) CanRegister() bool {
	return strings.TrimSpace(r.URL) != "" &&
		strings.TrimSpace(r.HostID) != "" &&
		strings.TrimSpace(r.Secret) != ""
}

// LimitsConfig bounds concurrent resources on the daemon (Phase 4 hardening).
// Zero means "use Defaults()".
type LimitsConfig struct {
	// MaxWSClients caps simultaneous WebSocket connections (0 → default 8).
	MaxWSClients int `mapstructure:"max_ws_clients"`
	// MaxLiveSessions caps concurrent provider sessions (0 → default 16).
	MaxLiveSessions int `mapstructure:"max_live_sessions"`
	// WSReadDeadlineSeconds is the authenticated connection's rolling read
	// deadline (0 → default 120; floor 15). Advertised to v2 clients in the
	// capability block, so it is contract, not just tuning (MADR 0068 D2).
	// 120 s absorbs brief phone Doze / mesh blips while awake clients still
	// app-ping every 10 s; kernel TCP keepalive reaps blackholes sooner.
	WSReadDeadlineSeconds int `mapstructure:"ws_read_deadline_seconds"`
	// TCPKeepalive configures kernel keepalive probes on accepted
	// connections (MADR 0068 P1): peers that die without a FIN — a
	// suspended phone mid-transfer — are reaped below the app deadline.
	TCPKeepalive KeepaliveConfig `mapstructure:"tcp_keepalive"`
	// WSResumeWindowSeconds bounds how long a v2 resume token stays valid
	// after issue (0 → default 120; MADR 0068 D4/Q1). Clients may request
	// a shorter window per connection, never a longer one.
	WSResumeWindowSeconds int `mapstructure:"ws_resume_window_seconds"`
}

// KeepaliveConfig mirrors net.KeepAliveConfig with config-file ergonomics.
// The zero value means "enabled with defaults"; set Disable to opt out.
type KeepaliveConfig struct {
	// Disable turns kernel keepalive probes off.
	Disable bool `mapstructure:"disable"`
	// IdleSeconds before the first probe (0 → 25).
	IdleSeconds int `mapstructure:"idle_seconds"`
	// IntervalSeconds between probes (0 → 5).
	IntervalSeconds int `mapstructure:"interval_seconds"`
	// Count of failed probes before the OS closes the connection (0 → 4).
	// Defaults reap a silent peer at ~25+4×5 = 45 s, inside the 120 s app
	// deadline.
	Count int `mapstructure:"count"`
}

// NetConfig renders the resolved keepalive settings for net.ListenConfig /
// net.Dialer.
func (k KeepaliveConfig) NetConfig() net.KeepAliveConfig {
	if k.Disable {
		return net.KeepAliveConfig{Enable: false}
	}
	idle, interval, count := k.IdleSeconds, k.IntervalSeconds, k.Count
	if idle <= 0 {
		idle = 25
	}
	if interval <= 0 {
		interval = 5
	}
	if count <= 0 {
		count = 4
	}
	return net.KeepAliveConfig{
		Enable:   true,
		Idle:     time.Duration(idle) * time.Second,
		Interval: time.Duration(interval) * time.Second,
		Count:    count,
	}
}

// ListenConfig is the HTTP/WebSocket bind address.
//
// Host accepts ListenHostTailscale as a sentinel; it is replaced with the
// host's Tailscale IPv4 by ResolveListenHost before the listener is opened.
type ListenConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// ListenHostTailscale is the listen.host sentinel meaning "bind only the
// tailnet interface". It resolves at startup to the host's Tailscale IPv4 and
// fails closed if there is none — it never widens to 0.0.0.0.
const ListenHostTailscale = "tailscale"

// TLS modes for TLSConfig.Mode.
const (
	// TLSModeLetsEncrypt obtains a publicly trusted certificate from an ACME
	// CA using the DNS-01 challenge (Route 53). The phone validates it with
	// the platform trust store — no fingerprint pinning.
	TLSModeLetsEncrypt = "letsencrypt"
	// TLSModeSelfSigned mints/loads the long-lived self-signed pair under the
	// data dir; the phone pins its fingerprint from the pair QR.
	TLSModeSelfSigned = "selfsigned"
	// TLSModeOff serves plaintext HTTP/WS.
	TLSModeOff = "off"
)

// TLSConfig controls transport security for the HTTP/WebSocket listener.
//
// Mode selects how the certificate is obtained. Left empty it resolves
// automatically: "letsencrypt" when a domain and an ACME email are
// configured, otherwise "selfsigned" — and "off" when the legacy
// enabled: false (or --tls=false) is set.
//
// Enabled is the pre-mode switch and is still honoured: false means
// TLSModeOff. Set it false only when something else terminates TLS in front
// of the daemon (or for a strictly loopback dev setup) — plaintext on a mesh
// address exposes the device token to anyone on the path.
type TLSConfig struct {
	Mode    string `mapstructure:"mode"`
	Enabled bool   `mapstructure:"enabled"`
	// CertFile / KeyFile override the managed pair under DataDir. When set,
	// the files are used as-is and never regenerated. Self-signed mode only.
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
	// LetsEncrypt carries the ACME/DNS-01 settings used by TLSModeLetsEncrypt.
	LetsEncrypt LetsEncryptConfig `mapstructure:"letsencrypt"`
}

// LetsEncryptConfig configures ACME issuance over the DNS-01 challenge.
//
// DNS-01 is the only challenge type that works here: daemon nodes live on a
// mesh (Tailscale/Headscale) address that no ACME validator can reach, so
// HTTP-01 and TLS-ALPN-01 can never complete. DNS-01 only needs a
// _acme-challenge TXT record in a public Route 53 zone.
type LetsEncryptConfig struct {
	// Domains are the names to request. The first is the primary and is what
	// `mcremote pair` advertises to the phone.
	Domains []string `mapstructure:"domains"`
	// Email is the ACME account contact (required by Let's Encrypt).
	Email string `mapstructure:"email"`
	// DirectoryURL overrides the ACME directory; point it at
	// https://acme-staging-v02.api.letsencrypt.org/directory while testing.
	DirectoryURL string `mapstructure:"directory_url"`
	// Staging is a shorthand for the Let's Encrypt staging directory. Ignored
	// when DirectoryURL is set.
	Staging bool `mapstructure:"staging"`
	// CacheDir holds certmagic's certificate/account storage. Defaults to
	// <data_dir>/acme.
	CacheDir string `mapstructure:"cache_dir"`
	// Route53 configures the DNS-01 solver.
	Route53 Route53Config `mapstructure:"route53"`
}

// Route53Config configures the libdns/route53 DNS-01 solver. Credentials come
// from the ambient AWS chain (env, shared config, instance role); only the
// zone/region/profile selectors live here.
type Route53Config struct {
	HostedZoneID string `mapstructure:"hosted_zone_id"`
	Region       string `mapstructure:"region"`
	Profile      string `mapstructure:"profile"`
	// MaxRetries for the AWS API calls; 0 uses the provider default.
	MaxRetries int `mapstructure:"max_retries"`
}

// LetsEncryptStagingDirectory is the ACME endpoint to use while testing:
// it issues untrusted certs but has far higher rate limits.
const LetsEncryptStagingDirectory = "https://acme-staging-v02.api.letsencrypt.org/directory"

// PrimaryDomain returns the first configured domain, or "".
func (l LetsEncryptConfig) PrimaryDomain() string {
	for _, d := range l.Domains {
		if d = strings.TrimSpace(d); d != "" {
			return d
		}
	}
	return ""
}

// TrimmedDomains returns the configured domains with blanks removed.
func (l LetsEncryptConfig) TrimmedDomains() []string {
	out := make([]string, 0, len(l.Domains))
	for _, d := range l.Domains {
		if d = strings.TrimSpace(d); d != "" {
			out = append(out, d)
		}
	}
	return out
}

// Directory returns the ACME directory URL to dial ("" = certmagic default,
// i.e. Let's Encrypt production).
func (l LetsEncryptConfig) Directory() string {
	if u := strings.TrimSpace(l.DirectoryURL); u != "" {
		return u
	}
	if l.Staging {
		return LetsEncryptStagingDirectory
	}
	return ""
}

// configured reports whether enough is set to attempt ACME issuance.
func (l LetsEncryptConfig) configured() bool {
	return l.PrimaryDomain() != "" && strings.TrimSpace(l.Email) != ""
}

// ResolvedMode maps the (possibly empty) Mode plus the legacy Enabled switch
// onto exactly one of the TLSMode* constants. Let's Encrypt is the default
// whenever a domain and an ACME email are configured.
func (t TLSConfig) ResolvedMode() string {
	switch m := strings.ToLower(strings.TrimSpace(t.Mode)); m {
	case TLSModeLetsEncrypt, TLSModeSelfSigned, TLSModeOff:
		return m
	case "":
		if !t.Enabled {
			return TLSModeOff
		}
		if t.LetsEncrypt.configured() {
			return TLSModeLetsEncrypt
		}
		return TLSModeSelfSigned
	default:
		// Validate reports the error; treat unknown as self-signed so callers
		// that skip Validate still get a working listener.
		return TLSModeSelfSigned
	}
}

// Normalized returns a copy with Mode resolved and Enabled kept in sync, so
// downstream code can switch on Mode without re-deriving it.
func (t TLSConfig) Normalized() TLSConfig {
	out := t
	out.Mode = t.ResolvedMode()
	out.Enabled = out.Mode != TLSModeOff
	return out
}

// WithEnabled applies the legacy --tls / tls.enabled switch on top of an
// already-resolved config: false forces off, true re-resolves.
func (t TLSConfig) WithEnabled(enabled bool) TLSConfig {
	out := t
	out.Enabled = enabled
	if !enabled {
		out.Mode = TLSModeOff
		return out
	}
	if strings.EqualFold(strings.TrimSpace(out.Mode), TLSModeOff) {
		out.Mode = ""
	}
	return out.Normalized()
}

// Active reports whether the listener terminates TLS.
func (t TLSConfig) Active() bool {
	return t.ResolvedMode() != TLSModeOff
}

// Managed reports whether the daemon owns the certificate lifecycle.
func (t TLSConfig) Managed() bool {
	return strings.TrimSpace(t.CertFile) == "" && strings.TrimSpace(t.KeyFile) == ""
}

// Pinned reports whether the fingerprint is the *only* thing that establishes
// trust, i.e. whether the client must refuse to consult the platform trust
// store. That is true for self-signed and false for Let's Encrypt, where the
// pin is an alternative to chain validation rather than a replacement for it.
//
// This expresses client policy only. It deliberately does NOT gate whether the
// fingerprint is advertised — see AdvertisesFingerprint. Conflating the two is
// what made the ACME fallback unreachable: the QR carried no pin, so a phone
// doing chain validation had nothing to fall back to when the daemon came up
// serving its self-signed fallback certificate.
func (t TLSConfig) Pinned() bool {
	return t.ResolvedMode() == TLSModeSelfSigned
}

// AdvertisesFingerprint reports whether the pair URI should carry fp=. Any
// mode that terminates TLS has a leaf worth advertising: in selfsigned it is
// the sole trust anchor, and in letsencrypt it is the recovery path when ACME
// issuance fails and the daemon falls back to the self-signed identity.
func (t TLSConfig) AdvertisesFingerprint() bool {
	return t.Active()
}

// Scheme returns the URL scheme clients should dial ("wss" or "ws").
func (t TLSConfig) Scheme() string {
	if t.Active() {
		return "wss"
	}
	return "ws"
}

// HTTPScheme returns the scheme for /healthz and /v1/hello.
func (t TLSConfig) HTTPScheme() string {
	if t.Active() {
		return "https"
	}
	return "http"
}

// LogConfig controls slog setup.
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// AuthConfig controls application authentication.
type AuthConfig struct {
	RequireDeviceToken bool `mapstructure:"require_device_token"`
	// RequireClientKey enforces the client-key allowlist (ADR 0005): a device
	// must present the client certificate whose SPKI fingerprint was recorded
	// at pair time. Default true (decision D7) — the fleet is a single
	// operator-owned phone, so re-pairing a legacy keyless device is the
	// accepted cost. When false, a device with no recorded key authenticates by
	// token alone and a presented key is recorded opportunistically on next pair.
	RequireClientKey bool `mapstructure:"require_client_key"`

	// AllowedOrigins is an opt-in allowlist of browser Origin host patterns for
	// the WebSocket upgrade (e.g. "app.example.com", "*.example.com"). Empty (the
	// default) is the secure baseline: native clients (which send no Origin) and
	// same-origin browser requests are accepted, and cross-origin browser pages
	// are rejected. Set this only to serve a browser-based (Flutter web) client
	// from a different origin — never "*".
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

// maxStreamCoalesceMs bounds providers.{opencode,goose,codex,grok}.stream_coalesce_ms.
// Past about a second the stream stops reading as live typing, and nothing
// downstream would flag the mistake (MADR 0024).
const maxStreamCoalesceMs = 1000

// ProvidersConfig enables individual providers.
type ProvidersConfig struct {
	Fake     FakeProviderConfig     `mapstructure:"fake"`
	Grok     GrokProviderConfig     `mapstructure:"grok"`
	Goose    GooseProviderConfig    `mapstructure:"goose"`
	Opencode OpencodeProviderConfig `mapstructure:"opencode"`
	Codex    CodexProviderConfig    `mapstructure:"codex"`
	Kilo     KiloProviderConfig     `mapstructure:"kilo"`
}

// FakeProviderConfig configures the test/demo provider.
type FakeProviderConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// MCPServerConfig configures one MCP server advertised to an ACP agent at
// session/new and session/load. Transport is "http" or "sse"; the server is
// only forwarded when the agent advertises the matching mcpCapabilities.
type MCPServerConfig struct {
	Name      string            `mapstructure:"name"`
	Transport string            `mapstructure:"transport"`
	URL       string            `mapstructure:"url"`
	Headers   map[string]string `mapstructure:"headers"`
}

// ACPProviderConfig is the configuration shared by every ACP CLI agent provider
// (grok today; goose and codex next). Each provider config embeds it squashed,
// so the agents share one config shape and a new ACP option is added here once.
type ACPProviderConfig struct {
	Enabled       bool     `mapstructure:"enabled"`
	Bin           string   `mapstructure:"bin"`
	Args          []string `mapstructure:"args"`
	AlwaysApprove bool     `mapstructure:"always_approve"`
	DefaultCWD    string   `mapstructure:"default_cwd"`
	Model         string   `mapstructure:"model"`
	// PermissionTimeoutSeconds bounds how long a remote permission request waits
	// for a decision before the agent stops waiting (treated as cancelled).
	// 0 disables the timeout. Default 120 — long enough to unlock the phone
	// and answer, short enough that a missed notification doesn't leave the
	// agent looking dead for a quarter hour.
	PermissionTimeoutSeconds int `mapstructure:"permission_timeout_seconds"`
	// Prewarm keeps one spare initialized agent process ready so session
	// create/resume/relaunch skips the engine cold start.
	Prewarm bool `mapstructure:"prewarm"`
	// TurnStallNoticeSeconds emits a notice when a running turn produces no
	// output for this long (0 disables).
	TurnStallNoticeSeconds int `mapstructure:"turn_stall_notice_seconds"`
	// FSRoots optionally confines the agent's fs read/write callbacks to these
	// roots (plus the session cwd). Empty (default) is unrestricted. This is
	// defense-in-depth and an audit surface, not a sandbox — the agent has
	// terminal access as the same user regardless.
	FSRoots []string `mapstructure:"fs_roots"`
	// AuthMethodID is the ACP auth method invoked automatically when the agent
	// reports (at initialize) that it requires authentication. Empty (default)
	// attempts no authentication — correct for agents that need none (grok).
	AuthMethodID string `mapstructure:"auth_method_id"`
	// MCPServers are MCP servers to advertise to the agent, extending it with
	// extra tools/context. Each is forwarded only when the agent advertises the
	// matching transport (mcpCapabilities.http / .sse); others are dropped.
	MCPServers []MCPServerConfig `mapstructure:"mcp_servers"`
}

// GrokProviderConfig configures the Grok Build ACP adapter.
type GrokProviderConfig struct {
	ACPProviderConfig `mapstructure:",squash"`
	// ReasoningEffort sets the reasoning effort level passed to grok agent (--reasoning-effort <EFFORT>).
	ReasoningEffort string `mapstructure:"reasoning_effort"`
	// PermissionMode sets Grok's permission mode (--permission-mode <MODE>).
	PermissionMode string `mapstructure:"permission_mode"`
	// Sandbox selects Grok's OS-level sandbox profile (--sandbox <PROFILE>):
	// off, workspace, devbox, read-only, strict, or a custom profile name
	// resolved from ~/.grok/sandbox.toml or .grok/sandbox.toml. Empty leaves
	// grok's own default. Deliberately not enum-validated beyond the built-ins
	// being documented: custom profiles are a supported grok feature and a hard
	// enum would break the day grok adds one (MADR 0050 D4).
	Sandbox string `mapstructure:"sandbox"`
	// AllowedTools whitelists built-in tools (--tools <csv>).
	AllowedTools []string `mapstructure:"allowed_tools"`
	// DisallowedTools blacklists built-in tools (--disallowed-tools <csv>).
	DisallowedTools []string `mapstructure:"disallowed_tools"`
	// AllowRules adds persistent permission allow rules (--allow <rule>).
	AllowRules []string `mapstructure:"allow_rules"`
	// DenyRules adds persistent permission deny rules (--deny <rule>).
	DenyRules []string `mapstructure:"deny_rules"`
	// NoSubagents disables subagent spawning (--no-subagents).
	NoSubagents bool `mapstructure:"no_subagents"`
	// DisableWebSearch disables built-in web search (--disable-web-search).
	DisableWebSearch bool `mapstructure:"disable_web_search"`
	// StreamCoalesceMs is how long assistant/thought text is held so it can be
	// emitted as one event instead of one per model token (MADR 0024 / 0057 H-1).
	// The first chunk of a run and the tail before any control event are never
	// delayed. 0 disables coalescing. Default 80. Max maxStreamCoalesceMs.
	StreamCoalesceMs int `mapstructure:"stream_coalesce_ms"`
}

// GooseProviderConfig configures the Goose ACP-over-HTTP adapter.
type GooseProviderConfig struct {
	ACPProviderConfig `mapstructure:",squash"`
	// WithBuiltins enables the named built-in Goose extensions on its shared
	// serve engine. This is intentionally a typed list, rather than arbitrary
	// process arguments, so the daemon's process boundary stays auditable.
	WithBuiltins []string `mapstructure:"with_builtins"`
	// StreamCoalesceMs is how long assistant/thought text is held so it can be
	// emitted as one event instead of one per model token (MADR 0024). The
	// first chunk of a run and the tail before any control event are never
	// delayed, so only mid-stream granularity is capped. 0 disables
	// coalescing (exact pre-0024 behaviour). Default 80.
	StreamCoalesceMs int `mapstructure:"stream_coalesce_ms"`
}

// OpencodeProviderConfig configures the OpenCode adapter
// (see docs/0011-MADR-opencode-provider-plan.md).
type OpencodeProviderConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Bin     string `mapstructure:"bin"`
	// RetiredTransport captures a `transport` key left over from before MADR
	// 0019 so validate can name it in the error. OpenCode is now always driven
	// through one shared `opencode serve` engine. Remove this field (and the
	// validate check) one release after 0019 ships.
	RetiredTransport string `mapstructure:"transport"`
	AlwaysApprove    bool   `mapstructure:"always_approve"`
	DefaultCWD       string `mapstructure:"default_cwd"`
	// Model is a provider/model id (e.g. "anthropic/claude-sonnet-4-5"),
	// pinned on the engine session at create time. Empty uses the OpenCode
	// config default.
	Model string `mapstructure:"model"`
	// PermissionTimeoutSeconds bounds how long a remote permission request waits
	// for a decision before the agent stops waiting (treated as cancelled).
	// 0 disables the timeout. Default 120.
	PermissionTimeoutSeconds int `mapstructure:"permission_timeout_seconds"`
	// Prewarm boots the shared `opencode serve` engine at daemon start so the
	// first session create skips the ~3-5s Bun cold start. Disable to boot it
	// lazily on first use and hold no idle engine (~250MB). Default true.
	Prewarm bool `mapstructure:"prewarm"`
	// TurnStallNoticeSeconds emits a notice when a running turn produces no
	// output for this long (0 disables). Default 120.
	TurnStallNoticeSeconds int `mapstructure:"turn_stall_notice_seconds"`
	// SessionTree enables multi-agent session-tree demux (MADR 0020 KD11).
	// When false: exact pre-0020 behavior — no childAliases, parent-only
	// EndTurn, no child event fan-in. Default true after Sprint 1.
	SessionTree bool `mapstructure:"session_tree"`
	// StreamCoalesceMs is how long assistant/thought text is held so it can be
	// sent as one event instead of one per model token (MADR 0024). The first
	// chunk of a reply and the tail before any control event are never
	// delayed, so time-to-first-token and end-of-turn latency are unchanged;
	// only mid-stream granularity is capped, at ~1000/StreamCoalesceMs updates
	// per second. 0 disables coalescing (one event per token, pre-0024
	// behaviour). Default 80.
	StreamCoalesceMs int `mapstructure:"stream_coalesce_ms"`
	// Pure runs opencode serve without loading external third-party plugins (--pure). Default false (MADR 0037 D2).
	Pure bool `mapstructure:"pure"`
}

// CodexProviderConfig configures the Codex app-server JSON-RPC provider
// (MADR 0028).
type CodexProviderConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	Bin           string `mapstructure:"bin"`
	AlwaysApprove bool   `mapstructure:"always_approve"`
	DefaultCWD    string `mapstructure:"default_cwd"`
	Model         string `mapstructure:"model"`
	// PermissionTimeoutSeconds bounds how long a remote permission request waits
	// for a decision before the agent stops waiting (treated as cancelled).
	// 0 disables the timeout. Default 900 — long enough to unlock the phone
	// and answer, balancing MADR 0028's default and the Codex app-server's
	// long-running tool expectation.
	PermissionTimeoutSeconds int `mapstructure:"permission_timeout_seconds"`
	// Prewarm boots the shared app-server engine at daemon start so the first
	// session create skips the ~500ms cold start.
	Prewarm bool `mapstructure:"prewarm"`
	// TurnStallNoticeSeconds emits a notice when a running turn produces no
	// output for this long (0 disables). Default 120 — same as the other
	// providers; codex tools often run multi-minute (tests, builds) and a
	// zero value left the phone with no stall UI during WS blips.
	TurnStallNoticeSeconds int `mapstructure:"turn_stall_notice_seconds"`
	// StreamCoalesceMs is how long assistant/thought text is held so it can be
	// emitted as one event instead of one per model token (MADR 0024). 0
	// disables coalescing. Default 80.
	StreamCoalesceMs int `mapstructure:"stream_coalesce_ms"`
	// ApprovalPolicy is an optional override for the Codex approval policy.
	// Empty means inherit Codex's own config.toml and trusted-project behavior.
	// Valid values: "untrusted", "on-request", "never".
	ApprovalPolicy string `mapstructure:"approval_policy"`
	// SandboxMode is an optional override for the Codex sandbox mode.
	// Empty means inherit Codex's own config.toml.
	// Valid values: "read-only", "workspace-write", "danger-full-access".
	SandboxMode string `mapstructure:"sandbox_mode"`
	// AllowFullAccess advertises the "full-access" session mode, which runs
	// with no approval prompts *and* no sandbox. Off by default: auto-approve
	// is one risk, auto-approve with nothing containing it is another, so the
	// unsandboxed variant is opt-in rather than one tap away (MADR 0044 D5).
	AllowFullAccess bool `mapstructure:"allow_full_access"`
	// SandboxBrokenPolicy controls create behaviour when the Linux sandbox
	// cannot create a user namespace (MADR 0048). Valid: empty/"warn" (default),
	// "require_full_access", "refuse".
	SandboxBrokenPolicy string `mapstructure:"sandbox_broken_policy"`
}

// KiloProviderConfig configures the Kilo CLI provider (shared `kilo serve`
// engine over httpagent, MADR 0075). It deliberately mirrors
// OpencodeProviderConfig — Kilo is an OpenCode fork with the same serve
// architecture — minus keys that never applied: no `transport` (retired in
// MADR 0019 before kilo existed), no `args`, and no auth keys (the engine
// spawns un-gated on loopback, MADR 0075 D5 as amended / plan PD1).
type KiloProviderConfig struct {
	// Enabled defaults false until MADR 0075 M1–M3 acceptance (plan §6).
	Enabled       bool   `mapstructure:"enabled"`
	Bin           string `mapstructure:"bin"`
	AlwaysApprove bool   `mapstructure:"always_approve"`
	DefaultCWD    string `mapstructure:"default_cwd"`
	// Model is a providerID/modelID pair split on the FIRST slash only (plan
	// PD3): Kilo model ids may themselves contain slashes ("kilo/~anthropic/x"
	// → provider "kilo", model "~anthropic/x"). Empty uses the engine default,
	// which is auth-state-dependent (kilo-auto/free vs kilo-auto/balanced —
	// MADR 0075 §2.6), so no default is hard-coded here.
	Model string `mapstructure:"model"`
	// PermissionTimeoutSeconds bounds how long a remote permission request waits
	// for a decision before the agent stops waiting (treated as cancelled).
	// 0 disables the timeout. Default 120 (same as OpenCode).
	PermissionTimeoutSeconds int `mapstructure:"permission_timeout_seconds"`
	// Prewarm boots the shared `kilo serve` engine at daemon start so the
	// first session create skips the Bun-class cold start. Default true.
	Prewarm bool `mapstructure:"prewarm"`
	// TurnStallNoticeSeconds emits a notice when a running turn produces no
	// output for this long (0 disables). Default 120 — never 0 in shipped
	// templates (MADR 0072 lesson).
	TurnStallNoticeSeconds int `mapstructure:"turn_stall_notice_seconds"`
	// SessionTree enables multi-agent session-tree demux. Default FALSE for
	// kilo (unlike OpenCode): the routes exist but child SSE events are
	// unproven — flip only after the MADR 0075 Q7 fixtures land (plan PD2).
	SessionTree bool `mapstructure:"session_tree"`
	// StreamCoalesceMs is how long assistant/thought text is held so it can be
	// sent as one event instead of one per model token (MADR 0024). 0 disables
	// coalescing. Default 80.
	StreamCoalesceMs int `mapstructure:"stream_coalesce_ms"`
	// Pure runs kilo serve without loading external third-party plugins
	// (--pure, live-confirmed on 7.4.20). Default false (match OpenCode).
	Pure bool `mapstructure:"pure"`
}

// validApprovalPolicy returns true for recognized Codex approval policy values.
func validApprovalPolicy(s string) bool {
	switch s {
	case "", "untrusted", "on-request", "never":
		return true
	}
	return false
}

// validSandboxMode returns true for recognized Codex sandbox mode values.
func validSandboxMode(s string) bool {
	switch s {
	case "", "read-only", "workspace-write", "danger-full-access":
		return true
	}
	return false
}

// validGrokPermissionMode returns true for recognized Grok permission modes.
// Empty means "inherit grok's own configuration" — legal, but see the default
// in Defaults() for why the daemon pins it (MADR 0050 D3).
func validGrokPermissionMode(s string) bool {
	switch s {
	case "", "default", "acceptEdits", "auto", "dontAsk", "bypassPermissions", "plan":
		return true
	}
	return false
}

// HeadscaleConfig is documentation/metadata only (no API calls).
type HeadscaleConfig struct {
	ControlURL string `mapstructure:"control_url"`
}

// Defaults returns a Config with Phase 2 defaults.
func Defaults() Config {
	return Config{
		Listen: ListenConfig{
			Host: "127.0.0.1",
			Port: 7531,
		},
		TLS: TLSConfig{
			Enabled: true,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
		DataDir: "",
		Auth: AuthConfig{
			RequireDeviceToken: true,
			RequireClientKey:   true,
		},
		Providers: ProvidersConfig{
			// Fake is opt-in for smoke/tests only (R6=A); enable explicitly.
			Fake: FakeProviderConfig{Enabled: false},
			Grok: GrokProviderConfig{
				// Pinned rather than left empty. Empty means "whatever this
				// host's grok resolves to" — ~/.grok/config.toml, project
				// config, or (since grok 0.2.102) fleet-wide remote config —
				// which the daemon cannot see. It advertises session modes and
				// a `dangerous` flag on the assumption it knows the approval
				// posture, so an unknown one makes the mode chip describe a
				// policy nobody set. `default` makes grok ask, which is what
				// gives the modes (and MADR 0049's `auto`) meaning.
				// Opt out with `bypassPermissions` (MADR 0050 D3).
				PermissionMode: "default",
				// ~12 mid-stream updates/sec instead of one per token
				// (MADR 0057 H-1). Matches goose/opencode/codex defaults.
				StreamCoalesceMs: 80,
				ACPProviderConfig: ACPProviderConfig{
					Enabled:                  true,
					Bin:                      "grok",
					AlwaysApprove:            false,
					PermissionTimeoutSeconds: 120,
					// Prewarm default on: first phone session skips cold start
					// (Phase 4.2). Disable if memory is tight.
					Prewarm:                true,
					TurnStallNoticeSeconds: 120,
				},
			},
			// Goose is enabled by default, selectable from the phone's
			// new-session provider menu. Default behaviour: no prewarm (goose
			// starts a child serve process per daemon, not per session); one
			// cold start at first use.
			Goose: GooseProviderConfig{
				ACPProviderConfig: ACPProviderConfig{
					Enabled:                  true,
					Bin:                      "goose",
					AlwaysApprove:            false,
					PermissionTimeoutSeconds: 120,
					Prewarm:                  false,
					TurnStallNoticeSeconds:   120,
				},
				StreamCoalesceMs: 80,
			},
			// OpenCode is enabled by default and selectable from the phone's
			// new-session provider menu. Registration is harmless when the
			// binary is absent — the provider just lists as not ready (with a
			// startup warning); grok stays the default selection.
			Opencode: OpencodeProviderConfig{
				Enabled:                  true,
				Bin:                      "opencode",
				AlwaysApprove:            false,
				PermissionTimeoutSeconds: 120,
				// Prewarm default on: boot the shared engine at daemon start so
				// the first phone session skips the Bun cold start.
				Prewarm:                true,
				TurnStallNoticeSeconds: 120,
				// Session tree demux default on (MADR 0020 Q5 / KD11).
				SessionTree: true,
				// ~12 mid-stream updates/sec instead of one per token
				// (MADR 0024). Inside the phone's 32ms event batch window.
				StreamCoalesceMs: 80,
			},
			Codex: CodexProviderConfig{
				Enabled:                  true,
				Bin:                      "codex",
				AlwaysApprove:            false,
				PermissionTimeoutSeconds: 900,
				Prewarm:                  false,
				// Match grok/goose/opencode: long tool runs (e.g. full
				// `flutter test`) must surface a stall notice on the phone
				// rather than looking like a frozen agent. 0 still disables.
				TurnStallNoticeSeconds: 120,
				StreamCoalesceMs:       80,
				ApprovalPolicy:         "",
				SandboxMode:            "",
				AllowFullAccess:        false,
			},
			Kilo: KiloProviderConfig{
				// Ships dark until MADR 0075 acceptance (plan §6).
				Enabled:                  false,
				Bin:                      "kilo",
				AlwaysApprove:            false,
				PermissionTimeoutSeconds: 120,
				// Same rationale as OpenCode: skip the Bun-class cold start.
				Prewarm:                true,
				TurnStallNoticeSeconds: 120,
				// Off until child-SSE fixtures prove tree demux (plan PD2).
				SessionTree:      false,
				StreamCoalesceMs: 80,
			},
		},
		Headscale: HeadscaleConfig{
			ControlURL: "http://localhost:8080",
		},
		Limits: LimitsConfig{
			MaxWSClients:    8,
			MaxLiveSessions: 16,
			// 120 s: covers Android Doze / brief mesh blips while the phone
			// is still expected to app-ping every 10 s when awake. TCP
			// keepalive (25+4×5 ≈ 45 s) still reaps blackholed peers first.
			WSReadDeadlineSeconds: 120,
			WSResumeWindowSeconds: 120,
			// TCPKeepalive zero value = enabled with 25/5/4 (NetConfig).
		},
		Relay: RelayConfig{}, // disabled until url/host_id/secret set
		Pair:  PairConfig{},  // empty => dynamic advertise-host detection
		// Receipts off by default (MADR 0077 D4); handoff attestation on by
		// default *when* receipts are enabled (MADR 0078 D6).
		Receipts: ReceiptsConfig{Handoffs: true},
	}
}

// Resolved returns Limits with zero fields filled from Defaults().
func (l LimitsConfig) Resolved() LimitsConfig {
	d := Defaults().Limits
	if l.MaxWSClients <= 0 {
		l.MaxWSClients = d.MaxWSClients
	}
	if l.MaxLiveSessions <= 0 {
		l.MaxLiveSessions = d.MaxLiveSessions
	}
	if l.WSReadDeadlineSeconds <= 0 {
		l.WSReadDeadlineSeconds = d.WSReadDeadlineSeconds
	}
	// Floor, not clamp-to-default: the deadline is advertised contract
	// (MADR 0068 D2) and values below the ping cadence would reap healthy
	// clients between pings.
	if l.WSReadDeadlineSeconds < 15 {
		l.WSReadDeadlineSeconds = 15
	}
	if l.WSResumeWindowSeconds <= 0 {
		l.WSResumeWindowSeconds = d.WSResumeWindowSeconds
	}
	return l
}

// validate checks the TLS block, including the interaction between the mode
// selector and the legacy enabled switch.
func (t TLSConfig) validate() error {
	explicit := strings.ToLower(strings.TrimSpace(t.Mode))
	switch explicit {
	case "", TLSModeLetsEncrypt, TLSModeSelfSigned, TLSModeOff:
	default:
		return fmt.Errorf("tls.mode must be %s|%s|%s, got %q",
			TLSModeLetsEncrypt, TLSModeSelfSigned, TLSModeOff, t.Mode)
	}
	if !t.Enabled && (explicit == TLSModeLetsEncrypt || explicit == TLSModeSelfSigned) {
		return fmt.Errorf("tls.enabled is false but tls.mode is %q; "+
			"drop tls.enabled or set tls.mode: %s", explicit, TLSModeOff)
	}

	mode := t.ResolvedMode()
	certSet := strings.TrimSpace(t.CertFile) != ""
	keySet := strings.TrimSpace(t.KeyFile) != ""
	if certSet != keySet {
		return fmt.Errorf("tls.cert_file and tls.key_file must be set together")
	}
	if certSet && mode != TLSModeSelfSigned {
		return fmt.Errorf("tls.cert_file/tls.key_file are only valid with tls.mode: %s (mode is %q)",
			TLSModeSelfSigned, mode)
	}

	le := t.LetsEncrypt
	if mode != TLSModeLetsEncrypt {
		return nil
	}
	domains := le.TrimmedDomains()
	if len(domains) == 0 {
		return fmt.Errorf("tls.mode is %s but tls.letsencrypt.domains is empty", TLSModeLetsEncrypt)
	}
	if strings.TrimSpace(le.Email) == "" {
		return fmt.Errorf("tls.mode is %s but tls.letsencrypt.email is empty", TLSModeLetsEncrypt)
	}
	for _, d := range domains {
		if net.ParseIP(d) != nil {
			return fmt.Errorf("tls.letsencrypt.domains: %q is an IP address; "+
				"Let's Encrypt only issues for DNS names (use tls.mode: %s for mesh IPs)",
				d, TLSModeSelfSigned)
		}
		if strings.ContainsAny(d, "/: ") {
			return fmt.Errorf("tls.letsencrypt.domains: %q must be a bare hostname (no scheme, port, or path)", d)
		}
	}
	if u := strings.TrimSpace(le.DirectoryURL); u != "" &&
		!strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		return fmt.Errorf("tls.letsencrypt.directory_url must be an http(s) URL, got %q", u)
	}
	return nil
}

// ResolveListenHost replaces the ListenHostTailscale sentinel in listen.host
// with this host's Tailscale IPv4. It is a no-op for every other value.
//
// Fails closed: when the sentinel is set and no Tailscale IPv4 can be found it
// returns an actionable error rather than binding a wider interface.
func (c *Config) ResolveListenHost() error {
	if !strings.EqualFold(strings.TrimSpace(c.Listen.Host), ListenHostTailscale) {
		return nil
	}
	ip := tailnet.IPv4()
	if ip == "" {
		return fmt.Errorf("listen.host is %q but no Tailscale IPv4 was found: "+
			"start Tailscale (`sudo tailscale up`) and check `tailscale ip -4`, "+
			"or set listen.host to an explicit address "+
			"(--listen-host / MCREMOTE_LISTEN_HOST); "+
			"refusing to fall back to 0.0.0.0", ListenHostTailscale)
	}
	c.Listen.Host = ip
	return nil
}

// Addr returns host:port for net.Listen.
func (c Config) Addr() string {
	return net.JoinHostPort(c.Listen.Host, fmt.Sprintf("%d", c.Listen.Port))
}

// ACMECacheDir is where certmagic keeps ACME accounts and issued certificates.
func (c Config) ACMECacheDir() string {
	if d := strings.TrimSpace(c.TLS.LetsEncrypt.CacheDir); d != "" {
		return d
	}
	return filepath.Join(c.DataDir, "acme")
}

// Validate checks configuration for obvious errors.
// validateACPProvider checks the shared ACP options (MCP servers). name is the
// provider key ("grok", "goose", …) for error messages.
func validateACPProvider(name string, c ACPProviderConfig) error {
	for i, m := range c.MCPServers {
		switch m.Transport {
		case "http", "sse":
		default:
			return fmt.Errorf("providers.%s.mcp_servers[%d].transport must be http|sse, got %q",
				name, i, m.Transport)
		}
		if m.URL == "" {
			return fmt.Errorf("providers.%s.mcp_servers[%d].url must not be empty", name, i)
		}
	}
	return nil
}

// Validate reports the first problem that would make this configuration
// unusable — an unroutable listen address, an out-of-range port, a provider
// section that contradicts itself. Called after load and after every override,
// so a bad flag fails at startup rather than at first use.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Listen.Host) == "" {
		return fmt.Errorf("listen.host must not be empty")
	}
	if c.Listen.Port < 1 || c.Listen.Port > 65535 {
		return fmt.Errorf("listen.port must be between 1 and 65535, got %d", c.Listen.Port)
	}
	if err := c.TLS.validate(); err != nil {
		return err
	}
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "warning", "error":
	default:
		return fmt.Errorf("log.level must be debug|info|warn|error, got %q", c.Log.Level)
	}
	switch strings.ToLower(c.Log.Format) {
	case "text", "json":
	default:
		return fmt.Errorf("log.format must be text|json, got %q", c.Log.Format)
	}
	if c.Limits.MaxWSClients < 0 {
		return fmt.Errorf("limits.max_ws_clients must be >= 0, got %d", c.Limits.MaxWSClients)
	}
	if c.Limits.MaxLiveSessions < 0 {
		return fmt.Errorf("limits.max_live_sessions must be >= 0, got %d", c.Limits.MaxLiveSessions)
	}
	if c.Providers.Grok.PermissionTimeoutSeconds < 0 {
		return fmt.Errorf("providers.grok.permission_timeout_seconds must be >= 0, got %d",
			c.Providers.Grok.PermissionTimeoutSeconds)
	}
	if v := c.Providers.Grok.StreamCoalesceMs; v < 0 || v > maxStreamCoalesceMs {
		return fmt.Errorf("providers.grok.stream_coalesce_ms must be 0..%d, got %d",
			maxStreamCoalesceMs, v)
	}
	if c.Providers.Grok.TurnStallNoticeSeconds < 0 {
		return fmt.Errorf("providers.grok.turn_stall_notice_seconds must be >= 0, got %d",
			c.Providers.Grok.TurnStallNoticeSeconds)
	}
	if c.Providers.Grok.ReasoningEffort != "" && strings.TrimSpace(c.Providers.Grok.ReasoningEffort) == "" {
		return fmt.Errorf("providers.grok.reasoning_effort must not be whitespace-only")
	}
	if err := validateACPProvider("grok", c.Providers.Grok.ACPProviderConfig); err != nil {
		return err
	}
	if c.Providers.Goose.PermissionTimeoutSeconds < 0 {
		return fmt.Errorf("providers.goose.permission_timeout_seconds must be >= 0, got %d",
			c.Providers.Goose.PermissionTimeoutSeconds)
	}
	if c.Providers.Goose.TurnStallNoticeSeconds < 0 {
		return fmt.Errorf("providers.goose.turn_stall_notice_seconds must be >= 0, got %d",
			c.Providers.Goose.TurnStallNoticeSeconds)
	}
	if v := c.Providers.Goose.StreamCoalesceMs; v < 0 || v > maxStreamCoalesceMs {
		return fmt.Errorf("providers.goose.stream_coalesce_ms must be between 0 and %d, got %d",
			maxStreamCoalesceMs, v)
	}
	if err := validateACPProvider("goose", c.Providers.Goose.ACPProviderConfig); err != nil {
		return err
	}
	seenBuiltins := make(map[string]struct{}, len(c.Providers.Goose.WithBuiltins))
	for i, builtin := range c.Providers.Goose.WithBuiltins {
		trimmed := strings.TrimSpace(builtin)
		if trimmed == "" {
			return fmt.Errorf("providers.goose.with_builtins[%d] must not be empty", i)
		}
		if trimmed != builtin {
			return fmt.Errorf("providers.goose.with_builtins[%d] must not contain surrounding whitespace", i)
		}
		if _, duplicate := seenBuiltins[trimmed]; duplicate {
			return fmt.Errorf("providers.goose.with_builtins contains duplicate %q", trimmed)
		}
		seenBuiltins[trimmed] = struct{}{}
	}
	// providers.opencode.transport was retired in MADR 0019: OpenCode is always
	// driven through the shared `opencode serve` engine. Fail loudly — viper
	// ignores unknown keys, so staying silent would quietly change behaviour
	// for a config that still pins the old per-session ACP transport.
	if c.Providers.Opencode.RetiredTransport != "" {
		return fmt.Errorf("providers.opencode.transport is no longer supported (found %q); "+
			"OpenCode now always uses one shared `opencode serve` engine — remove the key "+
			"(providers.opencode.args and providers.opencode.fs_roots were retired with it)",
			c.Providers.Opencode.RetiredTransport)
	}
	if c.Providers.Opencode.PermissionTimeoutSeconds < 0 {
		return fmt.Errorf("providers.opencode.permission_timeout_seconds must be >= 0, got %d",
			c.Providers.Opencode.PermissionTimeoutSeconds)
	}
	if c.Providers.Opencode.TurnStallNoticeSeconds < 0 {
		return fmt.Errorf("providers.opencode.turn_stall_notice_seconds must be >= 0, got %d",
			c.Providers.Opencode.TurnStallNoticeSeconds)
	}
	// Bounded above as well as below: nothing else guards this, and a window
	// of several seconds would make streaming look broken rather than smooth.
	if v := c.Providers.Opencode.StreamCoalesceMs; v < 0 || v > maxStreamCoalesceMs {
		return fmt.Errorf("providers.opencode.stream_coalesce_ms must be 0..%d, got %d",
			maxStreamCoalesceMs, v)
	}
	if c.Providers.Codex.PermissionTimeoutSeconds < 0 {
		return fmt.Errorf("providers.codex.permission_timeout_seconds must be >= 0, got %d",
			c.Providers.Codex.PermissionTimeoutSeconds)
	}
	if c.Providers.Codex.TurnStallNoticeSeconds < 0 {
		return fmt.Errorf("providers.codex.turn_stall_notice_seconds must be >= 0, got %d",
			c.Providers.Codex.TurnStallNoticeSeconds)
	}
	if v := c.Providers.Codex.StreamCoalesceMs; v < 0 || v > maxStreamCoalesceMs {
		return fmt.Errorf("providers.codex.stream_coalesce_ms must be 0..%d, got %d",
			maxStreamCoalesceMs, v)
	}
	if !validApprovalPolicy(c.Providers.Codex.ApprovalPolicy) {
		return fmt.Errorf("providers.codex.approval_policy must be empty, untrusted, on-request, or never, got %q",
			c.Providers.Codex.ApprovalPolicy)
	}
	if !validSandboxMode(c.Providers.Codex.SandboxMode) {
		return fmt.Errorf("providers.codex.sandbox_mode must be empty, read-only, workspace-write, or danger-full-access, got %q",
			c.Providers.Codex.SandboxMode)
	}
	switch strings.TrimSpace(c.Providers.Codex.SandboxBrokenPolicy) {
	case "", "warn", "require_full_access", "refuse":
	default:
		return fmt.Errorf("providers.codex.sandbox_broken_policy must be empty, warn, require_full_access, or refuse, got %q",
			c.Providers.Codex.SandboxBrokenPolicy)
	}
	if c.Providers.Kilo.PermissionTimeoutSeconds < 0 {
		return fmt.Errorf("providers.kilo.permission_timeout_seconds must be >= 0, got %d",
			c.Providers.Kilo.PermissionTimeoutSeconds)
	}
	if c.Providers.Kilo.TurnStallNoticeSeconds < 0 {
		return fmt.Errorf("providers.kilo.turn_stall_notice_seconds must be >= 0, got %d",
			c.Providers.Kilo.TurnStallNoticeSeconds)
	}
	if v := c.Providers.Kilo.StreamCoalesceMs; v < 0 || v > maxStreamCoalesceMs {
		return fmt.Errorf("providers.kilo.stream_coalesce_ms must be 0..%d, got %d",
			maxStreamCoalesceMs, v)
	}
	// Rejected at load rather than at session start: grok exits with
	// `error: unexpected argument` for an unknown value, which surfaces as a
	// provider that never becomes ready (MADR 0050).
	if !validGrokPermissionMode(c.Providers.Grok.PermissionMode) {
		return fmt.Errorf("providers.grok.permission_mode must be empty, default, acceptEdits, auto, dontAsk, bypassPermissions, or plan, got %q",
			c.Providers.Grok.PermissionMode)
	}
	if err := c.Relay.validate(); err != nil {
		return err
	}
	if err := c.Pair.validate(); err != nil {
		return err
	}
	return nil
}

func (p PairConfig) validate() error {
	h := strings.TrimSpace(p.AdvertiseHost)
	if h == "" {
		return nil
	}
	// It is dialled as an authority, not a URL: a scheme or path here would be
	// baked into the pair URI verbatim and break the phone's connect.
	if strings.Contains(h, "://") || strings.ContainsAny(h, "/ ") {
		return fmt.Errorf("pair.advertise_host must be a bare host or host:port (no scheme, path, or spaces), got %q", h)
	}
	return nil
}

func (r RelayConfig) validate() error {
	url := strings.TrimSpace(r.URL)
	id := strings.TrimSpace(r.HostID)
	sec := strings.TrimSpace(r.Secret)
	if url == "" && id == "" && sec == "" {
		return nil
	}
	// url and host_id are the public join route (pair QR). They must travel
	// together. Secret is only for host→mcrelay registration and may live
	// solely in the serve process env (MCREMOTE_RELAY_SECRET).
	if (url == "") != (id == "") {
		return fmt.Errorf("relay: url and host_id must both be set or both empty")
	}
	if sec != "" && (url == "" || id == "") {
		return fmt.Errorf("relay: secret requires url and host_id")
	}
	if sec != "" && len(sec) < 16 {
		return fmt.Errorf("relay.secret too short (min 16 characters)")
	}
	if len(id) > 128 {
		return fmt.Errorf("relay.host_id too long")
	}
	for _, ch := range id {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
		case ch == '-' || ch == '_' || ch == '.':
		default:
			return fmt.Errorf("relay.host_id has invalid character %q", ch)
		}
	}
	return nil
}
