//go:build linux

package updateclient

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// fakeSV is a stateful runit `sv` over a real process, since the library's
// adapter knows a service by the PID and start time /proc reports (MADR 0212
// P5 Deviation 2). up starts /bin/sleep as the service and records its pid;
// down kills it; status reports them as `sv status` does. With <dir>/broken
// the service crash-loops: up leaves it down while runit wants it up. <dir>
// is the "fake" directory beside it: sv runs with a built environment, which
// carries no variable a test sets (MADR 0212 D2-A). Shell builtins only,
// since PATH holds nothing else, and /bin/sleep by its path.
const fakeSV = `d="${0%/*}/fake"
echo "$*" >> "$d/calls"
s=down
[ -f "$d/state" ] && read s < "$d/state"
case "$1" in
status)
	case "$s" in
	up) read p < "$d/pid"; echo "run: $2: (pid $p) 5s" ;;
	crash) echo "down: $2: 0s, normally up, want up" ;;
	*) echo "down: $2: 9s, normally up" ;;
	esac ;;
down)
	if [ "$s" = up ]; then read p < "$d/pid"; kill "$p"; fi
	echo down > "$d/state" ;;
up)
	if [ -f "$d/broken" ]; then echo crash > "$d/state"; exit 0; fi
	[ "$s" = up ] && exit 0
	/bin/sleep 600 </dev/null >/dev/null 2>&1 &
	echo "$!" > "$d/pid"
	echo up > "$d/state" ;;
esac
exit 0`

// runitHost installs mcremote as a runit service on a scratch Linux host,
// through setup-service, so the update finds it by its record. state is the
// service's state before the update: "up", with a real process, or "down".
func runitHost(t *testing.T, state string) (svDir, serviceDir string) {
	t.Helper()
	t.Cleanup(service.OverrideInstallOS("linux"))
	t.Cleanup(service.OverrideDetectedKind(service.KindRunit))
	t.Cleanup(service.OverrideSupervisorWatching(func(string, string) bool { return true }))
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(k, filepath.Join(home, k))
	}
	// Above the adapter's 10 s settle window and runit's 45 s KillAfter (MADR
	// 0212 D17).
	t.Setenv("MCREMOTE_SVC_WAIT_SECS", "50")
	bin := t.TempDir()
	testexec.WriteShellStub(t, filepath.Join(bin, "sv"), fakeSV)
	svDir = filepath.Join(bin, "fake")
	if err := os.Mkdir(svDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Cleanup(service.OverrideSupervisorToolDirs(bin))
	t.Cleanup(func() { killService(t, svDir) })

	// Setup checks only the executable bit; the stub is never run here.
	placeholder := testexec.WriteShellStub(t, filepath.Join(t.TempDir(), "mcremote"), "")
	res, err := service.Setup(service.Options{Product: "mcremote", Binary: placeholder, NoStart: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Scope != string(service.KindRunit) {
		t.Fatalf("setup used %s, want runit", res.Scope)
	}
	if state == "up" {
		// The running service, a process of its own: not this one, nor an
		// ancestor, so the adapter's inside check finds this process outside.
		p := exec.Command("/bin/sleep", "600")
		if err := p.Start(); err != nil {
			t.Fatal(err)
		}
		// Reaped here, so a kill by the fake leaves no zombie /proc still
		// shows.
		go func() { _ = p.Wait() }()
		writeFile(t, filepath.Join(svDir, "pid"), strconv.Itoa(p.Process.Pid)+"\n")
	}
	writeFile(t, filepath.Join(svDir, "state"), state+"\n")
	return svDir, filepath.Dir(res.UnitPath)
}

// killService ends the process the fake last recorded, if it still runs.
func killService(t *testing.T, svDir string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(svDir, "pid"))
	if err != nil {
		return
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// svCalls are the sv commands the update ran, without the status probes.
func svCalls(t *testing.T, svDir string) []string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(svDir, "calls"))
	var out []string
	for c := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
		if c != "" && !strings.HasPrefix(c, "status ") {
			out = append(out, strings.Fields(c)[0])
		}
	}
	return out
}

// TestUpdateRestartsARunitService is MADR 0054 amendment S5-3, through the
// library's adapter since MADR 0212 P5: `update` stops and starts a runit
// service around the swap, starts a stopped one only when it is enabled, and
// rolls the binary back when the new one does not stay up. Before S5f, the
// update saw no systemd unit, replaced the binary alone, and left runit
// running the old process.
func TestUpdateRestartsARunitService(t *testing.T) {
	receipt := libservice.Output{Stdout: refreshStdout(t, service.RefreshResult{Verdict: service.VerdictUnchanged})}
	t.Run("running is restarted", func(t *testing.T) {
		svDir, _ := runitHost(t, "up")
		withChild(t, receipt)
		exe, err := runUpdate(t)
		if err != nil {
			t.Fatal(err)
		}
		requireReplaced(t, exe)
		if got := svCalls(t, svDir); !slices.Equal(got, []string{"down", "up"}) {
			t.Fatalf("sv calls %v, want down then up", got)
		}
	})
	t.Run("enabled and stopped is started", func(t *testing.T) {
		svDir, _ := runitHost(t, "down")
		withChild(t, receipt)
		exe, err := runUpdate(t)
		if err != nil {
			t.Fatal(err)
		}
		requireReplaced(t, exe)
		if got := svCalls(t, svDir); !slices.Equal(got, []string{"up"}) {
			t.Fatalf("sv calls %v, want up", got)
		}
	})
	t.Run("disabled and stopped stays stopped", func(t *testing.T) {
		svDir, serviceDir := runitHost(t, "down")
		writeFile(t, filepath.Join(serviceDir, "down"), "")
		withChild(t, receipt)
		exe, err := runUpdate(t)
		if err != nil {
			t.Fatal(err)
		}
		requireReplaced(t, exe)
		if got := svCalls(t, svDir); len(got) != 0 {
			t.Fatalf("sv calls %v, want none", got)
		}
	})
	t.Run("a new binary that does not stay up is rolled back", func(t *testing.T) {
		svDir, _ := runitHost(t, "up")
		writeFile(t, filepath.Join(svDir, "broken"), "")
		withChild(t, receipt)
		exe, err := runUpdate(t)
		if err == nil {
			t.Fatal("the update succeeded although the new binary crash-loops")
		}
		got, rerr := os.ReadFile(exe)
		if rerr != nil || string(got) != "mcremote v0.1.0" {
			t.Fatalf("after the failed update the executable is %q, %v; want the old one restored", got, rerr)
		}
	})
}
