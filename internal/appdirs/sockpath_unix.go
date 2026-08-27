//go:build unix

package appdirs

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// MaxUnixSocketPathLen is the usable path length for sockaddr_un.sun_path
// (excluding the trailing NUL). Platform-derived via RawSockaddrUnix.
func MaxUnixSocketPathLen() int {
	var sa unix.RawSockaddrUnix
	return len(sa.Path) - 1
}

// CheckSocketPathLength reports whether path fits in sun_path.
func CheckSocketPathLength(path string) error {
	max := MaxUnixSocketPathLen()
	if len(path) > max {
		return fmt.Errorf("appdirs: socket path length %d exceeds platform limit %d: %s", len(path), max, path)
	}
	return nil
}
