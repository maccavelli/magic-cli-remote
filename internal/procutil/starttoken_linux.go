//go:build linux

package procutil

// ProcessStartToken returns the OS process-start token for pid (clock ticks
// since boot from /proc/<pid>/stat field 22).
func ProcessStartToken(pid int) (string, bool) {
	return procStartTime(pid)
}
