package service

import (
	"fmt"
	"strings"
)

// isActiveWindows reports whether the scheduled task is currently running.
func isActiveWindows(product string) (bool, error) {
	out, err := runSchtasks("/query", "/tn", taskName(product), "/fo", "LIST", "/v")
	if err != nil {
		return false, nil // not registered → not active
	}
	return taskStatusRunning(out), nil
}

// taskStatusRunning parses `schtasks /query /fo LIST /v` output.
//
// The status line is localised on a non-English Windows, so this matches the
// English value and treats anything else as "not running" — the conservative
// answer, which at worst makes `update` start a task that is already up
// (harmless: MultipleInstancesPolicy is IgnoreNew).
func taskStatusRunning(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "Status") {
			continue
		}
		return strings.EqualFold(strings.TrimSpace(value), "Running")
	}
	return false
}

// isInstalledWindows reports whether the task is registered at all.
func isInstalledWindows(product string) (bool, error) {
	if _, err := runSchtasks("/query", "/tn", taskName(product)); err != nil {
		return false, nil
	}
	return true, nil
}

// startWindows runs the task now.
func startWindows(product string) error {
	name := taskName(product)
	if out, err := runSchtasks("/run", "/tn", name); err != nil {
		return fmt.Errorf("start scheduled task %q: %w (%s)", name, err, strings.TrimSpace(out))
	}
	return nil
}

// stopWindows ends the running task.
//
// `schtasks /end` is a TerminateProcess, NOT a graceful signal: the daemon gets
// no drain (MADR 0116 D9). That is survivable by construction — provider trees
// die with the Job Object (D8) and a left-behind admin socket is handled by the
// stale-socket path — but it is a real difference from SIGTERM on Unix, and a
// future reader must not mistake it for an oversight.
func stopWindows(product string) error {
	name := taskName(product)
	if out, err := runSchtasks("/end", "/tn", name); err != nil {
		return fmt.Errorf("stop scheduled task %q: %w (%s)", name, err, strings.TrimSpace(out))
	}
	return nil
}
