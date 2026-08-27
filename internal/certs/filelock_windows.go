//go:build windows

package certs

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

// lockCertDir takes an exclusive cross-process lock for cert (re)generation in
// dir, using LockFileEx (MADR 0116 D6). Returns an unlock func.
//
// This does not delegate to fsutil.WithLock because the contract differs:
// callers here take the lock and release it later, rather than running a
// closure under it. The retry loop and the byte range match fsutil's so the
// two cannot exclude differently.
func lockCertDir(dir string) (func(), error) {
	lockPath := filepath.Join(dir, certLockName)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("certs: open lock %s: %w", lockPath, err)
	}
	h := windows.Handle(f.Fd())
	deadline := time.Now().Add(certLockTimeout)
	for {
		var ol windows.Overlapped
		lerr := windows.LockFileEx(h,
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0, 1, 0, &ol)
		if lerr == nil {
			break
		}
		if lerr != windows.ERROR_LOCK_VIOLATION && lerr != windows.ERROR_IO_PENDING {
			_ = f.Close()
			return nil, fmt.Errorf("certs: lock %s: %w", lockPath, lerr)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("certs: lock %s busy for more than %s", lockPath, certLockTimeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return func() {
		var ol windows.Overlapped
		_ = windows.UnlockFileEx(h, 0, 1, 0, &ol)
		_ = f.Close()
	}, nil
}
