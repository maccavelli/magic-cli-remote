//go:build !linux && !darwin

package procutil

// ProcessStartToken is unavailable on this platform.
func ProcessStartToken(pid int) (string, bool) { return "", false }
