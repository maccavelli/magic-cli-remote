//go:build !windows

package launch

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

// resolve is exec.LookPath: every executable on Unix is invoked directly.
func resolve(bin string) (Resolved, error) {
	p, err := exec.LookPath(bin)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{Path: p, Kind: KindNative}, nil
}

// command is procutil.Command plus the length check, which is kept on both
// platforms so a pathological argv fails the same way everywhere. There is no
// interpreter in the way here: every executable on Unix is invoked directly, so
// the batch rules in launch_windows.go have no counterpart.
func command(ctx context.Context, r Resolved, args ...string) (*exec.Cmd, error) {
	if n := commandLineLen(r.Path, args); n > maxCommandLineNative {
		return nil, fmt.Errorf("%w: %d characters", ErrCommandLineTooLong, n)
	}
	return procutil.Command(ctx, r.Path, args...), nil
}

// commandLineLen approximates the assembled command line length.
func commandLineLen(path string, args []string) int {
	n := len(path) + 1
	for _, a := range args {
		n += len(a) + 3 // separator plus worst-case quoting
	}
	return n
}

// isExecutableFile reports whether path carries a POSIX executable bit.
func isExecutableFile(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	return st.Mode()&0o111 != 0
}
