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
}

// ProvidersConfig enables individual providers.
type ProvidersConfig struct {
	Fake FakeProviderConfig `mapstructure:"fake"`
	Grok GrokProviderConfig `mapstructure:"grok"`
}

// FakeProviderConfig configures the test/demo provider.
type FakeProviderConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// GrokProviderConfig configures the Grok Build ACP adapter.
type GrokProviderConfig struct {
	Enabled       bool     `mapstructure:"enabled"`
	Bin           string   `mapstructure:"bin"`
	Args          []string `mapstructure:"args"`
	AlwaysApprove bool     `mapstructure:"always_approve"`
	DefaultCWD    string   `mapstructure:"default_cwd"`
	Model         string   `mapstructure:"model"`
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
		},
		Providers: ProvidersConfig{
			Fake: FakeProviderConfig{Enabled: true},
			Grok: GrokProviderConfig{
				Enabled:       true,
				Bin:           "grok",
				AlwaysApprove: false,
			},
		},
		Headscale: HeadscaleConfig{
			ControlURL: "http://localhost:8080",
		},
	}
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
	return nil
}
