//go:build unix

package relay

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/sdnotify"
)

// notifyListener is a stand-in for systemd's notify socket. The directory is
// short, under $TMPDIR, because a unix socket path is limited to about 100
// bytes and t.TempDir's can be longer.
func notifyListener(t *testing.T) (string, func(t *testing.T, want string)) {
	t.Helper()
	dir, err := os.MkdirTemp("", "ntf")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "notify")
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	next := func(t *testing.T, want string) {
		t.Helper()
		buf := make([]byte, 64)
		_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("waiting for %s: %v", want, err)
		}
		if got := string(buf[:n]); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
	return path, next
}

// TestServeNotifiesSystemd is MADR 0207 D22: under the unit's Type=notify,
// systemctl start returns at READY=1, so serve sends it only once its listener
// accepts connections; serve takes NOTIFY_SOCKET out of its environment first,
// so nothing it starts inherits it; and it sends STOPPING=1 when shutdown
// begins.
func TestServeNotifiesSystemd(t *testing.T) {
	path, next := notifyListener(t)
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	_ = free.Close()
	cfg := filepath.Join(hermeticConfig(t), "mcrelay", "config.yaml")
	t.Setenv("NOTIFY_SOCKET", path)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := newRootCmd()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"serve", "--config", cfg, "--data-dir", t.TempDir(),
		"--listen-host", "127.0.0.1", "--listen-port", strconv.Itoa(port), "--tls-mode", "off"})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

	next(t, "READY=1")
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		t.Fatalf("READY=1 arrived before the listener accepted connections: %v", err)
	}
	_ = c.Close()
	if v, ok := os.LookupEnv("NOTIFY_SOCKET"); ok {
		t.Errorf("NOTIFY_SOCKET = %q after start-up, want it unset", v)
	}

	cancel()
	next(t, "STOPPING=1")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve = %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not return")
	}
}

// TestServeNotifiesStoppingWhenServingFails: a listener that fails under the
// relay ends it too, and systemd hears STOPPING=1 then, not only on a signal.
func TestServeNotifiesStoppingWhenServingFails(t *testing.T) {
	path, next := notifyListener(t)
	cred, _ := ParseAllowFlag("h1:0123456789abcdef")
	srv := New(Config{
		ListenAddr: "127.0.0.1:0",
		Allow:      []HostCredential{cred},
		Limits:     DefaultLimits(),
		Notify:     sdnotify.Socket(path),
	}, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(context.Background(), ln) }()

	next(t, "READY=1")
	_ = ln.Close()
	next(t, "STOPPING=1")
	select {
	case err := <-done:
		if err == nil || errors.Is(err, context.Canceled) {
			t.Fatalf("Serve = %v, want the listener's error", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Serve did not return")
	}
}
