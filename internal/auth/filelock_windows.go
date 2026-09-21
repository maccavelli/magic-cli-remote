//go:build windows

package auth

import "github.com/maccavelli/magic-cli-remote/internal/fsutil"

// withPathLock holds the cross-process lock for path.
//
// Windows gets a real lock via LockFileEx (MADR 0116 D6). The CLI and the
// daemon share devices.json there exactly as they do on Unix, so the pre-0116
// no-op was a silent correctness hole, not a platform limitation.
//
// Both platforms now delegate here; this file is no longer the exception it was
// when the Unix side kept its own acquire loop (MADR 0165 D3). The two bodies are
// identical, and are kept as separate build-tagged files only because that is
// what P2's scope allowed — unifying them is tidying, named in the execution
// record rather than done under a phase that did not list this file for it.
func withPathLock(path string, fn func() error) error {
	return fsutil.WithLock(path, lockTimeout, fn)
}
