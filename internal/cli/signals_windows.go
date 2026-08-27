//go:build windows

package cli

import "os"

// shutdownSignals returns the signals that must start a graceful drain.
//
// Windows never delivers SIGTERM. os.Interrupt covers CTRL_C_EVENT,
// CTRL_BREAK_EVENT and CTRL_CLOSE_EVENT for a console process, and it is the
// ONLY graceful trigger: the scheduled task that runs this daemon is stopped
// with `schtasks /end`, a TerminateProcess (MADR 0116 D9/D12). Provider trees
// still die with the Job Object, and a left-behind admin socket is handled by
// the stale-socket path.
func shutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
