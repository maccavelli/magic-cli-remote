// Package config defines typed daemon configuration and defaults.
package config

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/tailnet"
)

// Config is the full daemon configuration after load and validation.
type Config struct {
	Listen  ListenConfig `mapstructure:"listen"`
	TLS     TLSConfig    `mapstructure:"tls"`
	Log     LogConfig    `mapstructure:"log"`
	DataDir string       `mapstructure:"data_dir"`
	// DisplayName is the friendly host name reported to phones in auth_ok
	// and pair_ok (MADR 0102). Empty = phones show the dialled address.
	// Pure label: never used for routing, TLS, or pairing.
	DisplayName string          `mapstructure:"display_name"`
	Auth        AuthConfig      `mapstructure:"auth"`
	Providers   ProvidersConfig `mapstructure:"providers"`
	Headscale   HeadscaleConfig `mapstructure:"headscale"`
	Limits      LimitsConfig    `mapstructure:"limits"`
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
	// MaxFrameBytes caps a relayed WS frame on the host bridge (0115 F6).
	// Must match the relay's limits.max_message_bytes when that is set.
	// 0 = 10 MiB (the relay default, MADR 0223).
	MaxFrameBytes int `mapstructure:"max_frame_bytes"`
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
// host's Tailscale IPv4 by ResolveListenHost / ResolveListenHostWait before
// the listener is opened.
type ListenConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// ListenHostTailscale is the listen.host sentinel meaning "bind only the
// tailnet interface". It resolves at startup to the host's Tailscale IPv4.
// The daemon waits for that address rather than exiting (user units cannot
// order on system tailscaled); it never widens to 0.0.0.0.
const ListenHostTailscale = "tailscale"

// ListenHostRetryInterval is the pause between tailscale IPv4 probes while
// ResolveListenHostWait is blocked. Tests shrink it.
var ListenHostRetryInterval = time.Second

// listenHostRetryLogEvery is how often we re-log while still waiting.
var listenHostRetryLogEvery = 15 * time.Second

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

// MaxStreamCoalesceMs bounds every providers.<id>.stream_coalesce_ms.
// Past about a second the stream stops reading as live typing, and nothing
// downstream would flag the mistake (MADR 0024).
const MaxStreamCoalesceMs = 1000

// ProvidersConfig enables individual providers.
type ProvidersConfig struct {
	// Sections are the providers.<id> sections each provider's package owns,
	// decoded by Load from LoadOptions.Providers (0208 MADR D6).
	Sections ProviderSections `mapstructure:"-"`

	Codex CodexProviderConfig `mapstructure:"codex"`
	Kilo  KiloProviderConfig  `mapstructure:"kilo"`
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

// AgentCommon is the keys every agent CLI provider's section has. A section
// embeds it squashed, so the keys sit directly under providers.<id>, and gets
// IsEnabled and Agent from it (0208 MADR D6).
type AgentCommon struct {
	// Enabled registers the provider.
	Enabled bool `mapstructure:"enabled"`
	// Bin is the agent executable, by name on PATH or by path.
	Bin string `mapstructure:"bin"`
	// AlwaysApprove answers every permission request with its allow option,
	// without asking the phone.
	AlwaysApprove bool `mapstructure:"always_approve"`
	// DefaultCWD is a session's working directory when the phone names none.
	// Empty is the daemon user's home directory. A relative path is resolved
	// against the config file's directory.
	DefaultCWD string `mapstructure:"default_cwd"`
	// Model is the model a session starts on. Empty is the agent's own default.
	Model string `mapstructure:"model"`
	// PermissionTimeoutSeconds bounds how long a remote permission request waits
	// for a decision before the agent stops waiting (treated as cancelled).
	// 0 disables the timeout.
	PermissionTimeoutSeconds int `mapstructure:"permission_timeout_seconds"`
	// Prewarm starts the agent's engine, or one spare agent process, at daemon
	// start, so the first session skips the cold start (MADR 0089 D5).
	Prewarm bool `mapstructure:"prewarm"`
	// TurnStallNoticeSeconds emits a notice when a running turn produces no
	// output for this long (0 disables).
	TurnStallNoticeSeconds int `mapstructure:"turn_stall_notice_seconds"`
}

// IsEnabled reports providers.<id>.enabled.
func (a *AgentCommon) IsEnabled() bool { return a.Enabled }

// Agent returns the keys every agent has, for the daemon, doctor and prewarm.
func (a *AgentCommon) Agent() *AgentCommon { return a }

// ValidateAgent checks the keys every agent has. id names the section in
// errors.
func (a *AgentCommon) ValidateAgent(id string) error {
	if a.PermissionTimeoutSeconds < 0 {
		return fmt.Errorf("providers.%s.permission_timeout_seconds must be >= 0, got %d",
			id, a.PermissionTimeoutSeconds)
	}
	if a.TurnStallNoticeSeconds < 0 {
		return fmt.Errorf("providers.%s.turn_stall_notice_seconds must be >= 0, got %d",
			id, a.TurnStallNoticeSeconds)
	}
	return nil
}

// ACPProviderConfig is the configuration shared by every ACP CLI agent
// provider. Each provider's section embeds it squashed, so the agents share
// one config shape and a new ACP option is added here once.
type ACPProviderConfig struct {
	AgentCommon `mapstructure:",squash"`
	// Args replaces the agent's built-in argv when non-empty. Config-file only:
	// an entry may contain a comma, which an environment value would split.
	Args []string `mapstructure:"args" env:"-"`
	// FSRoots optionally confines the agent's fs read/write callbacks to these
	// roots (plus the session cwd). Empty (default) is unrestricted. This is
	// defense-in-depth and an audit surface, not a sandbox — the agent has
	// terminal access as the same user regardless. Config-file only.
	FSRoots []string `mapstructure:"fs_roots" env:"-"`
	// AuthMethodID is an optional pin of the ACP authenticate method.
	// Empty (default) lets MADR 0085 D2 auto-select a headless-safe advertised
	// method.
	AuthMethodID string `mapstructure:"auth_method_id"`
	// MCPServers are MCP servers to advertise to the agent, extending it with
	// extra tools/context. Each is forwarded only when the agent advertises the
	// matching transport (mcpCapabilities.http / .sse); others are dropped.
	// Config-file only: a list of tables.
	MCPServers []MCPServerConfig `mapstructure:"mcp_servers" env:"-"`
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
	// Transport is stdio (default), unix_ws, ws, or managed_daemon_proxy.
	Transport string `mapstructure:"transport"`
	// ListenAddress is optional and only applies to ws. It must be loopback.
	ListenAddress string `mapstructure:"listen_address"`
	// WSAuthMode is capability_token or signed_bearer. Secret material is
	// always generated into the daemon runtime directory, never configured.
	WSAuthMode string `mapstructure:"ws_auth_mode"`
	// ReconnectAttempts is capped at three. Default 3.
	ReconnectAttempts int `mapstructure:"reconnect_attempts"`
	// Environments is the administrator-owned remote execution catalog. Phones
	// see only ids, roots, and observational status/info projections.
	Environments []CodexExecutionEnvironmentConfig `mapstructure:"environments"`
	// StandaloneProcessesEnabled gates the unsandboxed process/* surface.
	StandaloneProcessesEnabled bool `mapstructure:"standalone_processes_enabled"`
	// StandaloneProcessEnvAllowlist contains host-approved non-secret names.
	StandaloneProcessEnvAllowlist []string `mapstructure:"standalone_process_env_allowlist"`
}

// CodexExecutionEnvironmentConfig is one host-admin registered exec server.
type CodexExecutionEnvironmentConfig struct {
	ID                    string   `mapstructure:"id"`
	ExecServerURL         string   `mapstructure:"exec_server_url"`
	ConnectTimeoutMS      int      `mapstructure:"connect_timeout_ms"`
	RuntimeWorkspaceRoots []string `mapstructure:"runtime_workspace_roots"`
}

// KiloProviderConfig configures the Kilo CLI provider (shared `kilo serve`
// engine over httpagent, MADR 0075). Kilo is a fork of OpenCode's 1.x serve
// architecture, so its keys follow that dialect's: no `transport` (retired in
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

func configLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
			Codex: CodexProviderConfig{
				Enabled:                  true,
				Bin:                      "codex",
				AlwaysApprove:            false,
				PermissionTimeoutSeconds: 900,
				Prewarm:                  false,
				// Match grok/opencode: long tool runs (e.g. full
				// `flutter test`) must surface a stall notice on the phone
				// rather than looking like a frozen agent. 0 still disables.
				TurnStallNoticeSeconds:     120,
				StreamCoalesceMs:           80,
				ApprovalPolicy:             "",
				SandboxMode:                "",
				AllowFullAccess:            false,
				Transport:                  "stdio",
				ReconnectAttempts:          3,
				StandaloneProcessesEnabled: false,
			},
			Kilo: KiloProviderConfig{
				// Default-on since MADR 0075 acceptance flip (2026-08-10).
				Enabled:                  true,
				Bin:                      "kilo",
				AlwaysApprove:            false,
				PermissionTimeoutSeconds: 120,
				// Off (MADR 0089 D5): first session pays the Bun-class
				// cold start instead of holding an idle engine.
				Prewarm:                false,
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
// returns an actionable error rather than binding a wider interface. The
// daemon calls ResolveListenHostWait so a boot race with tailscaled does not
// exit and trip systemd's start limit.
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

// ResolveListenHostWait is ResolveListenHost, but if the tailscale sentinel is
// set and no IPv4 exists yet it polls until one appears or ctx is cancelled.
// It still never widens to 0.0.0.0.
func (c *Config) ResolveListenHostWait(ctx context.Context, log *slog.Logger) error {
	err := c.ResolveListenHost()
	if err == nil {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(c.Listen.Host), ListenHostTailscale) {
		return err
	}
	if log != nil {
		log.Warn("waiting for Tailscale IPv4 before binding",
			slog.String("listen.host", ListenHostTailscale),
			slog.String("hint", "start Tailscale (`sudo tailscale up`) and check `tailscale ip -4`"),
		)
	}
	interval := ListenHostRetryInterval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastLog := time.Now()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("listen.host is %q but no Tailscale IPv4 was found before shutdown: %w",
				ListenHostTailscale, ctx.Err())
		case <-ticker.C:
			if err := c.ResolveListenHost(); err == nil {
				if log != nil {
					log.Info("Tailscale IPv4 became available",
						slog.String("addr", c.Addr()),
					)
				}
				return nil
			}
			if log != nil && time.Since(lastLog) >= listenHostRetryLogEvery {
				log.Info("still waiting for Tailscale IPv4")
				lastLog = time.Now()
			}
		}
	}
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

// ValidateACPProvider checks the shared ACP options (MCP servers). name is the
// provider key for error messages.
func ValidateACPProvider(name string, c ACPProviderConfig) error {
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
	if n := utf8.RuneCountInString(c.DisplayName); n > 128 {
		return fmt.Errorf("display_name must be 128 characters or fewer, got %d", n)
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
	if c.Providers.Codex.PermissionTimeoutSeconds < 0 {
		return fmt.Errorf("providers.codex.permission_timeout_seconds must be >= 0, got %d",
			c.Providers.Codex.PermissionTimeoutSeconds)
	}
	if c.Providers.Codex.TurnStallNoticeSeconds < 0 {
		return fmt.Errorf("providers.codex.turn_stall_notice_seconds must be >= 0, got %d",
			c.Providers.Codex.TurnStallNoticeSeconds)
	}
	if v := c.Providers.Codex.StreamCoalesceMs; v < 0 || v > MaxStreamCoalesceMs {
		return fmt.Errorf("providers.codex.stream_coalesce_ms must be 0..%d, got %d",
			MaxStreamCoalesceMs, v)
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
	switch strings.TrimSpace(c.Providers.Codex.Transport) {
	case "stdio":
		if c.Providers.Codex.ListenAddress != "" || c.Providers.Codex.WSAuthMode != "" {
			return fmt.Errorf("providers.codex stdio transport rejects listen_address and ws_auth_mode")
		}
	case "unix_ws", "managed_daemon_proxy":
		if runtime.GOOS == "windows" {
			return fmt.Errorf("providers.codex transport %q is Unix-only", c.Providers.Codex.Transport)
		}
		if c.Providers.Codex.ListenAddress != "" || c.Providers.Codex.WSAuthMode != "" {
			return fmt.Errorf("providers.codex transport %q owns its endpoint and rejects WS fields", c.Providers.Codex.Transport)
		}
	case "ws":
		if address := c.Providers.Codex.ListenAddress; address != "" {
			host, _, err := net.SplitHostPort(address)
			if err != nil || !configLoopbackHost(host) {
				return fmt.Errorf("providers.codex.listen_address must be loopback host:port")
			}
		}
		switch c.Providers.Codex.WSAuthMode {
		case "", "capability_token", "signed_bearer":
		default:
			return fmt.Errorf("providers.codex.ws_auth_mode must be empty, capability_token, or signed_bearer")
		}
	default:
		return fmt.Errorf("providers.codex.transport must be stdio, unix_ws, ws, or managed_daemon_proxy")
	}
	if attempts := c.Providers.Codex.ReconnectAttempts; attempts < 0 || attempts > 3 {
		return fmt.Errorf("providers.codex.reconnect_attempts must be 0..3, got %d", attempts)
	}
	if err := validateCodexExecutionConfig(c.Providers.Codex); err != nil {
		return err
	}
	if c.Providers.Kilo.PermissionTimeoutSeconds < 0 {
		return fmt.Errorf("providers.kilo.permission_timeout_seconds must be >= 0, got %d",
			c.Providers.Kilo.PermissionTimeoutSeconds)
	}
	if c.Providers.Kilo.TurnStallNoticeSeconds < 0 {
		return fmt.Errorf("providers.kilo.turn_stall_notice_seconds must be >= 0, got %d",
			c.Providers.Kilo.TurnStallNoticeSeconds)
	}
	if v := c.Providers.Kilo.StreamCoalesceMs; v < 0 || v > MaxStreamCoalesceMs {
		return fmt.Errorf("providers.kilo.stream_coalesce_ms must be 0..%d, got %d",
			MaxStreamCoalesceMs, v)
	}
	if err := c.Relay.validate(); err != nil {
		return err
	}
	if err := c.Pair.validate(); err != nil {
		return err
	}
	return nil
}

var codexEnvironmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateCodexExecutionConfig(c CodexProviderConfig) error {
	seenIDs := make(map[string]struct{}, len(c.Environments))
	for index, environment := range c.Environments {
		id := strings.TrimSpace(environment.ID)
		if id == "" {
			return fmt.Errorf("providers.codex.environments[%d] environment id must not be empty", index)
		}
		if _, exists := seenIDs[id]; exists {
			return fmt.Errorf("providers.codex.environments has duplicate environment id %q", id)
		}
		seenIDs[id] = struct{}{}
		parsed, err := url.Parse(environment.ExecServerURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.User != nil {
			return fmt.Errorf("providers.codex.environments[%d].exec_server_url must be an unauthenticated ws:// or wss:// URL", index)
		}
		if parsed.Scheme == "ws" && !configLoopbackHost(parsed.Hostname()) {
			return fmt.Errorf("providers.codex.environments[%d].exec_server_url ws:// is restricted to loopback", index)
		}
		if environment.ConnectTimeoutMS <= 0 || environment.ConnectTimeoutMS > 60_000 {
			return fmt.Errorf("providers.codex.environments[%d].connect_timeout_ms must be 1..60000", index)
		}
		logicalRoots := make(map[string]struct{}, len(environment.RuntimeWorkspaceRoots))
		canonicalRoots := make(map[string]struct{}, len(environment.RuntimeWorkspaceRoots))
		for _, root := range environment.RuntimeWorkspaceRoots {
			if !filepath.IsAbs(root) {
				return fmt.Errorf("providers.codex.environments[%d].runtime_workspace_roots must be absolute", index)
			}
			logical := filepath.Clean(root)
			if _, exists := logicalRoots[logical]; exists {
				return fmt.Errorf("providers.codex.environments[%d] has duplicate runtime_workspace_roots", index)
			}
			logicalRoots[logical] = struct{}{}
			canonical := logical
			if resolved, resolveErr := filepath.EvalSymlinks(logical); resolveErr == nil {
				canonical = filepath.Clean(resolved)
			}
			if _, exists := canonicalRoots[canonical]; exists {
				return fmt.Errorf("providers.codex.environments[%d] has duplicate canonical runtime_workspace_roots", index)
			}
			canonicalRoots[canonical] = struct{}{}
		}
	}
	seenNames := make(map[string]struct{}, len(c.StandaloneProcessEnvAllowlist))
	for _, rawName := range c.StandaloneProcessEnvAllowlist {
		name := strings.TrimSpace(rawName)
		if name == "" {
			return fmt.Errorf("providers.codex.standalone_process_env_allowlist contains an empty name")
		}
		if !codexEnvironmentName.MatchString(name) {
			return fmt.Errorf("providers.codex.standalone_process_env_allowlist has invalid environment name %q", name)
		}
		if codexSecretEnvironmentName(name) {
			return fmt.Errorf("providers.codex.standalone_process_env_allowlist rejects secret-like name %q", name)
		}
		if _, exists := seenNames[name]; exists {
			return fmt.Errorf("providers.codex.standalone_process_env_allowlist has duplicate name %q", name)
		}
		seenNames[name] = struct{}{}
	}
	return nil
}

func codexSecretEnvironmentName(name string) bool {
	upper := strings.ToUpper(name)
	for _, marker := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "PRIVATE_KEY", "CREDENTIAL"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
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
	if mfb := r.MaxFrameBytes; mfb != 0 && (mfb < 4096 || mfb > 16<<20) {
		return fmt.Errorf("relay.max_frame_bytes must be 0 (default) or 4096–16777216, got %d", mfb)
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

// secretConfigKeys are the viper keys whose values are credentials.
//
// This list is the single point of truth for "which settings are secret". It
// exists so the permission check in Load asks a question about the
// configuration type rather than about one field name: the next secret-bearing
// field will be added by someone who has not read MADR 0155, and a literal
// check at the call site would fail open for them with no signal (0155 D7,
// PLAN C3).
//
// Adding a field here is the whole change. TestSecretConfigKeysCoversEveryTag
// fails if a struct tag that looks like a credential is not listed.
var secretConfigKeys = []string{"relay.secret"}

// HasInlineSecret reports whether any secret-bearing key was set by the config
// FILE, as opposed to the environment or a command-line flag.
//
// Provenance is the entire point, and it is why this takes an oracle instead of
// inspecting a [Config] value: MCREMOTE_RELAY_SECRET and --relay-secret
// populate exactly the same field as relay.secret in YAML, so a merged Config
// cannot say where the value came from. A daemon that receives its secret from
// the environment has put nothing on disk to protect, and failing its startup
// over a world-readable config would be a false positive.
//
// Production passes viper's InConfig, which answers for the config file
// specifically. Tests pass a fake.
func HasInlineSecret(inFile func(key string) bool) bool {
	if inFile == nil {
		return false
	}
	return slices.ContainsFunc(secretConfigKeys, inFile)
}
