//go:build unix

package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// scriptBackendHost is a scratch Linux host with kind's definition of
// mcremote installed, the fakes on PATH, and a fast settle.
func scriptBackendHost(t *testing.T, kind Kind) *scriptBackend {
	t.Helper()
	uninstallHost(t, "linux")
	fakeHost(t, fakeStub(t), map[string]map[string]int{
		"sv": {}, "s6-svc": {}, "s6-svstat": {}, "rc-service": {}, "openrc": {},
	})
	fastSettle(t)
	writeDefinition(t, kind, "mcremote", "mcremote", "/opt/mc/mcremote")
	return newScriptBackend(kind, "mcremote")
}

// fakeOutput makes the named fake print out.
func fakeOutput(t *testing.T, name, out string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(os.Getenv("FAKE_CFG"), name+".out"), []byte(out+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestScriptBackendInstalledAndEnabled: installed is the definition existing;
// enabled is runit's and s6's missing down file, or OpenRC's default runlevel,
// as setup-service enables them (MADR 0054 PLAN S5f).
func TestScriptBackendInstalledAndEnabled(t *testing.T) {
	ctx := context.Background()
	for _, kind := range scriptKinds {
		t.Run(string(kind), func(t *testing.T) {
			b := scriptBackendHost(t, kind)
			if ok, err := b.Installed(ctx, "mcremote"); err != nil || !ok {
				t.Fatalf("Installed = %v, %v; want true", ok, err)
			}
			first, _ := b.Enabled(ctx, "mcremote")
			switch kind {
			case KindOpenRCUser:
				link := filepath.Join(filepath.Dir(b.def.ScanDir), "runlevels", "default", "mcremote")
				if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(b.def.Path, link); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(filepath.Join(b.def.Dir, "down"), nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			second, _ := b.Enabled(ctx, "mcremote")
			if kind == KindOpenRCUser && (first || !second) {
				t.Fatalf("OpenRC enabled %v before the runlevel link and %v after; want false, true", first, second)
			}
			if kind != KindOpenRCUser && (!first || second) {
				t.Fatalf("%s enabled %v before the down file and %v after; want true, false", kind, first, second)
			}
			if err := os.Remove(b.def.Path); err != nil {
				t.Fatal(err)
			}
			if ok, err := b.Installed(ctx, "mcremote"); err != nil || ok {
				t.Fatalf("Installed after removal = %v, %v; want false", ok, err)
			}
		})
	}
}

// TestScriptBackendStop: the stop command runs, and Stop returns once the
// service is down, or fails when it stays up.
func TestScriptBackendStop(t *testing.T) {
	ctx := context.Background()
	b := scriptBackendHost(t, KindRunit)
	// Outside the service: since MADR 0210 D4, Stop asks first, and off Linux
	// there is no /proc to ask.
	withAncestry(t, map[int]int{os.Getpid(): 1})
	fakeOutput(t, "sv", "down: "+b.def.Dir+": 1s, normally up")
	if err := b.Stop(ctx, "mcremote"); err != nil {
		t.Fatal(err)
	}
	if calls := fakeCalls(t); !slices.Contains(calls, "sv down "+b.def.Dir) {
		t.Fatalf("calls %v, want sv down", calls)
	}
	fakeOutput(t, "sv", "run: "+b.def.Dir+": (pid 9) 40s")
	prev := stopTimeout
	stopTimeout = 50 * time.Millisecond
	t.Cleanup(func() { stopTimeout = prev })
	if err := b.Stop(ctx, "mcremote"); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("a service that stays up: %v, want an error", err)
	}
}

// TestScriptBackendStart: through the user's own supervisor when one watches,
// otherwise by starting one for the session; OpenRC starts the user session
// when none runs.
func TestScriptBackendStart(t *testing.T) {
	ctx := context.Background()
	t.Run("runit, watched", func(t *testing.T) {
		b := scriptBackendHost(t, KindRunit)
		t.Cleanup(OverrideSupervisorWatching(func(string, string) bool { return true }))
		if err := b.Start(ctx, "mcremote"); err != nil {
			t.Fatal(err)
		}
		if calls := fakeCalls(t); !slices.Equal(calls, []string{"sv up " + b.def.Dir}) {
			t.Fatalf("calls %v, want sv up", calls)
		}
	})
	t.Run("s6, not watched", func(t *testing.T) {
		b := scriptBackendHost(t, KindS6)
		t.Cleanup(OverrideSupervisorWatching(func(string, string) bool { return false }))
		starts := recordSessionStarts(t)
		if err := b.Start(ctx, "mcremote"); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(*starts, []string{"s6-svscan " + b.def.ScanDir}) || len(fakeCalls(t)) != 0 {
			t.Fatalf("starts %v, calls %v; want a session s6-svscan and no s6-svc", *starts, fakeCalls(t))
		}
	})
	t.Run("OpenRC, no session", func(t *testing.T) {
		b := scriptBackendHost(t, KindOpenRCUser)
		if err := b.Start(ctx, "mcremote"); err != nil {
			t.Fatal(err)
		}
		want := []string{"openrc --user boot", "openrc --user default", "rc-service --user -- mcremote start"}
		if calls := fakeCalls(t); !slices.Equal(calls, want) {
			t.Fatalf("calls %v, want %v", calls, want)
		}
	})
}

// TestScriptBackendWaitHealthy is S5c's settle: confirmed running is healthy;
// anything else is an error the update rolls back on.
func TestScriptBackendWaitHealthy(t *testing.T) {
	ctx := context.Background()
	b := scriptBackendHost(t, KindRunit)
	t.Setenv(waitEnv, "3")
	samples(t, sample{up: true, pid: 9, uptime: 5 * time.Second})
	if err := b.WaitHealthy(ctx, "mcremote"); err != nil {
		t.Fatalf("a running service: %v", err)
	}
	samples(t, sample{exited: true})
	if err := b.WaitHealthy(ctx, "mcremote"); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("a crash loop: %v, want an error", err)
	}
}

// TestNewBackendFollowsTheRecord: on Linux the backend is the recorded
// supervisor's; with systemd recorded, or nothing recorded on a systemd host,
// it is the library's.
func TestNewBackendFollowsTheRecord(t *testing.T) {
	uninstallHost(t, "linux")
	b, err := NewBackend("mcremote", "")
	if _, ok := b.(*scriptBackend); ok {
		t.Fatal("with nothing recorded on a systemd host, NewBackend gave a script backend")
	}
	// The library's systemd backend refuses to be built off Linux.
	if runtime.GOOS == "linux" && err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" && (err == nil || !strings.Contains(err.Error(), "systemd")) {
		t.Fatalf("off Linux: %v, want the library's systemd refusal", err)
	}
	if err := recordBackend("mcremote", KindRunit); err != nil {
		t.Fatal(err)
	}
	b, err = NewBackend("mcremote", "")
	sb, ok := b.(*scriptBackend)
	if err != nil || !ok || sb.kind != KindRunit || sb.name != "mcremote" {
		t.Fatalf("with runit recorded: %T %+v, %v; want the runit script backend", b, b, err)
	}
}

// TestProbeStatusOnRunit: doctor names the run script and the runit command.
func TestProbeStatusOnRunit(t *testing.T) {
	b := scriptBackendHost(t, KindRunit)
	if err := recordBackend("mcremote", KindRunit); err != nil {
		t.Fatal(err)
	}
	fakeOutput(t, "sv", "down: "+b.def.Dir+": 3s")
	st := ProbeStatus("mcremote")
	if st.Kind != KindRunit || st.PlistOrUnit != b.def.Path || !st.PlistPresent || !st.Enabled || st.Active {
		t.Fatalf("status %+v, want runit, installed, enabled, not running", st)
	}
	if !strings.Contains(st.Hint, "sv up "+b.def.Dir) {
		t.Fatalf("hint %q, want the runit start command", st.Hint)
	}
	// IsInstalled, which update asks when no backend can be built, reads the
	// same definition.
	if ok, err := IsInstalled("mcremote"); err != nil || !ok {
		t.Fatalf("IsInstalled = %v, %v; want the runit definition found", ok, err)
	}
}

// withAncestry makes parentOf answer from parents, and fail for any other PID.
func withAncestry(t *testing.T, parents map[int]int) {
	t.Helper()
	prev := parentOf
	parentOf = func(pid int) (int, error) {
		if p, ok := parents[pid]; ok {
			return p, nil
		}
		return 0, os.ErrNotExist
	}
	t.Cleanup(func() { parentOf = prev })
}

// supervise makes b's supervisor report the service up as pid: `sv status`
// and `s6-svstat` print it, and OpenRC's pidfile holds it, at the path the
// OpenRC script renders.
func supervise(t *testing.T, b *scriptBackend, pid int) {
	t.Helper()
	switch b.kind {
	case KindRunit:
		fakeOutput(t, "sv", "run: "+b.def.Dir+": (pid "+strconv.Itoa(pid)+") 40s")
	case KindS6:
		fakeOutput(t, "s6-svstat", "up (pid "+strconv.Itoa(pid)+" pgid "+strconv.Itoa(pid)+") 40 seconds")
	case KindOpenRCUser:
		rd := t.TempDir()
		t.Setenv("XDG_RUNTIME_DIR", rd)
		if err := os.WriteFile(filepath.Join(rd, b.name+".pid"), []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// stopCalls are the calls that would stop b's service.
func stopCalls(calls []string) []string {
	var out []string
	for _, c := range calls {
		if strings.HasPrefix(c, "sv down") || strings.HasPrefix(c, "s6-svc -d") || strings.HasSuffix(c, " stop") {
			out = append(out, c)
		}
	}
	return out
}

// TestScriptBackendStopRefusesFromInside is MADR 0210 D4. An update an agent
// runs descends from the supervised daemon. Stopping it would end the agent's
// process group, and the update with it, and leave the service down: R1
// reproduced exactly that under runit, s6 and OpenRC. Stop refuses first,
// with the library's ErrInsideService, so the managed installer leaves the
// service running and fails the update.
func TestScriptBackendStopRefusesFromInside(t *testing.T) {
	ctx := context.Background()
	for _, kind := range scriptKinds {
		t.Run(string(kind), func(t *testing.T) {
			b := scriptBackendHost(t, kind)
			supervise(t, b, 4242)
			withAncestry(t, map[int]int{os.Getpid(): 100, 100: 4242, 4242: 1})
			err := b.Stop(ctx, "mcremote")
			if !errors.Is(err, libservice.ErrInsideService) {
				t.Fatalf("Stop = %v, want ErrInsideService", err)
			}
			if s := stopCalls(fakeCalls(t)); len(s) != 0 {
				t.Fatalf("a refused stop still ran %v", s)
			}
		})
	}
}

// TestScriptBackendStopsFromOutside: an operator's update, outside the
// service, stops it as before.
func TestScriptBackendStopsFromOutside(t *testing.T) {
	ctx := context.Background()
	b := scriptBackendHost(t, KindRunit)
	supervise(t, b, 4242)
	withAncestry(t, map[int]int{os.Getpid(): 100, 100: 1})
	prev := stopTimeout
	stopTimeout = 50 * time.Millisecond
	t.Cleanup(func() { stopTimeout = prev })
	_ = b.Stop(ctx, "mcremote") // the fake reports it still up; only the call matters here
	if s := stopCalls(fakeCalls(t)); !slices.Equal(s, []string{"sv down " + b.def.Dir}) {
		t.Fatalf("stop calls %v, want sv down", s)
	}
}

// TestScriptBackendStopWhenNotRunning: a service with no supervised process
// has no inside to protect, so Stop runs as before.
func TestScriptBackendStopWhenNotRunning(t *testing.T) {
	ctx := context.Background()
	b := scriptBackendHost(t, KindRunit)
	fakeOutput(t, "sv", "down: "+b.def.Dir+": 1s, normally up")
	withAncestry(t, nil) // never consulted: there is no supervised PID to look for
	if err := b.Stop(ctx, "mcremote"); err != nil {
		t.Fatalf("Stop = %v, want nil", err)
	}
	if s := stopCalls(fakeCalls(t)); !slices.Equal(s, []string{"sv down " + b.def.Dir}) {
		t.Fatalf("stop calls %v, want sv down", s)
	}
}

// TestProcParentReadsTheRealParent pins the /proc reader the fakes above
// stand in for: a parent is the one the kernel reports. The child matters:
// under `go test` this process often leads no group of its own, so its
// parent and its process group can be the same number, while a child's
// parent (this process) and group (`go`'s) differ.
func TestProcParentReadsTheRealParent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc/<pid>/stat is Linux's; no script backend runs elsewhere")
	}
	got, err := procParent(os.Getpid())
	if err != nil || got != os.Getppid() {
		t.Fatalf("procParent(self) = %d, %v; want %d", got, err, os.Getppid())
	}
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	got, err = procParent(child.Process.Pid)
	if err != nil || got != os.Getpid() {
		t.Fatalf("procParent(child) = %d, %v; want %d", got, err, os.Getpid())
	}
}

// TestScriptBackendStopWhenAncestryIsUnreadable: an error reading this
// process's ancestry is returned, never read as "outside", as the library's
// HandOffIfInside treats Inside's errors: proceeding could stop the caller's
// own service.
func TestScriptBackendStopWhenAncestryIsUnreadable(t *testing.T) {
	ctx := context.Background()
	b := scriptBackendHost(t, KindRunit)
	supervise(t, b, 4242)
	withAncestry(t, map[int]int{}) // this process's parent cannot be read
	err := b.Stop(ctx, "mcremote")
	if err == nil || errors.Is(err, libservice.ErrInsideService) {
		t.Fatalf("Stop = %v, want the ancestry error", err)
	}
	if s := stopCalls(fakeCalls(t)); len(s) != 0 {
		t.Fatalf("a stop that could not tell inside from outside still ran %v", s)
	}
}
