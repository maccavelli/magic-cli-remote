//go:build unix

package daemon

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/admin"
	"github.com/maccavelli/magic-cli-remote/internal/config"
)

// TestRunNotifiesSystemd: under Type=notify, systemctl start returns at
// READY=1, so the daemon sends it only once its listener accepts
// connections; and it sends STOPPING=1 when shutdown begins (MADR 0054 D5,
// amendment S8, S2).
func TestRunNotifiesSystemd(t *testing.T) {
	isolatedRoots(t)
	sockDir, err := os.MkdirTemp("", "ntf")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	notifyPath := filepath.Join(sockDir, "notify")
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notifyPath, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	t.Setenv("NOTIFY_SOCKET", notifyPath)

	// A free port, so the test can dial the listener when READY=1 arrives.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()

	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Listen.Host = "127.0.0.1"
	cfg.Listen.Port = port
	cfg.TLS.Mode = config.TLSModeOff
	cfg.TLS = cfg.TLS.Normalized()
	cfg.Auth.RequireClientKey = false
	cfg.Providers.Grok.Enabled = false
	cfg.Providers.Opencode.Enabled = false
	cfg.Providers.Codex.Enabled = false
	cfg.Providers.Kilo.Enabled = false
	cfg.Providers.Fake.Enabled = true
	if err := cfg.RecomputePaths(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{Config: cfg, Version: "test", Log: quietLogger(), Shutdown: cancel})
	}()

	next := func(want string) {
		t.Helper()
		buf := make([]byte, 256)
		if err := conn.SetReadDeadline(time.Now().Add(20 * time.Second)); err != nil {
			t.Fatal(err)
		}
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("waiting for %s: %v", want, err)
		}
		if got := string(buf[:n]); got != want {
			t.Fatalf("notify = %q, want %q", got, want)
		}
	}

	next("READY=1")
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		t.Fatalf("READY=1 arrived before the listener accepted connections: %v", err)
	}
	_ = c.Close()

	if err := admin.RequestShutdown(cfg.Paths.AdminSocket); err != nil {
		t.Fatalf("RequestShutdown: %v", err)
	}
	next("STOPPING=1")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return")
	}
}
