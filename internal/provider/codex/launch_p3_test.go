package codex

import (
	"runtime"
	"slices"
	"testing"
)

func TestLaunchArgumentsByTransport(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		endpoint string
		want     []string
	}{
		{"stdio", Config{Transport: TransportStdio}, "", []string{"app-server", "--listen", "stdio://"}},
		{"unix", Config{Transport: TransportUnixWS}, "/run/user/1/mcremote/codex.sock", []string{"app-server", "--listen", "unix:///run/user/1/mcremote/codex.sock"}},
		{"tcp", Config{Transport: TransportWS}, "127.0.0.1:43123", []string{"app-server", "--listen", "ws://127.0.0.1:43123"}},
		{"shutdown", Config{Transport: TransportStdio}, "off", []string{"app-server", "--listen", "off"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := launchArguments(tc.cfg, tc.endpoint, "")
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("args = %v, want %v", got, tc.want)
			}
		})
	}
	if _, err := launchArguments(Config{Transport: TransportWS}, "0.0.0.0:1", ""); err == nil {
		t.Fatal("wildcard listener accepted")
	}
	if runtime.GOOS != "windows" {
		if _, err := launchArguments(Config{Transport: TransportManagedDaemonProxy}, "", ""); err != nil {
			t.Fatalf("managed daemon proxy on Unix: %v", err)
		}
	}
}

func TestConfigTransportValidation(t *testing.T) {
	defaults, err := (Config{}).validated()
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Transport != TransportStdio || defaults.ReconnectAttempts != 3 {
		t.Fatalf("defaults = %+v", defaults)
	}
	for _, cfg := range []Config{
		{Transport: "invalid"},
		{Transport: TransportStdio, WSAuthMode: WSAuthCapabilityToken},
		{Transport: TransportWS, ListenAddress: "0.0.0.0:1234"},
		{Transport: TransportWS, ReconnectAttempts: 4},
		{Transport: TransportUnixWS, ListenAddress: "/tmp/public.sock"},
	} {
		if _, err := cfg.validated(); err == nil {
			t.Errorf("invalid config accepted: %+v", cfg)
		}
	}
}
