package admin_test

import (
	"context"
	"path/filepath"
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

func TestAdminDisconnectRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "admin.sock")
	d := &fakeDisc{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- admin.Serve(ctx, sock, d, nil) }()

	// Wait until socket exists.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := admin.Call(sock, admin.Request{Op: admin.OpPing}); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("admin socket never became ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

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
