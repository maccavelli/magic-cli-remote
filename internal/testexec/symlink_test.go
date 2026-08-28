package testexec

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

// TestSkipIfNoSymlinkProbeNeverFails asserts the probe's outcome is binary:
// it returns on a machine that holds the privilege and skips on one that does
// not, but it never fails. t.TempDir cleans the probe's target and link, so
// there is no artefact left to assert about.
//
// The nested subtest is deliberate: calling SkipIfNoSymlink(t) directly would
// skip this test on an unprivileged machine instead of observing that it did.
// On such a machine "probe" is reported as skipped, which is the helper
// working; the three skips MADR 0118 is about are the call sites.
func TestSkipIfNoSymlinkProbeNeverFails(t *testing.T) {
	if !t.Run("probe", func(t *testing.T) { SkipIfNoSymlink(t) }) {
		t.Fatal("the symlink probe must return or skip, never fail")
	}
}

// TestSkipIfNoSymlinkMatchesPrivilegeError pins the error unwrapping the D1
// probe measured on windows/amd64: os.Symlink's *os.LinkError unwraps through
// errors.As to a syscall.Errno carrying ERROR_PRIVILEGE_NOT_HELD.
//
// This runs on every platform, including the Unix hosts where the errno never
// occurs, because it is the assertion that would otherwise break silently if a
// future Go release changed the wrapping — turning the skip branch dead and
// every call site fatal.
func TestSkipIfNoSymlinkMatchesPrivilegeError(t *testing.T) {
	var errno syscall.Errno

	privilege := error(&os.LinkError{
		Op:  "symlink",
		Old: "target",
		New: "link",
		Err: errPrivilegeNotHeld,
	})
	if !errors.As(privilege, &errno) {
		t.Fatalf("os.LinkError must unwrap to syscall.Errno, got %T", privilege)
	}
	if errno != errPrivilegeNotHeld {
		t.Fatalf("errno = %d, want %d", errno, errPrivilegeNotHeld)
	}

	// D2: every other failure is fatal rather than a skip, so the match must
	// discriminate on the value and not merely on the type. 5 is
	// ERROR_ACCESS_DENIED — a plausible symlink failure that is not this one.
	denied := error(&os.LinkError{
		Op:  "symlink",
		Old: "target",
		New: "link",
		Err: syscall.Errno(5),
	})
	if errors.As(denied, &errno) && errno == errPrivilegeNotHeld {
		t.Fatal("a non-privilege errno must not match ERROR_PRIVILEGE_NOT_HELD")
	}
}
