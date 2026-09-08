package httpagent

import (
	"context"
	"flag"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

// This file is MADR 0150 P4: evidence that a *provider's* engine is
// supervised. procutil's own TestSuperviseStartedKillsTree already proves the
// job object kills a tree; what regressed was the wiring — a call that was
// never made — so the assertions below run through Provider.startServer and
// Provider.Shutdown rather than calling procutil directly.
//
// Two guards arm the two helper processes. Both are set with t.Setenv, so they
// reach the engine through the os.Environ() that startServer passes down, and
// the engine passes the second one to its own child the same way.
const (
	helperServeEnv       = "MC_HELPER_SERVE_HEALTH"
	helperGrandchildEnv  = "MC_HELPER_GRANDCHILD"
	helperDescendantFile = "MC_HELPER_DESCENDANT_FILE"
)

// TestHelperProcessGrandchild is not a test. It is the descendant whose
// survival is the whole question: on Windows it is placed outside the engine's
// console process group, so terminating the engine leaves it running and only
// closing the job object takes it too.
//
// It reports itself by listening on a loopback port and writing that port to a
// file, rather than by writing a PID. Liveness is then a TCP dial, which is the
// same check on every platform — Unix has signal 0 and Windows does not, and a
// PID-based check would have had to fork exactly where this test is meant to be
// strongest.
func TestHelperProcessGrandchild(t *testing.T) {
	if os.Getenv(helperGrandchildEnv) != "1" {
		return
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(3)
	}
	defer ln.Close()
	announce(os.Getenv(helperDescendantFile), ln.Addr().(*net.TCPAddr).Port)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	// Bounded like acphttp's TestHelperProcessBlocks: long enough that the
	// assertions below are decisive, short enough that a failure to kill leaves
	// a process that reaps itself rather than one that outlives the run.
	time.Sleep(60 * time.Second)
}

// TestHelperProcessServesHealth is not a test. It is the engine: it spawns the
// grandchild above, then serves the dialect's health path so the provider's
// startup poll goes green and the real publish and teardown paths run.
//
// Both guards must hold: the arming variable, and procutil.EnvEngineID, which
// startServer adds to the spawned engine's environment and which no parent test
// process carries (the idiom MADR 0147 D10 settled on).
func TestHelperProcessServesHealth(t *testing.T) {
	if os.Getenv(helperServeEnv) != "1" || os.Getenv(procutil.EnvEngineID) == "" {
		return
	}
	port := flag.Arg(0)
	if port == "" {
		os.Exit(3)
	}
	exe, err := os.Executable()
	if err != nil {
		os.Exit(3)
	}
	gc := exec.Command(exe, "-test.run=^TestHelperProcessGrandchild$")
	// helperServeEnv is cleared for the child so that, whatever -test.run does,
	// it cannot re-enter this function and fork a second engine.
	gc.Env = append(os.Environ(), helperGrandchildEnv+"=1", helperServeEnv+"=0")
	// On Windows the grandchild gets its own console process group, which is
	// what makes the assertion in this file worth anything. Measured on
	// 2026-09-07: with the grandchild left in the engine's group, deleting the
	// supervision entirely still killed it, because TerminateProcessGroup's
	// CTRL_BREAK_EVENT is delivered to every process in the group. The job
	// object is the guarantee for descendants that event cannot reach — those
	// in another group, those that ignore it, and every descendant on the
	// escalation path, where TerminateProcess hits the direct child alone.
	//
	// Off Windows the grandchild stays in the engine's group deliberately: a
	// setpgid escape there is the documented residual gap (startServer's
	// supervision comment), not something the no-op release could close, and a
	// test that walked into it would fail on Unix for the right reason at the
	// wrong time.
	if runtime.GOOS == "windows" {
		procutil.SetProcessGroup(gc)
	}
	if err := gc.Start(); err != nil {
		os.Exit(3)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	})
	// pumpEvents starts as soon as the engine is published; serve it a stream
	// that simply stays open, so it parks instead of reconnect-looping for the
	// rest of the test.
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	})
	_ = http.ListenAndServe("127.0.0.1:"+port, mux)
}

// announce publishes the listening port to path, via a rename so the reader
// never sees a half-written file.
func announce(path string, port int) {
	if path == "" {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(port)), 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// supervisedDialect points ServeArgs at the engine helper and hands it the port
// startServer picked, as the trailing positional argument that the test
// binary's flag parsing leaves in flag.Args().
type supervisedDialect struct{ fakeDialect }

func (d *supervisedDialect) ServeArgs(port int) []string {
	return []string{"-test.run=^TestHelperProcessServesHealth$", strconv.Itoa(port)}
}

// dialable reports whether something is accepting on addr. It is the liveness
// test for the grandchild.
func dialable(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func waitFor(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return cond()
}

// TestEngineTeardownSupervisesTheDescendantTree is the P4 assertion: an engine
// started through the provider's real spawn path has a release stored on it,
// the provider's real teardown invokes that release, and the engine's own child
// does not outlive the teardown.
//
// What each assertion is worth differs by platform, and saying so is the point
// of this comment:
//
//   - "release is stored" and "teardown invoked it" hold everywhere. They are
//     the direct regression test for MADR 0150 F1 — a SuperviseStarted that
//     existed, worked, and had no caller.
//   - "the grandchild is gone" is decisive only on Windows, and only because
//     the helper puts the grandchild in its own console process group there
//     (see the comment at that call). On Unix the descendant dies from the
//     group signal and would die with the supervision removed entirely, so
//     that arm of the assertion proves TerminateProcessGroup works, which was
//     never in doubt.
func TestEngineTeardownSupervisesTheDescendantTree(t *testing.T) {
	descFile := filepath.Join(t.TempDir(), "descendant.port")
	t.Setenv(helperServeEnv, "1")
	t.Setenv(helperDescendantFile, descFile)

	p := NewWithLogger(&supervisedDialect{fakeDialect{id: "test"}}, Config{Bin: testBin(t)}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := p.startServer(ctx); err != nil {
		t.Fatalf("startServer: %v", err)
	}
	// Shutdown is the assertion below, not the cleanup; this only stops a
	// failing run from leaving an engine behind for its full lifetime.
	t.Cleanup(p.Shutdown)

	p.mu.Lock()
	eng := p.eng
	p.mu.Unlock()
	if eng == nil {
		t.Fatal("startServer returned without publishing an engine")
	}
	if eng.release == nil {
		t.Fatal("engine has no release: SuperviseStarted was not called at the spawn site (MADR 0150 F1)")
	}

	var addr string
	if !waitFor(func() bool {
		b, err := os.ReadFile(descFile)
		if err != nil {
			return false
		}
		port := strings.TrimSpace(string(b))
		if port == "" {
			return false
		}
		addr = "127.0.0.1:" + port
		return dialable(addr)
	}, 30*time.Second) {
		t.Fatal("engine's grandchild never came up; the test would assert nothing about its death")
	}

	released := make(chan struct{})
	orig := eng.release
	p.mu.Lock()
	// The real release is still called, so the job handle is not leaked when
	// this test's wrapper replaces the stored one.
	eng.release = func() { close(released); orig() }
	p.mu.Unlock()

	p.Shutdown()

	select {
	case <-released:
	default:
		t.Fatal("Shutdown did not invoke the engine's release (MADR 0150 D3/C3)")
	}
	if !waitFor(func() bool { return !dialable(addr) }, 30*time.Second) {
		t.Fatalf("grandchild at %s survived teardown: the engine's descendant tree was not killed", addr)
	}
}
