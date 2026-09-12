//go:build unix

package appdirs

// notOwnerOnly is the Unix half of NotOwnerOnlyDetail. FileIsOwnerOnly here is
// Perm()&0o077 == 0, so the principals are "group/other" and the fix is the
// mode the product itself writes.
func notOwnerOnly(path string) (readers, remedy string) {
	return "group/other", "chmod 0600 " + path
}
