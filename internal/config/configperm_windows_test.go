//go:build windows

package config

import (
	"os/exec"
	"testing"
)

// makeNonPrivate grants a well-known group read access to path, so
// appdirs.FileIsOwnerOnly reports false.
//
// It adds an explicit ACE rather than relying on what the host happens to
// inherit. That distinction is the whole reason this file exists: the machine
// this was written on has a third-party group inherited into %TEMP%,
// Documents and the repository checkout, so a fixture that "just uses a temp
// file" would pass or fail according to which host ran it (MADR 0155 F5, and
// the same trap PLAN 0154 P2 hit twice).
//
// BUILTIN\Users is chosen because FileIsOwnerOnly tolerates only the owner,
// SYSTEM and Administrators — Users is a foreign trustee by that definition and
// exists on every Windows install.
func makeNonPrivate(t *testing.T, path string) {
	t.Helper()
	cmd := exec.Command("icacls", path, "/grant", "*S-1-5-32-545:(R)")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("icacls could not grant Users read on %s (%v): %s", path, err, out)
	}
}
