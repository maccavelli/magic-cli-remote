//go:build darwin

package procutil

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// SetDeathSignal is a no-op on Darwin (no PR_SET_PDEATHSIG). Orphan
// prevention uses the engine registry and graceful shutdown.
func SetDeathSignal(cmd *exec.Cmd) {}

// OwnerToken returns "<pid>:<starttoken>" for the calling process.
func OwnerToken() string {
	pid := os.Getpid()
	st, ok := ProcessStartToken(pid)
	if !ok {
		return strconv.Itoa(pid)
	}
	return strconv.Itoa(pid) + ":" + st
}

// OwnerAlive reports whether the token's process incarnation still runs.
func OwnerAlive(token string) bool {
	pidStr, want, hasStart := strings.Cut(token, ":")
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return false
	}
	got, ok := ProcessStartToken(pid)
	if !ok {
		return false
	}
	if !hasStart {
		return true
	}
	return got == want
}

// ProcessEnv is unavailable without /proc; Darwin uses the engine registry.
func ProcessEnv(pid int) (map[string]string, bool) { return nil, false }

// FindByEnv is unavailable without /proc; Darwin uses the engine registry.
func FindByEnv(key string) []int { return nil }

// Signal 0 liveness helper for tests.
func processExists(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
