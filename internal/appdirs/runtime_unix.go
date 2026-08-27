//go:build unix

package appdirs

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
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
