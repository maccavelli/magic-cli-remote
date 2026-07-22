//go:build !unix

package certs

// lockCertDir is a no-op where flock is unavailable. Cert generation is not
// serialized across processes there (the same platform limitation as the auth
// device-store lock).
func lockCertDir(dir string) (func(), error) {
	return func() {}, nil
}
