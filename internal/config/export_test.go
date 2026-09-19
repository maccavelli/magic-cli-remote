package config

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/appdirs"
)

// SetSystemRootsForTest points Load at fn for the rest of t (MADR 0162 D2).
// It lives in an _test.go file, so it exists only in test binaries.
func SetSystemRootsForTest(t testing.TB, fn func(appdirs.Product) (appdirs.Roots, []appdirs.Diagnostic, error)) {
	t.Helper()
	prev := systemRoots
	systemRoots = fn
	t.Cleanup(func() { systemRoots = prev })
}
