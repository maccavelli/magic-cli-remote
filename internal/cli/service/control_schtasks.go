package service

import (
	"regexp"
)

// taskNameRe bounds the task names this package opens. Task names here are
// product names, so this rejects nothing legitimate.
var taskNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// isInstalledWindows reports whether the task named name is registered at all,
// from one Task Scheduler read (MADR 0207 D17). It replaced a PowerShell
// Get-ScheduledTask probe, about 1.3 s a call (MADR 0159 probe 11).
//
// setup-service stops and starts the task through go-selfupdate-lib's
// taskscheduler backend, as the update does (MADR 0207 D4, D30). Its Stop
// disables the task first, because the task carries a repeating watchdog
// trigger (MADR 0159 D5): an end alone is undone within a minute.
func isInstalledWindows(name string) (bool, error) {
	st, err := taskStatus(name)
	if err != nil {
		return false, err
	}
	return st.Installed, nil
}
