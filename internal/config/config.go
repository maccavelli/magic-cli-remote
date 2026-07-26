// Package config defines typed daemon configuration and defaults.
package config

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"

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

// RelayConfig configures the mcremote → mcrelay host registration path.
//
// When URL is set, HostID and Secret are required. The daemon dials out
// (no inbound ports), registers, and on each dial opens a tunnel bridged to
// the local listener so phones can reach this host off-mesh.
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

// Enabled reports whether outbound relay registration should run.
func (r RelayConfig) Enabled() bool {
	return strings.TrimSpace(r.URL) != ""
}

// LimitsConfig bounds concurrent resources on the daemon (Phase 4 hardening).
// Zero means "use Defaults()".
type LimitsConfig struct {
	// MaxWSClients caps simultaneous WebSocket connections (0 → default 8).
	MaxWSClients int `mapstructure:"max_ws_clients"`
	// MaxLiveSessions caps concurrent provider sessions (0 → default 16).
	MaxLiveSessions int `mapstructure:"max_live_sessions"`
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

// maxStreamCoalesceMs bounds providers.opencode.stream_coalesce_ms. Past about
// a second the stream stops reading as live typing, and nothing downstream
// would flag the mistake (MADR 0024).
const maxStreamCoalesceMs = 1000

// ProvidersConfig enables individual providers.
type ProvidersConfig struct {
	Fake     FakeProviderConfig     `mapstructure:"fake"`
	Grok     GrokProviderConfig     `mapstructure:"grok"`
	Opencode OpencodeProviderConfig `mapstructure:"opencode"`
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
}

// OpencodeProviderConfig configures the OpenCode adapter
// (see docs/0011-opencode-provider-plan.md).
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
			Grok: GrokProviderConfig{ACPProviderConfig: ACPProviderConfig{
				Enabled:                  true,
				Bin:                      "grok",
				AlwaysApprove:            false,
				PermissionTimeoutSeconds: 120,
				// Prewarm default on: first phone session skips cold start
				// (Phase 4.2). Disable if memory is tight.
				Prewarm:                true,
				TurnStallNoticeSeconds: 120,
			}},
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
		},
		Headscale: HeadscaleConfig{
			ControlURL: "http://localhost:8080",
		},
		Limits: LimitsConfig{
			MaxWSClients:    8,
			MaxLiveSessions: 16,
		},
		Relay: RelayConfig{}, // disabled until url/host_id/secret set
		Pair:  PairConfig{},  // empty => dynamic advertise-host detection
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
	if c.Providers.Grok.TurnStallNoticeSeconds < 0 {
		return fmt.Errorf("providers.grok.turn_stall_notice_seconds must be >= 0, got %d",
			c.Providers.Grok.TurnStallNoticeSeconds)
	}
	if err := validateACPProvider("grok", c.Providers.Grok.ACPProviderConfig); err != nil {
		return err
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
	sec := r.Secret
	if url == "" && id == "" && sec == "" {
		return nil
	}
	if url == "" || id == "" || sec == "" {
		return fmt.Errorf("relay: url, host_id, and secret must all be set together (or all empty to disable)")
	}
	if len(sec) < 16 {
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
