package service

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate"
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
// the systemd, launchd and Task Scheduler backends' Options.Probe, and on
// Windows h.RequestStop and h.StopGrace (MADR 0054 D5, A1, A2). The runit, s6
// and OpenRC backends confirm a start with setup-service's verified start
// instead.
func NewBackendWith(product, unitName string, h HealthOptions) (Backend, error) {
	names, err := namesFor(product, unitName)
	if err != nil {
		return nil, err
	}
	// installOS, not runtime.GOOS: the same dispatch as Setup, Remove,
	// IsInstalled and ProbeStatus, so a test can pin it (PLAN S5f).
	osName := installOS
	if osName == "" {
		osName = runtime.GOOS
	}
	switch osName {
	case "linux":
		ensureUserRuntimeDir()
		// runit, s6 and OpenRC have this product's own backend; systemd has
		// the library's (MADR 0054 amendment S5-3, PLAN S5f).
		kind, err := ResolveBackend(product)
		if err != nil {
			return nil, err
		}
		if kind.isScriptKind() {
			return newScriptBackend(kind, strings.TrimSuffix(names.Unit, ".service")), nil
		}
		u, err := systemd.New(systemd.Options{
			Unit:  names.Unit,
			Scope: systemd.User,
			Probe: h.backendProbe(product, unitName),
		})
		if err != nil {
			return nil, err
		}
		return u, nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("home dir: %w", err)
		}
		j, err := launchd.New(launchd.Options{
			Label:  names.Label,
			Domain: launchd.GUI(os.Getuid()),
			Plist:  filepath.Join(home, "Library", "LaunchAgents", names.Label+".plist"),
			Probe:  h.backendProbe(product, unitName),
		})
		if err != nil {
			return nil, err
		}
		return j, nil
	case "windows":
		t, err := taskscheduler.New(taskOptions(product, unitName, names.Task, h))
		if err != nil {
			return nil, err
		}
		return t, nil
	default:
		return nil, fmt.Errorf("service control unsupported on %s", osName)
	}
}

// taskOptions are the Task Scheduler backend's options for product.
// taskscheduler.New refuses a StopGrace that is not shorter than the poll
// timeout when RequestStop is set (taskscheduler/task.go:149-150).
func taskOptions(product, unitName, task string, h HealthOptions) taskscheduler.Options {
	return taskscheduler.Options{
		Name:        task,
		Probe:       h.backendProbe(product, unitName),
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
