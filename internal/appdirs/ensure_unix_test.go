//go:build unix

package appdirs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsurePrivateDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "nested", "private")
	if err := EnsurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !fi.IsDir() {
		t.Fatal("not a dir")
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("mode %04o allows group/other", fi.Mode().Perm())
	}
	if err := ValidateRuntimeDir(dir); err != nil {
		t.Fatal(err)
	}
}

func TestEnsurePrivateDirRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	// EnsurePrivateDir MkdirAll is a no-op on existing symlink path behavior:
	// Lstat of final path is a symlink → error after MkdirAll.
	if err := EnsurePrivateDir(link); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestCheckSocketPathLength(t *testing.T) {
	if err := CheckSocketPathLength("/tmp/short.sock"); err != nil {
		t.Fatal(err)
	}
	long := "/" + strings.Repeat("a", MaxUnixSocketPathLen()+10)
	if err := CheckSocketPathLength(long); err == nil {
		t.Fatal("expected overlength error")
	}
}
