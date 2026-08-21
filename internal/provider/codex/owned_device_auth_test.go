package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// fakeCodexLogin writes a stand-in `codex` whose subcommands behave like the
// real CLI's contract: `login --device-auth` prints a code and writes into
// CODEX_HOME, and `login status` succeeds only when a credential is there.
func fakeCodexLogin(t *testing.T, loginBody string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex")
	script := `#!/bin/sh
if [ "$1" = "login" ] && [ "$2" = "status" ]; then
  [ -f "$CODEX_HOME/auth.json" ] || exit 1
  echo "Logged in"
  exit 0
fi
if [ "$1" = "login" ] && [ "$2" = "--device-auth" ]; then
  echo "To sign in, visit https://example.test/device and enter CODE-9999"
` + loginBody + `
fi
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func coordinatedCodex(t *testing.T, bin string) (*Provider, string) {
	t.Helper()
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	live := filepath.Join(codexHome, "auth.json")
	if err := os.WriteFile(live, []byte(`{"tokens":{"access_token":"a","refresh_token":"r"},"last_refresh":"2026-08-20T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	coord, err := providerauth.NewCoordinator(t.TempDir(), NewCredentialAdapter("codex", bin), providerauth.CoordinatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := coord.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	p := NewCoordinated(Config{Bin: bin}, nil, coord, nil)
	return p, live
}

// TestCoordinatedCodexPublishesOnSuccess proves the isolated login publishes
// only after its own probe passes.
func TestCoordinatedCodexPublishesOnSuccess(t *testing.T) {
	bin := fakeCodexLogin(t, `printf '{"tokens":{"access_token":"NEW","refresh_token":"NEW"},"last_refresh":"2026-08-21T00:00:00Z"}' > "$CODEX_HOME/auth.json"; chmod 600 "$CODEX_HOME/auth.json"`)
	p, live := coordinatedCodex(t, bin)

	h, err := p.StartOwnedDeviceAuth(context.Background(), "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.Flow().UserCode != "CODE-9999" {
		t.Fatalf("user code = %q", h.Flow().UserCode)
	}
	if err := h.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"access_token":"NEW"`) {
		t.Fatalf("live = %s, want the new credential", got)
	}
}

// TestCoordinatedCodexNeverSignsTheHostOut is the F14 regression gate. The old
// path let `codex login --device-auth` delete and revoke the live credential at
// start; the isolated path must leave it byte-identical on every incomplete
// outcome, and the child must find nothing to revoke.
func TestCoordinatedCodexNeverSignsTheHostOut(t *testing.T) {
	cases := []struct {
		name string
		body string
		kill bool
	}{
		{"user abandons the flow", "sleep 120", true},
		{"child fails", "exit 7", false},
		{"child writes nothing", "exit 0", false},
		{
			name: "child would have deleted a seeded credential",
			// Mirrors clear_existing_auth_before_login: if anything had been
			// copied into the pending home, this is where the real CLI would
			// revoke it. The isolated home must be empty, so there is nothing
			// to delete and the flow proceeds.
			body: `rm -f "$CODEX_HOME/auth.json"; exit 5`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bin := fakeCodexLogin(t, tc.body)
			p, live := coordinatedCodex(t, bin)
			before, err := os.ReadFile(live)
			if err != nil {
				t.Fatal(err)
			}

			h, err := p.StartOwnedDeviceAuth(context.Background(), "", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.kill {
				h.Cancel()
			}
			if err := h.Wait(context.Background()); err == nil {
				t.Fatal("an incomplete flow reported success")
			}

			after, err := os.ReadFile(live)
			if err != nil {
				t.Fatalf("the host credential is gone: %v", err)
			}
			if string(after) != string(before) {
				t.Fatal("an incomplete flow changed the live credential")
			}
		})
	}
}

// TestCoordinatedCodexNeedsNoDestructiveConfirmation proves D20: with an
// isolated empty home the flow is not destructive, so there is nothing to
// confirm and no ErrAuthConfirmRequired gate.
func TestCoordinatedCodexNeedsNoDestructiveConfirmation(t *testing.T) {
	bin := fakeCodexLogin(t, `printf '{"tokens":{"access_token":"N","refresh_token":"N"}}' > "$CODEX_HOME/auth.json"; chmod 600 "$CODEX_HOME/auth.json"`)
	p, _ := coordinatedCodex(t, bin)

	// No confirmDestructive parameter exists on the owned contract at all.
	h, err := p.StartOwnedDeviceAuth(context.Background(), "", "", nil)
	if err != nil {
		t.Fatalf("owned start required a confirmation it should not need: %v", err)
	}
	// Cancel is asynchronous; wait for the flow to resolve so its transaction
	// directory is released before the temp dir is torn down.
	h.Cancel()
	_ = h.Wait(context.Background())
}

// TestCoordinatedCodexRefusesUnsupportedBackend proves a keyring store is a
// typed refusal before anything spawns (MADR 0074 D22).
func TestCoordinatedCodexRefusesUnsupportedBackend(t *testing.T) {
	bin := fakeCodexLogin(t, "exit 0")
	p, _ := coordinatedCodex(t, bin)
	home := os.Getenv("CODEX_HOME")
	if err := os.WriteFile(filepath.Join(home, "config.toml"),
		[]byte("cli_auth_credentials_store = \"keyring\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.StartOwnedDeviceAuth(context.Background(), "", "", nil); err == nil {
		t.Fatal("a keyring-backed host was allowed to start a transaction")
	}
}

// TestProductionCodexExposesNoTransaction proves P18 stays dark: a provider
// built the way the daemon builds it advertises no owned contract, so this
// commit cannot activate a transactional flow before P20 (step 4).
func TestProductionCodexExposesNoTransaction(t *testing.T) {
	p := New(Config{Bin: "codex"})
	if _, ok := any(p).(provider.OwnedDeviceAuth); ok {
		if p.coord != nil {
			t.Fatal("production construction carries a coordinator")
		}
	}
	// The legacy contract must still be there for the pre-P20 path.
	if _, ok := any(p).(provider.DeviceAuth); !ok {
		t.Fatal("legacy DeviceAuth was removed before P20 could replace it")
	}
	if _, err := p.StartOwnedDeviceAuth(context.Background(), "", "", nil); err == nil {
		t.Fatal("an uncoordinated provider started an owned flow")
	}
}

// TestCoordinatedCodexSatisfiesOwnedContract pins the interface.
func TestCoordinatedCodexSatisfiesOwnedContract(t *testing.T) {
	var _ provider.OwnedDeviceAuth = (*Provider)(nil)
	_ = time.Second
}
