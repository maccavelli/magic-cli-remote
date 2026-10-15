package testexec

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// recordingTB records Fatalf instead of stopping the test, so a helper's
// refusal can be observed. Every other method is the real test's.
type recordingTB struct {
	testing.TB
	fatal string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.fatal = fmt.Sprintf(format, args...)
}

// The warm-up must run the stub, so macOS scans it before any test times it,
// and must never reach the body, so markers and counters see only the runs
// the test makes (MADR 0193 D1).
func TestWriteShellStubWarmsWithoutRunningTheBody(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "body-ran")
	WriteShellStub(t, filepath.Join(t.TempDir(), "stub"), `echo ran >> "`+marker+`"`)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the warm-up reached the stub's body")
	}
}

// A warm-up that did not go through the interpreter, or failed, must fail the
// test rather than pass silently.
func TestWarmUpThatDidNotRunIsRefused(t *testing.T) {
	SkipIfNoPOSIXShell(t)
	for _, tc := range []struct{ name, script, want string }{
		{"no guard, so no warm-up file", "#!/bin/sh\nexit 0\n", "did not create"},
		{"the warm-up run fails", "#!/bin/sh\nexit 3\n", "exit status 3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordingTB{TB: t}
			writeAndWarm(rec, filepath.Join(t.TempDir(), "stub"), tc.script)
			if !strings.Contains(rec.fatal, tc.want) {
				t.Fatalf("writeAndWarm failure = %q, want it to mention %q", rec.fatal, tc.want)
			}
		})
	}
}

// After the warm-up the stub behaves exactly as written: arguments reach the
// body and its exit status is the body's.
func TestWriteShellStubLeavesTheStubRunnable(t *testing.T) {
	stub := WriteShellStub(t, filepath.Join(t.TempDir(), "stub"), `echo "args:$*"`+"\nexit 7")
	out, err := exec.Command(stub, "doctor", "--json").Output()
	if got := strings.TrimSpace(string(out)); got != "args:doctor --json" {
		t.Fatalf("stub printed %q, want %q", got, "args:doctor --json")
	}
	var exit *exec.ExitError
	if err == nil || !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("stub exit = %v, want status 7", err)
	}
}

// The variable is set on the warm-up command only; the test process, and so
// every later run of the stub, never sees it.
func TestWarmUpVariableDoesNotLeak(t *testing.T) {
	WriteShellStub(t, filepath.Join(t.TempDir(), "stub"), "exit 0")
	if v, ok := os.LookupEnv(warmEnv); ok {
		t.Fatalf("%s=%q leaked into the test process", warmEnv, v)
	}
}

// WarmExecutable runs a binary once and fails the test when it cannot.
func TestWarmExecutableRunsTheBinary(t *testing.T) {
	SkipIfNoPOSIXShell(t)
	stub := WriteShellStub(t, filepath.Join(t.TempDir(), "bin"), `[ "$1" = version ] || exit 9`)
	rec := &recordingTB{TB: t}
	WarmExecutable(rec, stub, "version")
	if rec.fatal != "" {
		t.Fatalf("WarmExecutable failed on a runnable binary: %s", rec.fatal)
	}

	notExec := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(notExec, []byte("not a program\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec = &recordingTB{TB: t}
	WarmExecutable(rec, notExec)
	if rec.fatal == "" {
		t.Fatal("WarmExecutable accepted a file it cannot run")
	}
}
