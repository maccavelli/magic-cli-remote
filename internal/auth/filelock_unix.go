//go:build unix

package auth

import "github.com/maccavelli/magic-cli-remote/internal/fsutil"

// withPathLock holds an exclusive advisory flock on path+".lock" for the
// duration of fn. CLI and daemon both open their own Store instances against
// the same devices.json; without this, concurrent read-modify-write can drop
// devices or resurrect revokes (Phase 1.5 / P1-5).
//
// This used to carry its own copy of the acquire loop — character for character
// the same function as internal/fsutil's, while the Windows file next door
// already delegated. Two copies of one algorithm is how a fix reaches one
// platform and not the other: the queued acquire that removed the starvation in
// MADR 0165 F1 would have landed on Windows only, and Unix would have kept
// polling every 20ms with nothing to say so (MADR 0165 D3).
func withPathLock(path string, fn func() error) error {
	return fsutil.WithLock(path, lockTimeout, fn)
}
