//go:build unix

package service

import (
	"os"
	"testing"
)

// exposeDir makes dir group/other-readable, so appdirs.FileIsOwnerOnly reports
// it as not private. The Windows sibling adds an explicit ACE instead, because
// mode bits carry no access control there.
func exposeDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}
