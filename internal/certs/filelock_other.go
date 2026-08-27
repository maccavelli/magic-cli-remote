//go:build !unix && !windows

package certs

// lockCertDir is a no-op only on platforms with no file locking at all
// (js/wasm, plan9). Unix uses flock and Windows uses LockFileEx (MADR 0116
// D6); cert generation is unserialized across processes only in this residual
// build.
func lockCertDir(dir string) (func(), error) {
	return func() {}, nil
}
