//go:build unix

package appdirs

import "os"

// FileIsOwnerOnly reports whether path is readable only by its owner.
//
// On Unix that is the POSIX mode test callers used to inline:
// Perm()&0o077 == 0. The Windows implementation asks the same question of the
// file's owner SID and DACL, because mode bits carry no access control there
// (MADR 0116 D22).
func FileIsOwnerOnly(path string) (bool, error) {
	path, err := absClean(path, "path")
	if err != nil {
		return false, err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return fi.Mode().Perm()&0o077 == 0, nil
}
