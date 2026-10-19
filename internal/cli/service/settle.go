package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The verified start (MADR 0054 PLAN S5c, gap G2). A start command returning
// 0 means the start was accepted, not that the daemon survived it: `systemctl
// --user restart` returns once the job is issued, so a unit that dies at once
// was reported as set up (MADR 0099 F5). setup-service watches the service
// after starting it, and reports one of three outcomes. It ports install.sh's
// svc_settle, svc_confirm and svc_diagnose (scripts/install.sh:452-550).

// waitEnv names the variable that bounds the wait (MADR 0054 answer A3).
const waitEnv = "MCREMOTE_SVC_WAIT_SECS"

// defaultWaitSecs is install.sh's default (scripts/install.sh:30).
const defaultWaitSecs = 15

// waitSeconds reads MCREMOTE_SVC_WAIT_SECS. Unset means the default. A value
// that is not a positive integer is refused: install.sh compared it in a shell
// loop without checking it (:475), and this refuses it before anything is done.
func waitSeconds() (int, error) {
	v, ok := os.LookupEnv(waitEnv)
	if !ok {
		return defaultWaitSecs, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s=%q is not a positive whole number of seconds", waitEnv, v)
	}
	return n, nil
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

// settleTick is the time between samples. It is a variable for tests.
var settleTick = time.Second

// sample is one look at a service.
type sample struct {
	// up is true when the service's process is running.
	up bool
	// pid is the process id when known, so a restart while watching shows.
	pid int
	// uptime is how long the process has been up, when the manager says.
	uptime time.Duration
	// failed is true when the manager reports the service failed.
	failed bool
	// exited is true when the supervisor reports the service down while it
	// wants it up: it exited and is being restarted.
	exited bool
}

// sampleFunc takes one sample of the service setup just started.
type sampleFunc func() (sample, error)

// settle watches a started service for up to secs seconds. It returns the
// outcome, and for a failure the reason.
//
// systemd is watched as install.sh's svc_settle does (:467-506). Every other
// kind is up once it is seen up twice in a row with the same process, or once
// its manager reports it up for two seconds; a process that changes while
// watched is restarting, which install.sh's other kinds could not see.
func settle(kind Kind, res Result, secs int) (string, string) {
	if kind == KindSystemdUser {
		return settleSystemd(res.UnitName+".service", secs)
	}
	probe := newSampler(kind, res)
	seenUp := false
	lastPid, ok, exits := 0, 0, 0
	for i := 0; i < secs; i++ {
		s, err := probe()
		if s.up && res.replacedPid != 0 && s.pid == res.replacedPid {
			// Still the process the restart replaces.
			s = sample{}
		}
		switch {
		case err != nil || !s.up:
			ok = 0
			if s.failed {
				return HealthFailed, "the service manager reports it failed"
			}
			if s.exited {
				// One exit after it was up, or two, is a crash loop: s6
				// restarts a daemon that dies at once every second, so the
				// live probe's samples saw it down, almost never up.
				exits++
				if seenUp || exits >= 2 {
					return HealthFailed, "it keeps exiting while its supervisor restarts it"
				}
			}
		case seenUp && s.pid != 0 && lastPid != 0 && s.pid != lastPid:
			return HealthFailed, fmt.Sprintf("it restarted while watched (pid %d, then %d)", lastPid, s.pid)
		default:
			seenUp = true
			ok++
			if s.uptime >= 2*time.Second || ok >= 2 {
				return HealthRunning, ""
			}
		}
		if s.up {
			lastPid = s.pid
		}
		time.Sleep(settleTick)
	}
	if !seenUp {
		return HealthFailed, fmt.Sprintf("it was not seen running in %ds", secs)
	}
	return HealthStarting, ""
}

// settleSystemd is install.sh's svc_settle for a systemd unit. NRestarts is
// the only field that separates "still coming up" from "coming up over and
// over": with Restart=always a crash-looping unit never reaches failed.
// `active` must hold for three samples, because with Type=simple systemd
// reports it the moment the process is forked (MADR 0099).
func settleSystemd(unit string, secs int) (string, string) {
	show := func() (active, sub string, restarts int) {
		out, err := runSystemctlCapture("--user", "show", unit, "--property=ActiveState", "--property=SubState", "--property=NRestarts")
		if err != nil {
			return "unknown", "unknown", 0
		}
		for _, line := range strings.Split(out, "\n") {
			k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
			switch k {
			case "ActiveState":
				active = v
			case "SubState":
				sub = v
			case "NRestarts":
				restarts, _ = strconv.Atoi(v)
			}
		}
		return active, sub, restarts
	}
	_, _, base := show()
	ok := 0
	for i := 0; i < secs; i++ {
		active, sub, restarts := show()
		switch {
		case restarts > base:
			return HealthFailed, fmt.Sprintf("it restarted %d times while watched", restarts-base)
		case sub == "auto-restart" && restarts >= 2:
			return HealthFailed, fmt.Sprintf("it is restarting over and over (NRestarts=%d)", restarts)
		case active == "failed":
			return HealthFailed, "systemd reports it failed"
		case active == "active":
			ok++
			if ok >= 3 {
				return HealthRunning, ""
			}
		default:
			ok = 0
		}
		time.Sleep(settleTick)
	}
	return HealthStarting, ""
}

var (
	// runitStatusRe reads `sv status`: "run: DIR: (pid 87) 7s".
	runitStatusRe = regexp.MustCompile(`^run: .*\(pid (\d+)\) (\d+)s`)
	// s6StatusRe reads `s6-svstat`: "up (pid 45 pgid 45) 8 seconds".
	s6StatusRe = regexp.MustCompile(`^up \(pid (\d+)[^)]*\) (\d+) seconds`)
	// downWantUpRe is a service down that its supervisor wants up: runit's
	// "down: DIR: 1s, normally up, want up", and s6's
	// "down (exitcode 1) 0 seconds, normally up, want up".
	downWantUpRe = regexp.MustCompile(`^down[: ].*want up`)
	// launchdPidRe and launchdStateRe read `launchctl print`.
	launchdPidRe   = regexp.MustCompile(`(?m)^\s*pid = (\d+)`)
	launchdStateRe = regexp.MustCompile(`(?m)^\s*state = (.+)$`)
)

// newSampler is sampler. It is a variable for tests of the settle loop.
var newSampler = sampler

// sampler returns how kind's service is sampled.
func sampler(kind Kind, res Result) sampleFunc {
	switch kind {
	case KindRunit, KindS6:
		dir := filepath.Dir(res.UnitPath)
		return func() (sample, error) {
			name, args, re := "sv", []string{"status", dir}, runitStatusRe
			if kind == KindS6 {
				name, args, re = "s6-svstat", []string{dir}, s6StatusRe
			}
			out, err := runCmdOutput(name, args...)
			if err != nil {
				return sample{}, err
			}
			return parseStatus(re, out), nil
		}
	case KindOpenRCUser:
		// start-stop-daemon restarts nothing: a process that died is
		// "crashed" for good, which the live probe showed as the only sign.
		return func() (sample, error) {
			out, err := runCmdOutput("rc-service", "--user", res.UnitName, "status")
			return sample{up: err == nil, failed: strings.Contains(out, "status: crashed")}, nil
		}
	case KindLaunchdAgent:
		return func() (sample, error) {
			out, err := runLaunchctlCapture("print", launchdServiceTarget(res.Label))
			if err != nil {
				return sample{}, err
			}
			return parseLaunchctlPrint(out), nil
		}
	default:
		// The Task Scheduler task: the library backend doctor and update use.
		return func() (sample, error) {
			b, err := newProbeBackend(productOf(res), "")
			if err != nil {
				return sample{}, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
			defer cancel()
			up, err := b.Running(ctx, productOf(res))
			return sample{up: up}, err
		}
	}
}

// parseStatus reads `sv status` or `s6-svstat` output with re, whose groups
// are the pid and the seconds up. Any other line is a service that is not up.
func parseStatus(re *regexp.Regexp, out string) sample {
	out = strings.TrimSpace(out)
	m := re.FindStringSubmatch(out)
	if m == nil {
		return sample{exited: downWantUpRe.MatchString(out)}
	}
	pid, _ := strconv.Atoi(m[1])
	secs, _ := strconv.Atoi(m[2])
	return sample{up: true, pid: pid, uptime: time.Duration(secs) * time.Second}
}

// parseLaunchctlPrint reads `launchctl print`. Only "state = running" is up:
// install.sh also took "waiting" (:444), but that is a KeepAlive job held back
// after exiting, which is a crash loop, not a running daemon.
func parseLaunchctlPrint(out string) sample {
	s := sample{}
	if m := launchdPidRe.FindStringSubmatch(out); m != nil {
		s.pid, _ = strconv.Atoi(m[1])
	}
	if m := launchdStateRe.FindStringSubmatch(out); m != nil {
		s.up = strings.TrimSpace(m[1]) == "running"
	}
	return s
}

// productOf is the product a result is for: the task is named after it.
func productOf(res Result) string {
	if res.Label != "" {
		return res.Label
	}
	return "mcremote"
}

// diagnose is install.sh's svc_diagnose (:524-533): the command that shows
// why, and for systemd the journal's last lines, which name the bind error or
// the capability fault a generic "it failed" does not.
func diagnose(kind Kind, res Result) []string {
	switch kind {
	case KindSystemdUser:
		lines := []string{"  journalctl --user -u " + res.UnitName + " -n 20 --no-pager"}
		if out, err := runCmdOutput("journalctl", "--user", "-u", res.UnitName, "-n", "8", "--no-pager"); err == nil {
			for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
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

// verifyStart watches the service setup just started, records the outcome on
// res, and returns an error when it is confirmed not running. It is a variable
// so the package's tests that are not about it need not wait on a service
// manager.
var verifyStart = func(kind Kind, res Result) (Result, error) {
	secs, err := waitSeconds()
	if err != nil {
		return res, err
	}
	health, why := settle(kind, res, secs)
	res.Health = health
	switch health {
	case HealthStarting:
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s has not reported running yet after %ds", res.Label, secs))
	case HealthFailed:
		msg := fmt.Sprintf("%s is not running after setup: %s. The definition is installed; see why with:\n%s",
			res.Label, why, strings.Join(diagnose(kind, res), "\n"))
		return res, fmt.Errorf("%s", msg)
	}
	return res, nil
}
