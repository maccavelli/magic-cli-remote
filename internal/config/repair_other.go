//go:build !unix

package config

// repairOwnerOnly does nothing off Unix, and reports that it did nothing.
//
// A chmod on Windows toggles the read-only attribute and touches no ACL, so
// calling it here would let the caller believe a file was repaired when its
// DACL still names another trustee — the precise failure MADR 0116 D22 exists
// to prevent, restated by 0155.
//
// The repair on this platform is [appdirs.EnsurePrivateDir] on the *directory*,
// which re-propagates to the files inside it (0155 F7, PLAN P3). Returning
// false here is what routes the caller to the message that says so, instead of
// to `chmod 0600`.
func repairOwnerOnly(string) (bool, error) { return false, nil }

// ownerOnlyRemedy names the fix a Windows operator can actually perform.
//
// Never "chmod": there is no such command here, and the mode bits are not what
// was tested — FileIsOwnerOnly asks about the owner SID and the DACL on this
// platform (MADR 0116 D22). Telling a Windows operator to chmod is how the
// existing mcrelay message wasted an hour of this author's time (0155 F6).
func ownerOnlyRemedy(string) string {
	return "move it under the private config directory, or re-run: mcremote setup-service --force"
}
