//go:build !linux && !darwin && !windows

package procutil

// ProcessStartToken is unavailable on this platform.
func ProcessStartToken(pid int) (string, bool) { return "", false }
