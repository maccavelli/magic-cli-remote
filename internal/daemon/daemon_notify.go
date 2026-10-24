package daemon

import (
	"errors"
	"net"
	"os"
)

// notifySocket is the service manager's notify socket the daemon was started
// with, kept after it is taken out of the environment (MADR 0210 D10).
type notifySocket string

// takeNotifySocket reads NOTIFY_SOCKET and unsets it, as sd_notify(3)'s
// unset_environment does. The variable names this unit's socket, and every
// process the daemon spawns, an agent's update and its handoff included,
// would otherwise inherit it (MADR 0210 F5). Run calls it before it starts
// anything.
func takeNotifySocket() notifySocket {
	s := os.Getenv("NOTIFY_SOCKET")
	_ = os.Unsetenv("NOTIFY_SOCKET")
	return notifySocket(s)
}

// send writes state as one datagram to the kept socket, as the library's
// systemd.Notify does: a path, or "@" for the abstract namespace, which Go's
// syscall package maps to a leading NUL on Linux itself. With no socket it
// reports false and no error, as sd_notify returns 0 off a Type=notify unit.
func (s notifySocket) send(state string) (bool, error) {
	if s == "" {
		return false, nil
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: string(s), Net: "unixgram"})
	if err != nil {
		return false, err
	}
	_, werr := conn.Write([]byte(state))
	return werr == nil, errors.Join(werr, conn.Close())
}

// ready reports that start-up is complete: READY=1.
func (s notifySocket) ready() (bool, error) { return s.send("READY=1") }

// stopping reports that shutdown began: STOPPING=1.
func (s notifySocket) stopping() (bool, error) { return s.send("STOPPING=1") }
