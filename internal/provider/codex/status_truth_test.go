package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
	"github.com/maccavelli/magic-cli-remote/internal/providerauth"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// TestStatusRequiresUsableCredentialNotJustAFile is a regression for what the
// operator saw on 2026-08-21: the phone showed Codex configured and green
// while the daemon was simultaneously refusing to sign in.
//
// The cause was that presence of ~/.codex/auth.json was the whole test. That
// host's file was the three-byte stub `{}` — a file, but not a credential. A
// green chip that means "a file exists here" is worse than no chip: it is
// confidently wrong.
func TestStatusRequiresUsableCredentialNotJustAFile(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"no file", "", provider.AuthMissing},
		{"empty stub", `{}`, provider.AuthMissing},
		{"whitespace", "   \n", provider.AuthMissing},
		{"unparseable", `not json`, provider.AuthMissing},
		{"null tokens and blank key", `{"OPENAI_API_KEY":"","tokens":null}`, provider.AuthMissing},
		{"real chatgpt session", `{"tokens":{"access_token":"a","refresh_token":"r"}}`, provider.AuthConfigured},
		{"real api key", `{"OPENAI_API_KEY":"sk-test"}`, provider.AuthConfigured},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("CODEX_HOME", home)
			if tc.body != "" {
				if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(tc.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			st, err := New(Config{Bin: "codex"}).AuthStatus(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if st.Status != tc.want {
				t.Fatalf("status = %q, want %q", st.Status, tc.want)
			}
			if len(st.Upstreams) != 1 {
				t.Fatalf("upstreams = %d, want 1", len(st.Upstreams))
			}
			if st.Upstreams[0].Status != tc.want {
				t.Fatalf("upstream status = %q, want %q", st.Upstreams[0].Status, tc.want)
			}
		})
	}
}

// TestStatusDoesNotSpawnAProcessPerCall guards a regression introduced while
// fixing the lockout: the backup projection began probing the CLI on every
// AuthStatus call, and providers.list calls that for every provider.
//
// The probe is worth running, but not once per list refresh.
func TestStatusDoesNotSpawnAProcessPerCall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// A binary that records every invocation.
	counter := filepath.Join(t.TempDir(), "calls")
	bin := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\necho x >> \"" + counter + "\"\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	coord := newTestCoordinator(t, bin)
	p := NewCoordinated(Config{Bin: bin}, nil, coord, nil)

	for range 5 {
		if _, err := p.AuthStatus(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	calls := 0
	if b, err := os.ReadFile(counter); err == nil {
		for _, c := range b {
			if c == '\n' {
				calls++
			}
		}
	}
	if calls > 1 {
		t.Fatalf("the CLI was probed %d times across 5 status calls; want at most 1", calls)
	}
}

// TestRealityProbeRefreshesAfterItsWindow proves the cache is a bound, not a
// freeze: a credential that appears is noticed.
func TestRealityProbeRefreshesAfterItsWindow(t *testing.T) {
	testexec.SkipIfNoPOSIXShell(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	bin, control := fakeDoctorBin(t, "incomplete-file.json")

	first, err := ObserveCredentialStoreCached(context.Background(), bin, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if first != RealityBroken {
		t.Fatalf("reality = %q, want broken", first)
	}

	// A usable credential lands; a zero window forces a fresh look.
	setDoctorFixture(t, control, "file-protected.json")
	got, err := ObserveCredentialStoreCached(context.Background(), bin, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got != RealityFileProtected {
		t.Fatalf("reality = %q, want file_protected after the credential appeared", got)
	}
}

// newTestCoordinator builds a coordinator over a temporary data directory.
func newTestCoordinator(t *testing.T, bin string) *providerauth.Coordinator {
	t.Helper()
	c, err := providerauth.NewCoordinator(t.TempDir(),
		NewCredentialAdapter("codex", bin), providerauth.CoordinatorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
