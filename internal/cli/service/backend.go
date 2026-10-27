package service

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
	libservice "github.com/maccavelli/go-selfupdate-lib/selfupdate/service"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/launchd"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/systemd"
	"github.com/maccavelli/go-selfupdate-lib/selfupdate/service/taskscheduler"
)

// Backend is go-selfupdate-lib's view of one product's service: the lifecycle
// a managed update drives, and the enabled probe that decides whether it
// starts a stopped service again (MADR 0054 D2, D3). doctor reads the same
// probes, so update and doctor cannot disagree about the service (D6).
type Backend interface {
	selfupdate.Lifecycle
	selfupdate.EnabledLifecycle
}

// NewBackend returns the library backend for this OS's service manager.
// unitName is the name setup-service registered the definition under; empty
// means the name it recorded, else the product (MADR 0201 D30). Each OS turns
// it into the definition's name exactly as setup does, so the backend manages
// the definition setup wrote.
func NewBackend(product, unitName string) (Backend, error) {
	return NewBackendWith(product, unitName, HealthOptions{})
}

// NewBackendWith is NewBackend with the product's health wiring: h.Probe as
// every backend's Probe, and on Windows h.RequestStop and h.StopGrace (MADR
// 0054 D5, A1, A2).
func NewBackendWith(product, unitName string, h HealthOptions) (Backend, error) {
	// installOS, not runtime.GOOS: the same dispatch as Setup, Remove,
	// IsInstalled and ProbeStatus, so a test can pin it (PLAN S5f).
	osName := installOS
	if osName == "" {
		osName = runtime.GOOS
	}
	switch osName {
	case "linux":
		kind, err := ResolveBackend(product)
		if err != nil {
			return nil, err
		}
		return newBackendFor(kind, product, unitName, h, libservice.PollOptions{})
	case "darwin":
		return newBackendFor(KindLaunchdAgent, product, unitName, h, libservice.PollOptions{})
	case "windows":
		return newBackendFor(KindWindowsTask, product, unitName, h, libservice.PollOptions{})
	default:
		return nil, fmt.Errorf("service control unsupported on %s", osName)
	}
}

// newBackendFor is NewBackendWith for kind. Setup passes the kind it detected,
// since a record an earlier setup left can name another (MADR 0207 D40), and
// poll, its own bounds (D42). A zero poll is the backend's defaults, the
// update's. The runit, s6 and OpenRC adapter takes its bounds from
// supervisedPoll either way.
func newBackendFor(kind Kind, product, unitName string, h HealthOptions, poll libservice.PollOptions) (Backend, error) {
	names, err := namesFor(product, unitName)
	if err != nil {
		return nil, err
	}
	probe := h.backendProbe(kind, product, unitName)
	switch {
	case kind.isScriptKind():
		// runit, s6 and OpenRC have the library's adapter over this package's
		// state and commands (MADR 0212 stage 2).
		ensureUserRuntimeDir()
		s, err := newSupervisedBackend(kind, strings.TrimSuffix(names.Unit, ".service"), h, product, unitName)
		if err != nil {
			return nil, err
		}
		return s, nil
	case kind == KindSystemdUser:
		ensureUserRuntimeDir()
		u, err := systemd.New(systemd.Options{
			Unit:  names.Unit,
			Scope: systemd.User,
			Poll:  poll,
			Probe: probe,
		})
		if err != nil {
			return nil, err
		}
		return u, nil
	case kind == KindLaunchdAgent:
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("home dir: %w", err)
		}
		j, err := launchd.New(launchd.Options{
			Label:  names.Label,
			Domain: launchd.GUI(os.Getuid()),
			Plist:  filepath.Join(home, "Library", "LaunchAgents", names.Label+".plist"),
			Poll:   poll,
			Probe:  probe,
		})
		if err != nil {
			return nil, err
		}
		return j, nil
	case kind == KindWindowsTask:
		o := taskOptions(product, unitName, names.Task, h)
		o.Poll = poll
		t, err := taskscheduler.New(o)
		if err != nil {
			return nil, err
		}
		return t, nil
	default:
		return nil, fmt.Errorf("service control unsupported for %s", kind)
	}
}

// setupBackend is the backend setup-service stops, starts and confirms a start
// through: kind's, with the product's health wiring and setup's bounds (MADR
// 0207 D40, D42). It is a variable for tests.
var setupBackend = func(kind Kind, product, unitName string, h HealthOptions) (Backend, error) {
	poll, err := setupPoll(kind)
	if err != nil {
		return nil, err
	}
	return newBackendFor(kind, product, unitName, h, poll)
}

// OverrideSetupBackend replaces the backend setup-service builds, for tests
// outside this package. Restore with the returned func.
func OverrideSetupBackend(fn func(kind Kind, product, unitName string, h HealthOptions) (Backend, error)) (restore func()) {
	prev := setupBackend
	setupBackend = fn
	return func() { setupBackend = prev }
}

// taskOptions are the Task Scheduler backend's options for product.
// taskscheduler.New refuses a StopGrace that is not shorter than the poll
// timeout when RequestStop is set (taskscheduler/task.go:149-150).
func taskOptions(product, unitName, task string, h HealthOptions) taskscheduler.Options {
	return taskscheduler.Options{
		Name:        task,
		Probe:       h.backendProbe(KindWindowsTask, product, unitName),
		RequestStop: h.backendRequestStop(product, unitName),
		StopGrace:   h.StopGrace,
	}
}

// ensureUserRuntimeDir sets XDG_RUNTIME_DIR to the user's runtime directory
// when it is unset and that directory exists. The systemd backend needs it to
// reach the user manager, and Magic has always derived it for systemctl and
// for the refresh child (withUserRuntimeEnv), so an update run from cron,
// `su -` or a bare SSH session keeps working (MADR 0054 PLAN S1, Deviation 1).
// The refresh child inherits it.
func ensureUserRuntimeDir() {
	if os.Getenv("XDG_RUNTIME_DIR") != "" {
		return
	}
	if dir, ok := userRuntimeDir(); ok {
		_ = os.Setenv("XDG_RUNTIME_DIR", dir)
	}
}
