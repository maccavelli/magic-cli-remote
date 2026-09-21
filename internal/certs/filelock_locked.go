//go:build unix || windows

package certs

import (
	"path/filepath"
	"strings"

	"github.com/maccavelli/magic-cli-remote/internal/fsutil"
)

// lockCertDir takes an exclusive cross-process lock for cert (re)generation in
// dir. The daemon and a concurrent CLI `pair` (which materializes the bundle to
// print its fingerprint) both call Ensure; without this, two first-run generates
// race and can install a key and cert that do not match. Returns an unlock func.
//
// One file for both platforms, because there is now one implementation behind
// them. This replaces a per-platform copy of fsutil's acquire loop — a copy whose
// own comment asked that "the retry loop and the byte range match fsutil's so the
// two cannot exclude differently", which is a parity a comment can request but
// not enforce. Delegating makes it true by construction, and it is how the queued
// acquire from MADR 0165 D1 reaches cert generation at all rather than stopping
// at internal/auth.
//
// The build tag mirrors fsutil's own coverage exactly; platforms with no file
// locking keep the no-op in filelock_other.go.
func lockCertDir(dir string) (func(), error) {
	return fsutil.Acquire(certLockPathBase(dir), certLockTimeout)
}

// certLockPathBase is what fsutil.Acquire must be given, and getting it wrong is a
// bug this repository has already shipped twice rather than a hypothetical one.
//
// Acquire derives the lock file by appending ".lock", and certLockName already
// ends in ".lock". Passing certLockName itself would lock
// <dir>/.certs.lock.lock — a file nothing else takes — so two processes would
// stop excluding each other while both believed they held the lock. The failure
// is silent, which is why it has dedicated regression tests elsewhere:
// internal/provider/credstore/lockfile_test.go and
// internal/providerauth/nativelock_test.go (MADR 0165 C2).
func certLockPathBase(dir string) string {
	return filepath.Join(dir, strings.TrimSuffix(certLockName, ".lock"))
}
