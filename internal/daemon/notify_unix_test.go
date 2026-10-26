//go:build unix

package daemon

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

	// READY=1 means the TCP listener is open. The admin socket is served by
	// its own goroutine, which may still be binding: wait for it to answer,
	// as the update's probe does, before asking it to shut the daemon down.
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := admin.Call(cfg.Paths.AdminSocket, admin.Request{Op: admin.OpPing}); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the daemon's admin socket never answered after READY=1")
		}
		time.Sleep(20 * time.Millisecond)
	}
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

// TestRunTakesItsNotifySocket is MADR 0210 D10: the daemon takes
// NOTIFY_SOCKET out of its environment before it starts anything, so no
// process it spawns, an agent's update and its handoff included, inherits the
// unit's socket; and it still sends READY=1 and STOPPING=1 to the socket it
// kept.
func TestRunTakesItsNotifySocket(t *testing.T) {
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
	if v, ok := os.LookupEnv("NOTIFY_SOCKET"); ok {
		t.Errorf("NOTIFY_SOCKET = %q after start-up, want it unset", v)
	}
	// A process started now, as an agent's terminal or update would be,
	// inherits the daemon's environment.
	out, err := exec.Command("env").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "NOTIFY_SOCKET=") {
		t.Errorf("a child started after start-up inherited NOTIFY_SOCKET:\n%s", out)
	}
	cancel()
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
