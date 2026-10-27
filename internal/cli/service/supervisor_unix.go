//go:build unix

package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

// procRoot is where process command lines are read. It is a variable for
// tests.
var procRoot = "/proc"

// supervisorWatching reports whether a scanner (runsvdir or s6-svscan) of this
// user already watches dir. It is install.sh's
// `pgrep -u "$(id -u)" -f "runsvdir .*$DIR"` (:786, :801), read from /proc
// so it needs no pgrep, and matched on the argument rather than a pattern.
var supervisorWatching = func(scanner, dir string) bool {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return false
	}
	want := filepath.Clean(dir)
	uid := uint32(os.Getuid())
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		p := filepath.Join(procRoot, e.Name())
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); !ok || st.Uid != uid {
			continue
		}
		b, err := os.ReadFile(filepath.Join(p, "cmdline"))
		if err != nil {
			continue
		}
		argv := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
		if len(argv) < 2 || filepath.Base(argv[0]) != scanner {
			continue
		}
		for _, a := range argv[1:] {
			if filepath.Clean(a) == want {
				return true
			}
		}
	}
	return false
}

// startSessionSupervisor starts scanner on dir in a session of its own, so it
// outlives setup-service and the terminal it ran in, and returns its pid. Its
// output goes nowhere, as install.sh's `runsvdir "$DIR" >/dev/null 2>&1 &`.
// Nothing starts it at boot; the result says so.
var startSessionSupervisor = func(scanner, dir string) (int, error) {
	path, err := resolveTool(scanner)
	if err != nil {
		return 0, fmt.Errorf("start %s %s: %w", scanner, dir, err)
	}
	cmd := procutil.Command(context.Background(), path, dir)
	// A new session, not only a new process group: a process group still
	// receives the hangup when the session's terminal closes.
	cmd.SysProcAttr.Setpgid = false
	cmd.SysProcAttr.Setsid = true
	cmd.Env = withUserRuntimeEnv(os.Environ())
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start %s %s: %w", scanner, dir, err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		return pid, fmt.Errorf("release %s: %w", scanner, err)
	}
	return pid, nil
}
