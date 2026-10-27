package service

import (
	"bytes"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// fastSettle makes the settle loop tick without waiting, for one test.
func fastSettle(t *testing.T) {
	t.Helper()
	prev := settleTick
	settleTick = time.Millisecond
	t.Cleanup(func() { settleTick = prev })
}

// TestWaitSeconds is answer A3: unset waits install.sh's 15 seconds, a set
// value is honoured, and anything but a positive whole number is refused,
// naming the variable and the value.
func TestWaitSeconds(t *testing.T) {
	t.Setenv(waitEnv, "") // restores the caller's value afterwards
	if err := os.Unsetenv(waitEnv); err != nil {
		t.Fatal(err)
	}
	if n, err := waitSeconds(); err != nil || n != 15 {
		t.Fatalf("unset: %d, %v; want 15", n, err)
	}
	t.Setenv(waitEnv, "7")
	if n, err := waitSeconds(); err != nil || n != 7 {
		t.Fatalf("7: %d, %v; want 7", n, err)
	}
	for _, bad := range []string{"0", "-1", "abc", "", "1.5"} {
		t.Setenv(waitEnv, bad)
		_, err := waitSeconds()
		if err == nil || !strings.Contains(err.Error(), waitEnv) || !strings.Contains(err.Error(), `"`+bad+`"`) {
			t.Errorf("%q: %v, want a refusal naming %s and the value", bad, err, waitEnv)
		}
	}
}

// systemdShows feeds settleSystemd one `systemctl show` reply per call; the
// first call is the NRestarts baseline. The last reply repeats.
func systemdShows(t *testing.T, replies ...string) {
	t.Helper()
	prev := runSystemctlCapture
	i := 0
	runSystemctlCapture = func(args ...string) (string, error) {
		if len(args) < 2 || args[1] != "show" {
			return "", errors.New("unexpected systemctl " + strings.Join(args, " "))
		}
		r := replies[min(i, len(replies)-1)]
		i++
		return r, nil
	}
	t.Cleanup(func() { runSystemctlCapture = prev })
}

func show(active, sub string, restarts int) string {
	return "ActiveState=" + active + "\nSubState=" + sub + "\nNRestarts=" + strconv.Itoa(restarts) + "\n"
}

// TestSettleSystemd is install.sh's svc_settle: active must hold for three
// samples, and a restart counter that rises, a unit restarting over and over,
// or a failed unit is not running.
func TestSettleSystemd(t *testing.T) {
	fastSettle(t)
	cases := []struct {
		name    string
		replies []string
		want    string
	}{
		{"active three times", []string{show("active", "running", 0)}, HealthRunning},
		{"restarted while watched", []string{show("active", "running", 0), show("active", "running", 0), show("activating", "auto-restart", 1)}, HealthFailed},
		{"restarting before the watch", []string{show("activating", "auto-restart", 4)}, HealthFailed},
		{"failed", []string{show("activating", "start", 0), show("failed", "failed", 0)}, HealthFailed},
		{"still activating", []string{show("activating", "start", 0)}, HealthStarting},
		{"active, not sustained", []string{show("inactive", "dead", 0), show("active", "running", 0), show("active", "running", 0),
			show("inactive", "dead", 0), show("active", "running", 0), show("inactive", "dead", 0)}, HealthStarting},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			systemdShows(t, tc.replies...)
			if got, why := settleSystemd("mcremote.service", 5); got != tc.want {
				t.Fatalf("settleSystemd = %s (%s), want %s", got, why, tc.want)
			}
		})
	}
}

// samples feeds the settle loop one sample per call; the last repeats.
func samples(t *testing.T, ss ...sample) {
	t.Helper()
	prev := newSampler
	i := 0
	newSampler = func(Kind, Result) sampleFunc {
		return func() (sample, error) {
			s := ss[min(i, len(ss)-1)]
			i++
			return s, nil
		}
	}
	t.Cleanup(func() { newSampler = prev })
}

// TestSettleOtherKinds: up twice in a row with the same process, or up for two
// seconds by the manager's own count, is running; a process that changes while
// watched is restarting; never up is not running.
func TestSettleOtherKinds(t *testing.T) {
	fastSettle(t)
	cases := []struct {
		name string
		ss   []sample
		want string
	}{
		{"up twice, same pid", []sample{{up: true, pid: 9}}, HealthRunning},
		{"up two seconds", []sample{{}, {up: true, pid: 9, uptime: 2 * time.Second}}, HealthRunning},
		{"pid changes", []sample{{up: true, pid: 9}, {}, {up: true, pid: 10}}, HealthFailed},
		{"never up", []sample{{}}, HealthFailed},
		{"up once, then down", []sample{{up: true, pid: 9}, {}}, HealthStarting},
		{"reported failed", []sample{{up: true, pid: 9}, {failed: true}}, HealthFailed},
		{"exits twice", []sample{{exited: true}, {}, {exited: true}}, HealthFailed},
		{"exits after being up", []sample{{up: true, pid: 9}, {exited: true}}, HealthFailed},
		{"exits once, then up", []sample{{exited: true}, {up: true, pid: 9}}, HealthRunning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			samples(t, tc.ss...)
			if got, why := settle(KindRunit, Result{UnitName: "mcremote"}, 4); got != tc.want {
				t.Fatalf("settle = %s (%s), want %s", got, why, tc.want)
			}
		})
	}
}

// TestSamplersReadTheirManagers parses the status lines the 2026-10-19 live
// probes printed, and launchctl's. s6's are `s6-svstat -o`'s, the form the
// state reads (MADR 0212 D7-A), with the same samples the human form gave.
func TestSamplersReadTheirManagers(t *testing.T) {
	prev := startToken
	startToken = func(int) string { return "" } // fixture PIDs name no process here
	t.Cleanup(func() { startToken = prev })
	cases := []struct {
		name, out string
		re        func(string) (sample, bool)
		want      sample
	}{
		{"runit up", "run: /home/user/.local/share/runit/service/mcremote: (pid 87) 7s", parseRunit, sample{up: true, pid: 87, uptime: 7 * time.Second}},
		{"runit down", "down: /home/user/.local/share/runit/service/mcremote: 3s, normally up", parseRunit, sample{}},
		{"s6 up", "true true true false 45 -1 8", parseS6, sample{up: true, pid: 45, uptime: 8 * time.Second}},
		{"s6 down, want up", "false true true false -1 1 0", parseS6, sample{exited: true}},
		{"s6 down, want down", "false false true false -1 0 9", parseS6, sample{}},
		{"runit down, want up", "down: /home/user/.local/share/runit/service/mcremote: 1s, normally up, want up", parseRunit, sample{exited: true}},
		{"launchd running", "gui/501/x = {\n\tstate = running\n\tpid = 123\n}", parseLaunchd, sample{up: true, pid: 123}},
		{"launchd waiting", "gui/501/x = {\n\tstate = waiting\n}", parseLaunchd, sample{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := tc.re(tc.out); got != tc.want {
				t.Fatalf("parsed %+v, want %+v", got, tc.want)
			}
		})
	}
}

func parseRunit(out string) (sample, bool) {
	return sampleOf(parseRunitStatus(libservice.SupervisedState{}, out)), true
}

func parseS6(out string) (sample, bool) {
	obs, err := parseS6Status(libservice.SupervisedState{}, out)
	return sampleOf(obs), err == nil
}
func parseLaunchd(out string) (sample, bool) { return parseLaunchctlPrint(out), true }

// TestVerifyStart: running sets the health; still starting is a warning; not
// running is an error that says how to see why.
func TestVerifyStart(t *testing.T) {
	fastSettle(t)
	t.Setenv(waitEnv, "4")
	res := Result{UnitName: "mcremote", Label: "mcremote"}

	systemdShows(t, show("active", "running", 0))
	got, err := realVerifyStart(KindSystemdUser, res)
	if err != nil || got.Health != HealthRunning {
		t.Fatalf("running: %+v, %v", got, err)
	}

	systemdShows(t, show("activating", "start", 0))
	got, err = realVerifyStart(KindSystemdUser, res)
	if err != nil || got.Health != HealthStarting || len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "after 4s") {
		t.Fatalf("starting: %+v, %v; want a warning", got, err)
	}

	systemdShows(t, show("failed", "failed", 0))
	_, err = realVerifyStart(KindSystemdUser, res)
	if err == nil || !strings.Contains(err.Error(), "not running") || !strings.Contains(err.Error(), "journalctl --user -u mcremote") {
		t.Fatalf("failed: %v, want an error with the journal command", err)
	}
}

// TestPrintSetupResultReportsTheHealth: the result says whether the start was
// confirmed, for the unit and for a script kind, and prints the warnings.
func TestPrintSetupResultReportsTheHealth(t *testing.T) {
	for _, scope := range []string{string(KindSystemdUser), string(KindRunit)} {
		var out bytes.Buffer
		PrintSetupResult(&out, Result{Scope: scope, UnitName: "mcremote", Label: "mcremote", UnitPath: "/x/mcremote/run",
			Started: true, Health: HealthRunning}, false, "mcremote")
		if !strings.Contains(out.String(), "Running:           yes, confirmed after the start") {
			t.Errorf("%s running result:\n%s", scope, out.String())
		}
		out.Reset()
		PrintSetupResult(&out, Result{Scope: scope, UnitName: "mcremote", Label: "mcremote", UnitPath: "/x/mcremote/run",
			Started: true, Health: HealthStarting, Warnings: []string{"mcremote has not reported running yet after 15s"}}, false, "mcremote")
		if !strings.Contains(out.String(), "not yet confirmed") || !strings.Contains(out.String(), "warning: mcremote has not reported running yet after 15s") {
			t.Errorf("%s starting result:\n%s", scope, out.String())
		}
	}
}

// TestSettleIgnoresTheReplacedProcess: samples of the process a restart is
// replacing are not the restarted service, however long it has been up.
func TestSettleIgnoresTheReplacedProcess(t *testing.T) {
	fastSettle(t)
	res := Result{UnitName: "mcremote", replacedPid: 81}
	samples(t, sample{up: true, pid: 81, uptime: 40 * time.Second})
	if got, why := settle(KindS6, res, 4); got != HealthFailed {
		t.Fatalf("only the replaced process up: %s (%s), want failed", got, why)
	}
	samples(t, sample{up: true, pid: 81, uptime: 40 * time.Second}, sample{up: true, pid: 90})
	if got, why := settle(KindS6, res, 4); got != HealthRunning {
		t.Fatalf("the new process up twice: %s (%s), want running", got, why)
	}
}
