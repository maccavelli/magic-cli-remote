// Package config defines typed daemon configuration and defaults.
package config

import (
	"fmt"
	"net"
	"strings"
)

// Config is the full daemon configuration after load and validation.
type Config struct {
	Listen    ListenConfig    `mapstructure:"listen"`
	Log       LogConfig       `mapstructure:"log"`
	DataDir   string          `mapstructure:"data_dir"`
	Auth      AuthConfig      `mapstructure:"auth"`
	Providers ProvidersConfig `mapstructure:"providers"`
	Headscale HeadscaleConfig `mapstructure:"headscale"`
}

// ListenConfig is the HTTP/WebSocket bind address.
type ListenConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
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

// GrokProviderConfig configures the Grok Build adapter (stub in Phase 1).
type GrokProviderConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Bin     string `mapstructure:"bin"`
}

// HeadscaleConfig is documentation/metadata only in Phase 1.
type HeadscaleConfig struct {
	ControlURL string `mapstructure:"control_url"`
}

// Defaults returns a Config with Phase 1 defaults.
func Defaults() Config {
	return Config{
		Listen: ListenConfig{
			Host: "127.0.0.1",
			Port: 7531,
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
				Enabled: false,
				Bin:     "grok",
			},
		},
		Headscale: HeadscaleConfig{
			ControlURL: "http://localhost:8080",
		},
	}
}

// Addr returns host:port for net.Listen.
func (c Config) Addr() string {
	return net.JoinHostPort(c.Listen.Host, fmt.Sprintf("%d", c.Listen.Port))
}

// Validate checks configuration for obvious errors.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Listen.Host) == "" {
		return fmt.Errorf("listen.host must not be empty")
	}
	if c.Listen.Port < 1 || c.Listen.Port > 65535 {
		return fmt.Errorf("listen.port must be between 1 and 65535, got %d", c.Listen.Port)
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
