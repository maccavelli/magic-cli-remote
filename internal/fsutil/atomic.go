// Package fsutil provides shared filesystem primitives (atomic write, locks).
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AtomicOptions controls WriteFileAtomic.
type AtomicOptions struct {
	Perm     os.FileMode
	SyncFile bool
	SyncDir  bool
}

// fileOps are the filesystem operations WriteFileAtomic performs. Production
// always uses realOps; tests replace individual members to inject a failure at
// one step without touching production call sites (MADR 0074 D25/P17).
type fileOps struct {
	createTemp func(dir, pattern string) (*os.File, error)
	chmodFile  func(f *os.File, mode os.FileMode) error
	writeFile  func(f *os.File, b []byte) (int, error)
	syncFile   func(f *os.File) error
	closeFile  func(f *os.File) error
	rename     func(oldpath, newpath string) error
	syncDir    func(dir string) error
	// sleep paces the rename retry. Injected so the retry tables cost no real
	// time; production sleeps (MADR 0153 D5).
	sleep func(d time.Duration)
}

func realOps() fileOps {
	return fileOps{
		createTemp: os.CreateTemp,
		chmodFile:  (*os.File).Chmod,
		writeFile:  (*os.File).Write,
		syncFile:   (*os.File).Sync,
		closeFile:  (*os.File).Close,
		rename:     os.Rename,
		syncDir:    syncDir,
		sleep:      time.Sleep,
	}
}

// WriteFileAtomic replaces path with data using a unique same-directory temp
// file, optional fsync, rename, and optional parent-directory sync.
//
// A requested SyncDir failure is returned rather than discarded: a caller that
// asked for durability must not be told the write succeeded when the rename was
// never durably recorded (MADR 0074 D25). The rename has already happened at
// that point, so the new bytes are visible and the caller decides what to do.
func WriteFileAtomic(path string, data []byte, opts AtomicOptions) error {
	return writeFileAtomic(path, data, opts, realOps())
}

func writeFileAtomic(path string, data []byte, opts AtomicOptions, ops fileOps) error {
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
	f, err := ops.createTemp(dir, pattern)
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

	if err := ops.chmodFile(f, opts.Perm); err != nil {
		_ = ops.closeFile(f)
		return fmt.Errorf("fsutil: chmod temp: %w", err)
	}
	if _, err := ops.writeFile(f, data); err != nil {
		_ = ops.closeFile(f)
		return fmt.Errorf("fsutil: write temp: %w", err)
	}
	if opts.SyncFile {
		if err := ops.syncFile(f); err != nil {
			_ = ops.closeFile(f)
			return fmt.Errorf("fsutil: sync temp: %w", err)
		}
	}
	if err := ops.closeFile(f); err != nil {
		return fmt.Errorf("fsutil: close temp: %w", err)
	}
	if err := renameWithRetry(ops, tmp, path); err != nil {
		return fmt.Errorf("fsutil: rename: %w", err)
	}
	cleanup = false
	// Inert on Windows, where Chmod only toggles the read-only attribute: the
	// access control there comes from the private DACL appdirs puts on the
	// parent directory (MADR 0116 D4), not from a file mode.
	_ = os.Chmod(path, opts.Perm)

	if opts.SyncDir {
		if err := ops.syncDir(dir); err != nil {
			return fmt.Errorf("fsutil: sync dir: %w", err)
		}
	}
	return nil
}

// renameBackoffs paces the retry in [renameWithRetry]: five attempts, ~150ms
// of waiting in total.
//
// The budget is derived from the failure being transient rather than from a
// measurement of how long a real holder keeps a file — MADR 0153 D4 records
// that as unverified. It is long enough to outlast something touching a small
// file and short enough that a permanent ERROR_ACCESS_DENIED (0153 F4) costs a
// caller a sixth of a second before failing exactly as it does today.
var renameBackoffs = []time.Duration{
	10 * time.Millisecond,
	20 * time.Millisecond,
	40 * time.Millisecond,
	80 * time.Millisecond,
}

// renameWithRetry renames oldpath to newpath, retrying while the destination is
// merely held open by something else.
//
// On POSIX this is exactly one call: retryableRenameErr is a compile-time false
// there, because a rename cannot be blocked by a reader (MADR 0153 C1). On
// Windows it rides out an antivirus scan, a backup agent, an editor, or the
// daemon's own concurrent reader — any of which makes MoveFileEx fail while its
// handle is open, and none of which lasts (0153 F1, F3).
//
// The error returned on exhaustion is the last attempt's, unchanged, so a
// permanent failure reports what it always reported (0153 C3).
func renameWithRetry(ops fileOps, oldpath, newpath string) error {
	err := ops.rename(oldpath, newpath)
	for attempt := 0; err != nil && attempt < len(renameBackoffs); attempt++ {
		if !retryableRenameErr(err) {
			return err
		}
		ops.sleep(renameBackoffs[attempt])
		err = ops.rename(oldpath, newpath)
	}
	return err
}
