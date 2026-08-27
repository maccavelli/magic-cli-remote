//go:build !linux && !darwin && !windows

package procutil

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// SetDeathSignal is a no-op off Linux: there is no portable equivalent of
// PR_SET_PDEATHSIG. Orphan prevention there rests entirely on the graceful
// shutdown path (on Windows, on the Job Object instead — MADR 0116 D8).
func SetDeathSignal(cmd *exec.Cmd) {}

// OwnerToken returns the calling process's pid. Without a portable process
// start time this cannot distinguish a live process from a recycled pid, so
// [OwnerAlive] is correspondingly weaker here.
func OwnerToken() string { return strconv.Itoa(os.Getpid()) }

// OwnerAlive reports whether a process with the token's pid exists. It cannot
// tell an original process from one that reused its pid, so callers must not
// use it to authorise destructive action off Linux.
func OwnerAlive(token string) bool {
	pidStr, _, _ := strings.Cut(token, ":")
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// ProcessEnv is unavailable off Linux (no /proc/<pid>/environ).
func ProcessEnv(pid int) (map[string]string, bool) { return nil, false }

// FindByEnv is unavailable off Linux; it reports no processes so callers
// degrade to "nothing to reap" rather than to a wrong answer.
func FindByEnv(key string) []int { return nil }
