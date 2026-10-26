//go:build unix

package sdnotify

import (
	"bytes"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestSocketSends: the kept socket takes a path or, on Linux, an abstract "@"
// name, as the library's systemd.Notify does; with none, it sends nothing and
// reports no error. Moved from the daemon with the sender (MADR 0207 PLAN U8
// Deviation 1).
func TestSocketSends(t *testing.T) {
	recv := func(t *testing.T, conn *net.UnixConn) string {
		t.Helper()
		buf := make([]byte, 64)
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		return string(buf[:n])
	}
	t.Run("path", func(t *testing.T) {
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
		t.Setenv("NOTIFY_SOCKET", path)
		s := Take()
		if _, ok := os.LookupEnv("NOTIFY_SOCKET"); ok {
			t.Error("Take left NOTIFY_SOCKET set")
		}
		if sent, err := s.Send("READY=1"); !sent || err != nil {
			t.Fatalf("Send(READY=1) = %v, %v", sent, err)
		}
		if got := recv(t, conn); got != "READY=1" {
			t.Errorf("got %q, want READY=1", got)
		}
	})
	t.Run("abstract", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("the abstract namespace is Linux's")
		}
		name := "@mcremote-notify-test-" + strconv.Itoa(os.Getpid())
		conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: "\x00" + name[1:], Net: "unixgram"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		t.Setenv("NOTIFY_SOCKET", name)
		if sent, err := Take().Send("STOPPING=1"); !sent || err != nil {
			t.Fatalf("Send(STOPPING=1) = %v, %v", sent, err)
		}
		if got := recv(t, conn); got != "STOPPING=1" {
			t.Errorf("got %q, want STOPPING=1", got)
		}
	})
	t.Run("none", func(t *testing.T) {
		t.Setenv("NOTIFY_SOCKET", "")
		if sent, err := Take().Send("READY=1"); sent || err != nil {
			t.Errorf("Send with no socket = %v, %v; want false, nil", sent, err)
		}
	})
}

// TestNotifyLogsAFailedSend: a send that fails is a warning naming the state,
// never an error the daemon acts on; with no socket nothing is logged.
func TestNotifyLogsAFailedSend(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	Socket(filepath.Join(t.TempDir(), "absent")).Notify(log, "READY=1")
	if out := buf.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "sd_notify failed") ||
		!strings.Contains(out, `state="READY=1"`) {
		t.Errorf("a failed send logged %q; want a warning naming READY=1", out)
	}

	buf.Reset()
	Socket("").Notify(log, "READY=1")
	if out := buf.String(); out != "" {
		t.Errorf("no socket logged %q; want nothing", out)
	}
}
