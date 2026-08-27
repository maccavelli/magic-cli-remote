//go:build windows

package procutil

import "golang.org/x/sys/windows"

// ProcessStartToken returns pid's creation time as a decimal string, which
// distinguishes a live process from one that reused its pid.
func ProcessStartToken(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	return creationTime(h)
}
