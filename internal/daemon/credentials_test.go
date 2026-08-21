package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.NewFile(0, os.DevNull), &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestGuardIsAbsentWhenNoTransactionalProviderIsEnabled proves a host running
// neither Codex nor Grok carries none of this machinery and advertises no
// transactional capability (MADR 0074 P20 step 12).
func TestGuardIsAbsentWhenNoTransactionalProviderIsEnabled(t *testing.T) {
	g, err := newCredentialGuard(t.TempDir(), false, false, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	if g.enabled() {
		t.Fatal("a guard was built with no transactional provider enabled")
	}
	// Every method must be nil-safe, because that is the normal case here.
	g.recover(context.Background())
	g.startWatchers(context.Background())
	g.close(context.Background())
	if g.coordinator("codex") != nil {
		t.Fatal("a disabled provider produced a coordinator")
	}
}

// TestGuardBuildsOnlyEnabledProviders proves construction follows config.
func TestGuardBuildsOnlyEnabledProviders(t *testing.T) {
	dataDir := t.TempDir()
	g, err := newCredentialGuard(dataDir, true, false, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	if !g.enabled() {
		t.Fatal("codex was enabled but no guard was built")
	}
	if g.coordinator("codex") == nil {
		t.Fatal("no codex coordinator")
	}
	if g.coordinator("grok") != nil {
		t.Fatal("grok was disabled but got a coordinator")
	}
	// The private store exists with owner-only permissions.
	fi, err := os.Stat(filepath.Join(dataDir, "provider-auth", "codex"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("store mode = %04o, want 0700", fi.Mode().Perm())
	}
}

// TestGuardRecoverReportsEveryProvider proves one provider needing an operator
// decision does not hide or block the other (MADR 0074 P20 step 10).
func TestGuardRecoverReportsEveryProvider(t *testing.T) {
	dataDir := t.TempDir()
	codexHome := t.TempDir()
	grokHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("GROK_HOME", grokHome)

	// Codex has a healthy credential; grok has none.
	if err := os.WriteFile(filepath.Join(codexHome, "auth.json"),
		[]byte(`{"tokens":{"access_token":"a","refresh_token":"r"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	g, err := newCredentialGuard(dataDir, true, true, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	g.recover(context.Background())

	cs, err := g.coordinator("codex").Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cs.BackupState != providerauth.BackupCurrent {
		t.Fatalf("codex backup state = %s, want current", cs.BackupState)
	}
	gs, err := g.coordinator("grok").Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gs.BackupState != providerauth.BackupUnmanaged {
		t.Fatalf("grok backup state = %s, want unmanaged on a cold host", gs.BackupState)
	}
}

// TestGuardWatcherFailureIsNotFatal proves an unavailable watcher degrades to
// reconciliation instead of breaking startup — the Linux inotify-limit case.
func TestGuardWatcherFailureIsNotFatal(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("CODEX_HOME", t.TempDir())
	g, err := newCredentialGuard(dataDir, true, false, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	g.startWatchers(ctx)
	// Whether or not the watcher started, the coordinator stays usable.
	if err := g.coordinator("codex").Reconcile(ctx); err != nil {
		t.Fatalf("coordinator unusable after watcher startup: %v", err)
	}
	g.close(ctx)
	g.close(ctx) // idempotent
}

// TestGuardCloseIsSafeBeforeStart proves shutdown ordering cannot panic when
// startup failed partway.
func TestGuardCloseIsSafeBeforeStart(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	g, err := newCredentialGuard(t.TempDir(), true, false, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	g.close(context.Background())
}
