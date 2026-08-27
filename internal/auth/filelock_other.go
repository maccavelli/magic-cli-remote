//go:build !unix && !windows

package auth

// withPathLock is a no-op only on platforms with no file locking at all
// (js/wasm, plan9). Unix uses flock and Windows uses LockFileEx (MADR 0116
// D6); this build is the residue, and there in-process Store.mu is the only
// serialization available.
func withPathLock(_ string, fn func() error) error {
	return fn()
}
