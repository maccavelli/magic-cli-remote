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

// ownerOnlyAlternative is mcremote's product-specific addition to
// appdirs.NotOwnerOnlyDetail, which already names who can read the file and
// gives an icacls command that works for any file (MADR 0155 D4, F6).
//
// mcremote alone owns a private config directory and a setup-service that
// writes into it, so only mcremote can offer this. mcrelay has neither, which
// is why this is not part of the shared detail. Never "chmod": there is no
// such command here.
func ownerOnlyAlternative() string {
	return "; or move it under the private config directory, or re-run: mcremote setup-service --force"
}
