package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/taskscheduler"
)

// taskStatusReader is a backend that reads every state of its service in one
// call: the Task Scheduler backend, *taskscheduler.Task (MADR 0207 D17).
type taskStatusReader interface {
	Status(ctx context.Context, product string) (taskscheduler.Status, error)
}

// IsInstalled reports whether a service definition exists for product,
// regardless of whether it is enabled or running. A managed update asks it
// only when the service manager's backend cannot be built, to decide between
// refusing and a binary-only update (MADR 0054 PLAN S1).
//
// Linux asks systemd for LoadState rather than is-enabled, which exits non-zero
// for an installed-but-disabled unit, and falls back to the unit file so a
// definition written but not yet reloaded still counts. A masked unit counts as
// not installed: it cannot be started, and calling it installed would
// reintroduce exactly the failure this probe exists to prevent (MADR 0100 F3).
func IsInstalled(product string) (bool, error) {
	osName := installOS
	if osName == "" {
		osName = runtime.GOOS
	}
	names, err := namesFor(product, "")
	if err != nil {
		return false, err
	}
	switch osName {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return false, fmt.Errorf("home dir: %w", err)
		}
		plist := filepath.Join(home, "Library", "LaunchAgents", names.Label+".plist")
		fi, err := os.Stat(plist)
		return err == nil && !fi.IsDir(), nil
	case "linux":
		if kind, err := ResolveBackend(product); err != nil {
			return false, err
		} else if kind.isScriptKind() {
			return newScriptBackend(kind, strings.TrimSuffix(names.Unit, ".service")).Installed(context.Background(), product)
		}
		unit := names.Unit
		if out, err := runSystemctlCapture("--user", "show", "-p", "LoadState", "--value", unit); err == nil {
			switch strings.TrimSpace(out) {
			case "loaded":
				return true, nil
			case "masked":
				return false, nil
			}
		}
		unitPath := filepath.Join(xdgConfigHome(), "systemd", "user", unit)
		fi, err := os.Stat(unitPath)
		return err == nil && !fi.IsDir(), nil
	case "windows":
		return isInstalledWindows(names.Task)
	default:
		return false, fmt.Errorf("service control unsupported on %s", osName)
	}
}

// Status describes whether the product's user service is installed / loaded /
// enabled / active. Used by `mcremote doctor` (MADR 0072 P2).
type Status struct {
	// OS is the installOS override or runtime.GOOS used for the probe.
	OS string
	// PlistOrUnit is the LaunchAgent path or systemd unit name.
	PlistOrUnit string
	// PlistPresent is true when the service manager knows the definition: the
	// backend's Installed.
	PlistPresent bool
	// Loaded is true when launchd has the job (print succeeds). Elsewhere it
	// is PlistPresent.
	Loaded bool
	// Enabled is true when the definition is configured to start unattended:
	// the backend's Enabled, which also decides whether an update starts a
	// stopped service (MADR 0054 D6).
	Enabled bool
	// Active is true when the process is running: the backend's Running.
	Active bool
	// Hint is a short operator action when something is wrong; empty when OK.
	Hint string
	// Kind is the supervisor on Linux when it is runit, s6 or OpenRC, whose
	// definition PlistOrUnit then names; empty for systemd (MADR 0054 PLAN S5f).
	Kind Kind
}

// probeTimeout bounds doctor's service probes.
const probeTimeout = 15 * time.Second

// newProbeBackend builds the backend ProbeStatus reads. It is a variable for
// tests.
var newProbeBackend = NewBackend

// ProbeStatus reports install/load/enabled/active for product without
// mutating state. It reads the same backend probes a managed update does, so
// doctor and update cannot disagree (MADR 0054 D6). launchd's "loaded" has no
// backend probe, so macOS still asks launchctl for it, which keeps the
// bootout-left-down hint (MADR 0054 PLAN S1, Deviation 2).
func ProbeStatus(product string) Status {
	osName := installOS
	if osName == "" {
		osName = runtime.GOOS
	}
	st := Status{OS: osName}
	names, err := namesFor(product, "")
	if err != nil {
		st.Hint = err.Error()
		return st
	}
	switch osName {
	case "darwin":
		st.PlistOrUnit = fmt.Sprintf("%s/Library/LaunchAgents/%s.plist", os.Getenv("HOME"), names.Label)
	case "linux":
		st.PlistOrUnit = names.Unit
		if kind, err := ResolveBackend(product); err == nil && kind.isScriptKind() {
			st.Kind = kind
			st.PlistOrUnit = scriptDefFor(kind, strings.TrimSuffix(names.Unit, ".service"), "").Path
		}
	case "windows":
		st.PlistOrUnit = `Task Scheduler\` + names.Task
	default:
		st.Hint = "service status unsupported on " + osName
		return st
	}

	b, err := newProbeBackend(product, "")
	if err != nil {
		st.Hint = "cannot reach the service manager: " + err.Error()
		return st
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	var probeErr error
	keep := func(err error) {
		if probeErr == nil {
			probeErr = err
		}
	}
	if s, ok := b.(taskStatusReader); ok {
		// Task Scheduler answers all three in one read (MADR 0207 D17).
		ts, err := s.Status(ctx, product)
		keep(err)
		st.PlistPresent, st.Enabled, st.Active = ts.Installed, ts.Installed && ts.Enabled, ts.Installed && ts.Running
	} else {
		st.PlistPresent, err = b.Installed(ctx, product)
		keep(err)
		if st.PlistPresent {
			st.Enabled, err = b.Enabled(ctx, product)
			keep(err)
			st.Active, err = b.Running(ctx, product)
			keep(err)
		}
	}
	st.Loaded = st.PlistPresent
	svc := launchdServiceTarget(names.Label)
	if osName == "darwin" {
		_, err := runLaunchctlCapture("print", svc)
		st.Loaded = err == nil
	}

	switch {
	case probeErr != nil:
		st.Hint = "service probe failed: " + probeErr.Error()
	case !st.PlistPresent:
		switch osName {
		case "darwin":
			st.Hint = "plist missing — run: mcremote setup-service --force"
		case "windows":
			st.Hint = "scheduled task missing — run: mcremote setup-service --force"
		default:
			st.Hint = "unit missing — run: mcremote setup-service --force"
		}
	case osName == "darwin" && !st.Loaded:
		st.Hint = "plist present but not loaded (bootout left down) — run: mcremote setup-service --force"
	case !st.Enabled:
		st.Hint = "installed but not enabled, so an update leaves it stopped — run: mcremote setup-service --force"
	case !st.Active:
		switch osName {
		case "darwin":
			st.Hint = "loaded but not running — run: launchctl kickstart -k " + svc
		case "windows":
			st.Hint = "registered but not running — run: schtasks /run /tn " + names.Task
		default:
			switch st.Kind {
			case KindRunit:
				st.Hint = "run: sv up " + filepath.Dir(st.PlistOrUnit) + " (or mcremote setup-service --force)"
			case KindS6:
				st.Hint = "run: s6-svc -u " + filepath.Dir(st.PlistOrUnit) + " (or mcremote setup-service --force)"
			case KindOpenRCUser:
				st.Hint = "run: rc-service --user " + filepath.Base(st.PlistOrUnit) + " start (or mcremote setup-service --force)"
			default:
				st.Hint = "run: systemctl --user start " + st.PlistOrUnit + " (or mcremote setup-service --force)"
			}
		}
	}
	return st
}

// Capture helpers — fall back to run* when only error matters.

var runLaunchctlCapture = func(args ...string) (string, error) {
	return runCmdOutput("launchctl", args...)
}

var runSystemctlCapture = func(args ...string) (string, error) {
	return runCmdOutput("systemctl", args...)
}
