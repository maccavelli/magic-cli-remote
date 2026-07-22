package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/xdg"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// LoadOptions controls configuration loading.
type LoadOptions struct {
	// ConfigFile is an explicit path; empty uses XDG default (may be missing).
	ConfigFile string
	// Flags are optional cobra-bound flags to bind into viper.
	Flags *pflag.FlagSet
}

// Load reads configuration from defaults, optional YAML file, env, and flags.
// Precedence: flags > env > file > defaults.
func Load(opts LoadOptions) (Config, error) {
	v := viper.New()
	setDefaults(v)

	v.SetEnvPrefix("MCREMOTE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	// Explicit env aliases for nested keys commonly set via env.
	_ = v.BindEnv("listen.host", "MCREMOTE_LISTEN_HOST")
	_ = v.BindEnv("listen.port", "MCREMOTE_LISTEN_PORT")
	_ = v.BindEnv("log.level", "MCREMOTE_LOG_LEVEL")
	_ = v.BindEnv("log.format", "MCREMOTE_LOG_FORMAT")
	_ = v.BindEnv("data_dir", "MCREMOTE_DATA_DIR")
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

	configFile := opts.ConfigFile
	if configFile == "" {
		if env := os.Getenv("MCREMOTE_CONFIG"); env != "" {
			configFile = env
		}
	}

	if configFile != "" {
		// An explicitly named config (flag or MCREMOTE_CONFIG) that cannot be
		// read is always an error: silently starting on pure defaults when the
		// operator's file has a typo'd path is far worse than failing loudly.
		v.SetConfigFile(configFile)
		if err := v.ReadInConfig(); err != nil {
			return Config{}, fmt.Errorf("read config %s: %w", configFile, err)
		}
	} else {
		dir, err := xdg.ConfigHome()
		if err != nil {
			return Config{}, err
		}
		v.AddConfigPath(dir)
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		if err := v.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok && !isNotExist(err) {
				return Config{}, fmt.Errorf("read config: %w", err)
			}
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

	if cfg.DataDir == "" {
		data, err := xdg.DataHome()
		if err != nil {
			return Config{}, err
		}
		cfg.DataDir = data
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	// Resolve tls.mode once, after validation, so every consumer switches on a
	// concrete mode instead of re-deriving it from mode+enabled.
	cfg.TLS = cfg.TLS.Normalized()
	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	d := Defaults()
	v.SetDefault("listen.host", d.Listen.Host)
	v.SetDefault("listen.port", d.Listen.Port)
	v.SetDefault("log.level", d.Log.Level)
	v.SetDefault("log.format", d.Log.Format)
	v.SetDefault("data_dir", d.DataDir)
	v.SetDefault("auth.require_device_token", d.Auth.RequireDeviceToken)
	v.SetDefault("auth.require_client_key", d.Auth.RequireClientKey)
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
	v.SetDefault("providers.fake.enabled", d.Providers.Fake.Enabled)
	v.SetDefault("providers.grok.enabled", d.Providers.Grok.Enabled)
	v.SetDefault("providers.grok.bin", d.Providers.Grok.Bin)
	v.SetDefault("providers.grok.always_approve", d.Providers.Grok.AlwaysApprove)
	v.SetDefault("providers.grok.default_cwd", d.Providers.Grok.DefaultCWD)
	v.SetDefault("providers.grok.model", d.Providers.Grok.Model)
	// Without a default the key is absent from viper's key set and the
	// MCREMOTE_PROVIDERS_*_PERMISSION_TIMEOUT_SECONDS env vars are silently
	// ignored (AutomaticEnv only resolves known keys).
	v.SetDefault("providers.grok.permission_timeout_seconds", d.Providers.Grok.PermissionTimeoutSeconds)
	v.SetDefault("providers.grok.prewarm", d.Providers.Grok.Prewarm)
	v.SetDefault("providers.grok.turn_stall_notice_seconds", d.Providers.Grok.TurnStallNoticeSeconds)
	v.SetDefault("providers.opencode.enabled", d.Providers.Opencode.Enabled)
	v.SetDefault("providers.opencode.bin", d.Providers.Opencode.Bin)
	v.SetDefault("providers.opencode.always_approve", d.Providers.Opencode.AlwaysApprove)
	v.SetDefault("providers.opencode.default_cwd", d.Providers.Opencode.DefaultCWD)
	v.SetDefault("providers.opencode.model", d.Providers.Opencode.Model)
	v.SetDefault("providers.opencode.permission_timeout_seconds", d.Providers.Opencode.PermissionTimeoutSeconds)
	v.SetDefault("providers.opencode.prewarm", d.Providers.Opencode.Prewarm)
	v.SetDefault("providers.opencode.turn_stall_notice_seconds", d.Providers.Opencode.TurnStallNoticeSeconds)
	v.SetDefault("headscale.control_url", d.Headscale.ControlURL)
	v.SetDefault("limits.max_ws_clients", d.Limits.MaxWSClients)
	v.SetDefault("limits.max_live_sessions", d.Limits.MaxLiveSessions)
}

func bindFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	mappings := map[string]string{
		"config":      "", // handled separately
		"log-level":   "log.level",
		"log-format":  "log.format",
		"listen-host": "listen.host",
		"listen-port": "listen.port",
		"data-dir":    "data_dir",
		"tls":         "tls.enabled",

		"tls-mode":            "tls.mode",
		"tls-domain":          "tls.letsencrypt.domains",
		"tls-email":           "tls.letsencrypt.email",
		"tls-acme-directory":  "tls.letsencrypt.directory_url",
		"tls-acme-staging":    "tls.letsencrypt.staging",
		"tls-route53-zone-id": "tls.letsencrypt.route53.hosted_zone_id",
		"tls-route53-region":  "tls.letsencrypt.route53.region",
		"tls-route53-profile": "tls.letsencrypt.route53.profile",
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
