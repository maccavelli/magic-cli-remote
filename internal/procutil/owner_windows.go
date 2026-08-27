//go:build windows

package procutil

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

// SetDeathSignal is a no-op on Windows: there is no PR_SET_PDEATHSIG
// equivalent. Orphan prevention here is the job object instead — see
// [SuperviseStarted], which kills the whole descendant tree when the job
// handle closes (MADR 0116 D8).
func SetDeathSignal(cmd *exec.Cmd) {}

// OwnerToken returns "pid:creationTime" for the calling process, mirroring the
// Linux pid:starttime shape so a recycled pid is detectable.
func OwnerToken() string {
	pid := os.Getpid()
	if ct, ok := ProcessStartToken(pid); ok {
		return strconv.Itoa(pid) + ":" + ct
	}
	return strconv.Itoa(pid)
}

// OwnerAlive reports whether the token's process is still running.
//
// Liveness is WaitForSingleObject(h, 0) == WAIT_TIMEOUT: a running process
// never signals its own handle. GetExitCodeProcess + STILL_ACTIVE is NOT used
// — STILL_ACTIVE is not exported by x/sys/windows or syscall at this version,
// and a process that legitimately exits with code 259 would be
// indistinguishable from a running one.
//
// When the token carries a creation time, a pid whose creation time differs
// reads as dead, so a recycled pid cannot authorise destructive action.
func OwnerAlive(token string) bool {
	pidStr, rest, hasTime := strings.Cut(token, ":")
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	if hasTime && rest != "" {
		got, ok := creationTime(h)
		if !ok || got != rest {
			return false
		}
	}
	event, err := windows.WaitForSingleObject(h, 0)
	if err != nil {
		return false
	}
	return event == uint32(windows.WAIT_TIMEOUT)
}

// processAlive reports whether pid names a running process.
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	event, err := windows.WaitForSingleObject(h, 0)
	return err == nil && event == uint32(windows.WAIT_TIMEOUT)
}

// creationTime returns the process creation time as a decimal string.
func creationTime(h windows.Handle) (string, bool) {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return "", false
	}
	return fmt.Sprintf("%d", creation.Nanoseconds()), true
}

// ProcessEnv is unavailable on Windows.
//
// Reading another process's environment there requires
// NtQueryInformationProcess plus a cross-bitness PEB walk — deliberately out
// of scope (MADR 0116 D15). Callers degrade to "nothing to reap" rather than
// to a wrong answer.
func ProcessEnv(pid int) (map[string]string, bool) { return nil, false }

// FindByEnv is unavailable on Windows, for the same reason as [ProcessEnv].
func FindByEnv(key string) []int { return nil }
