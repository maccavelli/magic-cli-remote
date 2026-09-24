// Package launch resolves a configured engine binary and builds the command
// that runs it.
//
// It exists because Windows resolves an npm-installed CLI to a batch shim, and
// a batch shim is interpreted by cmd.exe whatever the caller does about it.
//
// The interpreter is not optional and never was. CreateProcessW starts a .cmd
// directly — measured: exec.Command on npm's codex.cmd returns its version
// normally — because it spawns cmd.exe implicitly for a batch image. An earlier
// version of this comment quoted CreateProcessW's "to run a batch file, you must
// start the command interpreter" and concluded that routing through cmd.exe was
// how the interpreter got involved. It is not: it is how the interpreter gets
// CONTROLLED (MADR 0159 F33).
//
// That distinction is the whole point, because os/exec escapes arguments by
// CommandLineToArgvW's rules while cmd.exe parses by its own. Go does not
// reconcile the two and has no plan to (golang/go#68313, #69939); the sanctioned
// workaround is to build the command line yourself and hand it over as
// SysProcAttr.CmdLine. Until MADR 0159 this package had no caller at all
// (0159 F29), so nothing reconciled them here either: an argument of `a&calc`
// reached a shim as two commands and ran Calculator (0159 F30, probe 12). That
// is the BatBadBut class, CVE-2024-24576.
//
// So [Command] does three things on Windows (0159 D23): it invokes
// `cmd.exe /d /s /v:off /c`, it quotes every element into one command line it
// builds itself, and it refuses the handful of characters that quoting cannot
// neutralise. Quoting handles & | < > ^ ! ( ) and spaces; nothing handles a
// literal `%`, which expands inside quotes too.
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

// ErrUnsafeBatchArgs is returned when an argument to a .bat/.cmd cannot be
// represented — quoted or otherwise — without cmd.exe changing its meaning.
var ErrUnsafeBatchArgs = errors.New("launch: argument cannot be passed to a batch shim")

// ErrCommandLineTooLong is returned when the assembled command line would
// exceed the ceiling that applies to it. Which ceiling that is depends on who
// parses the line: see [maxCommandLineNative] and [maxCommandLineBatch].
var ErrCommandLineTooLong = errors.New("launch: command line too long")

// maxCommandLineNative is the CreateProcessW lpCommandLine ceiling, including
// the terminating NUL. It applies when the image is started directly.
const maxCommandLineNative = 32767

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
