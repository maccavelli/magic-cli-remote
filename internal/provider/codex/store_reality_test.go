package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
)

// fakeStatusBin writes a stand-in codex whose `login status` exit code is
// fixed, imitating a host whose real session lives outside auth.json.
func fakeStatusBin(t *testing.T, exitCode int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex")
	body := "#!/bin/sh\nif [ \"$1\" = \"login\" ] && [ \"$2\" = \"status\" ]; then exit " +
		itoaReality(exitCode) + "; fi\nexit 0\n"
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func itoaReality(i int) string {
	if i == 0 {
		return "0"
	}
	var d []byte
	for i > 0 {
		d = append([]byte{byte('0' + i%10)}, d...)
		i /= 10
	}
	return string(d)
}

// TestObserveCredentialStoreAssertsReality is the fix for a false assumption
// that produced a production lockout on 2026-08-21.
//
// Reading `cli_auth_credentials_store` and defaulting to "file" answers what
// the config says, not where the credential is. The host that broke had no
// such key — so config said "file" — while its auth.json was the stub `{}` and
// `codex login status` reported a live ChatGPT session. Detection must compare
// the two and report what it actually observes.
func TestObserveCredentialStoreAssertsReality(t *testing.T) {
	const usable = `{"tokens":{"access_token":"a","refresh_token":"r"}}`

	cases := []struct {
		name       string
		authJSON   string
		statusExit int
		want       StoreReality
	}{
		{
			name:     "file holds the credential",
			authJSON: usable, statusExit: 0, want: RealityFileProtected,
		},
		{
			// The production case: authenticated, but not from the file.
			name:     "authenticated from outside the file",
			authJSON: `{}`, statusExit: 0, want: RealityExternal,
		},
		{
			name:     "genuinely logged out",
			authJSON: `{}`, statusExit: 1, want: RealityLoggedOut,
		},
		{
			name:     "no file at all and logged out",
			authJSON: "", statusExit: 1, want: RealityLoggedOut,
		},
		{
			name:     "no file but authenticated elsewhere",
			authJSON: "", statusExit: 0, want: RealityExternal,
		},
		{
			name:     "unparseable file while authenticated elsewhere",
			authJSON: `not json`, statusExit: 0, want: RealityExternal,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("CODEX_HOME", home)
			if tc.authJSON != "" {
				if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(tc.authJSON), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := ObserveCredentialStore(context.Background(), fakeStatusBin(t, tc.statusExit))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("reality = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestObserveRespectsAConfiguredNonFileStore proves an explicitly configured
// keyring is still reported without probing, because no login will ever put a
// credential in a file there.
func TestObserveRespectsAConfiguredNonFileStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"),
		[]byte("cli_auth_credentials_store = \"keyring\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ObserveCredentialStore(context.Background(), fakeStatusBin(t, 0))
	if got != RealityUnsupported {
		t.Fatalf("reality = %q, want %q", got, RealityUnsupported)
	}
	if !errors.Is(err, providerauth.ErrUnsupportedBackend) {
		t.Fatalf("err = %v, want ErrUnsupportedBackend", err)
	}
}

// TestExternalCredentialStillAllowsSignIn is the lockout lesson applied to
// this probe.
//
// A credential we cannot see is a reason to tell the operator the truth, not a
// reason to refuse the login that would replace it with one we can protect.
// Only a store that will never produce a protectable file blocks a transaction.
func TestExternalCredentialStillAllowsSignIn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := fakeStatusBin(t, 0)

	reality, err := ObserveCredentialStore(context.Background(), bin)
	if err != nil {
		t.Fatal(err)
	}
	if reality != RealityExternal {
		t.Fatalf("reality = %q, want external", reality)
	}
	// The gate that gates transactions must not block this host.
	if err := NewCredentialAdapter("codex", bin).CheckBackend(); err != nil {
		t.Fatalf("an externally stored credential blocked sign-in: %v", err)
	}
}

// TestConfiguredKeyringBlocksSignIn proves the one case that should block still
// blocks: a login there produces nothing this coordinator can protect.
func TestConfiguredKeyringBlocksSignIn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.toml"),
		[]byte("cli_auth_credentials_store = \"keyring\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewCredentialAdapter("codex", fakeStatusBin(t, 0)).CheckBackend(); !errors.Is(err, providerauth.ErrUnsupportedBackend) {
		t.Fatalf("err = %v, want ErrUnsupportedBackend", err)
	}
}

// TestObserveWithoutABinaryFallsBackToConfig proves the probe degrades to the
// config answer rather than guessing when it cannot run the CLI.
func TestObserveWithoutABinaryFallsBackToConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	got, err := ObserveCredentialStore(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got != RealityUnknown {
		t.Fatalf("reality = %q, want %q when the CLI cannot be probed", got, RealityUnknown)
	}
}
