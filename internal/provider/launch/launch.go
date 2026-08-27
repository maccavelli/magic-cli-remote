// Package launch resolves a configured engine binary and builds the command
// that runs it.
//
// It exists because Windows resolves an npm-installed CLI to a batch shim.
// exec.LookPath honours PATHEXT and finds foo.cmd, but syscall.StartProcess
// always passes argv0 as lpApplicationName, and CreateProcessW is explicit:
//
//	To run a batch file, you must start the command interpreter; set
//	lpApplicationName to cmd.exe and set lpCommandLine to the following
//	arguments: /c plus the name of the batch file.
//
// Routing through cmd.exe re-introduces its metacharacter interpretation over
// arguments the daemon builds from session state, so [Command] refuses an
// argument cmd.exe would reinterpret rather than hoping quoting holds
// (MADR 0116 D11).
//
// On Unix every function here is a thin pass-through: behaviour is unchanged.
package launch

import (
	"context"
	"errors"
	"os/exec"
)

// Kind classifies how a resolved executable must be invoked.
type Kind int

const (
	// KindNative is a directly executable image (ELF, Mach-O, PE).
	KindNative Kind = iota
	// KindBatch is a Windows .bat/.cmd shim, which needs cmd.exe /c.
	KindBatch
)

// String renders a Kind for error messages and tests.
func (k Kind) String() string {
	if k == KindBatch {
		return "batch"
	}
	return "native"
}

// Resolved is the outcome of resolving a configured engine binary name.
type Resolved struct {
	// Path is the absolute path to the resolved file.
	Path string
	// Kind says how it must be invoked.
	Kind Kind
}

// ErrUnsafeBatchArgs is returned when a .bat/.cmd must be invoked through
// cmd.exe but an argument contains a character cmd.exe would reinterpret.
var ErrUnsafeBatchArgs = errors.New("launch: argument unsafe for cmd.exe")

// ErrCommandLineTooLong is returned when the assembled command line would
// exceed the CreateProcessW limit.
var ErrCommandLineTooLong = errors.New("launch: command line exceeds 32767 characters")

// maxCommandLine is the CreateProcessW lpCommandLine ceiling, including the
// terminating NUL.
const maxCommandLine = 32767

// Resolve finds bin on PATH and classifies it.
func Resolve(bin string) (Resolved, error) { return resolve(bin) }

// Command builds an *exec.Cmd for a Resolved plus args.
func Command(ctx context.Context, r Resolved, args ...string) (*exec.Cmd, error) {
	return command(ctx, r, args...)
}

// IsExecutableFile reports whether path names a file this platform can run.
//
// On Unix that is the POSIX executable bit. On Windows there is no such bit —
// os.Stat reports 0666 for every regular file — so the question is whether the
// extension is one the loader will execute, which is what PATHEXT lists
// (MADR 0116 D24).
func IsExecutableFile(path string) bool { return isExecutableFile(path) }
