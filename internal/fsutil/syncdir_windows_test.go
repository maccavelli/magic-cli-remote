//go:build windows

package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSyncDirIsNoOpOnWindows pins MADR 0116 D5: syncing a directory handle is
// "Access is denied" on Windows (golang/go#75541), so syncDir reports success
// rather than failing every durable write.
func TestSyncDirIsNoOpOnWindows(t *testing.T) {
	if err := syncDir(t.TempDir()); err != nil {
		t.Fatalf("syncDir on Windows returned %v, want nil", err)
	}
	// Also nil for a path that does not exist: the no-op inspects nothing.
	if err := syncDir(filepath.Join(t.TempDir(), "absent")); err != nil {
		t.Fatalf("syncDir on a missing dir returned %v, want nil", err)
	}
}

// TestWriteFileAtomicSyncDirSucceedsOnWindows is the direct MADR 0116 F5
// regression: the device token store writes with SyncDir true, and before D5
// that returned an error after the rename had already landed.
func TestWriteFileAtomicSyncDirSucceedsOnWindows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	want := []byte(`{"devices":[]}`)
	if err := WriteFileAtomic(path, want, AtomicOptions{
		Perm: 0o600, SyncFile: true, SyncDir: true,
	}); err != nil {
		t.Fatalf("WriteFileAtomic with SyncDir: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("content = %q, want %q", got, want)
	}
	// Rewriting an existing file must also succeed (rename over a target).
	if err := WriteFileAtomic(path, []byte("{}"), AtomicOptions{
		Perm: 0o600, SyncFile: true, SyncDir: true,
	}); err != nil {
		t.Fatalf("second WriteFileAtomic: %v", err)
	}
}
