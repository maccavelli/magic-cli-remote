//go:build windows

package auth

import "github.com/maccavelli/magic-cli-remote/internal/fsutil"

// withPathLock holds the cross-process lock for path.
//
// Windows gets a real lock via LockFileEx (MADR 0116 D6). The CLI and the
// daemon share devices.json there exactly as they do on Unix, so the pre-0116
// no-op was a silent correctness hole, not a platform limitation.
func withPathLock(path string, fn func() error) error {
	return fsutil.WithLock(path, lockTimeout, fn)
}
