//go:build windows

package appdirs

// ValidateRuntimeDir ensures dir is a private, user-owned directory suitable
// for binding a Unix domain socket. It does not create the directory, and it
// does not repair one that is not already private.
func ValidateRuntimeDir(dir string) error {
	dir, err := absClean(dir, "runtime dir")
	if err != nil {
		return err
	}
	return checkPrivateDir(dir, false)
}
