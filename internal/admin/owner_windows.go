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
// The index is not in fi: os.Lstat's Sys() on Windows is a
// *syscall.Win32FileAttributeData, which carries no index at all, so the old
// type assertion to *windows.ByHandleFileInformation failed on every call and
// the shutdown guard never ran (MADR 0159 F4). The index has to come from a
// handle. An AF_UNIX socket file is a reparse point (attrs 0x420), so the
// open must not follow it: FILE_FLAG_OPEN_REPARSE_POINT. Zero access rights
// and a full share mode keep the open from interfering with the listener.
//
// Any failure reports (0, false), which the caller's `sockInode != 0` guard
// treats as "do not remove" — the conservative branch.
func socketIdentity(path string, _ fs.FileInfo) (uint64, bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, false
	}
	h, err := windows.CreateFile(p, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return 0, false
	}
	id := uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)
	return id, id != 0
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
