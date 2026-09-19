//go:build windows

package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
	"github.com/maccavelli/magic-cli-remote/internal/fsutil"
)

// TestWriteFileAtomicIsOwnerOnlyInPrivateDir is the Windows half of
// WriteFileAtomic's contract (MADR 0159 D12). The POSIX tests assert mode bits,
// which Windows does not have, and skip here correctly; the property that
// matters on Windows is the ACL. A file written atomically into a private
// directory must be owner-only both when it is created and when it replaces an
// existing file, because the rename-over path is how every config and state
// file is rewritten.
func TestWriteFileAtomicIsOwnerOnlyInPrivateDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := appdirs.EnsurePrivateDir(dir); err != nil {
		t.Fatalf("EnsurePrivateDir: %v", err)
	}
	path := filepath.Join(dir, "state.json")
	opts := fsutil.AtomicOptions{Perm: 0o600}

	for i, body := range []string{`{"v":1}`, `{"v":2}`} {
		if err := fsutil.WriteFileAtomic(path, []byte(body), opts); err != nil {
			t.Fatalf("write %d: %v", i+1, err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != body {
			t.Fatalf("write %d: content = %q, want %q", i+1, got, body)
		}
		ok, err := appdirs.FileIsOwnerOnly(path)
		if err != nil {
			t.Fatalf("write %d: FileIsOwnerOnly: %v", i+1, err)
		}
		if !ok {
			t.Fatalf("write %d: file is readable by another principal after an atomic write into a private dir", i+1)
		}
	}
}
