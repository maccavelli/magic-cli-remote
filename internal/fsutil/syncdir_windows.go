//go:build windows

package fsutil

// syncDir is a no-op on Windows.
//
// File.Sync is FlushFileBuffers (internal/poll/fd_fsync_windows.go), which
// requires GENERIC_WRITE on the handle; os.Open gives read access, so syncing
// a directory handle returns "Access is denied" — golang/go#75541. There is no
// Windows API that flushes a directory entry independently of its files.
//
// MADR 0116 D5 decided this is a no-op rather than an error: returning the
// error would break every SyncDir caller (the device token store included,
// internal/auth/store.go), and swallowing it inside callers would
// re-introduce exactly the silent success MADR 0074 D25 forbids. The
// consequence is real and documented in docs/ops-windows-install.md: on NTFS
// the rename is ordered, but the directory entry is not separately flushed, so
// a power loss in the window after WriteFileAtomic returns can lose the
// rename. SyncFile is unaffected — FlushFileBuffers on a file handle works
// normally.
func syncDir(dir string) error { return nil }
