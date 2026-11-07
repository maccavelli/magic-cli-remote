//go:build unix

package saved

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTheFileIsOwnerOnly: a saved catalog holds no credential, but it names
// an account's models, so it and its directory are the owner's alone
// (MADR 0235 D1).
func TestTheFileIsOwnerOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "instances", "k", "catalogs")
	s := Open(dir, nil)
	s.Save("vendor", "", live("vendor-a"))
	s.Flush()

	fi, err := os.Stat(filepath.Join(dir, "vendor.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %o, want 600", got)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := di.Mode().Perm(); got != 0o700 {
		t.Errorf("directory mode = %o, want 700", got)
	}
}
