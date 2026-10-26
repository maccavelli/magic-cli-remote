// Package sdnotify sends sd_notify states to the service manager's notify
// socket a daemon was started with, kept after it is taken out of the
// environment (MADR 0210 D10). mcremote and mcrelay both send through it
// (MADR 0207 D22).
//
// The library's systemd.Notify reads NOTIFY_SOCKET on every call, so it cannot
// send once the variable is unset, which is what sd_notify(3)'s
// unset_environment practice asks of a daemon that spawns processes.
package sdnotify

import (
	"errors"
	"log/slog"
	"net"
	"os"
)

// Socket is the notify socket a daemon was started with: a path, "@" and a
// name in Linux's abstract namespace, or empty when the daemon is not a
// Type=notify service.
type Socket string

// Take reads NOTIFY_SOCKET and unsets it, as sd_notify(3)'s
// unset_environment does. The variable names this unit's socket, and every
// process the daemon spawns, an agent's update and its handoff included,
// would otherwise inherit it (MADR 0210 F5). Call it before starting
// anything.
func Take() Socket {
	s := os.Getenv("NOTIFY_SOCKET")
	_ = os.Unsetenv("NOTIFY_SOCKET")
	return Socket(s)
}

// Send writes state as one datagram to the kept socket, as the library's
// systemd.Notify does: a path, or "@" for the abstract namespace, which Go's
// syscall package maps to a leading NUL on Linux itself. With no socket it
// reports false and no error, as sd_notify returns 0 off a Type=notify unit;
// so it does under every other service manager, and on Windows, where Go has
// no unixgram sockets, it never dials.
func (s Socket) Send(state string) (bool, error) {
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

// Notify sends state and logs the outcome. A failure is logged, never fatal:
// the daemon serves the same either way, and under Type=notify systemd
// reports a missing READY=1 by timing the start out.
func (s Socket) Notify(log *slog.Logger, state string) {
	sent, err := s.Send(state)
	switch {
	case err != nil:
		log.Warn("sd_notify failed", slog.String("state", state), slog.String("err", err.Error()))
	case sent:
		log.Debug("sd_notify sent", slog.String("state", state))
	}
}
