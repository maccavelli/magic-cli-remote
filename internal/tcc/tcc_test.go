package tcc

import (
	"os"
	"syscall"
	"testing"
)

// MADR 0069 P5 (U6) — probe classification, injected so no test owns the
// host's TCC state.
func TestProbeWith(t *testing.T) {
	home := func() (string, error) { return "/Users/x", nil }
	stat := func(err error) func(string) (os.FileInfo, error) {
		return func(p string) (os.FileInfo, error) {
			if err != nil {
				return nil, &os.PathError{Op: "stat", Path: p, Err: err}
			}
			return os.Stat(os.TempDir()) // any real FileInfo
		}
	}

	if got := probeWith("linux", home, stat(nil)); got != NotApplicable {
		t.Fatalf("linux = %v, want NotApplicable", got)
	}
	if got := probeWith("darwin", home, stat(nil)); got != Granted {
		t.Fatalf("stat ok = %v, want Granted", got)
	}
	if got := probeWith("darwin", home, stat(syscall.EPERM)); got != Denied {
		t.Fatalf("EPERM = %v, want Denied", got)
	}
	if got := probeWith("darwin", home, stat(syscall.ENOENT)); got != Unknown {
		t.Fatalf("ENOENT = %v, want Unknown", got)
	}
	if got := probeWith("darwin", func() (string, error) { return "", nil }, stat(nil)); got != Unknown {
		t.Fatalf("no home = %v, want Unknown", got)
	}
}
