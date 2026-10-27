package service

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
)

// The update's backend for a runit, s6 or OpenRC user service: the library's
// adapter, *libservice.Supervised, over this package's state, commands and
// session supervisors (MADR 0212 stage 2; the library's 0035-MADR). The
// library owns the contract: stopping and starting under ctx, its typed
// errors, a new instance, the inside check and the handoff. This package owns
// the supervisor: its definitions, the forms its tools print, its session
// supervisor and its log service (amendment D, D19).

// supervisedTimeout is Poll.Timeout for these supervisors, the stop bound the
// library's systemd backend gives Magic's unit (MADR 0212 D11-A, D17).
const supervisedTimeout = 75 * time.Second

// supervisedSettle is the health wait's settle window, the library's default.
const supervisedSettle = libservice.DefaultPollSettle

// runitKillAfter is when a runit Stop kills a daemon that is still up: the
// drain the daemon gets everywhere else. s6 kills it itself, by timeout-kill,
// and OpenRC by its retry schedule (D17).
const runitKillAfter = 45 * time.Second

// supervisedPoll is D17's bounds. MCREMOTE_SVC_WAIT_SECS, when set, becomes
// the timeout, and is refused when it is not above the settle window, or, on
// runit, not above KillAfter, which is spent inside it.
func supervisedPoll(kind Kind) (libservice.PollOptions, error) {
	p := libservice.PollOptions{Timeout: supervisedTimeout, Settle: supervisedSettle}
	if _, set := os.LookupEnv(waitEnv); !set {
		return p, nil
	}
	secs, err := waitSeconds()
	if err != nil {
		return p, err
	}
	p.Timeout = time.Duration(secs) * time.Second
	if p.Timeout <= p.Settle {
		return p, fmt.Errorf("%s=%d is not above the %s settle window", waitEnv, secs, p.Settle)
	}
	if kind == KindRunit && p.Timeout <= runitKillAfter {
		return p, fmt.Errorf("%s=%d is not above the %s runit waits before it kills the daemon", waitEnv, secs, runitKillAfter)
	}
	return p, nil
}

// supervisedOptions are kind's SupervisedOptions for the service name, with
// h's application check.
func supervisedOptions(kind Kind, name string, h HealthOptions, product, unitName string) (libservice.SupervisedOptions, error) {
	poll, err := supervisedPoll(kind)
	if err != nil {
		return libservice.SupervisedOptions{}, err
	}
	def := scriptDefFor(kind, name, "")
	o := libservice.SupervisedOptions{
		Name: name,
		State: func(ctx context.Context) (libservice.SupervisedState, error) {
			obs, err := observe(ctx, kind, def, name)
			return obs.state, err
		},
		Up:    func(ctx context.Context) error { return supervisedUp(ctx, def, name) },
		Down:  func(ctx context.Context) error { return supervisedDown(ctx, def, name) },
		Poll:  poll,
		Probe: h.backendProbe(product, unitName),
	}
	switch kind {
	case KindRunit:
		o.Kill = func(ctx context.Context) error {
			_, err := toolFor(kind, def.Dir, "sv", "kill", def.Dir).run(ctx)
			return err
		}
		o.KillAfter = runitKillAfter
	case KindOpenRCUser:
		o.DetachCheck = func(ctx context.Context) error { return openRCDetachCheck(ctx, def, name) }
	}
	return o, nil
}

// newSupervisedBackend is the update's backend for kind's service name.
func newSupervisedBackend(kind Kind, name string, h HealthOptions, product, unitName string) (*libservice.Supervised, error) {
	o, err := supervisedOptions(kind, name, h, product, unitName)
	if err != nil {
		return nil, err
	}
	return libservice.NewSupervised(o)
}

// supervisedUp is SupervisedOptions.Up: start the service and keep it up. It
// does not wait; the adapter does.
//
//   - With no supervisor for the session, it starts one, as setup-service
//     does: after a logout or a reboot the session's runsvdir, s6-svscan or
//     OpenRC user session is gone. A new scanner starts the service itself.
//   - On runit and s6, a service that is down has its log service adopted
//     first, when a refresh added it (MADR 0212 D21; P4 Deviations 3 and 4).
//     One that is up is left alone: that is the adapter's restore after a
//     failed stop, and the daemon there still runs.
//   - Then the up command.
//
// It can run again: an adopted log service is not adopted twice, and the up
// commands change nothing on a service already up.
func supervisedUp(ctx context.Context, def scriptDef, name string) error {
	switch def.Kind {
	case KindRunit, KindS6:
		scanner, up := "runsvdir", toolFor(def.Kind, def.Dir, "sv", "up", def.Dir)
		if def.Kind == KindS6 {
			scanner, up = "s6-svscan", toolFor(def.Kind, def.Dir, "s6-svc", "-u", def.Dir)
		}
		if !supervisorWatching(scanner, def.ScanDir) {
			_, err := startSessionSupervisor(scanner, def.ScanDir)
			return err
		}
		if obs, err := observe(ctx, def.Kind, def, name); err == nil && obs.state.Supervised && !obs.state.Up {
			if _, err := adoptLogService(ctx, def); err != nil {
				return err
			}
		}
		_, err := up.run(ctx)
		return err
	case KindOpenRCUser:
		rd := os.Getenv("XDG_RUNTIME_DIR")
		if rd == "" {
			rd, _ = userRuntimeDir()
		}
		envs := []string{"XDG_RUNTIME_DIR=" + rd}
		if _, err := os.Stat(filepath.Join(rd, "openrc", "softlevel")); err != nil {
			for _, lvl := range []string{"boot", "default"} {
				if _, err := toolFor(def.Kind, lvl, "openrc", "--user", lvl).withEnv(envs...).run(ctx); err != nil {
					return err
				}
			}
		}
		_, err := toolFor(def.Kind, name, "rc-service", "--user", "--", name, "start").withEnv(envs...).run(ctx)
		return err
	}
	return fmt.Errorf("no start for %s", def.Kind)
}

// supervisedDown is SupervisedOptions.Down: stop the service and do not
// restart it. runit and s6 return at once. OpenRC's stop runs the script's
// retry schedule before it returns, up to 50 s (P4 step 3), inside the
// adapter's 75 s.
func supervisedDown(ctx context.Context, def scriptDef, name string) error {
	var err error
	switch def.Kind {
	case KindRunit:
		_, err = toolFor(def.Kind, def.Dir, "sv", "down", def.Dir).run(ctx)
	case KindS6:
		_, err = toolFor(def.Kind, def.Dir, "s6-svc", "-d", def.Dir).run(ctx)
	case KindOpenRCUser:
		_, err = toolFor(def.Kind, name, "rc-service", "--user", "--", name, "stop").run(ctx)
	default:
		err = fmt.Errorf("no stop for %s", def.Kind)
	}
	return err
}

// openRCDetachCheck is OpenRC's SupervisedOptions.DetachCheck (MADR 0212
// D3-B). It refuses when this process shares the daemon's cgroup and that
// cgroup is OpenRC's, named with the "openrc." prefix: a stop with cgroup
// cleanup on kills everything in it, the detached update too. It reads the
// kernel, not rc.conf, so a host with OpenRC cgroups but cleanup off keeps
// the refusal. A daemon that is not up has nothing to share.
func openRCDetachCheck(ctx context.Context, def scriptDef, name string) error {
	obs, err := observe(ctx, KindOpenRCUser, def, name)
	if err != nil {
		return err
	}
	if !obs.state.Up || obs.state.PID <= 0 {
		return nil
	}
	mine, err := cgroupsOf("self")
	if err != nil {
		return err
	}
	theirs, err := cgroupsOf(strconv.Itoa(obs.state.PID))
	if err != nil {
		return err
	}
	for hierarchy, cg := range theirs {
		if strings.HasPrefix(path.Base(cg), "openrc.") && mine[hierarchy] == cg {
			return fmt.Errorf("this process shares the %s daemon's OpenRC cgroup %s, which OpenRC may kill on stop with the update in it; run the update from a shell", name, cg)
		}
	}
	return nil
}

// cgroupsOf reads /proc/<pid>/cgroup: each line's hierarchy ("ID:controllers")
// and its cgroup path (cgroups(7)).
func cgroupsOf(pid string) (map[string]string, error) {
	raw, err := os.ReadFile(filepath.Join(stateProcRoot, pid, "cgroup"))
	if err != nil {
		return nil, fmt.Errorf("read the cgroup of process %s: %w", pid, err)
	}
	out := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		// The path, the third field, may hold a colon itself.
		f := strings.SplitN(line, ":", 3)
		if len(f) != 3 {
			continue
		}
		out[f[0]+":"+f[1]] = f[2]
	}
	return out, nil
}

// scriptInstalled reports whether def's definition exists, as a file: what a
// managed update asks when no backend can be built (IsInstalled).
func scriptInstalled(def scriptDef) (bool, error) {
	fi, err := os.Stat(def.Path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !fi.IsDir(), nil
}
