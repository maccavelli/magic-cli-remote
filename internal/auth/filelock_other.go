//go:build !unix

package auth

// withPathLock is a no-op on non-unix: multi-process CLI+daemon sharing of the
// auth files is a Linux/macOS deployment concern. In-process Store.mu still
// serializes within one process.
func withPathLock(_ string, fn func() error) error {
	return fn()
}
