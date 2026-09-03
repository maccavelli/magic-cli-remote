package credstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/fsutil"
)

// TestAuthLockPathsFlockTheFileTheProviderHonors pins the only property that
// matters about a native lock path: which file ends up flocked (MADR 0133).
//
// The pre-0133 tests asserted the string these functions return and passed
// while the daemon locked `auth.json.lock.lock` — because fsutil.WithLock
// appends `.lock` itself, and both functions had already applied it. Grok's own
// writer honors `auth.json.lock`, so mcremote held a lock nothing else took and
// raced the refresh it was supposed to serialize against (MADR 0074 D25/F12).
func TestAuthLockPathsFlockTheFileTheProviderHonors(t *testing.T) {
	t.Setenv("HOME", "/tmp/decoy-home")
	codexHome := t.TempDir()
	grokHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("GROK_HOME", grokHome)

	cases := []struct {
		name string
		home string
		path func() (string, error)
	}{
		{"codex", codexHome, CodexAuthLockPath},
		{"grok", grokHome, GrokAuthLockPath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := tc.path()
			if err != nil {
				t.Fatal(err)
			}
			if err := fsutil.WithLock(p, time.Second, func() error { return nil }); err != nil {
				t.Fatalf("WithLock(%q): %v", p, err)
			}
			want := filepath.Join(tc.home, "auth.json.lock")
			if _, err := os.Stat(want); err != nil {
				t.Errorf("no lock file at %s: %v", want, err)
			}
			if _, err := os.Stat(want + ".lock"); err == nil {
				t.Errorf("flocked %s instead of %s: the .lock suffix is applied twice",
					want+".lock", want)
			}
		})
	}
}
