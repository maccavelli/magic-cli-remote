//go:build windows

package service

import (
	"os/exec"
	"testing"
)

// exposeDir gives BUILTIN\Users inheritable read access to dir, so
// appdirs.FileIsOwnerOnly reports it as not private. The grant is explicit, not
// taken from whatever %TEMP% inherits (MADR 0155 F5).
//
// It lives in a _windows_test.go file because running icacls by bare name is
// only acceptable in platform-specific tests (MADR 0147 D9/D10,
// testexec.TestNoTestResolvesABareBinaryName), the same split as
// internal/config's configperm_windows_test.go.
func exposeDir(t *testing.T, dir string) {
	t.Helper()
	out, err := exec.Command("icacls", dir, "/grant", "*S-1-5-32-545:(OI)(CI)(R)").CombinedOutput()
	if err != nil {
		t.Fatalf("icacls grant: %v: %s", err, out)
	}
}
