//go:build windows

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBinaryWarningIgnoresCaseOnWindows is MADR 0207 D16: Windows paths are
// case-insensitive, so a definition that names this binary in another case
// runs this binary, and a refresh does not warn that it runs another; a
// definition that names another file still warns.
func TestBinaryWarningIgnoresCaseOnWindows(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	other := strings.ToUpper(exe)
	if other == exe {
		other = strings.ToLower(exe)
	}
	if other == exe {
		t.Fatalf("this binary's path %q has no letter whose case can change", exe)
	}
	if got := binaryWarning(other); got != nil {
		t.Errorf("binaryWarning(this binary in another case) = %q, want none", got)
	}
	another := filepath.Join(t.TempDir(), "other.exe")
	if err := os.WriteFile(another, []byte("another binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := binaryWarning(another); len(got) != 1 {
		t.Errorf("binaryWarning(another file) = %q, want one warning", got)
	}
}
