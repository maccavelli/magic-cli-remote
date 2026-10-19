//go:build windows

package service

import "errors"

// runit, s6 and OpenRC do not run on Windows; detection never reports them
// there, so these are never reached.

var supervisorWatching = func(string, string) bool { return false }

var startSessionSupervisor = func(string, string) (int, error) {
	return 0, errors.New("runit and s6 are not supported on Windows")
}
