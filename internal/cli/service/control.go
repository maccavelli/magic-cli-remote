package service

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// IsActive reports whether the product's user service is running.
func IsActive(product string) (bool, error) {
	osName := installOS
	if osName == "" {
		osName = runtime.GOOS
	}
	switch osName {
	case "darwin":
		label := launchdLabel(product)
		// launchctl print gui/$UID/label → exit 0 if loaded; look for state.
		// Match install-binary.sh unit_active: state=exited/not running is down
		// even if a dying pid is still printed (SIGTERM drain race).
		out, err := runLaunchctlCapture("print", fmt.Sprintf("gui/%d/%s", os.Getuid(), label))
		if err != nil {
			// Not loaded / not found → not active.
			return false, nil
		}
		if strings.Contains(out, "state = exited") || strings.Contains(out, "state = not running") {
			return false, nil
		}
		if strings.Contains(out, "state = running") || strings.Contains(out, "state = waiting") {
			return true, nil
		}
		// No usable state line: fall back to a non-zero pid.
		if strings.Contains(out, "pid = 0\n") || strings.Contains(out, "pid = 0 ") {
			return false, nil
		}
		return strings.Contains(out, "pid = "), nil
	case "linux":
		unit := product + ".service"
		out, err := runSystemctlCapture("--user", "is-active", unit)
		if err != nil {
			return false, nil
		}
		return strings.TrimSpace(out) == "active", nil
	case "windows":
		return isActiveWindows(product)
	default:
		return false, fmt.Errorf("service control unsupported on %s", osName)
	}
}

// Stop stops the product's user service if present.
func Stop(product string) error {
	osName := installOS
	if osName == "" {
		osName = runtime.GOOS
	}
	switch osName {
	case "darwin":
		// Waits for the job to leave the domain. bootout alone returns while
		// teardown is still in flight, and every caller of Stop follows it with
		// something that cannot survive that window (MADR 0125 D1).
		return stopAndWaitDarwin(launchdServiceTarget(product))
	case "linux":
		return runSystemctl("--user", "stop", product+".service")
	case "windows":
		return stopWindows(product)
	default:
		return fmt.Errorf("service control unsupported on %s", osName)
	}
}

// Start starts (or boots) the product's user service.
//
// On macOS a prior Stop used bootout, which removes the job from the domain.
// kickstart alone then fails with "Could not find service". Order:
// print → if loaded, kickstart -k; else bootstrap plist then kickstart -k
// (MADR 0072 P2).
func Start(product string) error {
	osName := installOS
	if osName == "" {
		osName = runtime.GOOS
	}
	switch osName {
	case "darwin":
		label := launchdLabel(product)
		svc := fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
		domain := fmt.Sprintf("gui/%d", os.Getuid())
		plist := fmt.Sprintf("%s/Library/LaunchAgents/%s.plist", os.Getenv("HOME"), label)

		if _, err := runLaunchctlCapture("print", svc); err == nil {
			if err := runLaunchctl("kickstart", "-k", svc); err != nil {
				return fmt.Errorf("kickstart %s: %w", svc, err)
			}
			return nil
		}
		// Not loaded (typical after bootout). Bootstrap, then kickstart.
		if err := runLaunchctl("bootstrap", domain, plist); err != nil {
			// Race: another path may have bootstrapped; try kickstart once.
			if kerr := runLaunchctl("kickstart", "-k", svc); kerr == nil {
				return nil
			}
			return fmt.Errorf("bootstrap %s: %w (plist %s)", svc, err, plist)
		}
		if err := runLaunchctl("kickstart", "-k", svc); err != nil {
			return fmt.Errorf("kickstart after bootstrap %s: %w", svc, err)
		}
		return nil
	case "linux":
		return runSystemctl("--user", "start", product+".service")
	case "windows":
		return startWindows(product)
	default:
		return fmt.Errorf("service control unsupported on %s", osName)
	}
}

// IsInstalled reports whether a service definition exists for product,
// regardless of whether it is enabled or running.
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
	switch osName {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return false, fmt.Errorf("home dir: %w", err)
		}
		plist := filepath.Join(home, "Library", "LaunchAgents", launchdLabel(product)+".plist")
		fi, err := os.Stat(plist)
		return err == nil && !fi.IsDir(), nil
	case "linux":
		unit := product + ".service"
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
		return isInstalledWindows(product)
	default:
		return false, fmt.Errorf("service control unsupported on %s", osName)
	}
}

// Status describes whether the product's user service is installed / loaded /
// active. Used by `mcremote doctor` (MADR 0072 P2).
type Status struct {
	// OS is the installOS override or runtime.GOOS used for the probe.
	OS string
	// PlistOrUnit is the LaunchAgent path or systemd unit name.
	PlistOrUnit string
	// PlistPresent is true when the LaunchAgent file (or unit file) exists.
	PlistPresent bool
	// Loaded is true when launchd has the job (print succeeds) or systemd
	// knows the unit.
	Loaded bool
	// Active is true when the process is running (IsActive).
	Active bool
	// Hint is a short operator action when something is wrong; empty when OK.
	Hint string
}

// ProbeStatus reports install/load/active for product without mutating state.
func ProbeStatus(product string) Status {
	osName := installOS
	if osName == "" {
		osName = runtime.GOOS
	}
	st := Status{OS: osName}
	switch osName {
	case "darwin":
		label := launchdLabel(product)
		st.PlistOrUnit = fmt.Sprintf("%s/Library/LaunchAgents/%s.plist", os.Getenv("HOME"), label)
		if fi, err := os.Stat(st.PlistOrUnit); err == nil && !fi.IsDir() {
			st.PlistPresent = true
		}
		svc := fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
		if _, err := runLaunchctlCapture("print", svc); err == nil {
			st.Loaded = true
		}
		active, _ := IsActive(product)
		st.Active = active
		switch {
		case !st.PlistPresent:
			st.Hint = "plist missing — run: mcremote setup-service --force"
		case !st.Loaded:
			st.Hint = "plist present but not loaded (bootout left down) — run: mcremote setup-service --force"
		case !st.Active:
			st.Hint = "loaded but not running — run: launchctl kickstart -k " + svc
		}
	case "linux":
		st.PlistOrUnit = product + ".service"
		out, err := runSystemctlCapture("--user", "is-enabled", st.PlistOrUnit)
		st.PlistPresent = err == nil && strings.TrimSpace(out) != "not-found"
		st.Loaded = st.PlistPresent
		active, _ := IsActive(product)
		st.Active = active
		if !st.Active {
			st.Hint = "run: systemctl --user start " + st.PlistOrUnit + " (or mcremote setup-service --force)"
		}
	case "windows":
		st.PlistOrUnit = `Task Scheduler\` + taskNameFor(product)
		installed, _ := IsInstalled(product)
		st.PlistPresent = installed
		st.Loaded = installed
		active, _ := IsActive(product)
		st.Active = active
		switch {
		case !st.PlistPresent:
			st.Hint = "scheduled task missing — run: mcremote setup-service --force"
		case !st.Active:
			st.Hint = "registered but not running — run: schtasks /run /tn " + taskNameFor(product)
		}
	default:
		st.Hint = "service status unsupported on " + osName
	}
	return st
}

func launchdLabel(product string) string {
	return "com.magiccliremote." + product
}

// Capture helpers — fall back to run* when only error matters.

var runLaunchctlCapture = func(args ...string) (string, error) {
	return runCmdOutput("launchctl", args...)
}

var runSystemctlCapture = func(args ...string) (string, error) {
	return runCmdOutput("systemctl", args...)
}
