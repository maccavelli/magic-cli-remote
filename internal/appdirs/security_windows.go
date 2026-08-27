//go:build windows

package appdirs

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/windows"
)

// privateDACL grants the owner and SYSTEM full control and nobody else, with
// inheritance severed ("P" = SDDL_PROTECTED). It is the Windows equivalent of
// mode 0700: Administrators is deliberately omitted, matching the Unix rule
// that only the owning uid may read the daemon's secrets (MADR 0116 D4).
const privateDACL = "D:P(A;OICI;FA;;;OW)(A;OICI;FA;;;SY)"

// privateACL parses privateDACL once per process. SetNamedSecurityInfo takes a
// parsed *ACL, not an SDDL string, so the descriptor is built here and its
// DACL extracted.
var privateACL = sync.OnceValues(func() (*windows.ACL, error) {
	sd, err := windows.SecurityDescriptorFromString(privateDACL)
	if err != nil {
		return nil, fmt.Errorf("appdirs: parse private DACL: %w", err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return nil, fmt.Errorf("appdirs: extract private DACL: %w", err)
	}
	return acl, nil
})

// currentUserSID returns the SID of the process token's user.
func currentUserSID() (*windows.SID, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, fmt.Errorf("appdirs: open process token: %w", err)
	}
	defer token.Close()
	u, err := token.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("appdirs: token user: %w", err)
	}
	return u.User.Sid, nil
}

// checkPrivateDir verifies dir is a real directory owned by the current user
// with the private DACL applied, repairing only the DACL. Ownership mismatch
// and reparse points are hard errors, never repaired.
//
// It is shared by EnsurePrivateDir and ValidateRuntimeDir so the two cannot
// drift. repair=false makes it read-only (the ValidateRuntimeDir contract).
func checkPrivateDir(dir string, repair bool) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("appdirs: %s is a symlink", dir)
	}
	// A non-symlink reparse point (junction, AF_UNIX socket, OneDrive
	// placeholder) surfaces as ModeIrregular. Refusing it is the Windows
	// analogue of the existing symlink refusal.
	if fi.Mode()&os.ModeIrregular != 0 {
		return fmt.Errorf("appdirs: %s is a reparse point", dir)
	}
	if !fi.IsDir() {
		return fmt.Errorf("appdirs: %s is not a directory", dir)
	}

	want, err := currentUserSID()
	if err != nil {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("appdirs: read security info for %s: %w", dir, err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("appdirs: read owner of %s: %w", dir, err)
	}
	if !owner.Equals(want) {
		return fmt.Errorf("appdirs: %s not owned by current user", dir)
	}

	// Compare the effective descriptor's SDDL against the target. Only write
	// when it differs — that comparison is what makes a second call a no-op
	// (the MADR 0116 D4 idempotency contract).
	if sddlEquivalent(sd.String(), privateDACL) {
		return nil
	}
	if !repair {
		return fmt.Errorf("appdirs: %s does not carry the private DACL", dir)
	}
	acl, err := privateACL()
	if err != nil {
		return err
	}
	if err := windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil); err != nil {
		return fmt.Errorf("appdirs: set private DACL on %s: %w", dir, err)
	}
	// Re-read and verify, mirroring the Unix chmod re-check.
	sd, err = windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("appdirs: re-read security info for %s: %w", dir, err)
	}
	if !sddlEquivalent(sd.String(), privateDACL) {
		return fmt.Errorf("appdirs: %s still not private after applying DACL", dir)
	}
	return nil
}

// sddlEquivalent reports whether got carries the same DACL portion as want.
// GetNamedSecurityInfo returns a descriptor that also carries owner and group,
// so a raw string comparison against a DACL-only SDDL never matches.
func sddlEquivalent(got, want string) bool {
	return extractDACL(got) == extractDACL(want)
}

// extractDACL returns the "D:..." component of an SDDL string, or "" when
// absent. Components are single-letter prefixed and ordered O:G:D:S:.
func extractDACL(sddl string) string {
	i := indexComponent(sddl, "D:")
	if i < 0 {
		return ""
	}
	rest := sddl[i:]
	if j := indexComponent(rest[2:], "S:"); j >= 0 {
		return rest[:j+2]
	}
	return rest
}

// indexComponent finds an SDDL component prefix at nesting depth zero, so a
// "D:" inside an ACE string is not mistaken for the DACL marker.
func indexComponent(s, prefix string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && i+len(prefix) <= len(s) && s[i:i+len(prefix)] == prefix {
			return i
		}
	}
	return -1
}

// absClean validates and cleans a directory argument shared by the Windows
// EnsurePrivateDir and ValidateRuntimeDir entry points.
func absClean(dir, what string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("appdirs: empty %s", what)
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("appdirs: %s must be absolute: %q", what, dir)
	}
	return filepath.Clean(dir), nil
}
