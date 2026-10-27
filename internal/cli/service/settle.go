package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// The verified start (MADR 0054 PLAN S5c, gap G2). A start command returning
// 0 means the start was accepted, not that the daemon survived it: `systemctl
// --user restart` returns once the job is issued, so a unit that dies at once
// was reported as set up (MADR 0099 F5). setup-service confirms a start
// through the backend that made it, by its WaitHealthy with the product's
// probe, as the update and the installer hook do (MADR 0207 D4, D41). It
// reports one of three outcomes.

// waitEnv names the variable that bounds the wait (MADR 0054 answer A3).
const waitEnv = "MCREMOTE_SVC_WAIT_SECS"

// waitSeconds reads MCREMOTE_SVC_WAIT_SECS, which its callers read only when
// it is set. A value that is not a positive integer is refused: install.sh
// compared it in a shell loop without checking it (scripts/install.sh:475),
// and this refuses it before anything is done.
func waitSeconds() (int, error) {
	v := os.Getenv(waitEnv)
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s=%q is not a positive whole number of seconds", waitEnv, v)
	}
	return n, nil
}

// setupPoll is the start check's bounds for kind (MADR 0207 D42): the
// backend's own, a settle window of 10 s and a timeout of 60 s, or 75 s on
// runit, s6 and OpenRC (MADR 0212 D17). MCREMOTE_SVC_WAIT_SECS, when set, is
// the timeout, and is refused when it is not above the settle window.
func setupPoll(kind Kind) (libservice.PollOptions, error) {
	if kind.isScriptKind() {
		return supervisedPoll(kind)
	}
	var p libservice.PollOptions
	if _, set := os.LookupEnv(waitEnv); !set {
		return p, nil
	}
	secs, err := waitSeconds()
	if err != nil {
		return p, err
	}
	p.Timeout = time.Duration(secs) * time.Second
	if p.Timeout <= libservice.DefaultPollSettle {
		return p, fmt.Errorf("%s=%d is not above the %s settle window", waitEnv, secs, libservice.DefaultPollSettle)
	}
	return p, nil
}

// The outcomes of a verified start.
const (
	// HealthRunning is a service confirmed running.
	HealthRunning = "running"
	// HealthStarting is a service still coming up when the wait ended: slow,
	// not necessarily broken. It is a warning, not a failure.
	HealthStarting = "starting"
	// HealthFailed is a service confirmed not running: failed, never up, or
	// restarting over and over.
	HealthFailed = "failed"
)

// settleTick is how often a poll in this package samples. It is a variable for
// tests.
var settleTick = time.Second

// startCheckTimeout bounds the start check from outside. The backend's own
// timeout ends it first; this one only stops a service manager that never
// answers from holding setup. It is a variable for tests.
var startCheckTimeout = 5 * time.Minute

// diagnose is install.sh's svc_diagnose (:524-533): the command that shows
// why, and for systemd the journal's last lines, which name the bind error or
// the capability fault a generic "it failed" does not.
func diagnose(kind Kind, res Result) []string {
	switch kind {
	case KindSystemdUser:
		lines := []string{"  journalctl --user -u " + res.UnitName + " -n 20 --no-pager"}
		if out, err := runCmdOutput("journalctl", "--user", "-u", res.UnitName, "-n", "8", "--no-pager"); err == nil {
			for l := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
				lines = append(lines, "  | "+l)
			}
		}
		return lines
	case KindRunit:
		return []string{"  sv status " + filepath.Dir(res.UnitPath)}
	case KindS6:
		return []string{"  s6-svstat " + filepath.Dir(res.UnitPath)}
	case KindOpenRCUser:
		return []string{"  rc-service --user " + res.UnitName + " status"}
	case KindLaunchdAgent:
		return []string{"  launchctl print " + launchdServiceTarget(res.Label)}
	case KindWindowsTask:
		return []string{"  schtasks /query /tn " + res.Label + " /v"}
	}
	return nil
}

// verifyStart waits on the backend that started the service, records the
// outcome on res, and returns an error when the service is confirmed not
// running. A wait that ran out is a warning: the service may still come up.
// It is a variable so the package's tests that are not about it need not wait
// on a service manager.
var verifyStart = func(kind Kind, res Result) (Result, error) {
	if res.started == nil {
		return res, fmt.Errorf("%s was started by nothing that can confirm it", res.Label)
	}
	ctx, cancel := context.WithTimeout(context.Background(), startCheckTimeout)
	defer cancel()
	err := res.started.b.WaitHealthy(ctx, res.started.product)
	switch {
	case err == nil:
		res.Health = HealthRunning
	case errors.Is(err, libservice.ErrTimeout):
		res.Health = HealthStarting
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s has not reported running yet: %v", res.Label, err))
	default:
		res.Health = HealthFailed
		return res, fmt.Errorf("%s is not running after setup: %w. The definition is installed; see why with:\n%s",
			res.Label, err, strings.Join(diagnose(kind, res), "\n"))
	}
	return res, nil
}
