package procutil_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

// selfArgs runs this test binary with no tests selected: a child that exists on
// every platform, starts fast and exits 0. It is how these tests check that a
// constructed command actually runs, rather than only that its fields look
// right — the creation flags procutil.Command sets on Windows are exactly the
// kind of thing that inspects clean and fails to start.
func selfArgs() (string, []string) {
	return os.Args[0], []string{"-test.run=^$"}
}

// TestCommandBuildsARunnableCommand is the portable half of MADR 0159 D18.
func TestCommandBuildsARunnableCommand(t *testing.T) {
	name, args := selfArgs()
	cmd := procutil.Command(context.Background(), name, args...)

	if cmd.Path != name {
		t.Errorf("Path = %q, want %q", cmd.Path, name)
	}
	wantArgs := append([]string{name}, args...)
	if len(cmd.Args) != len(wantArgs) {
		t.Fatalf("Args = %q, want %q", cmd.Args, wantArgs)
	}
	for i := range wantArgs {
		if cmd.Args[i] != wantArgs[i] {
			t.Errorf("Args[%d] = %q, want %q", i, cmd.Args[i], wantArgs[i])
		}
	}
	if cmd.SysProcAttr == nil {
		t.Error("SysProcAttr is nil: no platform setup was applied")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("the constructed command did not run: %v\n%s", err, out)
	}
}

// TestCommandHonoursContextCancellation proves the constructor really is
// exec.CommandContext underneath, so callers keep the cancellation they had
// before the switch away from exec.CommandContext (0159 P17).
func TestCommandHonoursContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	name, args := selfArgs()
	err := procutil.Command(ctx, name, args...).Run()
	if err == nil {
		t.Fatal("Run() on an already-cancelled context returned nil")
	}
	if !errors.Is(err, context.Canceled) {
		// Windows reports the kill rather than the cause on some paths; an
		// *exec.ExitError is an acceptable shape, a nil error is not.
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Errorf("Run() error = %v, want context.Canceled or *exec.ExitError", err)
		}
	}
}
