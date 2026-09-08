//go:build windows

package fsutil

import (
	"errors"

	"golang.org/x/sys/windows"
)

// retryableRenameErr reports whether err is Windows saying the destination is
// currently held open, rather than a permanent refusal.
//
// On POSIX a rename replaces a directory entry and existing readers keep their
// inode, so no holder can block it. Windows has no such rule: MoveFileEx fails
// while any handle to the destination is open without FILE_SHARE_DELETE, and
// that is what [WriteFileAtomic] hits when antivirus, a backup agent, Windows
// Search, an editor — or the daemon's own reader — has the file open.
//
// ERROR_ACCESS_DENIED is what os.Rename actually returns for this. It was
// measured, not assumed (MADR 0153 F2): ERROR_SHARING_VIOLATION is the name the
// condition goes by and the one a predicate written from intuition would key
// on, and Windows does not produce it here. Both are matched anyway, because
// the documentation describes the second and a different sharing mode or
// filesystem may yet produce it.
//
// The ambiguity is deliberate and costed. ERROR_ACCESS_DENIED is also the
// permanent "you may not write here" error, so a genuine permission failure
// exhausts the retry budget before failing with the same error it would have
// returned at once (0153 F4). That is the accepted price of keying on what the
// operating system returns rather than on what the failure is called.
func retryableRenameErr(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
