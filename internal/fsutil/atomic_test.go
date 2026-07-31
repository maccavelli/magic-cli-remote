package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.json")
	if err := WriteFileAtomic(path, []byte(`{"a":1}`), AtomicOptions{Perm: 0o600, SyncFile: true}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"a":1}` {
		t.Fatalf("content = %q", b)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("mode %04o", fi.Mode().Perm())
	}
	// No leftover temps.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %v", entries)
	}

	if err := WriteFileAtomic(path, []byte(`{"a":2}`), AtomicOptions{Perm: 0o600, SyncFile: true, SyncDir: true}); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"a":2}` {
		t.Fatalf("overwrite content = %q", b)
	}
}

func TestWriteFileAtomicRejectsSymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.WriteFile(real, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(link, []byte("y"), AtomicOptions{Perm: 0o600}); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestWithLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.json")
	var ran bool
	if err := WithLock(path, 0, func() error {
		ran = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("fn not run")
	}
	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Fatal(err)
	}
}
