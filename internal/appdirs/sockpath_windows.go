//go:build windows

package appdirs

import "fmt"

// maxWindowsSockaddrUn is the usable sun_path length in the Windows AF_UNIX
// sockaddr_un (108 bytes including the trailing NUL). Windows does not export
// a RawSockaddrUnix through x/sys, so the constant is stated here rather than
// derived, and pinned by TestMaxUnixSocketPathLen.
const maxWindowsSockaddrUn = 107

// MaxUnixSocketPathLen is the usable path length for sockaddr_un.sun_path
// (excluding the trailing NUL).
func MaxUnixSocketPathLen() int { return maxWindowsSockaddrUn }

// CheckSocketPathLength reports whether path fits in sun_path.
//
// Unlike the directory-depth check in roots_windows.go, this refuses rather
// than warning: an over-long sun_path cannot work at all (MADR 0116 D3).
func CheckSocketPathLength(path string) error {
	maxLen := MaxUnixSocketPathLen()
	if len(path) > maxLen {
		return fmt.Errorf("appdirs: socket path length %d exceeds platform limit %d: %s", len(path), maxLen, path)
	}
	return nil
}
