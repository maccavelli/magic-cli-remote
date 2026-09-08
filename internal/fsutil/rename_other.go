//go:build !windows

package fsutil

// retryableRenameErr is always false off Windows, and that is a property of the
// platform rather than a simplification.
//
// A POSIX rename(2) replaces a directory entry atomically. Processes holding
// the old file keep their open inode and are unaffected, so no reader can make
// the call fail — there is nothing to wait out and nothing to retry.
//
// Returning a constant here is what makes MADR 0153 C1 a compile-time
// guarantee: the retry loop in [WriteFileAtomic] runs exactly once on every
// non-Windows target no matter what the loop is later edited to do. A single
// cross-platform predicate that string-matched the message or compared against
// syscall.EACCES would compile everywhere and quietly make POSIX retry real
// permission failures — turning an immediate error into a 150ms one on the
// platform that never had the bug.
func retryableRenameErr(error) bool { return false }
