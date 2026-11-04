package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// LoadOptions controls configuration loading.
type LoadOptions struct {
	// ConfigFile is an explicit path; empty uses XDG default (may be missing).
	ConfigFile string
	// Flags are optional cobra-bound flags to bind into viper.
	Flags *pflag.FlagSet
	// Providers are the sections of the registered providers, which Load
	// decodes into Config.Providers.Sections (0208 MADR D6).
	Providers []ProviderSectionSpec
}

// systemRoots resolves the platform roots Load searches for config.yaml. It
// is a variable only so tests can point it at a temp dir (MADR 0162 D1).
var systemRoots = appdirs.SystemRoots

// Load reads configuration from defaults, optional YAML file, env, and flags.
// Precedence: flags > env > file > defaults.
func Load(opts LoadOptions) (Config, error) {
	roots, diags, err := systemRoots(appdirs.ProductMcremote)
	if err != nil {
		return Config{}, err
	}
	basePaths, err := appdirs.Resolve(appdirs.ProductMcremote, roots, "")
	if err != nil {
		return Config{}, err
	}

	v := viper.New()
	setDefaults(v)
	setSectionDefaults(v, opts.Providers)

	v.SetEnvPrefix("MCREMOTE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	// Explicit env aliases for nested keys commonly set via env.
	_ = v.BindEnv("listen.host", "MCREMOTE_LISTEN_HOST")
	_ = v.BindEnv("listen.port", "MCREMOTE_LISTEN_PORT")
	_ = v.BindEnv("log.level", "MCREMOTE_LOG_LEVEL")
	_ = v.BindEnv("log.format", "MCREMOTE_LOG_FORMAT")
	_ = v.BindEnv("data_dir", "MCREMOTE_DATA_DIR")
	_ = v.BindEnv("display_name", "MCREMOTE_DISPLAY_NAME")
	_ = v.BindEnv("auth.require_device_token", "MCREMOTE_AUTH_REQUIRE_DEVICE_TOKEN")
	_ = v.BindEnv("auth.require_client_key", "MCREMOTE_AUTH_REQUIRE_CLIENT_KEY")
	_ = v.BindEnv("tls.enabled", "MCREMOTE_TLS_ENABLED")
	_ = v.BindEnv("tls.cert_file", "MCREMOTE_TLS_CERT_FILE")
	_ = v.BindEnv("tls.key_file", "MCREMOTE_TLS_KEY_FILE")
	_ = v.BindEnv("tls.mode", "MCREMOTE_TLS_MODE")
	_ = v.BindEnv("tls.letsencrypt.domains", "MCREMOTE_TLS_DOMAINS")
	_ = v.BindEnv("tls.letsencrypt.email", "MCREMOTE_TLS_EMAIL")
	_ = v.BindEnv("tls.letsencrypt.directory_url", "MCREMOTE_TLS_ACME_DIRECTORY_URL")
	_ = v.BindEnv("tls.letsencrypt.staging", "MCREMOTE_TLS_ACME_STAGING")
	_ = v.BindEnv("tls.letsencrypt.cache_dir", "MCREMOTE_TLS_ACME_CACHE_DIR")
	_ = v.BindEnv("tls.letsencrypt.route53.hosted_zone_id", "MCREMOTE_TLS_ROUTE53_HOSTED_ZONE_ID")
	_ = v.BindEnv("tls.letsencrypt.route53.region", "MCREMOTE_TLS_ROUTE53_REGION")
	_ = v.BindEnv("tls.letsencrypt.route53.profile", "MCREMOTE_TLS_ROUTE53_PROFILE")
	_ = v.BindEnv("relay.url", "MCREMOTE_RELAY_URL")
	_ = v.BindEnv("relay.host_id", "MCREMOTE_RELAY_HOST_ID")
	_ = v.BindEnv("relay.secret", "MCREMOTE_RELAY_SECRET")
	_ = v.BindEnv("relay.insecure_skip_verify", "MCREMOTE_RELAY_INSECURE_SKIP_VERIFY")
	_ = v.BindEnv("relay.max_frame_bytes", "MCREMOTE_RELAY_MAX_FRAME_BYTES")
	// MCREMOTE_PAIR_HOST is the retained legacy name (was CLI-pair-only) and
	// maps to the same key as the canonical MCREMOTE_PAIR_ADVERTISE_HOST.
	_ = v.BindEnv("pair.advertise_host", "MCREMOTE_PAIR_ADVERTISE_HOST", "MCREMOTE_PAIR_HOST")

	var configFile string
	switch {
	case opts.ConfigFile != "":
		abs, err := absAgainstCWD(opts.ConfigFile)
		if err != nil {
			return Config{}, fmt.Errorf("config path: %w", err)
		}
		configFile = abs
	case os.Getenv("MCREMOTE_CONFIG") != "":
		env := os.Getenv("MCREMOTE_CONFIG")
		if !filepath.IsAbs(env) {
			return Config{}, fmt.Errorf("MCREMOTE_CONFIG must be an absolute path, got %q", env)
		}
		configFile = filepath.Clean(env)
	}

	var usedConfigFile string
	if configFile != "" {
		// An explicitly named config (flag or MCREMOTE_CONFIG) that cannot be
		// read is always an error: silently starting on pure defaults when the
		// operator's file has a typo'd path is far worse than failing loudly.
		v.SetConfigFile(configFile)
		if err := v.ReadInConfig(); err != nil {
			return Config{}, fmt.Errorf("read config %s: %w", configFile, err)
		}
		usedConfigFile = configFile
	} else {
		v.AddConfigPath(basePaths.ConfigDir)
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		if err := v.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok && !isNotExist(err) {
				return Config{}, fmt.Errorf("read config: %w", err)
			}
		} else {
			usedConfigFile = v.ConfigFileUsed()
		}
	}

	if opts.Flags != nil {
		if err := bindFlags(v, opts.Flags); err != nil {
			return Config{}, err
		}
	}

	cfg := Defaults()
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}
	if cfg.Providers.Sections, err = decodeSections(v, opts.Providers, os.Environ()); err != nil {
		return Config{}, err
	}
	cfg.DisplayName = strings.TrimSpace(cfg.DisplayName)
	cfg.Diagnostics = diags
	cfg.ConfigFile = usedConfigFile
	noteRetiredGoose(v, os.Environ(), &cfg)

	// The config may hold relay.secret, which is the same registration
	// credential mcrelay stores as hosts[].secret and guards at read time.
	// mcremote guarded neither until MADR 0155.
	//
	// This runs after Unmarshal on purpose, and the ordering looks backwards:
	// the natural instinct is to refuse to read a file before parsing it.
	// Reading it is not the harm — the file is already on disk and already
	// readable by whoever the permissions allow. The harm is continuing to
	// *serve* with a credential that has been exposed, and deciding that needs
	// the parsed config's provenance (0155 D2/D3).
	if usedConfigFile != "" {
		if err := guardConfigFile(&cfg, usedConfigFile, v.InConfig); err != nil {
			return Config{}, err
		}
	}

	if err := cfg.finalizePaths(basePaths, opts); err != nil {
		return Config{}, err
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	// Resolve tls.mode once, after validation, so every consumer switches on a
	// concrete mode instead of re-deriving it from mode+enabled.
	cfg.TLS = cfg.TLS.Normalized()
	return cfg, nil
}

// finalizePaths absolutizes filesystem fields and attaches appdirs.Paths.
func (cfg *Config) finalizePaths(basePaths appdirs.Paths, opts LoadOptions) error {
	configBase := ""
	if cfg.ConfigFile != "" {
		configBase = filepath.Dir(cfg.ConfigFile)
	}

	dataFromEnv := os.Getenv("MCREMOTE_DATA_DIR")
	if dataFromEnv != "" && !filepath.IsAbs(dataFromEnv) {
		// Only reject when the effective value came from env (flag may still win).
		if opts.Flags == nil || !opts.Flags.Changed("data-dir") {
			if cfg.DataDir == dataFromEnv || cfg.DataDir == "" {
				return fmt.Errorf("MCREMOTE_DATA_DIR must be an absolute path, got %q", dataFromEnv)
			}
		}
	}

	if cfg.DataDir == "" {
		cfg.DataDir = basePaths.DataDir
	} else if !filepath.IsAbs(cfg.DataDir) {
		// Flag: vs CWD. YAML: vs config file dir.
		fromFlag := opts.Flags != nil && opts.Flags.Changed("data-dir")
		if fromFlag {
			abs, err := absAgainstCWD(cfg.DataDir)
			if err != nil {
				return fmt.Errorf("data_dir: %w", err)
			}
			cfg.DataDir = abs
		} else if configBase != "" {
			cfg.DataDir = filepath.Clean(filepath.Join(configBase, cfg.DataDir))
		} else {
			abs, err := absAgainstCWD(cfg.DataDir)
			if err != nil {
				return fmt.Errorf("data_dir: %w", err)
			}
			cfg.DataDir = abs
		}
	} else {
		cfg.DataDir = filepath.Clean(cfg.DataDir)
	}

	paths, err := appdirs.WithDataDir(basePaths, cfg.DataDir)
	if err != nil {
		return err
	}
	cfg.Paths = paths

	rel := func(p string) (string, error) {
		p = strings.TrimSpace(p)
		if p == "" {
			return "", nil
		}
		if filepath.IsAbs(p) {
			return filepath.Clean(p), nil
		}
		if configBase != "" {
			return filepath.Clean(filepath.Join(configBase, p)), nil
		}
		return absAgainstCWD(p)
	}
	if cfg.TLS.CertFile, err = rel(cfg.TLS.CertFile); err != nil {
		return fmt.Errorf("tls.cert_file: %w", err)
	}
	if cfg.TLS.KeyFile, err = rel(cfg.TLS.KeyFile); err != nil {
		return fmt.Errorf("tls.key_file: %w", err)
	}
	if cfg.TLS.LetsEncrypt.CacheDir, err = rel(cfg.TLS.LetsEncrypt.CacheDir); err != nil {
		return fmt.Errorf("tls.letsencrypt.cache_dir: %w", err)
	}
	for id, sec := range cfg.Providers.Sections {
		agent, ok := sec.(AgentSection)
		if !ok {
			continue
		}
		if agent.Agent().DefaultCWD, err = rel(agent.Agent().DefaultCWD); err != nil {
			return fmt.Errorf("providers.%s.default_cwd: %w", id, err)
		}
	}
	if cfg.Providers.Kilo.DefaultCWD, err = rel(cfg.Providers.Kilo.DefaultCWD); err != nil {
		return fmt.Errorf("providers.kilo.default_cwd: %w", err)
	}
	return nil
}

// RecomputePaths updates Paths after an external DataDir change (e.g. tests).
func (cfg *Config) RecomputePaths() error {
	if cfg.Paths.RuntimeBase == "" {
		p, diags, err := appdirs.SystemPaths(appdirs.ProductMcremote, cfg.DataDir)
		if err != nil {
			return err
		}
		cfg.Paths = p
		cfg.Diagnostics = append(cfg.Diagnostics, diags...)
		return nil
	}
	p, err := appdirs.WithDataDir(cfg.Paths, cfg.DataDir)
	if err != nil {
		return err
	}
	cfg.Paths = p
	return nil
}

// AbsDataDir makes a --data-dir value absolute against the working directory,
// as Load does for the flag it binds. For a caller that reads the flag from a
// flag set Load did not see (MADR 0201 D24).
func AbsDataDir(raw string) (string, error) {
	return absAgainstCWD(raw)
}

func absAgainstCWD(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return filepath.Abs(p)
}

func setDefaults(v *viper.Viper) {
	d := Defaults()
	v.SetDefault("listen.host", d.Listen.Host)
	v.SetDefault("listen.port", d.Listen.Port)
	v.SetDefault("log.level", d.Log.Level)
	v.SetDefault("log.format", d.Log.Format)
	v.SetDefault("data_dir", d.DataDir)
	v.SetDefault("display_name", d.DisplayName)
	v.SetDefault("auth.require_device_token", d.Auth.RequireDeviceToken)
	v.SetDefault("auth.require_client_key", d.Auth.RequireClientKey)
	v.SetDefault("auth.allowed_origins", d.Auth.AllowedOrigins)
	v.SetDefault("tls.enabled", d.TLS.Enabled)
	v.SetDefault("tls.cert_file", d.TLS.CertFile)
	v.SetDefault("tls.key_file", d.TLS.KeyFile)
	v.SetDefault("tls.mode", d.TLS.Mode)
	v.SetDefault("tls.letsencrypt.email", d.TLS.LetsEncrypt.Email)
	v.SetDefault("tls.letsencrypt.directory_url", d.TLS.LetsEncrypt.DirectoryURL)
	v.SetDefault("tls.letsencrypt.staging", d.TLS.LetsEncrypt.Staging)
	v.SetDefault("tls.letsencrypt.cache_dir", d.TLS.LetsEncrypt.CacheDir)
	v.SetDefault("tls.letsencrypt.route53.hosted_zone_id", d.TLS.LetsEncrypt.Route53.HostedZoneID)
	v.SetDefault("tls.letsencrypt.route53.region", d.TLS.LetsEncrypt.Route53.Region)
	v.SetDefault("tls.letsencrypt.route53.profile", d.TLS.LetsEncrypt.Route53.Profile)
	// Without a default the key is absent from viper's key set and
	// MCREMOTE_TLS_LETSENCRYPT_ROUTE53_MAX_RETRIES is silently ignored
	// (AutomaticEnv only resolves known keys).
	v.SetDefault("tls.letsencrypt.route53.max_retries", d.TLS.LetsEncrypt.Route53.MaxRetries)
	v.SetDefault("providers.kilo.enabled", d.Providers.Kilo.Enabled)
	v.SetDefault("providers.kilo.bin", d.Providers.Kilo.Bin)
	v.SetDefault("providers.kilo.always_approve", d.Providers.Kilo.AlwaysApprove)
	v.SetDefault("providers.kilo.default_cwd", d.Providers.Kilo.DefaultCWD)
	v.SetDefault("providers.kilo.model", d.Providers.Kilo.Model)
	v.SetDefault("providers.kilo.permission_timeout_seconds", d.Providers.Kilo.PermissionTimeoutSeconds)
	v.SetDefault("providers.kilo.prewarm", d.Providers.Kilo.Prewarm)
	v.SetDefault("providers.kilo.turn_stall_notice_seconds", d.Providers.Kilo.TurnStallNoticeSeconds)
	v.SetDefault("providers.kilo.session_tree", d.Providers.Kilo.SessionTree)
	v.SetDefault("providers.kilo.stream_coalesce_ms", d.Providers.Kilo.StreamCoalesceMs)
	v.SetDefault("providers.kilo.pure", d.Providers.Kilo.Pure)
	v.SetDefault("headscale.control_url", d.Headscale.ControlURL)
	v.SetDefault("limits.max_ws_clients", d.Limits.MaxWSClients)
	v.SetDefault("limits.max_live_sessions", d.Limits.MaxLiveSessions)
	// Without these defaults the keys are absent from viper's key set and
	// their MCREMOTE_LIMITS_* variables are silently ignored (MADR 0201 D23).
	v.SetDefault("limits.ws_read_deadline_seconds", d.Limits.WSReadDeadlineSeconds)
	v.SetDefault("limits.ws_resume_window_seconds", d.Limits.WSResumeWindowSeconds)
	v.SetDefault("limits.tcp_keepalive.disable", d.Limits.TCPKeepalive.Disable)
	v.SetDefault("limits.tcp_keepalive.idle_seconds", d.Limits.TCPKeepalive.IdleSeconds)
	v.SetDefault("limits.tcp_keepalive.interval_seconds", d.Limits.TCPKeepalive.IntervalSeconds)
	v.SetDefault("limits.tcp_keepalive.count", d.Limits.TCPKeepalive.Count)
	v.SetDefault("relay.url", d.Relay.URL)
	v.SetDefault("relay.host_id", d.Relay.HostID)
	v.SetDefault("relay.secret", d.Relay.Secret)
	v.SetDefault("relay.insecure_skip_verify", d.Relay.InsecureSkipVerify)
	v.SetDefault("relay.max_frame_bytes", d.Relay.MaxFrameBytes)
	// Without a default the key is absent from viper's key set and the
	// MCREMOTE_PAIR_ADVERTISE_HOST / MCREMOTE_PAIR_HOST env vars are silently
	// ignored (AutomaticEnv only resolves known keys).
	v.SetDefault("pair.advertise_host", d.Pair.AdvertiseHost)
	v.SetDefault("receipts.enabled", d.Receipts.Enabled)
	v.SetDefault("receipts.allow_patterns", d.Receipts.AllowPatterns)
	v.SetDefault("receipts.deny_patterns", d.Receipts.DenyPatterns)
	v.SetDefault("receipts.handoffs", d.Receipts.Handoffs)
}

func bindFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	mappings := map[string]string{
		"config":      "", // handled separately
		"log-level":   "log.level",
		"log-format":  "log.format",
		"listen-host": "listen.host",
		"listen-port": "listen.port",
		"data-dir":    "data_dir",
		// NOTE: --tls is deliberately NOT bound to tls.enabled here. serve applies
		// it after Load via TLS.WithEnabled, which reconciles mode+enabled as one
		// step. Binding it into viper made Validate see enabled=false alongside a
		// configured tls.mode (e.g. selfsigned) and hard-error before the override
		// could run — so `--tls=false` was unusable whenever config set tls.mode.

		"tls-mode":            "tls.mode",
		"tls-domain":          "tls.letsencrypt.domains",
		"tls-email":           "tls.letsencrypt.email",
		"tls-acme-directory":  "tls.letsencrypt.directory_url",
		"tls-acme-staging":    "tls.letsencrypt.staging",
		"tls-route53-zone-id": "tls.letsencrypt.route53.hosted_zone_id",
		"tls-route53-region":  "tls.letsencrypt.route53.region",
		"tls-route53-profile": "tls.letsencrypt.route53.profile",
		"relay-url":           "relay.url",
		"relay-host-id":       "relay.host_id",
		"relay-secret":        "relay.secret",
	}
	for flagName, key := range mappings {
		if key == "" {
			continue
		}
		f := fs.Lookup(flagName)
		if f == nil {
			continue
		}
		if err := v.BindPFlag(key, f); err != nil {
			return fmt.Errorf("bind flag %s: %w", flagName, err)
		}
	}
	return nil
}

func isNotExist(err error) bool {
	if err == nil {
		return false
	}
	if os.IsNotExist(err) {
		return true
	}
	// viper may wrap path errors
	return strings.Contains(err.Error(), "no such file")
}

// configPermCode is the Diagnostic code for a config file other principals can
// read. Callers that render diagnostics (mcremote paths --json) key on it.
const configPermCode = "config_not_owner_only"

// guardConfigFile enforces MADR 0155 on the config file that was actually read.
//
// Fatal when the file itself carries a credential; a warning plus a Diagnostic
// otherwise (0155 D3). The asymmetry is deliberate: a world-readable config
// with no secret in it is untidy, and stopping a daemon over it would be a
// self-update that breaks working installations — the measured reason 0155
// rejected mirroring mcrelay's hard fail.
//
// On Unix a loose file is tightened to 0600 (D8). On Windows the equivalent
// repair is a private DACL on the directory, which PLAN 0155 P3 applies at
// startup.
//
// Exposure is judged on the file as it was found, not as repaired (MADR 0155
// amendment, 2026-09-12). A repair that succeeds does not cancel a refusal: once
// the file is private there is no later moment at which anyone could still tell
// the operator that the credential inside it was readable. Deciding after the
// repair is how the first implementation made the fatal branch unreachable on
// every POSIX host while Windows refused the same file.
//
// PLAN 0155 C1: nothing here logs a secret, a field name, or a value. It names
// the path. An operator who needs to know which setting is exposed can open the
// file; a log line that names it is a map for anyone who later gets read
// access.
func guardConfigFile(cfg *Config, path string, inFile func(string) bool) error {
	private, err := appdirs.FileIsOwnerOnly(path)
	if err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	if private {
		return nil
	}

	// Exposed as found. Repair regardless of what comes next: a tighter file is
	// the right state whether or not the daemon goes on to start.
	repaired := false
	if ok, rerr := repairOwnerOnly(path); rerr == nil && ok {
		slog.Default().Warn("tightened config file permissions",
			slog.String("path", path),
			slog.String("mode", "0600"))
		// Trust the predicate, not the chmod's return: a repair that did not
		// actually make the file private is treated as no repair.
		if repaired, err = appdirs.FileIsOwnerOnly(path); err != nil {
			return fmt.Errorf("config %s: %w", path, err)
		}
	}

	// Whether exposure stops the daemon depends on what is in the file (D3).
	if HasInlineSecret(inFile) {
		// MADR 0155 D9. Fixing the permissions does not un-read a file that was
		// already readable, and an operator told only to tighten it will
		// reasonably believe the problem is over. The advice belongs here and
		// not in the warning below, where there is no credential to rotate.
		if repaired {
			// The remedy has already been applied, so do not tell the operator
			// to chmod; tell them what is still theirs to do.
			return fmt.Errorf("config %s was readable by another principal and contains a credential; "+
				"its permissions have been tightened to 0600, but treat that credential as exposed: "+
				"rotate it, then start again", path)
		}
		return fmt.Errorf("config %s contains a credential and is %s%s; "+
			"treat that credential as exposed and rotate it",
			path, appdirs.NotOwnerOnlyDetail(path), ownerOnlyAlternative())
	}
	if repaired {
		// No credential, and the file is private now: nothing is left to report
		// beyond the repair already logged.
		return nil
	}
	msg := fmt.Sprintf("config %s is %s%s", path, appdirs.NotOwnerOnlyDetail(path), ownerOnlyAlternative())
	slog.Default().Warn("config file is not private", slog.String("path", path))
	cfg.Diagnostics = append(cfg.Diagnostics, appdirs.Diagnostic{Code: configPermCode, Message: msg})
	return nil
}
