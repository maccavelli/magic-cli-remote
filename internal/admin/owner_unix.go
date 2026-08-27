//go:build unix

package admin

import (
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// ownedByCurrentUser reports whether fi belongs to the calling user.
func ownedByCurrentUser(_ string, fi fs.FileInfo) (bool, error) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("cannot inspect ownership")
	}
	return int(st.Uid) == os.Getuid(), nil
}

// socketIdentity returns a value that changes if the path stops naming the
// same socket. On Unix that is the inode.
func socketIdentity(fi fs.FileInfo) (uint64, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Ino, true
}

// secureSocket restricts socketPath to the owning user.
func secureSocket(socketPath string) error {
	return os.Chmod(socketPath, 0o600)
}
