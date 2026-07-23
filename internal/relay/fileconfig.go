package relay

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/xdg"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// FileConfig is the YAML / env / flag configuration surface for mcrelay
// (aligned with mcremote's config package shape).
type FileConfig struct {
	Listen  ListenConfig `mapstructure:"listen"`
	TLS     TLSConfig    `mapstructure:"tls"`
	Log     LogConfig    `mapstructure:"log"`
	DataDir string       `mapstructure:"data_dir"`
	Hosts   []HostEntry  `mapstructure:"hosts"`
	Limits  LimitsConfig `mapstructure:"limits"`
}

// ListenConfig is the public edge bind address.
type ListenConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// TLS modes for the public edge.
const (
	TLSModeFiles       = "files"
	TLSModeLetsEncrypt = "letsencrypt"
	TLSModeOff         = "off"
)

// TLSConfig controls outer TLS on the relay (join plane only; MADR 0015 S9).
//
// Mode: empty → "letsencrypt" when domains+email set, else "files" when
// cert+key set, else "off". Explicit: files | letsencrypt | off.
//
// letsencrypt uses ACME HTTP-01 (public name on port 80 by default) — not
// DNS-01 (that path is mesh-only mcremote).
type TLSConfig struct {
	Mode        string            `mapstructure:"mode"`
	CertFile    string            `mapstructure:"cert_file"`
	KeyFile     string            `mapstructure:"key_file"`
	LetsEncrypt LetsEncryptConfig `mapstructure:"letsencrypt"`
}

// LetsEncryptConfig is ACME HTTP-01 for the public relay edge.
type LetsEncryptConfig struct {
	// Domains are DNS names to request (first is primary). Must resolve to this host.
	Domains []string `mapstructure:"domains"`
	// Email is the ACME account contact.
	Email string `mapstructure:"email"`
	// DirectoryURL overrides the CA directory; empty = production LE.
	DirectoryURL string `mapstructure:"directory_url"`
	// Staging is a shorthand for the Let's Encrypt staging directory.
	Staging bool `mapstructure:"staging"`
	// CacheDir holds certmagic storage; empty → <data_dir>/acme.
	CacheDir string `mapstructure:"cache_dir"`
	// HTTPPort for HTTP-01 (0 = 80). Public LE requires port 80 from the internet.
	HTTPPort int `mapstructure:"http_port"`
}

// Directory returns the ACME directory URL to use.
func (l LetsEncryptConfig) Directory() string {
	if d := strings.TrimSpace(l.DirectoryURL); d != "" {
		return d
	}
	if l.Staging {
		return "https://acme-staging-v02.api.letsencrypt.org/directory"
	}
	return ""
}

// ACMECacheDir resolves the certmagic storage path.
func (c FileConfig) ACMECacheDir() string {
	if d := strings.TrimSpace(c.TLS.LetsEncrypt.CacheDir); d != "" {
		return d
	}
	return filepath.Join(c.DataDir, "acme")
}

// LogConfig matches mcremote log knobs.
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// HostEntry is one allowed mcremote registration (id + secret).
// Secrets may also be supplied via --allow or MCRELAY_HOSTS.
type HostEntry struct {
	ID     string `mapstructure:"id"`
	Secret string `mapstructure:"secret"`
}

// LimitsConfig is the YAML/env form of [Limits] (durations as seconds).
type LimitsConfig struct {
	MaxHosts            int `mapstructure:"max_hosts"`
	MaxPhonesPerHost    int `mapstructure:"max_phones_per_host"`
	MaxMessageBytes     int `mapstructure:"max_message_bytes"`
	MaxConcurrentJoin   int `mapstructure:"max_concurrent_join"`
	AcceptPerMinute     int `mapstructure:"accept_per_minute"`
	TunnelWaitSeconds   int `mapstructure:"tunnel_wait_seconds"`
	RegisterIdleSeconds int `mapstructure:"register_idle_seconds"`
	// SpliceIdleSeconds: silence before ending an opaque splice (0 = default).
	// Negative disables idle kill.
	SpliceIdleSeconds int `mapstructure:"splice_idle_seconds"`
	// SpliceMaxSeconds: absolute splice lifetime (0 = default). Negative disables.
	SpliceMaxSeconds int `mapstructure:"splice_max_seconds"`
}

// DefaultsFile returns built-in FileConfig defaults.
func DefaultsFile() FileConfig {
	d := DefaultLimits()
	return FileConfig{
		Listen: ListenConfig{Host: "0.0.0.0", Port: 8443},
		TLS:    TLSConfig{Mode: ""},
		Log:    LogConfig{Level: "info", Format: "text"},
		Limits: LimitsConfig{
			MaxHosts:            d.MaxHosts,
			MaxPhonesPerHost:    d.MaxPhonesPerHost,
			MaxMessageBytes:     d.MaxMessageBytes,
			MaxConcurrentJoin:   d.MaxConcurrentJoin,
			AcceptPerMinute:     d.AcceptPerMinute,
			TunnelWaitSeconds:   int(d.TunnelWait / time.Second),
			RegisterIdleSeconds: int(d.RegisterIdle / time.Second),
			SpliceIdleSeconds:   int(d.SpliceIdle / time.Second),
			SpliceMaxSeconds:    int(d.SpliceMax / time.Second),
		},
	}
}

// LoadOptions controls FileConfig loading.
type LoadOptions struct {
	ConfigFile string
	Flags      *pflag.FlagSet
	// AllowExtra are host_id:secret pairs from --allow (merged after file/env).
	AllowExtra []string
}

// Load reads defaults → optional YAML → env → flags → --allow merge.
// Precedence: flags > env > file > defaults; --allow always merges in.
func Load(opts LoadOptions) (FileConfig, error) {
	v := viper.New()
	setFileDefaults(v)

	v.SetEnvPrefix("MCRELAY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	_ = v.BindEnv("listen.host", "MCRELAY_LISTEN_HOST")
	_ = v.BindEnv("listen.port", "MCRELAY_LISTEN_PORT")
	_ = v.BindEnv("log.level", "MCRELAY_LOG_LEVEL")
	_ = v.BindEnv("log.format", "MCRELAY_LOG_FORMAT")
	_ = v.BindEnv("data_dir", "MCRELAY_DATA_DIR")
	_ = v.BindEnv("tls.mode", "MCRELAY_TLS_MODE")
	_ = v.BindEnv("tls.cert_file", "MCRELAY_TLS_CERT_FILE")
	_ = v.BindEnv("tls.key_file", "MCRELAY_TLS_KEY_FILE")
	_ = v.BindEnv("tls.letsencrypt.domains", "MCRELAY_TLS_DOMAINS")
	_ = v.BindEnv("tls.letsencrypt.email", "MCRELAY_TLS_EMAIL")
	_ = v.BindEnv("tls.letsencrypt.directory_url", "MCRELAY_TLS_ACME_DIRECTORY_URL")
	_ = v.BindEnv("tls.letsencrypt.staging", "MCRELAY_TLS_ACME_STAGING")
	_ = v.BindEnv("tls.letsencrypt.cache_dir", "MCRELAY_TLS_ACME_CACHE_DIR")
	_ = v.BindEnv("tls.letsencrypt.http_port", "MCRELAY_TLS_ACME_HTTP_PORT")
	_ = v.BindEnv("limits.max_hosts", "MCRELAY_LIMITS_MAX_HOSTS")
	_ = v.BindEnv("limits.max_phones_per_host", "MCRELAY_LIMITS_MAX_PHONES_PER_HOST")
	_ = v.BindEnv("limits.max_message_bytes", "MCRELAY_LIMITS_MAX_MESSAGE_BYTES")
	_ = v.BindEnv("limits.max_concurrent_join", "MCRELAY_LIMITS_MAX_CONCURRENT_JOIN")
	_ = v.BindEnv("limits.accept_per_minute", "MCRELAY_LIMITS_ACCEPT_PER_MINUTE")
	_ = v.BindEnv("limits.tunnel_wait_seconds", "MCRELAY_LIMITS_TUNNEL_WAIT_SECONDS")
	_ = v.BindEnv("limits.register_idle_seconds", "MCRELAY_LIMITS_REGISTER_IDLE_SECONDS")
	_ = v.BindEnv("limits.splice_idle_seconds", "MCRELAY_LIMITS_SPLICE_IDLE_SECONDS")
	_ = v.BindEnv("limits.splice_max_seconds", "MCRELAY_LIMITS_SPLICE_MAX_SECONDS")
	// Comma-separated host_id:secret list (secrets prefer env over YAML).
	_ = v.BindEnv("hosts_csv", "MCRELAY_HOSTS")

	configFile := opts.ConfigFile
	if configFile == "" {
		if env := os.Getenv("MCRELAY_CONFIG"); env != "" {
			configFile = env
		}
	}

	if configFile != "" {
		v.SetConfigFile(configFile)
		if err := v.ReadInConfig(); err != nil {
			return FileConfig{}, fmt.Errorf("read config %s: %w", configFile, err)
		}
	} else {
		dir, err := xdg.ConfigHomeFor(xdg.AppMcrelay)
		if err != nil {
			return FileConfig{}, err
		}
		v.AddConfigPath(dir)
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		if err := v.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok && !isNotExist(err) {
				return FileConfig{}, fmt.Errorf("read config: %w", err)
			}
		}
	}

	if opts.Flags != nil {
		if err := bindRelayFlags(v, opts.Flags); err != nil {
			return FileConfig{}, err
		}
	}

	cfg := DefaultsFile()
	if err := v.Unmarshal(&cfg); err != nil {
		return FileConfig{}, fmt.Errorf("unmarshal config: %w", err)
	}

	// Merge MCRELAY_HOSTS / hosts_csv and --allow (later entries override same id).
	byID := map[string]HostEntry{}
	for _, h := range cfg.Hosts {
		byID[h.ID] = h
	}
	if csv := strings.TrimSpace(v.GetString("hosts_csv")); csv != "" {
		for _, part := range splitHostsCSV(csv) {
			ent, err := hostEntryFromAllow(part)
			if err != nil {
				return FileConfig{}, fmt.Errorf("hosts_csv / MCRELAY_HOSTS: %w", err)
			}
			byID[ent.ID] = ent
		}
	}
	for _, a := range opts.AllowExtra {
		ent, err := hostEntryFromAllow(a)
		if err != nil {
			return FileConfig{}, err
		}
		byID[ent.ID] = ent
	}
	cfg.Hosts = cfg.Hosts[:0]
	for _, h := range byID {
		cfg.Hosts = append(cfg.Hosts, h)
	}

	if cfg.DataDir == "" {
		data, err := xdg.DataHomeFor(xdg.AppMcrelay)
		if err != nil {
			return FileConfig{}, err
		}
		cfg.DataDir = data
	}

	if err := cfg.Validate(); err != nil {
		return FileConfig{}, err
	}
	cfg.TLS = cfg.TLS.Normalized()
	return cfg, nil
}

func setFileDefaults(v *viper.Viper) {
	d := DefaultsFile()
	v.SetDefault("listen.host", d.Listen.Host)
	v.SetDefault("listen.port", d.Listen.Port)
	v.SetDefault("log.level", d.Log.Level)
	v.SetDefault("log.format", d.Log.Format)
	v.SetDefault("data_dir", d.DataDir)
	v.SetDefault("tls.mode", d.TLS.Mode)
	v.SetDefault("tls.cert_file", d.TLS.CertFile)
	v.SetDefault("tls.key_file", d.TLS.KeyFile)
	v.SetDefault("tls.letsencrypt.email", d.TLS.LetsEncrypt.Email)
	v.SetDefault("tls.letsencrypt.directory_url", d.TLS.LetsEncrypt.DirectoryURL)
	v.SetDefault("tls.letsencrypt.staging", d.TLS.LetsEncrypt.Staging)
	v.SetDefault("tls.letsencrypt.cache_dir", d.TLS.LetsEncrypt.CacheDir)
	v.SetDefault("tls.letsencrypt.http_port", d.TLS.LetsEncrypt.HTTPPort)
	v.SetDefault("limits.max_hosts", d.Limits.MaxHosts)
	v.SetDefault("limits.max_phones_per_host", d.Limits.MaxPhonesPerHost)
	v.SetDefault("limits.max_message_bytes", d.Limits.MaxMessageBytes)
	v.SetDefault("limits.max_concurrent_join", d.Limits.MaxConcurrentJoin)
	v.SetDefault("limits.accept_per_minute", d.Limits.AcceptPerMinute)
	v.SetDefault("limits.tunnel_wait_seconds", d.Limits.TunnelWaitSeconds)
	v.SetDefault("limits.register_idle_seconds", d.Limits.RegisterIdleSeconds)
	v.SetDefault("limits.splice_idle_seconds", d.Limits.SpliceIdleSeconds)
	v.SetDefault("limits.splice_max_seconds", d.Limits.SpliceMaxSeconds)
	v.SetDefault("hosts_csv", "")
}

func bindRelayFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	pairs := []struct{ flag, key string }{
		{"config", ""}, // handled by LoadOptions
		{"listen-host", "listen.host"},
		{"listen-port", "listen.port"},
		{"log-level", "log.level"},
		{"log-format", "log.format"},
		{"data-dir", "data_dir"},
		{"tls-mode", "tls.mode"},
		{"tls-cert", "tls.cert_file"},
		{"tls-key", "tls.key_file"},
		{"tls-domain", "tls.letsencrypt.domains"},
		{"tls-email", "tls.letsencrypt.email"},
		{"tls-acme-directory", "tls.letsencrypt.directory_url"},
		{"tls-acme-staging", "tls.letsencrypt.staging"},
		{"tls-acme-http-port", "tls.letsencrypt.http_port"},
	}
	for _, p := range pairs {
		if p.key == "" {
			continue
		}
		f := fs.Lookup(p.flag)
		if f == nil || !f.Changed {
			continue
		}
		if err := v.BindPFlag(p.key, f); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks FileConfig.
func (c FileConfig) Validate() error {
	if c.Listen.Port < 1 || c.Listen.Port > 65535 {
		return fmt.Errorf("listen.port must be 1–65535")
	}
	if strings.TrimSpace(c.Listen.Host) == "" {
		return fmt.Errorf("listen.host is required")
	}
	switch strings.ToLower(c.Log.Level) {
	case "debug", "info", "warn", "error", "":
	default:
		return fmt.Errorf("log.level must be debug|info|warn|error")
	}
	switch strings.ToLower(c.Log.Format) {
	case "text", "json", "":
	default:
		return fmt.Errorf("log.format must be text|json")
	}
	mode := strings.ToLower(strings.TrimSpace(c.TLS.Mode))
	switch mode {
	case "", TLSModeFiles, TLSModeLetsEncrypt, TLSModeOff:
	default:
		return fmt.Errorf("tls.mode must be files|letsencrypt|off (or empty for auto)")
	}
	if (c.TLS.CertFile == "") != (c.TLS.KeyFile == "") {
		return fmt.Errorf("tls.cert_file and tls.key_file must both be set or both empty")
	}
	// After Normalized, empty mode is resolved; validate LE requirements when
	// mode is letsencrypt or will auto-select to letsencrypt.
	le := c.TLS.LetsEncrypt
	willLE := mode == TLSModeLetsEncrypt ||
		(mode == "" && len(nonEmptyDomains(le.Domains)) > 0 && strings.TrimSpace(le.Email) != "")
	if willLE {
		if len(nonEmptyDomains(le.Domains)) == 0 {
			return fmt.Errorf("tls.letsencrypt.domains is required for letsencrypt mode")
		}
		if strings.TrimSpace(le.Email) == "" {
			return fmt.Errorf("tls.letsencrypt.email is required for letsencrypt mode")
		}
		if le.HTTPPort < 0 || le.HTTPPort > 65535 {
			return fmt.Errorf("tls.letsencrypt.http_port must be 0–65535")
		}
	}
	if mode == TLSModeFiles || (mode == "" && !willLE && c.TLS.CertFile != "") {
		if c.TLS.CertFile == "" || c.TLS.KeyFile == "" {
			return fmt.Errorf("tls.cert_file and tls.key_file are required for files mode")
		}
	}
	if len(c.Hosts) == 0 {
		return fmt.Errorf("at least one host must be configured (hosts: in YAML, MCRELAY_HOSTS, or --allow)")
	}
	seen := map[string]struct{}{}
	for i, h := range c.Hosts {
		if err := validateHostID(h.ID); err != nil {
			return fmt.Errorf("hosts[%d].id: %w", i, err)
		}
		if len(h.Secret) < 16 {
			return fmt.Errorf("hosts[%d] %q: secret too short (min 16)", i, h.ID)
		}
		if _, ok := seen[h.ID]; ok {
			return fmt.Errorf("duplicate host id %q", h.ID)
		}
		seen[h.ID] = struct{}{}
	}
	return nil
}

// Normalized resolves empty tls.mode.
func (t TLSConfig) Normalized() TLSConfig {
	mode := strings.ToLower(strings.TrimSpace(t.Mode))
	if mode == "" {
		if len(nonEmptyDomains(t.LetsEncrypt.Domains)) > 0 && strings.TrimSpace(t.LetsEncrypt.Email) != "" {
			mode = TLSModeLetsEncrypt
		} else if t.CertFile != "" && t.KeyFile != "" {
			mode = TLSModeFiles
		} else {
			mode = TLSModeOff
		}
	}
	t.Mode = mode
	return t
}

func nonEmptyDomains(in []string) []string {
	out := make([]string, 0, len(in))
	for _, d := range in {
		if d = strings.TrimSpace(d); d != "" {
			out = append(out, d)
		}
	}
	return out
}

// Addr returns host:port for net.Listen.
func (c FileConfig) Addr() string {
	host := c.Listen.Host
	if host == "" {
		host = "0.0.0.0"
	}
	return net.JoinHostPort(host, strconv.Itoa(c.Listen.Port))
}

// ToServerConfig converts file config into the runtime server config.
func (c FileConfig) ToServerConfig() Config {
	creds := make([]HostCredential, 0, len(c.Hosts))
	for _, h := range c.Hosts {
		creds = append(creds, HostCredential{
			HostID:     h.ID,
			SecretHash: HashSecret(h.Secret),
		})
	}
	lim := DefaultLimits()
	if c.Limits.MaxHosts > 0 {
		lim.MaxHosts = c.Limits.MaxHosts
	}
	if c.Limits.MaxPhonesPerHost > 0 {
		lim.MaxPhonesPerHost = c.Limits.MaxPhonesPerHost
	}
	if c.Limits.MaxMessageBytes > 0 {
		lim.MaxMessageBytes = c.Limits.MaxMessageBytes
	}
	if c.Limits.MaxConcurrentJoin > 0 {
		lim.MaxConcurrentJoin = c.Limits.MaxConcurrentJoin
	}
	if c.Limits.AcceptPerMinute > 0 {
		lim.AcceptPerMinute = c.Limits.AcceptPerMinute
	}
	if c.Limits.TunnelWaitSeconds > 0 {
		lim.TunnelWait = time.Duration(c.Limits.TunnelWaitSeconds) * time.Second
	}
	if c.Limits.RegisterIdleSeconds > 0 {
		lim.RegisterIdle = time.Duration(c.Limits.RegisterIdleSeconds) * time.Second
	}
	// Splice idle/max: negative disables; positive overrides; zero keeps DefaultLimits
	// until ResolvedLimits fills them.
	if c.Limits.SpliceIdleSeconds != 0 {
		lim.SpliceIdle = time.Duration(c.Limits.SpliceIdleSeconds) * time.Second
	}
	if c.Limits.SpliceMaxSeconds != 0 {
		lim.SpliceMax = time.Duration(c.Limits.SpliceMaxSeconds) * time.Second
	}
	tls := c.TLS.Normalized()
	return Config{
		ListenAddr:  c.Addr(),
		TLSCertFile: tls.CertFile,
		TLSKeyFile:  tls.KeyFile,
		Allow:       creds,
		Limits:      lim,
	}
}

func hostEntryFromAllow(s string) (HostEntry, error) {
	c, err := ParseAllowFlag(s)
	if err != nil {
		return HostEntry{}, err
	}
	i := strings.IndexByte(s, ':')
	return HostEntry{ID: c.HostID, Secret: s[i+1:]}, nil
}

func splitHostsCSV(csv string) []string {
	// Support comma-separated host_id:secret entries. Secrets may contain
	// colons after the first; split only on commas.
	var out []string
	for _, part := range strings.Split(csv, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func isNotExist(err error) bool {
	return err != nil && (os.IsNotExist(err) || strings.Contains(err.Error(), "no such file"))
}

// ConfigPathHint returns the default config path for docs/errors.
func ConfigPathHint() string {
	p, err := xdg.DefaultConfigFileFor(xdg.AppMcrelay)
	if err != nil {
		return "~/.config/mcrelay/config.yaml"
	}
	return p
}

// DataDirHint returns the default data dir for docs.
func DataDirHint() string {
	p, err := xdg.DataHomeFor(xdg.AppMcrelay)
	if err != nil {
		return "~/.local/share/mcrelay"
	}
	return p
}

// EnsureDataDir creates the data directory with 0700.
func EnsureDataDir(dir string) error {
	return xdg.EnsureDir(dir)
}

// AbsPath returns an absolute path or empty if in is empty.
func AbsPath(in string) (string, error) {
	if in == "" {
		return "", nil
	}
	if filepath.IsAbs(in) {
		return in, nil
	}
	return filepath.Abs(in)
}
