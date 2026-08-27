package appdirs

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestFileIsOwnerOnly pins the property MADR 0116 D22 extracted from
// providerauth: "private to me", asked once and answered per platform.
func TestFileIsOwnerOnly(t *testing.T) {
	dir := t.TempDir()

	tight := filepath.Join(dir, "tight")
	if err := os.WriteFile(tight, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	ok, err := FileIsOwnerOnly(tight)
	if err != nil {
		t.Fatalf("FileIsOwnerOnly(0600): %v", err)
	}
	if !ok {
		t.Error("a 0600 file reported as not owner-only")
	}

	// The negative case is Unix-only: on Windows the mode argument is ignored
	// and privacy comes from the DACL, so a "0666" file is still owner-only
	// until its ACL says otherwise. Asserting the Unix answer there is exactly
	// the mistake F23a records.
	if runtime.GOOS != "windows" {
		loose := filepath.Join(dir, "loose")
		if err := os.WriteFile(loose, []byte("secret"), 0o666); err != nil {
			t.Fatal(err)
		}
		ok, err := FileIsOwnerOnly(loose)
		if err != nil {
			t.Fatalf("FileIsOwnerOnly(0666): %v", err)
		}
		if ok {
			t.Error("a 0666 file reported as owner-only")
		}
	}
}

// TestFileIsOwnerOnlyRejectsBadInput covers the shared preconditions.
func TestFileIsOwnerOnlyRejectsBadInput(t *testing.T) {
	if _, err := FileIsOwnerOnly(""); err == nil {
		t.Error("accepted an empty path")
	}
	if _, err := FileIsOwnerOnly("relative"); err == nil {
		t.Error("accepted a relative path")
	}
	if _, err := FileIsOwnerOnly(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("accepted a missing path")
	}
}
