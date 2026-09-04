package daemon

import (
	"fmt"
	"os"
	"runtime"
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
	stdinBefore, stdinStatOK := stdinIdentity()

	code := m.Run()
	_ = os.RemoveAll(dir)

	// Finalizers are the whole failure mode, so collect before checking: a
	// descriptor leaked into an *os.File is not closed until its wrapper is
	// collected, and a check that runs first sees nothing wrong.
	runtime.GC()
	runtime.GC()

	if stdinStatOK && !stdinIsIntact(stdinBefore) {
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

// stdinIdentity snapshots what stdin is, so the end of the run can tell whether
// it is still the same thing.
//
// os.Stdin rather than a raw descriptor number: syscall.Stat_t does not exist
// on Windows, and this repository runs Windows CI. It is also the stronger
// check — a bare fstat(0) succeeds once the freed descriptor has been handed to
// another file, which is precisely the state this guards against.
func stdinIdentity() (os.FileInfo, bool) {
	fi, err := os.Stdin.Stat()
	return fi, err == nil
}

// stdinIsIntact reports whether stdin is still the file it was at startup.
//
// Two failures, one answer: the descriptor was closed and left closed (Stat
// fails), or it was closed and reused by another file (Stat succeeds on
// something else, and SameFile says so).
//
// Deliberately stdin and not "a descriptor we opened": this guards process
// state that one test helper can corrupt for every other test in the package,
// which is exactly the class that went unnoticed because its victims were
// always somewhere else (MADR 0140).
func stdinIsIntact(before os.FileInfo) bool {
	after, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return os.SameFile(before, after)
}
