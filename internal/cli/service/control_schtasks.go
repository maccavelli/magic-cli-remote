package service

import (
	"fmt"
	"regexp"
	"strings"
)

// taskNameRe bounds the task names this package opens. Task names here are
// product names, so this rejects nothing legitimate.
var taskNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// isInstalledWindows reports whether the task named name is registered at all,
// from one Task Scheduler read (MADR 0207 D17). It replaced a PowerShell
// Get-ScheduledTask probe, about 1.3 s a call (MADR 0159 probe 11).
func isInstalledWindows(name string) (bool, error) {
	st, err := taskStatus(name)
	if err != nil {
		return false, err
	}
	return st.Installed, nil
}

// startWindows enables the task, then runs it now, for setup-service. Enabling
// first undoes a stop (which disables; see stopWindows), so the watchdog
// trigger is live again once the daemon is (MADR 0159 D5). A managed update
// starts the task through go-selfupdate-lib's taskscheduler backend instead
// (MADR 0054 D3).
func startWindows(product string) error {
	name := taskName(product)
	if out, err := runSchtasks("/change", "/tn", name, "/enable"); err != nil {
		return fmt.Errorf("enable scheduled task %q: %w (%s)", name, err, strings.TrimSpace(out))
	}
	if out, err := runSchtasks("/run", "/tn", name); err != nil {
		return fmt.Errorf("start scheduled task %q: %w (%s)", name, err, strings.TrimSpace(out))
	}
	return nil
}

// stopWindows disables the task, then ends it if it is running, for
// setup-service --remove. A managed update stops the task through
// go-selfupdate-lib's taskscheduler backend, which disables it first for the
// same reason (MADR 0054 D3).
//
// Disabling comes first because the task carries a repeating watchdog trigger
// (MADR 0159 D5): a bare /end is undone within a minute, which would restart a
// daemon that was stopped on purpose. A disabled task
// is not relaunched (measured, MADR 0159 probe 8). Whether to /end is decided
// by Task Scheduler's own state rather than by parsing /end's localised error
// for a task that is not running.
//
// `schtasks /end` is a TerminateProcess, NOT a graceful signal: the daemon gets
// no drain (MADR 0116 D9). That is survivable by construction — provider trees
// die with the Job Object (D8) and a left-behind admin socket is handled by the
// stale-socket path — but it is a real difference from SIGTERM on Unix, and a
// future reader must not mistake it for an oversight.
func stopWindows(product string) error {
	name := taskName(product)
	if out, err := runSchtasks("/change", "/tn", name, "/disable"); err != nil {
		return fmt.Errorf("disable scheduled task %q: %w (%s)", name, err, strings.TrimSpace(out))
	}
	st, err := taskStatus(name)
	if err != nil {
		return fmt.Errorf("stop scheduled task %q: %w", name, err)
	}
	if !st.Installed || !st.Running {
		return nil
	}
	if out, err := runSchtasks("/end", "/tn", name); err != nil {
		return fmt.Errorf("stop scheduled task %q: %w (%s)", name, err, strings.TrimSpace(out))
	}
	return nil
}
