package config_test

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/config"
)

// TestEnvOverridesTheLimitsAndSandboxKeys is MADR 0201 D23 (0073 §6.4).
// These keys had no viper default, so AutomaticEnv never resolved their
// MCREMOTE_* variables and the overrides were silently ignored.
func TestEnvOverridesTheLimitsAndSandboxKeys(t *testing.T) {
	isolateConfig(t)
	t.Setenv("MCREMOTE_LIMITS_WS_READ_DEADLINE_SECONDS", "300")
	t.Setenv("MCREMOTE_LIMITS_WS_RESUME_WINDOW_SECONDS", "600")
	t.Setenv("MCREMOTE_LIMITS_TCP_KEEPALIVE_DISABLE", "true")
	t.Setenv("MCREMOTE_LIMITS_TCP_KEEPALIVE_IDLE_SECONDS", "31")
	t.Setenv("MCREMOTE_LIMITS_TCP_KEEPALIVE_INTERVAL_SECONDS", "7")
	t.Setenv("MCREMOTE_LIMITS_TCP_KEEPALIVE_COUNT", "6")
	t.Setenv("MCREMOTE_PROVIDERS_CODEX_SANDBOX_BROKEN_POLICY", "refuse")
	cfg, err := config.Load(config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	l := cfg.Limits
	for _, c := range []struct {
		name      string
		got, want any
	}{
		{"limits.ws_read_deadline_seconds", l.WSReadDeadlineSeconds, 300},
		{"limits.ws_resume_window_seconds", l.WSResumeWindowSeconds, 600},
		{"limits.tcp_keepalive.disable", l.TCPKeepalive.Disable, true},
		{"limits.tcp_keepalive.idle_seconds", l.TCPKeepalive.IdleSeconds, 31},
		{"limits.tcp_keepalive.interval_seconds", l.TCPKeepalive.IntervalSeconds, 7},
		{"limits.tcp_keepalive.count", l.TCPKeepalive.Count, 6},
		{"providers.codex.sandbox_broken_policy", cfg.Providers.Codex.SandboxBrokenPolicy, "refuse"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v from the environment", c.name, c.got, c.want)
		}
	}
}
