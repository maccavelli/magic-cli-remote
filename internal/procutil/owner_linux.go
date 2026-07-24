//go:build linux

package procutil

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// SetDeathSignal asks the kernel to SIGKILL cmd's process as soon as the
// calling process dies. This is the first line of defence against orphaned
// children when the parent is killed without running its shutdown path.
//
// It is deliberately best-effort. The kernel keys the death signal on the
// death of the *thread* that forked, and the Go runtime may retire that thread
// while the process lives on, which silently disarms it. Callers must not rely
// on this alone — pair it with a graceful shutdown path and a startup sweep.
func SetDeathSignal(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

// procStartTime reads field 22 (starttime, in clock ticks since boot) from
// /proc/<pid>/stat. Paired with a pid it identifies a specific process
// incarnation, which a bare pid cannot: pids are recycled, and acting on a
// recycled one means signalling an unrelated process.
func procStartTime(pid int) (string, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", false
	}
	// comm (field 2) is parenthesised and may itself contain spaces or
	// parens, so everything before the FINAL ')' has to be skipped.
	end := bytes.LastIndexByte(data, ')')
	if end < 0 {
		return "", false
	}
	fields := strings.Fields(string(data[end+1:]))
	// After the comm, fields[0] is field 3 (state), so field N is at N-3.
	const starttimeIndex = 22 - 3
	if len(fields) <= starttimeIndex {
		return "", false
	}
	return fields[starttimeIndex], true
}

// OwnerToken returns a token identifying the calling process that stops
// matching once it exits: "<pid>:<starttime>".
func OwnerToken() string {
	pid := os.Getpid()
	st, ok := procStartTime(pid)
	if !ok {
		return strconv.Itoa(pid)
	}
	return strconv.Itoa(pid) + ":" + st
}

// OwnerAlive reports whether the process a token was minted by is still
// running. A token whose pid exists but whose start time differs describes a
// process that has exited and had its pid reused — reported as not alive,
// which is what makes reaping by owner safe.
func OwnerAlive(token string) bool {
	pidStr, want, hasStart := strings.Cut(token, ":")
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return false
	}
	got, ok := procStartTime(pid)
	if !ok {
		return false
	}
	if !hasStart {
		// Token predates start-time stamping: pid liveness is all we have.
		return true
	}
	return got == want
}

// ProcessEnv reads the environment of a running process. Only same-uid
// processes are readable, which is exactly the set this process could have
// spawned.
func ProcessEnv(pid int) (map[string]string, bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		return nil, false
	}
	env := make(map[string]string)
	for _, kv := range bytes.Split(data, []byte{0}) {
		if len(kv) == 0 {
			continue
		}
		k, v, ok := strings.Cut(string(kv), "=")
		if ok {
			env[k] = v
		}
	}
	return env, true
}

// FindByEnv returns the pids of running processes whose environment contains
// key. Processes that cannot be read (other users, or exited mid-scan) are
// skipped silently.
func FindByEnv(key string) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		env, ok := ProcessEnv(pid)
		if !ok {
			continue
		}
		if _, found := env[key]; found {
			out = append(out, pid)
		}
	}
	return out
}
