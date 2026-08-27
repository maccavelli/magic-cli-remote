//go:build windows

package admin

import (
	"fmt"
	"io/fs"

	"golang.org/x/sys/windows"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
)

// ownedByCurrentUser reports whether the file at path belongs to the calling
// user, by comparing its owner SID to the process token's user SID.
//
// The fs.FileInfo is unused on Windows: Lstat carries no owner, so the answer
// has to come from the security descriptor.
func ownedByCurrentUser(path string, _ fs.FileInfo) (bool, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return false, fmt.Errorf("read owner: %w", err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return false, fmt.Errorf("read owner: %w", err)
	}
	self, err := appdirs.CurrentUserSID()
	if err != nil {
		return false, err
	}
	return owner.Equals(self), nil
}

// socketIdentity returns a value that changes if the path stops naming the
// same socket. On Windows that is the NTFS file index.
//
// Any failure reports (0, false), which the caller's `sockInode != 0` guard
// already treats as "do not remove" — the conservative branch.
func socketIdentity(fi fs.FileInfo) (uint64, bool) {
	sys, ok := fi.Sys().(*windows.ByHandleFileInformation)
	if !ok {
		return 0, false
	}
	return uint64(sys.FileIndexHigh)<<32 | uint64(sys.FileIndexLow), true
}

// secureSocket restricts socketPath to the owning user.
//
// os.Chmod would be inert here: on Windows it only toggles the read-only
// attribute and grants no access control at all, which is what made the
// package's "auth is filesystem permissions" premise false (MADR 0116 F6).
// An owner-only DACL is the real equivalent, and a failure to apply it is
// fatal to Serve rather than logged.
func secureSocket(socketPath string) error {
	return appdirs.SecurePrivateFile(socketPath)
}
