//go:build windows

package procutil

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// TestOwnerAliveForLiveProcess is the direct MADR 0116 F7 regression. The
// pre-0116 fallback used Signal(0), which Windows does not support, so
// OwnerAlive returned false for EVERY live process — inverted, not merely
// weak.
func TestOwnerAliveForLiveProcess(t *testing.T) {
	if got := OwnerAlive(OwnerToken()); !got {
		t.Error("OwnerAlive(OwnerToken()) = false for this very process")
	}
}

// TestOwnerAliveRejectsRecycledPID proves the creation-time half of the token
// is load-bearing: the same pid with a different start time reads as dead.
func TestOwnerAliveRejectsRecycledPID(t *testing.T) {
	token := OwnerToken()
	if got := OwnerAlive(token + "0"); got {
		t.Error("OwnerAlive accepted a token with a mismatched creation time")
	}
	if got := OwnerAlive("999999999"); got {
		t.Error("OwnerAlive accepted an implausible pid")
	}
	if got := OwnerAlive("not-a-pid"); got {
		t.Error("OwnerAlive accepted a malformed token")
	}
}

// TestProcessStartToken proves a start token is available, which is what
// restores pid-recycle detection to Linux parity.
func TestProcessStartToken(t *testing.T) {
	tok, ok := ProcessStartToken(os.Getpid())
	if !ok || tok == "" {
		t.Fatalf("ProcessStartToken(self) = %q, %v; want a value", tok, ok)
	}
	if _, ok := ProcessStartToken(-1); ok {
		t.Error("ProcessStartToken accepted a negative pid")
	}
}

// TestSuperviseStartedKillsTree proves the job object kills a grandchild, the
// guarantee the no-op fallback never provided (MADR 0116 D8).
func TestSuperviseStartedKillsTree(t *testing.T) {
	// cmd.exe spawns ping as a child; killing the job must take both.
	cmd := exec.Command("cmd.exe", "/c", "ping -n 30 127.0.0.1 > NUL")
	SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	release, err := SuperviseStarted(cmd.Process)
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("SuperviseStarted: %v", err)
	}
	pid := cmd.Process.Pid
	if !processAlive(pid) {
		t.Fatal("child died before the job was closed")
	}

	release() // closing the job kills the tree

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			_ = cmd.Wait()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	t.Fatal("process survived the job object closing")
}

// TestSuperviseStartedNilProcess proves the nil guard matches the other
// entry points rather than panicking.
func TestSuperviseStartedNilProcess(t *testing.T) {
	release, err := SuperviseStarted(nil)
	if err != nil {
		t.Fatalf("SuperviseStarted(nil): %v", err)
	}
	release()
}

// TestTerminateProcessGroupNil mirrors the Unix contract for a nil process.
func TestTerminateProcessGroupNil(t *testing.T) {
	if !TerminateProcessGroup(nil, nil, time.Second) {
		t.Error("TerminateProcessGroup(nil) = false, want true")
	}
	if err := KillProcessGroup(nil); err != nil {
		t.Errorf("KillProcessGroup(nil) = %v, want nil", err)
	}
}

// --- MADR 0159 D17/D20: a child of a console-less process gets no window ---

// TestCreationFlagsAddNoWindowOnlyWithoutAConsole is acceptance criterion A16.
//
// The with-a-console row asserts an ABSENCE, which makes it look redundant next
// to the row below it. It is not: if CREATE_NO_WINDOW were set unconditionally,
// a child would stop sharing this process's console, CTRL_BREAK could no longer
// be addressed to it from here, and `mcremote serve` in a terminal would lose
// its graceful stop the same way the task-launched daemon did (0159 F24). That
// regression is invisible without this row.
func TestCreationFlagsAddNoWindowOnlyWithoutAConsole(t *testing.T) {
	withConsole := creationFlags(true)
	if withConsole&windows.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Error("creationFlags(true) lacks CREATE_NEW_PROCESS_GROUP")
	}
	if withConsole&windows.CREATE_NO_WINDOW != 0 {
		t.Errorf("creationFlags(true) = 0x%x, must NOT set CREATE_NO_WINDOW: "+
			"a child of a process that has a console inherits it, opens no window, "+
			"and stays reachable by CTRL_BREAK", withConsole)
	}

	withoutConsole := creationFlags(false)
	if withoutConsole&windows.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Error("creationFlags(false) lacks CREATE_NEW_PROCESS_GROUP")
	}
	if withoutConsole&windows.CREATE_NO_WINDOW == 0 {
		t.Errorf("creationFlags(false) = 0x%x, must set CREATE_NO_WINDOW: "+
			"without it Windows gives the child a fresh console with a visible "+
			"window (0159 F22)", withoutConsole)
	}
}

// consoleRoleEnv selects a helper role in the re-executed test binary. The
// window question needs three processes, not two: this test process cannot be
// assumed to lack a console, so a middle process drops its own and does the
// spawning.
const consoleRoleEnv = "GO_PROCUTIL_CONSOLE_ROLE"

// childConsoleMarker prefixes the one line the innermost process prints, so it
// can be found among the test framework's own output.
const childConsoleMarker = "PROCUTIL_CHILD console_hwnd="

var (
	testFreeConsole      = windows.NewLazySystemDLL("kernel32.dll").NewProc("FreeConsole")
	testGetConsoleWindow = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")
)

// TestConsoleRoleHelper is not a test. It is the re-executed helper for
// TestChildOfAConsolelessProcessGetsNoWindow, and does nothing unless a role is
// requested. exec.Command appears here rather than procutil.Command because the
// parent role must spawn through the code under test, and the outer test needs a
// plain spawn to set up the experiment — 0159 D19's ban covers non-test files.
func TestConsoleRoleHelper(t *testing.T) {
	switch os.Getenv(consoleRoleEnv) {
	case "":
		t.Skip("no helper role requested; this is not a test")

	case "child":
		hwnd, _, _ := testGetConsoleWindow.Call()
		fmt.Printf("%s0x%x\n", childConsoleMarker, hwnd)

	case "sleeper":
		// A child that drains on a polite stop. CTRL_BREAK_EVENT arrives as
		// SIGINT, not as a Windows-specific signal — syscall.SIGBREAK does not
		// exist on this platform (MADR 0159 F35).
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		if path := os.Getenv(readyFileEnv); path != "" {
			if err := os.WriteFile(path, []byte("ready"), 0o600); err != nil {
				fmt.Printf("SLEEPER_ERR %v\n", err)
				os.Exit(9)
			}
		}
		select {
		case <-sig:
			os.Exit(drainExitCode)
		case <-time.After(10 * time.Second):
			os.Exit(8) // never signalled
		}

	case "politestop":
		detachAndForgetConsole()
		cmd, exited, err := startSleeper()
		if err != nil {
			fmt.Printf("START_ERR %v\n", err)
			return
		}
		polite := TerminateProcessGroup(cmd.Process, exited, 5*time.Second)
		<-exited
		fmt.Printf("%s%v exit=%d\n", politeMarker, polite, cmd.ProcessState.ExitCode())
		// Probe 11's most important observation: borrowing the child's console
		// must not signal, or strand, the process doing the borrowing.
		fmt.Printf("%shasConsole=%v\n", parentAliveMarker, hasConsole())

	case "concurrentstop":
		detachAndForgetConsole()
		type result struct {
			polite bool
			code   int
		}
		results := make(chan result, 2)
		for range 2 {
			go func() {
				cmd, exited, err := startSleeper()
				if err != nil {
					results <- result{false, -1}
					return
				}
				polite := TerminateProcessGroup(cmd.Process, exited, 5*time.Second)
				<-exited
				results <- result{polite, cmd.ProcessState.ExitCode()}
			}()
		}
		for range 2 {
			r := <-results
			fmt.Printf("%s%v exit=%d\n", politeMarker, r.polite, r.code)
		}

	case "deadchild":
		detachAndForgetConsole()
		cmd, exited, err := startSleeper()
		if err != nil {
			fmt.Printf("START_ERR %v\n", err)
			return
		}
		_ = KillProcessGroup(cmd.Process)
		<-exited
		// The polite phase cannot reach a process that is already gone, and must
		// report that as "nothing left to do" rather than as a hard kill.
		polite := TerminateProcessGroup(cmd.Process, exited, time.Second)
		fmt.Printf("%s%v exit=%d\n", politeMarker, polite, cmd.ProcessState.ExitCode())

	case "parent":
		detachAndForgetConsole()
		cmd := Command(context.Background(), os.Args[0], "-test.run=^TestConsoleRoleHelper$")
		cmd.Env = append(os.Environ(), consoleRoleEnv+"=child")
		out, err := cmd.CombinedOutput()
		fmt.Printf("%s", out)
		if err != nil {
			fmt.Printf("CHILD_ERR %v\n", err)
		}
	}
}

// TestChildOfAConsolelessProcessGetsNoWindow is acceptance criterion A15, and
// the regression test for the window the owner saw open for every agent session
// on v0.18.1 (MADR 0159 F22, probe 8). It must fail without CREATE_NO_WINDOW.
func TestChildOfAConsolelessProcessGetsNoWindow(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestConsoleRoleHelper$")
	cmd.Env = append(os.Environ(), consoleRoleEnv+"=parent")
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf

	if err := cmd.Start(); err != nil {
		t.Skipf("cannot re-execute this test binary (%v); the window check needs "+
			"three processes and cannot run here", err)
	}
	waitErr := cmd.Wait()

	out := buf.String()
	idx := strings.Index(out, childConsoleMarker)
	if idx < 0 {
		t.Fatalf("the innermost process printed no %q line (wait err %v); output:\n%s",
			childConsoleMarker, waitErr, out)
	}
	got := strings.Fields(out[idx+len(childConsoleMarker):])[0]
	if got != "0x0" {
		t.Errorf("child console_hwnd = %s, want 0x0: a child started by a "+
			"console-less parent was given a console window, which is one terminal "+
			"window per agent session (0159 F22/F23)", got)
	}
}

// TestHasConsoleIsNotFooledByAPseudoconsole defends the choice of API in D17
// against the substitution a future reader is most likely to make, because
// GetConsoleWindow is the obvious call and it is wrong here.
//
// CONOUT$ is the independent oracle: it opens if and only if this process is
// attached to a console. Under a pseudoconsole host — Windows Terminal, VS Code,
// mintty, and the harness this was developed in — CONOUT$ opens while
// GetConsoleWindow returns 0, so hasConsole must say true where a window test
// would say false. Swap the implementation for GetConsoleWindow() == 0 and this
// fails wherever it can tell the difference.
func TestHasConsoleIsNotFooledByAPseudoconsole(t *testing.T) {
	h, openErr := windows.CreateFile(
		windows.StringToUTF16Ptr("CONOUT$"),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
	attached := openErr == nil
	if attached {
		defer windows.CloseHandle(h)
	}

	if got := hasConsole(); got != attached {
		t.Errorf("hasConsole() = %v, but CONOUT$ open = %v", got, attached)
	}

	hwnd, _, _ := testGetConsoleWindow.Call()
	if attached && hwnd == 0 && !hasConsole() {
		t.Error("hasConsole() = false for a pseudoconsole-hosted process: " +
			"this is the GetConsoleWindow mistake D17 exists to prevent")
	}
	t.Logf("console attached=%v console_hwnd=0x%x (a pseudoconsole reports 0)", attached, hwnd)
}

// --- MADR 0159 D22: a console-less parent still stops a child politely ---

const (
	// readyFileEnv names the file a sleeper touches once its signal handler is
	// installed. A file rather than a pipe: os/exec forbids reading a StdoutPipe
	// concurrently with Wait, and a fixed sleep would be a race dressed up as a
	// delay.
	readyFileEnv = "GO_PROCUTIL_READY_FILE"
	// drainExitCode is what a sleeper exits with when it drained on a signal
	// rather than being killed, which is how a test tells the two apart.
	drainExitCode = 7

	politeMarker      = "PROCUTIL_POLITE="
	parentAliveMarker = "PROCUTIL_PARENT_ALIVE "
)

// detachAndForgetConsole makes this process look like the task-launched daemon:
// no console, and no cached belief that it has one.
//
// Production code needs no such reset — `serve --detach-console` detaches before
// anything spawns, and DetachConsole calls NoteConsoleDetached — but a test
// process is already running by the time it decides to become the daemon.
func detachAndForgetConsole() {
	testFreeConsole.Call()
	consoleStateMu.Lock()
	consoleCached = nil
	consoleStateMu.Unlock()
}

// startSleeper launches a child that drains on a polite stop, and returns once
// that child's signal handler is actually installed.
func startSleeper() (*exec.Cmd, <-chan struct{}, error) {
	dir, err := os.MkdirTemp("", "procutil-ready")
	if err != nil {
		return nil, nil, err
	}
	ready := filepath.Join(dir, "ready")

	cmd := Command(context.Background(), os.Args[0], "-test.run=^TestConsoleRoleHelper$")
	cmd.Env = append(os.Environ(), consoleRoleEnv+"=sleeper", readyFileEnv+"="+ready)
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, nil, err
	}

	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		_ = os.RemoveAll(dir)
		close(exited)
	}()

	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			return cmd, exited, nil
		}
		select {
		case <-exited:
			return nil, nil, fmt.Errorf("sleeper exited before signalling readiness")
		default:
		}
		if time.Now().After(deadline) {
			_ = KillProcessGroup(cmd.Process)
			return nil, nil, fmt.Errorf("sleeper never became ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// runRole re-executes this test binary in the named helper role and returns
// everything it printed.
func runRole(t *testing.T, role string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestConsoleRoleHelper$")
	cmd.Env = append(os.Environ(), consoleRoleEnv+"="+role)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot re-execute this test binary (%v); role %q needs a child process", err, role)
	}
	waitErr := cmd.Wait()
	out := buf.String()
	if !strings.Contains(out, politeMarker) && !strings.Contains(out, childConsoleMarker) {
		t.Fatalf("role %q printed no marker (wait err %v); output:\n%s", role, waitErr, out)
	}
	return out
}

// TestPoliteStopWorksWithoutAConsole is acceptance criterion A18, and the
// regression test for MADR 0159 F24: from v0.18.1 a task-launched daemon could
// not deliver CTRL_BREAK at all, so every provider was hard-killed instead of
// asked to drain. Probe 11 is what this pins.
func TestPoliteStopWorksWithoutAConsole(t *testing.T) {
	out := runRole(t, "politestop")

	if want := politeMarker + "true"; !strings.Contains(out, want) {
		t.Errorf("wanted %q — the polite phase should have sufficed; got:\n%s", want, out)
	}
	if want := fmt.Sprintf("exit=%d", drainExitCode); !strings.Contains(out, want) {
		t.Errorf("wanted %q — the child should have drained on the signal rather than "+
			"being killed; got:\n%s", want, out)
	}
	// Borrowing the child's console must leave the borrower unsignalled and
	// console-less afterwards; holding it would silently deny the next child
	// CREATE_NO_WINDOW (0159 D17).
	if want := parentAliveMarker + "hasConsole=false"; !strings.Contains(out, want) {
		t.Errorf("wanted %q — the parent must survive and give the console back; got:\n%s",
			want, out)
	}
}

// TestPoliteStopIsSerialized is acceptance criterion A19. Console attachment is
// process-wide, so two providers stopping at once would detach each other's
// console mid-signal without the mutex.
func TestPoliteStopIsSerialized(t *testing.T) {
	out := runRole(t, "concurrentstop")

	if got := strings.Count(out, politeMarker+"true"); got != 2 {
		t.Errorf("polite stops that succeeded = %d, want 2; output:\n%s", got, out)
	}
	if got := strings.Count(out, fmt.Sprintf("exit=%d", drainExitCode)); got != 2 {
		t.Errorf("children that drained = %d, want 2; output:\n%s", got, out)
	}
}

// TestPoliteStopOnAnAlreadyDeadChild pins the Unix parity D22 restores: a group
// that is already gone is "nothing left to do", not a failed polite phase.
func TestPoliteStopOnAnAlreadyDeadChild(t *testing.T) {
	out := runRole(t, "deadchild")

	if want := politeMarker + "true"; !strings.Contains(out, want) {
		t.Errorf("wanted %q — an already-exited child reports the group gone, as "+
			"Unix does for ESRCH; got:\n%s", want, out)
	}
}
