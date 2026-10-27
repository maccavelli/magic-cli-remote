//go:build unix

package updateclient

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// fakeSV is a stateful runit `sv`: up and down change a state file, up gives
// the service a new pid, and status reports them as `sv status` does. With
// <dir>/broken the service crash-loops: up leaves it down while runit wants it
// up. <dir> is the "fake" directory beside it: sv runs with a built
// environment, which carries no variable a test sets (MADR 0212 D2-A). Shell
// builtins only, since PATH holds nothing else.
const fakeSV = `d="${0%/*}/fake"
echo "$*" >> "$d/calls"
case "$1" in
status)
	s=down
	[ -f "$d/state" ] && read s < "$d/state"
	case "$s" in
	up) read p < "$d/pid"; echo "run: $2: (pid $p) 5s" ;;
	crash) echo "down: $2: 0s, normally up, want up" ;;
	*) echo "down: $2: 9s, normally up" ;;
	esac ;;
down) echo down > "$d/state" ;;
up)
	if [ -f "$d/broken" ]; then echo crash > "$d/state"; exit 0; fi
	n=0
	[ -f "$d/pid" ] && read n < "$d/pid"
	n=$((n+1))
	echo "$n" > "$d/pid"
	echo up > "$d/state" ;;
esac
exit 0`

// runitHost installs mcremote as a runit service on a scratch Linux host,
// through setup-service, so the update finds it by its record. state is the
// service's state before the update: "up" or "down".
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
	t.Setenv("MCREMOTE_SVC_WAIT_SECS", "3")
	bin := t.TempDir()
	testexec.WriteShellStub(t, filepath.Join(bin, "sv"), fakeSV)
	svDir = filepath.Join(bin, "fake")
	if err := os.Mkdir(svDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Cleanup(service.OverrideSupervisorToolDirs(bin))

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
		// A pid no process can have, so the inside check's walk up this
		// process's real /proc ancestry never matches it. PIDs on 64-bit
		// Linux stay below PID_MAX_LIMIT, 2^22; pid 1, which this was, is
		// every process's ancestor (MADR 0210 H2, L2 Deviation 1).
		writeFile(t, filepath.Join(svDir, "pid"), "4194304\n")
	}
	writeFile(t, filepath.Join(svDir, "state"), state+"\n")
	return svDir, filepath.Dir(res.UnitPath)
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

// TestUpdateRestartsARunitService is MADR 0054 amendment S5-3: `update` stops
// and starts a runit service around the swap, starts a stopped one only when
// it is enabled, and rolls the binary back when the new one does not stay up.
// Before S5f, the update saw no systemd unit, replaced the binary alone, and
// left runit running the old process.
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
