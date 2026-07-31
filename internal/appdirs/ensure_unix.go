//go:build unix

package appdirs

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// EnsurePrivateDir creates dir (and parents) as a user-owned directory mode 0700.
// It refuses to chmod through a final symlink: the leaf must be a real directory
// owned by the current uid after creation.
func EnsurePrivateDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("appdirs: empty directory")
	}
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("appdirs: directory must be absolute: %q", dir)
	}
	dir = filepath.Clean(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return validatePrivateDir(dir)
}

func validatePrivateDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("appdirs: %s is a symlink", dir)
	}
	if !fi.IsDir() {
		return fmt.Errorf("appdirs: %s is not a directory", dir)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("appdirs: cannot inspect ownership of %s", dir)
	}
	if int(st.Uid) != os.Getuid() {
		return fmt.Errorf("appdirs: %s not owned by current user", dir)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("chmod %s: %w", dir, err)
		}
		// Re-check after chmod.
		fi, err = os.Lstat(dir)
		if err != nil {
			return err
		}
		if fi.Mode().Perm()&0o077 != 0 {
			return fmt.Errorf("appdirs: %s still group/other accessible after chmod", dir)
		}
	}
	return nil
}
