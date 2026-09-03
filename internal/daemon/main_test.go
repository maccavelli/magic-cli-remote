package daemon

import (
	"fmt"
	"os"
	"testing"
)

// TestMain points every home-derived path at a throwaway directory for the
// whole package.
//
// This is a floor, not a convenience. Tests here call the real Run(), which
// boots the daemon against config.Defaults() — and those defaults enable the
// providers, so startup reconciles the credential stores under $HOME. With the
// developer's real HOME in scope, TestRunAllowsClientKeyWithTLS rewrote
// ~/.config/goose/config.yaml and tightened the permissions on ~/.codex,
// ~/.grok and ~/.config/mcremote on every run. `cfg.DataDir = t.TempDir()`
// isolates mcremote's own state and nothing else.
//
// Setting this per-test is what failed: the same lesson as the codex fake-CLI
// helper, which wrote "$HOME/.codex/auth.json" and destroyed a live ChatGPT
// credential because one of three callers forgot to isolate. A test that
// forgets should be contained by default, so the isolation lives here where no
// caller can omit it. Individual tests may still narrow further with t.Setenv.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mcremote-daemon-test-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon tests: cannot create an isolated home: %v\n", err)
		os.Exit(1)
	}
	// Every variable the provider credential paths resolve through. Missing one
	// silently reopens the hole, so the list is exhaustive rather than minimal.
	for k, v := range map[string]string{
		"HOME":            dir,
		"XDG_CONFIG_HOME": dir + "/.config",
		"XDG_DATA_HOME":   dir + "/.local/share",
		"XDG_CACHE_HOME":  dir + "/.cache",
		"XDG_STATE_HOME":  dir + "/.local/state",
		"CODEX_HOME":      dir + "/.codex",
		"GROK_HOME":       dir + "/.grok",
	} {
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintf(os.Stderr, "daemon tests: cannot set %s: %v\n", k, err)
			os.Exit(1)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
