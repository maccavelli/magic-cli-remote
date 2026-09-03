package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// fakeDoctorBin writes a stand-in codex whose `doctor --json` prints a recorded
// report fixture, and returns a control file the test can rewrite to change the
// CLI's answer between calls (MADR 0136).
//
// The stub exits NON-ZERO for doctor, because the real one does whenever it
// finds a problem — which is precisely when this classification matters. If the
// probe ever regresses to trusting an exit code, every case below breaks.
func fakeDoctorBin(t *testing.T, fixture string) (bin, control string) {
	t.Helper()
	testexec.SkipIfNoPOSIXShell(t)
	dir := t.TempDir()
	control = filepath.Join(dir, "which-fixture")
	setDoctorFixture(t, control, fixture)
	bin = filepath.Join(dir, "codex")
	body := "#!/bin/sh\n" +
		"if [ \"$1\" = \"doctor\" ]; then cat \"$(cat " + control + ")\"; exit 1; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(bin, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, control
}

// setDoctorFixture points the stub at a fixture by name.
func setDoctorFixture(t *testing.T, control, fixture string) {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", "doctor", fixture))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("fixture %s: %v", fixture, err)
	}
	if err := os.WriteFile(control, []byte(abs), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fakeGarbageDoctorBin is a codex whose doctor output cannot be parsed.
func fakeGarbageDoctorBin(t *testing.T) string {
	t.Helper()
	testexec.SkipIfNoPOSIXShell(t)
	bin := filepath.Join(t.TempDir(), "codex")
	body := "#!/bin/sh\nif [ \"$1\" = \"doctor\" ]; then echo 'not json'; exit 1; fi\nexit 0\n"
	if err := os.WriteFile(bin, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin
}

// TestObserveCredentialStoreAssertsReality pins the classification against
// every recorded `codex doctor --json` shape (MADR 0136).
//
// This test previously drove `codex login status` and asserted that a `{}`
// auth.json plus exit 0 meant "authenticated from outside the file". Both
// halves of that were wrong. The exit status is always zero — a home with no
// auth.json prints "Not logged in" and still exits 0 — so it carried no
// information, and the `{}` file it treated as evidence of an external store
// was written by this repository's OWN test helper, not by Codex
// (see the MADR 0136 amendment history).
//
// The cases now read a structured verdict, and the two that matter most are
// `incomplete-file` and `env-provided`: both are a file backend holding an
// unusable credential, which is broken, not unprotectable.
func TestObserveCredentialStoreAssertsReality(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		want    StoreReality
		wantErr bool
	}{
		{"file holds a usable credential", "file-protected.json", RealityFileProtected, false},
		{"nothing stored anywhere", "no-credentials.json", RealityLoggedOut, false},
		{"stored but incomplete", "incomplete-file.json", RealityBroken, false},
		{"env auth over an incomplete file", "env-provided.json", RealityBroken, false},
		{"resolved keyring backend", "keyring-backend.json", RealityUnsupported, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CODEX_HOME", t.TempDir())
			bin, _ := fakeDoctorBin(t, tc.fixture)
			got, err := ObserveCredentialStore(context.Background(), bin)
			if tc.wantErr && !errors.Is(err, providerauth.ErrUnsupportedBackend) {
				t.Fatalf("err = %v, want ErrUnsupportedBackend", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("reality = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestObserveNeverGuessesFromAnUnusableReport is the conservative fallback.
//
// An unparseable report, and a missing binary, must both leave the caller with
// no reason to silence an escalation. Unknown is that answer.
func TestObserveNeverGuessesFromAnUnusableReport(t *testing.T) {
	t.Run("garbage output", func(t *testing.T) {
		t.Setenv("CODEX_HOME", t.TempDir())
		got, err := ObserveCredentialStore(context.Background(), fakeGarbageDoctorBin(t))
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got != RealityUnknown {
			t.Fatalf("reality = %q, want unknown", got)
		}
	})

	t.Run("no binary to ask", func(t *testing.T) {
		t.Setenv("CODEX_HOME", t.TempDir())
		got, err := ObserveCredentialStore(context.Background(), "")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got != RealityUnknown {
			t.Fatalf("reality = %q, want unknown", got)
		}
	})
}

// TestObserveIgnoresEnvironmentAuthForClassification pins that a per-process
// fact stays out of a host-wide verdict.
//
// The env-provided fixture is the reporting host: `auth env vars present:
// OPENAI_API_KEY`, and Codex reporting auth as coming from the environment.
// The operator's shell had that variable and the daemon's LaunchAgent did not,
// so classifying on it would be wrong for one of the two (MADR 0136).
func TestObserveIgnoresEnvironmentAuthForClassification(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	bin, _ := fakeDoctorBin(t, "env-provided.json")
	got, err := ObserveCredentialStore(context.Background(), bin)
	if err != nil {
		t.Fatal(err)
	}
	if got == RealityFileProtected || got == RealityUnsupported {
		t.Fatalf("reality = %q: environment auth must not decide the classification", got)
	}
	if got != RealityBroken {
		t.Fatalf("reality = %q, want broken", got)
	}
}

// TestObserveRespectsAConfiguredNonFileStore proves a keyring backend is
// reported as unprotectable, and that the CONFIG is only consulted when the CLI
// cannot be asked.
//
// The resolved backend from the report outranks config.toml, which is the point
// of reading it: config.toml is parsed here by a reader that sees only bare
// top-level keys, so it is blind to a profile key, a -c override, and to what
// `auto` actually resolved to (MADR 0136).
func TestObserveRespectsAConfiguredNonFileStore(t *testing.T) {
	t.Run("report says keyring", func(t *testing.T) {
		t.Setenv("CODEX_HOME", t.TempDir())
		bin, _ := fakeDoctorBin(t, "keyring-backend.json")
		got, err := ObserveCredentialStore(context.Background(), bin)
		if got != RealityUnsupported {
			t.Fatalf("reality = %q, want %q", got, RealityUnsupported)
		}
		if !errors.Is(err, providerauth.ErrUnsupportedBackend) {
			t.Fatalf("err = %v, want ErrUnsupportedBackend", err)
		}
	})

	t.Run("config says keyring and there is no CLI to ask", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("CODEX_HOME", home)
		if err := os.WriteFile(filepath.Join(home, "config.toml"),
			[]byte("cli_auth_credentials_store = \"keyring\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := ObserveCredentialStore(context.Background(), "")
		if got != RealityUnsupported {
			t.Fatalf("reality = %q, want %q", got, RealityUnsupported)
		}
		if !errors.Is(err, providerauth.ErrUnsupportedBackend) {
			t.Fatalf("err = %v, want ErrUnsupportedBackend", err)
		}
	})
}

// TestBrokenCredentialStillAllowsSignIn is the lockout lesson applied to this
// probe.
//
// A credential we cannot use is a reason to tell the operator the truth, not a
// reason to refuse the login that would replace it. Only a backend that will
// never produce a protectable file blocks a transaction.
func TestBrokenCredentialStillAllowsSignIn(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	bin, _ := fakeDoctorBin(t, "incomplete-file.json")

	reality, err := ObserveCredentialStore(context.Background(), bin)
	if err != nil {
		t.Fatal(err)
	}
	if reality != RealityBroken {
		t.Fatalf("reality = %q, want broken", reality)
	}
	// The gate that gates transactions must not block this host.
	if err := NewCredentialAdapter("codex", bin).CheckBackend(); err != nil {
		t.Fatalf("a broken stored credential blocked sign-in: %v", err)
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
	keyringBin, _ := fakeDoctorBin(t, "keyring-backend.json")
	if err := NewCredentialAdapter("codex", keyringBin).CheckBackend(); !errors.Is(err, providerauth.ErrUnsupportedBackend) {
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
