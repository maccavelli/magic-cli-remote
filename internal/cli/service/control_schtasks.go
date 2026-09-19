package service

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// taskStateRunning is Task Scheduler's numeric state for a running task
// (MSFT_ScheduledTask State: 0 Unknown, 1 Disabled, 2 Queued, 3 Ready, 4 Running).
const taskStateRunning = 4

// taskNameRe bounds what may be interpolated into the PowerShell probe. Task
// names here are product names, so this rejects nothing legitimate.
var taskNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// taskState reports a task's numeric state, and whether it is registered at
// all, without parsing localised text (MADR 0159 D9). `schtasks /query /fo LIST`
// prints "Status: Running" in the UI language, which the previous parser matched
// as English. Get-ScheduledTask's State is an enum, and absent tasks come back
// as $null (measured: 4 for the live task, "absent" for a missing one, about
// 1.3 s per call; MADR 0159 probe 11). A failure to run the probe is an error,
// not "not running". It is a variable so tests can drive it.
var taskState = func(name string) (state int, found bool, err error) {
	if !taskNameRe.MatchString(name) {
		return 0, false, fmt.Errorf("invalid task name %q", name)
	}
	script := `$t = Get-ScheduledTask -TaskPath '\' -TaskName '` + name +
		"' -ErrorAction SilentlyContinue; if ($t) { [int]$t.State } else { 'absent' }"
	out, err := runCmdOutput("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	text := strings.TrimSpace(out)
	if err != nil {
		return 0, false, fmt.Errorf("query scheduled task %q: %w (%s)", name, err, text)
	}
	if text == "absent" {
		return 0, false, nil
	}
	n, convErr := strconv.Atoi(text)
	if convErr != nil {
		return 0, false, fmt.Errorf("query scheduled task %q: unexpected output %q", name, text)
	}
	return n, true, nil
}

// isActiveWindows reports whether the scheduled task is currently running.
func isActiveWindows(product string) (bool, error) {
	state, found, err := taskState(taskName(product))
	if err != nil {
		return false, err
	}
	return found && state == taskStateRunning, nil
}

// isInstalledWindows reports whether the task is registered at all.
func isInstalledWindows(product string) (bool, error) {
	_, found, err := taskState(taskName(product))
	if err != nil {
		return false, err
	}
	return found, nil
}

// startWindows enables the task, then runs it now. Enabling first undoes a
// stop (which disables; see stopWindows), so the watchdog trigger is live
// again once the daemon is (MADR 0159 D5).
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

// stopWindows disables the task, then ends it if it is running.
//
// Disabling comes first because the task carries a repeating watchdog trigger
// (MADR 0159 D5): a bare /end is undone within a minute, which would restart a
// daemon that `update` stopped in order to replace its binary. A disabled task
// is not relaunched (measured, MADR 0159 probe 8). Whether to /end is decided
// by the state probe rather than by parsing /end's localised error for a task
// that is not running.
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
	state, found, err := taskState(name)
	if err != nil {
		return fmt.Errorf("stop scheduled task %q: %w", name, err)
	}
	if !found || state != taskStateRunning {
		return nil
	}
	if out, err := runSchtasks("/end", "/tn", name); err != nil {
		return fmt.Errorf("stop scheduled task %q: %w (%s)", name, err, strings.TrimSpace(out))
	}
	return nil
}
