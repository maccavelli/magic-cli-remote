//go:build unix

package relay

import (
	"os"
	"syscall"
)

// shutdownSignals returns the signals that must start a graceful drain.
func shutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
