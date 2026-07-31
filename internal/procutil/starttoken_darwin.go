//go:build darwin

package procutil

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// ProcessStartToken returns a Darwin process start token from KinfoProc.
func ProcessStartToken(pid int) (string, bool) {
	kinfo, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", false
	}
	sec := kinfo.Proc.P_starttime.Sec
	usec := kinfo.Proc.P_starttime.Usec
	return fmt.Sprintf("%d.%d", sec, usec), true
}
