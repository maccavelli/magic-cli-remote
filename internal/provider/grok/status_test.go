package grok

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// TestGrokStatusProjectsBackup: a coordinated grok provider's status carries
// its coordinator's recovery projection, as codex's does, and a wrapper with no
// coordinator carries none (MADR 0074 D42).
func TestGrokStatusProjectsBackup(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	t.Setenv("GROK_HOME", home)
	t.Setenv("XAI_API_KEY", "")
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(liveGrokCred), 0o600); err != nil {
		t.Fatal(err)
	}
	coord, err := providerauth.NewCoordinator(t.TempDir(), NewCredentialAdapter("grok"), providerauth.CoordinatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := coord.Seed(ctx); err != nil {
		t.Fatal(err)
	}
	want, err := coord.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want.BackupState != providerauth.BackupCurrent || !want.RecoveryAvailable {
		t.Fatalf("seeded coordinator: %q recovery_available=%v, want current and true", want.BackupState, want.RecoveryAvailable)
	}

	st, err := NewCoordinated(New(Config{}), "grok", nil, coord, nil).AuthStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.BackupState != provider.BackupCurrent || !st.RecoveryAvailable {
		t.Fatalf("coordinated grok status: backup_state=%q recovery_available=%v, want %q and true (the coordinator's projection)",
			st.BackupState, st.RecoveryAvailable, provider.BackupCurrent)
	}
	if st.Status != provider.AuthConfigured || len(st.Upstreams) != 1 {
		t.Fatalf("coordinated grok status lost the plain provider's picture: %+v", st)
	}

	plain, err := NewCoordinated(New(Config{}), "grok", nil, nil, nil).AuthStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if plain.BackupState != "" || plain.RecoveryAvailable {
		t.Fatalf("grok with no coordinator: backup_state=%q recovery_available=%v, want neither",
			plain.BackupState, plain.RecoveryAvailable)
	}
}
