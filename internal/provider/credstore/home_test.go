package credstore

import (
	"path/filepath"
	"testing"
)

// TestEffectiveHomesPreferProviderVariable is the F7/F10 gate: mcremote must
// inspect the same credential home the provider CLI mutates. Resolving $HOME
// unconditionally is a deterministic bug on any host that sets CODEX_HOME or
// GROK_HOME (MADR 0074 D22).
func TestEffectiveHomesPreferProviderVariable(t *testing.T) {
	cases := []struct {
		name     string
		env      string
		home     func() (string, error)
		authPath func() (string, error)
		fallback string
	}{
		{"codex", "CODEX_HOME", CodexHome, CodexAuthPath, ".codex"},
		{"grok", "GROK_HOME", GrokHome, GrokAuthPath, ".grok"},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/provider variable wins", func(t *testing.T) {
			// Conflicting values: the provider variable must win outright.
			t.Setenv("HOME", "/tmp/decoy-home")
			custom := t.TempDir()
			t.Setenv(tc.env, custom)

			got, err := tc.home()
			if err != nil {
				t.Fatal(err)
			}
			if got != custom {
				t.Fatalf("home = %q, want %q", got, custom)
			}
			auth, err := tc.authPath()
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(custom, "auth.json"); auth != want {
				t.Fatalf("auth path = %q, want %q", auth, want)
			}
		})

		t.Run(tc.name+"/falls back to HOME", func(t *testing.T) {
			base := t.TempDir()
			t.Setenv("HOME", base)
			t.Setenv(tc.env, "")

			got, err := tc.home()
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(base, tc.fallback); got != want {
				t.Fatalf("home = %q, want %q", got, want)
			}
		})

		t.Run(tc.name+"/blank provider variable falls back", func(t *testing.T) {
			base := t.TempDir()
			t.Setenv("HOME", base)
			t.Setenv(tc.env, "   ")

			got, err := tc.home()
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(base, tc.fallback); got != want {
				t.Fatalf("home = %q, want %q", got, want)
			}
		})
	}
}

// TestDerivedPathsShareOneHome proves every per-provider path is derived from
// the single effective-home result, so status, mutation, locking, and the child
// environment can never disagree about which file they mean.
func TestDerivedPathsShareOneHome(t *testing.T) {
	t.Setenv("HOME", "/tmp/decoy-home")
	codex := t.TempDir()
	grok := t.TempDir()
	t.Setenv("CODEX_HOME", codex)
	t.Setenv("GROK_HOME", grok)

	codexAuth, err := CodexAuthPath()
	if err != nil {
		t.Fatal(err)
	}
	codexLock, err := CodexAuthLockPath()
	if err != nil {
		t.Fatal(err)
	}
	// MADR 0133: the lock path is the base path WithLock derives the lock file
	// from, not the lock file itself.
	if filepath.Dir(codexAuth) != codex || codexLock != codexAuth {
		t.Fatalf("codex paths disagree: auth=%q lock=%q", codexAuth, codexLock)
	}

	grokAuth, err := GrokAuthPath()
	if err != nil {
		t.Fatal(err)
	}
	grokCfg, err := GrokConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	grokLock, err := GrokAuthLockPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(grokAuth) != grok || filepath.Dir(grokCfg) != grok {
		t.Fatalf("grok paths disagree: auth=%q cfg=%q", grokAuth, grokCfg)
	}
	if grokLock != grokAuth {
		t.Fatalf("grok lock = %q, want %q — WithLock appends the .lock suffix", grokLock, grokAuth)
	}
}

// TestChildEnvPointsAtIsolatedHome proves the child environment overlay names
// the provider's own home variable, which is what makes an isolated pending
// login possible at all (MADR 0074 D22).
func TestChildEnvPointsAtIsolatedHome(t *testing.T) {
	if got := CodexHomeEnv("/pending/home"); got != "CODEX_HOME=/pending/home" {
		t.Fatalf("codex env = %q", got)
	}
	if got := GrokHomeEnv("/pending/home"); got != "GROK_HOME=/pending/home" {
		t.Fatalf("grok env = %q", got)
	}
}
