//go:build unix

package daemon

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/admin"
	"github.com/maccavelli/magic-cli-remote/internal/config"
)

// isolatedRoots points every XDG root at a scratch directory, so a whole Run
// writes nothing under the developer's home. The runtime root is short: the
// admin socket's path must fit 104 bytes on macOS.
func isolatedRoots(t *testing.T) {
	t.Helper()
	for _, env := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR"} {
		dir, err := os.MkdirTemp("", "xdg")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(env, dir)
	}
	t.Setenv("MCREMOTE_CONFIG", "")
	t.Setenv("MCREMOTE_DATA_DIR", "")
}

// TestRunShutsDownOnTheAdminRequest: the admin socket's shutdown operation
// calls Options.Shutdown, which serve wires to the context its signals
// cancel, and Run returns from the same graceful shutdown as after a signal
// (MADR 0054 A2; PLAN S8 step 1b).
func TestRunShutsDownOnTheAdminRequest(t *testing.T) {
	isolatedRoots(t)
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	cfg.Listen.Host = "127.0.0.1"
	cfg.Listen.Port = 0
	cfg.TLS.Mode = config.TLSModeOff
	cfg.TLS = cfg.TLS.Normalized()
	// A client key needs TLS, which this test does not serve.
	cfg.Auth.RequireClientKey = false
	// No agent CLI is started: only the fake provider.
	cfg.Providers.Grok.Enabled = false
	cfg.Providers.Opencode.Enabled = false
	cfg.Providers.Codex.Enabled = false
	cfg.Providers.Kilo.Enabled = false
	cfg.Providers.Fake.Enabled = true
	if err := cfg.RecomputePaths(); err != nil {
		t.Fatal(err)
	}

	requested := errors.New("shutdown requested over the admin socket")
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Config:   cfg,
			Version:  "test",
			Log:      quietLogger(),
			Shutdown: func() { cancel(requested) },
		})
	}()

	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := admin.Call(cfg.Paths.AdminSocket, admin.Request{Op: admin.OpPing}); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("Run returned before its admin socket answered: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the daemon's admin socket never answered")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := admin.RequestShutdown(cfg.Paths.AdminSocket); err != nil {
		t.Fatalf("RequestShutdown: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run after the shutdown request = %v, want nil, as after a signal", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Run did not return after the shutdown request")
	}
	if got := context.Cause(ctx); !errors.Is(got, requested) {
		t.Errorf("the run context ended with %v, want the request", got)
	}
}
