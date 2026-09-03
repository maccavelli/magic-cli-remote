package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.NewFile(0, os.DevNull), &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestGuardIsAbsentWhenNoTransactionalProviderIsEnabled proves a host running
// neither Codex nor Grok carries none of this machinery and advertises no
// transactional capability (MADR 0074 P20 step 12).
func TestGuardIsAbsentWhenNoTransactionalProviderIsEnabled(t *testing.T) {
	g, err := newCredentialGuard(t.TempDir(), false, false, "", quietLog())
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
	testexec.SkipIfNoPOSIXModes(t)
	dataDir := t.TempDir()
	g, err := newCredentialGuard(dataDir, true, false, "codex", quietLog())
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

	g, err := newCredentialGuard(dataDir, true, true, "codex", quietLog())
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
	g, err := newCredentialGuard(dataDir, true, false, "codex", quietLog())
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
	g, err := newCredentialGuard(t.TempDir(), true, false, "codex", quietLog())
	if err != nil {
		t.Fatal(err)
	}
	g.close(context.Background())
}

// TestCredentialGuardPassesTheCodexBinary is the regression for a wiring defect
// MADR 0134's unit tests could not see (found in its Phase 6, on a live host).
//
// The reality probe that separates "no credential anywhere" from "a credential
// this coordinator cannot see" runs the provider's own CLI. An adapter built
// without a binary path answers RealityUnknown for both, so every host in the
// external state silently fell back to the pre-0134 escalation — the daemon
// kept demanding an operator decision, and nothing in providerauth could tell,
// because its tests supply their own adapter.
//
// The assertion is end-to-end on purpose: a stub CLI that exits zero stands in
// for `codex login status` succeeding, the credential file holds Codex's `{}`
// stub, and recovery must reach external. With the binary dropped it cannot.
func TestCredentialGuardPassesTheCodexBinary(t *testing.T) {
	codexHome := t.TempDir()
	authPath := filepath.Join(codexHome, "auth.json")
	// Seed the way the real host is seeded: a genuine credential first, so a
	// CURRENT generation exists. Without one, recovery takes the unmanaged
	// seeding branch and never reaches the escalation this classifies — which
	// is harmless (no warning, and the provider still projects unsupported)
	// but is not the state the reporting host is in.
	realCred := `{"OPENAI_API_KEY":null,"auth_mode":"chatgpt","tokens":` +
		`{"access_token":"a","refresh_token":"r"},"last_refresh":"2026-09-01T00:00:00Z"}`
	if err := os.WriteFile(authPath, []byte(realCred), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)

	// A CLI that reports "signed in" by exiting zero.
	stub := filepath.Join(t.TempDir(), "codex-stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	g, err := newCredentialGuard(t.TempDir(), true, false, stub, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	c := g.coordinator("codex")
	if c == nil {
		t.Fatal("no codex coordinator")
	}
	if err := c.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Now Codex does what it does on the reporting host: keeps the session
	// elsewhere and leaves a stub behind.
	if err := os.WriteFile(authPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := c.Recover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st != providerauth.StateExternal {
		t.Fatalf("state = %s, want external: the guard must hand the adapter a "+
			"binary, or the reality probe can never answer", st)
	}
}
