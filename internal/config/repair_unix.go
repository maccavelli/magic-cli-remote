//go:build unix

package config

import "os"

// repairOwnerOnly tightens path to 0600 and reports whether it did.
//
// This is MADR 0155 D8. It exists because [appdirs.EnsurePrivateDir] cannot
// self-heal a file on this platform: the unix implementation is MkdirAll plus a
// check of the *directory*, and it never enumerates children. Windows gets the
// same outcome for free, because installing a private DACL on the directory
// re-propagates to the files already inside it (0155 F7).
//
// Tightening matches what the product already does when it creates the config
// (setup.go chmods 0600 before the rename), so this restores the state the
// product asked for rather than inventing a new one.
//
// It only ever removes access (PLAN 0155 C2): the mode is set to 0600, which
// cannot be wider than what a non-owner-readable file already had.
func repairOwnerOnly(path string) (bool, error) {
	if err := os.Chmod(path, 0o600); err != nil {
		return false, err
	}
	return true, nil
}

// ownerOnlyAlternative is mcremote's product-specific addition to
// appdirs.NotOwnerOnlyDetail. On Unix there is none: chmod 0600 is the whole
// fix, and the detail already says it (MADR 0155 D4, PLAN P4).
func ownerOnlyAlternative() string { return "" }
