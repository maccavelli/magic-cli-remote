package updateclient

import (
	_ "embed" // the release spec
	"fmt"

	"github.com/maccavelli/go-selfupdate-lib/selfupdate/releasespec"
)

// releaseSpecJSON is the release spec: the products, the platforms and the
// extras of every release, read by this updater for its asset selector and,
// from S4 of the MADR 0054 PLAN, by the library's build and publish
// workflows. It encodes MADR 0116 D19 (no windows/arm64) and MADR 0120 (no
// darwin/amd64) once (MADR 0054 D8).
//
//go:embed selfupdate-release.json
var releaseSpecJSON []byte

// releaseSpec parses the embedded spec. A spec this program cannot use fails
// here, and in the tests, before any release is built from it.
func releaseSpec() (releasespec.Spec, error) {
	spec, err := releasespec.Parse(releaseSpecJSON)
	if err != nil {
		return releasespec.Spec{}, fmt.Errorf("updateclient: release spec: %w", err)
	}
	return spec, nil
}
