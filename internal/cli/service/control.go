package service

import (
	"fmt"
	"os"
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
		label := launchdLabel(product)
		return runLaunchctl("bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), label))
	case "linux":
		return runSystemctl("--user", "stop", product+".service")
	default:
		return fmt.Errorf("service control unsupported on %s", osName)
	}
}

// Start starts (or boots) the product's user service.
func Start(product string) error {
	osName := installOS
	if osName == "" {
		osName = runtime.GOOS
	}
	switch osName {
	case "darwin":
		label := launchdLabel(product)
		// Prefer kickstart -k if already loaded; else bootstrap from standard path.
		if err := runLaunchctl("kickstart", "-k", fmt.Sprintf("gui/%d/%s", os.Getuid(), label)); err == nil {
			return nil
		}
		plist := fmt.Sprintf("%s/Library/LaunchAgents/%s.plist", os.Getenv("HOME"), label)
		return runLaunchctl("bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), plist)
	case "linux":
		return runSystemctl("--user", "start", product+".service")
	default:
		return fmt.Errorf("service control unsupported on %s", osName)
	}
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
