// Package fsutil provides shared filesystem primitives (atomic write, locks).
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AtomicOptions controls WriteFileAtomic.
type AtomicOptions struct {
	Perm     os.FileMode
	SyncFile bool
	SyncDir  bool
}

// WriteFileAtomic replaces path with data using a unique same-directory temp
// file, optional fsync, rename, and optional parent-directory sync.
func WriteFileAtomic(path string, data []byte, opts AtomicOptions) error {
	if path == "" {
		return fmt.Errorf("fsutil: empty path")
	}
	if !filepath.IsAbs(path) {
		// Relative is allowed for tests; clean for rename safety.
		path = filepath.Clean(path)
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if opts.Perm == 0 {
		opts.Perm = 0o600
	}

	// Refuse if the final target is a symlink when it already exists.
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("fsutil: target %s is a symlink", path)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	pattern := "." + base + "-*"
	// CreateTemp rejects patterns with path separators; base should be a name.
	if strings.Contains(base, string(os.PathSeparator)) {
		return fmt.Errorf("fsutil: invalid base name %q", base)
	}
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return fmt.Errorf("fsutil: create temp: %w", err)
	}
	tmp := f.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmp)
		}
	}()

	if err := f.Chmod(opts.Perm); err != nil {
		_ = f.Close()
		return fmt.Errorf("fsutil: chmod temp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("fsutil: write temp: %w", err)
	}
	if opts.SyncFile {
		if err := f.Sync(); err != nil {
			_ = f.Close()
			return fmt.Errorf("fsutil: sync temp: %w", err)
		}
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("fsutil: close temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("fsutil: rename: %w", err)
	}
	cleanup = false
	_ = os.Chmod(path, opts.Perm)

	if opts.SyncDir {
		_ = syncDir(dir)
	}
	return nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
