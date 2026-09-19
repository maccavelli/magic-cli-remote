package procutil

import (
	"context"
	"os/exec"
)

// Command builds the *exec.Cmd for a child process. It is the only way a child
// process is started in this repository.
//
// It applies the platform's process-group setup ([SetProcessGroup]) and, on
// Windows, decides whether the child must be given CREATE_NO_WINDOW: a child of
// a process that has no console is handed a brand new console *with a visible
// window*, which is how detaching the daemon's own console (MADR 0159 D7) came
// to open one terminal window per agent session instead of none (0159 F22).
//
// Why a constructor rather than a function callers apply to a command they
// built: a post-hoc mutation can be forgotten, and was, three times — the job
// object had no caller at all until MADR 0150 (F1), five spawn sites never
// called [SetProcessGroup] (0159 F28), and launch.Command, which carries the
// Windows argument guard, had no caller either (0159 F29). A constructor cannot
// be skipped, because it is how the command is obtained. A test enforces that
// no other file reaches for exec.Command directly (0159 D19).
//
// Callers that need a command shaped differently — a batch shim needs its own
// command line, which os/exec cannot escape correctly (0159 D23) — still start
// here and adjust the result.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	return newCommand(ctx, name, args...)
}
