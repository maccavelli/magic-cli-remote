package service

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

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
// means the product. Each OS turns it into the definition's name exactly as
// setup does, so the backend manages the definition setup wrote.
func NewBackend(product, unitName string) (Backend, error) {
	if unitName == "" {
		unitName = product
	}
	switch runtime.GOOS {
	case "linux":
		ensureUserRuntimeDir()
		u, err := systemd.New(systemd.Options{Unit: unitName + ".service", Scope: systemd.User})
		if err != nil {
			return nil, err
		}
		return u, nil
	case "darwin":
		label, err := LaunchdLabel(product, unitName)
		if err != nil {
			return nil, err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("home dir: %w", err)
		}
		j, err := launchd.New(launchd.Options{
			Label:  label,
			Domain: launchd.GUI(os.Getuid()),
			Plist:  filepath.Join(home, "Library", "LaunchAgents", label+".plist"),
		})
		if err != nil {
			return nil, err
		}
		return j, nil
	case "windows":
		t, err := taskscheduler.New(taskscheduler.Options{Name: taskNameFor(product)})
		if err != nil {
			return nil, err
		}
		return t, nil
	default:
		return nil, fmt.Errorf("service control unsupported on %s", runtime.GOOS)
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
