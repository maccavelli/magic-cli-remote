//go:build !darwin

package updateclient

import "github.com/maccavelli/mcplib/selfupdate"

// newCodesignTransformer is a no-op off macOS.
//
// The previous implementation attempted codesign on any operating system when
// the environment variable was set, and printed a macOS TCC note on systems
// that have no TCC at all (MADR 0005 F8). Signing is a macOS concern, so the
// identity is ignored here rather than producing a confusing failure on Linux
// or Windows.
func newCodesignTransformer(string) selfupdate.Transformer { return nil }
