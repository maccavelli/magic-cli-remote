//go:build !windows

package launch

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// resolve is exec.LookPath: every executable on Unix is invoked directly.
func resolve(bin string) (Resolved, error) {
	p, err := exec.LookPath(bin)
	if err != nil {
		return Resolved{}, err
	}
	return Resolved{Path: p, Kind: KindNative}, nil
}

// command is exec.CommandContext. The length check is kept on both platforms
// so a pathological argv fails the same way everywhere.
func command(ctx context.Context, r Resolved, args ...string) (*exec.Cmd, error) {
	if n := commandLineLen(r.Path, args); n > maxCommandLine {
		return nil, fmt.Errorf("%w: %d characters", ErrCommandLineTooLong, n)
	}
	return exec.CommandContext(ctx, r.Path, args...), nil
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
