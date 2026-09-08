//go:build unix

package config

import (
	"os"
	"testing"
)

// makeNonPrivate makes path readable by someone other than its owner.
//
// On Unix that is a mode change. The Windows sibling adds an explicit ACE
// instead, because mode bits carry no access control there — the fixture has to
// speak each platform's actual mechanism or the test measures nothing.
func makeNonPrivate(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
}

// makeUnrepairable puts path in a state where repairOwnerOnly cannot fix it, so
// the fatal branch is reachable.
//
// Removing write permission from the containing directory is enough: chmod
// needs to modify the inode, which requires ownership, and the test process
// still owns it — so instead the file is made immutable to this process by
// dropping owner write on the directory and the file together. Where that does
// not hold (running as root), the caller skips.
func makeUnrepairable(t *testing.T, path string) bool {
	t.Helper()
	if os.Geteuid() == 0 {
		return false // root can chmod anything; the fatal branch is unreachable
	}
	dir := path[:len(path)-len("/config.yaml")]
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	return true
}
