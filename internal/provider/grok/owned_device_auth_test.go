package grok

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

const liveGrokCred = `{"https://auth.x.ai::c1":{"key":"OLD","auth_mode":"oauth",` +
	`"refresh_token":"OLD","expires_at":"2026-08-20T00:00:00Z"}}`

func fakeGrokLogin(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "grok")
	script := `#!/bin/sh
if [ "$1" = "login" ] && [ "$2" = "--device-auth" ]; then
  echo "Visit https://x.ai/device and enter code ABCD-1234"
` + body + `
fi
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func coordinatedGrok(t *testing.T, bin string) (*CoordinatedProvider, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GROK_HOME", home)
	live := filepath.Join(home, "auth.json")
	if err := os.WriteFile(live, []byte(liveGrokCred), 0o600); err != nil {
		t.Fatal(err)
	}
	coord, err := providerauth.NewCoordinator(t.TempDir(), NewCredentialAdapter("grok"), providerauth.CoordinatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := coord.Seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := NewCoordinated(nil, bin, nil, coord, nil)
	// Bypass the macOS sandbox-exec wrapper: the transaction contract under
	// test is platform-independent, and the wrapper has its own test.
	c.wrap = func(b string) (string, []string, error) {
		return b, []string{"login", "--device-auth"}, nil
	}
	return c, live
}

func TestCoordinatedGrokPublishesOnSuccess(t *testing.T) {
	bin := fakeGrokLogin(t, `printf '{"https://auth.x.ai::c1":{"key":"NEW","auth_mode":"oauth","refresh_token":"NEW","expires_at":"2026-08-22T00:00:00Z"}}' > "$GROK_HOME/auth.json"; chmod 600 "$GROK_HOME/auth.json"`)
	c, live := coordinatedGrok(t, bin)

	h, err := c.StartOwnedDeviceAuth(context.Background(), "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.Flow().UserCode != "ABCD-1234" {
		t.Fatalf("user code = %q", h.Flow().UserCode)
	}
	if err := h.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"key":"NEW"`) {
		t.Fatalf("live = %s, want the new credential", got)
	}
}

// TestCoordinatedGrokIncompleteOutcomesLeaveLiveIdentical covers F9: grok's
// flow never deleted the credential, but it could still orphan a child and
// publish over a rotated file. Every incomplete outcome must be inert.
func TestCoordinatedGrokIncompleteOutcomesLeaveLiveIdentical(t *testing.T) {
	cases := []struct {
		name string
		body string
		kill bool
	}{
		{"cancelled", "sleep 120", true},
		{"child fails", "exit 6", false},
		{"child writes nothing", "exit 0", false},
		{"child writes junk", `printf 'nope' > "$GROK_HOME/auth.json"; chmod 600 "$GROK_HOME/auth.json"`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bin := fakeGrokLogin(t, tc.body)
			c, live := coordinatedGrok(t, bin)

			h, err := c.StartOwnedDeviceAuth(context.Background(), "", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.kill {
				h.Cancel()
			}
			if err := h.Wait(context.Background()); err == nil {
				t.Fatal("an incomplete flow reported success")
			}
			got, err := os.ReadFile(live)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != liveGrokCred {
				t.Fatal("an incomplete flow changed the live credential")
			}
		})
	}
}

// TestCoordinatedGrokStartsWithAnEmptyHomeAndCleansStubs proves the child sees
// no credential and that browser stubs live inside the transaction, so they are
// removed with it rather than left in the system temp directory.
func TestCoordinatedGrokStartsWithAnEmptyHomeAndCleansStubs(t *testing.T) {
	// Fails if the pending home was seeded; records where the stubs were.
	bin := fakeGrokLogin(t, `[ -e "$GROK_HOME/auth.json" ] && exit 3
command -v open > "$GROK_HOME/../openstub-path" 2>/dev/null || true
printf '{"https://auth.x.ai::c1":{"key":"N","auth_mode":"oauth","refresh_token":"N","expires_at":"2026-08-22T00:00:00Z"}}' > "$GROK_HOME/auth.json"; chmod 600 "$GROK_HOME/auth.json"`)
	c, _ := coordinatedGrok(t, bin)

	h, err := c.StartOwnedDeviceAuth(context.Background(), "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Wait(context.Background()); err != nil {
		t.Fatalf("child saw a seeded credential in its pending home: %v", err)
	}

	// The pre-transaction flow put stubs in the system temp directory and
	// relied on a deferred RemoveAll that an orphaned wait would never run.
	stubs, _ := filepath.Glob(filepath.Join(os.TempDir(), "mcremote-grok-open-*"))
	for _, d := range stubs {
		t.Errorf("a browser stub outlived the flow in the system temp dir: %s", d)
	}
}

func TestCoordinatedGrokRejectsUnknownUpstreamAndMethod(t *testing.T) {
	bin := fakeGrokLogin(t, "exit 0")
	c, _ := coordinatedGrok(t, bin)
	if _, err := c.StartOwnedDeviceAuth(context.Background(), "nope", "", nil); err == nil {
		t.Error("unknown upstream was accepted")
	}
	if _, err := c.StartOwnedDeviceAuth(context.Background(), "", "xai:mystery", nil); err == nil {
		t.Error("unknown method was accepted")
	}
}

func TestCoordinatedGrokSatisfiesOwnedContract(t *testing.T) {
	var _ provider.OwnedDeviceAuth = (*CoordinatedProvider)(nil)
}

// TestUncoordinatedGrokStartsNoTransaction proves P18 stays dark for Grok too.
func TestUncoordinatedGrokStartsNoTransaction(t *testing.T) {
	c := NewCoordinated(nil, "grok", nil, nil, nil)
	if _, err := c.StartOwnedDeviceAuth(context.Background(), "", "", nil); err == nil {
		t.Fatal("a provider with no coordinator started an owned flow")
	}
}
