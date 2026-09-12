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
