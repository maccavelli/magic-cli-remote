package daemon

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
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
	fd0Open := standardFDOpen()

	code := m.Run()
	_ = os.RemoveAll(dir)

	// Finalizers are the whole failure mode, so collect before checking: a
	// descriptor leaked into an *os.File is not closed until its wrapper is
	// collected, and a check that runs first sees nothing wrong.
	runtime.GC()
	runtime.GC()

	if fd0Open && !standardFDOpen() {
		fmt.Fprintln(os.Stderr, fdZeroClosedMessage)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

// fdZeroClosedMessage explains a failure whose victim is never its cause.
const fdZeroClosedMessage = "daemon tests: file descriptor 0 was open before this package ran and is closed now.\n" +
	"Something here wrapped a standard descriptor in an os.File. os.NewFile's first argument\n" +
	"is a descriptor, not a path, and it attaches a finalizer that closes it; the descriptor is\n" +
	"then reused by the next file opened in the process and closed underneath its owner, which\n" +
	"surfaces as `bad file descriptor` on an unrelated test. See MADR 0140.\n" +
	"Use io.Discard or slog.DiscardHandler instead."

// standardFDOpen reports whether file descriptor 0 is still a live descriptor.
//
// Deliberately fd 0 and not "a descriptor we opened": this guards process state
// that one test helper can corrupt for every other test in the package, which
// is exactly the class that went unnoticed because its victims were always
// somewhere else (MADR 0140).
func standardFDOpen() bool {
	var st syscall.Stat_t
	return syscall.Fstat(0, &st) == nil
}
