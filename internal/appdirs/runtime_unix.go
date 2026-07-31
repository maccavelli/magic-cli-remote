//go:build unix

package appdirs

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// ValidateRuntimeDir ensures dir is a private, user-owned directory suitable
// for binding a Unix domain socket. It does not create the directory.
func ValidateRuntimeDir(dir string) error {
	if dir == "" || !filepath.IsAbs(dir) {
		return fmt.Errorf("appdirs: runtime dir must be absolute")
	}
	dir = filepath.Clean(dir)
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("appdirs: runtime dir %s is a symlink", dir)
	}
	if !fi.IsDir() {
		return fmt.Errorf("appdirs: runtime dir %s is not a directory", dir)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("appdirs: cannot inspect runtime dir %s", dir)
	}
	if int(st.Uid) != os.Getuid() {
		return fmt.Errorf("appdirs: runtime dir %s not owned by current user", dir)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("appdirs: runtime dir %s mode %04o allows group/other", dir, fi.Mode().Perm())
	}
	return nil
}

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
