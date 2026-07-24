//go:build linux

package procutil_test

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

func TestOwnerTokenIncludesStartTime(t *testing.T) {
	tok := procutil.OwnerToken()
	pidStr, start, ok := strings.Cut(tok, ":")
	if !ok {
		t.Fatalf("token %q has no start time; pid reuse would go undetected", tok)
	}
	if pidStr != strconv.Itoa(os.Getpid()) {
		t.Fatalf("token pid = %s, want %d", pidStr, os.Getpid())
	}
	if start == "" {
		t.Fatal("empty start time")
	}
}

func TestOwnerAliveForSelf(t *testing.T) {
	if !procutil.OwnerAlive(procutil.OwnerToken()) {
		t.Fatal("own token should report alive")
	}
}

func TestOwnerAliveFalseForExitedProcess(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	// Mint a token for it the same way the spawner would.
	env, ok := procutil.ProcessEnv(pid)
	_ = env
	if !ok {
		t.Skip("cannot read child environ")
	}
	tok := tokenFor(t, pid)
	if !procutil.OwnerAlive(tok) {
		t.Fatal("running child should report alive")
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	// Give the kernel a moment to drop it from /proc.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && procutil.OwnerAlive(tok) {
		time.Sleep(20 * time.Millisecond)
	}
	if procutil.OwnerAlive(tok) {
		t.Fatal("exited child still reports alive")
	}
}

// The whole point of stamping a start time: a token whose pid has been reused
// by a different process must NOT read as alive, or a reaper would signal an
// unrelated process.
func TestOwnerAliveFalseOnStartTimeMismatch(t *testing.T) {
	real := procutil.OwnerToken()
	pidStr, start, _ := strings.Cut(real, ":")
	bumped, err := strconv.ParseUint(start, 10, 64)
	if err != nil {
		t.Fatalf("parse start time %q: %v", start, err)
	}
	// Same live pid, different incarnation — exactly what pid reuse looks like.
	forged := pidStr + ":" + strconv.FormatUint(bumped+1, 10)
	if procutil.OwnerAlive(forged) {
		t.Fatal("token with a mismatched start time reported alive — pid reuse would be mistaken for a live owner")
	}
}

func TestOwnerAliveFalseForGarbage(t *testing.T) {
	for _, tok := range []string{"", "not-a-pid", "-1", "0", "999999999:1"} {
		if procutil.OwnerAlive(tok) {
			t.Fatalf("garbage token %q reported alive", tok)
		}
	}
}

func TestFindByEnvLocatesMarkedProcess(t *testing.T) {
	const key = "MCREMOTE_PROCUTIL_TEST_MARKER"
	cmd := exec.Command("/bin/sh", "-c", "while :; do sleep 0.05; done")
	cmd.Env = append(os.Environ(), key+"=yes")
	procutil.SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = procutil.KillProcessGroup(cmd.Process)
		_ = cmd.Wait()
	})
	time.Sleep(200 * time.Millisecond)

	found := false
	for _, pid := range procutil.FindByEnv(key) {
		if pid == cmd.Process.Pid {
			found = true
		}
	}
	if !found {
		t.Fatalf("marked process %d not found by FindByEnv", cmd.Process.Pid)
	}

	// And an unmarked process must not match.
	for _, pid := range procutil.FindByEnv(key) {
		if pid == os.Getpid() {
			t.Fatal("unmarked test process matched the marker scan")
		}
	}
}

// tokenFor mints an owner token for another pid, mirroring OwnerToken's shape.
func tokenFor(t *testing.T, pid int) string {
	t.Helper()
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		t.Fatalf("read stat: %v", err)
	}
	s := string(data)
	end := strings.LastIndex(s, ")")
	fields := strings.Fields(s[end+1:])
	return strconv.Itoa(pid) + ":" + fields[22-3]
}
