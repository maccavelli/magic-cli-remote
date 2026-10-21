package admin_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/admin"
)

type fakeDisc struct {
	mu   sync.Mutex
	seen []string
}

func (f *fakeDisc) DisconnectDevice(id string) int {
	return f.DisconnectDevices([]string{id})
}

func (f *fakeDisc) DisconnectDevices(ids []string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, ids...)
	return len(ids)
}

// serveForTest runs Serve on a fresh socket until the test ends, and returns
// the socket once it answers a ping.
func serveForTest(t *testing.T, d admin.Disconnector, shutdown func()) string {
	t.Helper()
	// A short directory: t.TempDir's carries the test name, and a Unix
	// socket path is limited to 104 bytes on macOS.
	dir, err := os.MkdirTemp("", "adm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "admin.sock")
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- admin.Serve(ctx, sock, d, nil, shutdown) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
			t.Error("admin Serve did not exit")
		}
	})
	waitReady(t, sock)
	return sock
}

// waitReady waits until the admin socket answers a ping.
func waitReady(t *testing.T, sock string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := admin.Call(sock, admin.Request{Op: admin.OpPing}); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("admin socket never became ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestShutdownAnswersThenRequestsIt: the shutdown operation replies OK and
// then calls the daemon's shutdown, which serve wires to the context its
// signals cancel (MADR 0054 A2; PLAN S8 step 1b).
func TestShutdownAnswersThenRequestsIt(t *testing.T) {
	requested := make(chan struct{})
	var once sync.Once
	sock := serveForTest(t, &fakeDisc{}, func() { once.Do(func() { close(requested) }) })

	if err := admin.RequestShutdown(sock); err != nil {
		t.Fatalf("RequestShutdown: %v", err)
	}
	select {
	case <-requested:
	case <-time.After(2 * time.Second):
		t.Fatal("the shutdown operation did not call the daemon's shutdown")
	}
}

// TestShutdownRefusedWithoutAHandler: a daemon that wires no shutdown refuses
// the operation rather than answering OK and doing nothing.
func TestShutdownRefusedWithoutAHandler(t *testing.T) {
	sock := serveForTest(t, &fakeDisc{}, nil)
	err := admin.RequestShutdown(sock)
	if err == nil || !strings.Contains(err.Error(), "shutdown not supported") {
		t.Fatalf("RequestShutdown = %v, want it refused as not supported", err)
	}
}

// TestUnknownOpStillRefused: adding an operation leaves the default refusal.
func TestUnknownOpStillRefused(t *testing.T) {
	sock := serveForTest(t, &fakeDisc{}, func() {})
	_, err := admin.Call(sock, admin.Request{Op: "reboot"})
	if err == nil || !strings.Contains(err.Error(), "unknown op: reboot") {
		t.Fatalf("Call(reboot) = %v, want unknown op", err)
	}
}

func TestAdminDisconnectRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "admin.sock")
	d := &fakeDisc{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- admin.Serve(ctx, sock, d, nil, nil) }()

	waitReady(t, sock)

	n, err := admin.NotifyDisconnect(sock, "dev-a", "dev-b")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("disconnected=%d want 2", n)
	}
	d.mu.Lock()
	if len(d.seen) != 2 || d.seen[0] != "dev-a" || d.seen[1] != "dev-b" {
		t.Fatalf("seen=%v", d.seen)
	}
	d.mu.Unlock()

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("admin Serve did not exit")
	}
}

func TestNotifyWithoutDaemon(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "admin.sock")
	_, err := admin.NotifyDisconnect(sock, "x")
	if err == nil {
		t.Fatal("expected ErrDaemonNotRunning")
	}
}

// TestServeRefusesNonSocket proves Serve fails closed on a path it
// cannot verify as its own socket, and does not unlink what is there
// (MADR 0116 D7 / contract C3).
func TestServeRefusesNonSocket(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, admin.SocketName)
	if err := os.WriteFile(sock, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()
	err := admin.Serve(ctx, sock, &fakeDisc{}, nil, nil)
	if err == nil {
		t.Fatal("Serve accepted a non-socket at the socket path")
	}
	if !strings.Contains(err.Error(), "not a socket") {
		t.Errorf("error = %v, want it to name the non-socket cause", err)
	}
	if _, statErr := os.Stat(sock); statErr != nil {
		t.Errorf("Serve removed the pre-existing file: %v", statErr)
	}
}
