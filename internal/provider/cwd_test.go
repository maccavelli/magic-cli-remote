package provider

import (
	"errors"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"testing"
)

// MADR 0069 P1 (U4) — cwd validation preserves the errno. The incident
// shape: a macOS TCC denial (EPERM on stat) was reported as "cwd is not a
// directory", sending the operator hunting for a typo instead of a privacy
// grant.

func statErr(err error) func(string) (os.FileInfo, error) {
	return func(path string) (os.FileInfo, error) {
		return nil, &os.PathError{Op: "stat", Path: path, Err: err}
	}
}

func TestResolveSessionCWDPermissionDenied(t *testing.T) {
	_, err := ResolveSessionCWD("/protected/dir", "", statErr(syscall.EPERM))
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("EPERM not preserved in chain: %v", err)
	}
	if strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("EPERM misreported as not-a-directory: %v", err)
	}
	// EACCES takes the same branch.
	if _, err := ResolveSessionCWD("/p", "", statErr(syscall.EACCES)); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("EACCES not preserved: %v", err)
	}
}

func TestResolveSessionCWDNotExist(t *testing.T) {
	_, err := ResolveSessionCWD("/nope", "", statErr(syscall.ENOENT))
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("want does-not-exist message, got %v", err)
	}
	if errors.Is(err, fs.ErrPermission) {
		t.Fatalf("ENOENT must not classify as permission: %v", err)
	}
}

func TestResolveSessionCWDNotADirectory(t *testing.T) {
	f := t.TempDir() + "/file"
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveSessionCWD(f, "", nil)
	if err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("want not-a-directory for a plain file, got %v", err)
	}
}

func TestResolveSessionCWDDefaultsAndHome(t *testing.T) {
	dir := t.TempDir()
	got, err := ResolveSessionCWD("", dir, nil)
	if err != nil || got != dir {
		t.Fatalf("default_cwd fallback: got %q, %v", got, err)
	}
	home, herr := os.UserHomeDir()
	if herr != nil {
		t.Skip("no home dir in test environment")
	}
	got, err = ResolveSessionCWD("", "", nil)
	if err != nil || got != home {
		// Never os.Getwd(): the empty-input contract (0069 P1).
		t.Fatalf("empty means home: got %q, %v", got, err)
	}
}
