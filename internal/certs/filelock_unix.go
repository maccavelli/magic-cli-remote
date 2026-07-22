//go:build unix

package certs

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// certLockTimeout bounds waiting for the cert-generation lock. A generate is a
// couple of small file writes; a hold this long means a peer wedged, and a
// bounded failure beats blocking daemon startup forever.
const certLockTimeout = 10 * time.Second

// lockCertDir takes an exclusive cross-process lock for cert (re)generation in
// dir. The daemon and a concurrent CLI `pair` (which materializes the bundle to
// print its fingerprint) both call Ensure; without this, two first-run generates
// race and can install a key and cert that do not match. Returns an unlock func.
func lockCertDir(dir string) (func(), error) {
	lockPath := filepath.Join(dir, ".certs.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("certs: open lock %s: %w", lockPath, err)
	}
	deadline := time.Now().Add(certLockTimeout)
	for {
		lerr := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if lerr == nil {
			break
		}
		if lerr != unix.EWOULDBLOCK {
			_ = f.Close()
			return nil, fmt.Errorf("certs: flock %s: %w", lockPath, lerr)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("certs: lock %s busy for more than %s", lockPath, certLockTimeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, nil
}
