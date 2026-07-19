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

	configFile := opts.ConfigFile
	if configFile == "" {
		if env := os.Getenv("MCREMOTE_CONFIG"); env != "" {
			configFile = env
		}
	}

	if configFile != "" {
		v.SetConfigFile(configFile)
		if err := v.ReadInConfig(); err != nil {
			// Missing file is OK only when using the implicit default path.
			if opts.ConfigFile != "" || !os.IsNotExist(err) {
				// viper wraps not-exist; also allow ConfigFileNotFoundError
				if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
					if !isNotExist(err) {
						return Config{}, fmt.Errorf("read config %s: %w", configFile, err)
					}
				}
			}
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
	v.SetDefault("providers.fake.enabled", d.Providers.Fake.Enabled)
	v.SetDefault("providers.grok.enabled", d.Providers.Grok.Enabled)
	v.SetDefault("providers.grok.bin", d.Providers.Grok.Bin)
	v.SetDefault("providers.grok.always_approve", d.Providers.Grok.AlwaysApprove)
	v.SetDefault("providers.grok.default_cwd", d.Providers.Grok.DefaultCWD)
	v.SetDefault("providers.grok.model", d.Providers.Grok.Model)
	v.SetDefault("headscale.control_url", d.Headscale.ControlURL)
}

func bindFlags(v *viper.Viper, fs *pflag.FlagSet) error {
	mappings := map[string]string{
		"config":      "", // handled separately
		"log-level":   "log.level",
		"log-format":  "log.format",
		"listen-host": "listen.host",
		"listen-port": "listen.port",
		"data-dir":    "data_dir",
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
