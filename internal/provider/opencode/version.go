package opencode

import (
	"fmt"

	"github.com/maccavelli/magic-cli-remote/internal/provider"
)

// MinVersion is the minimum OpenCode engine version required for full
// session-tree behaviour (MADR 0020 KD10). Verified against 1.18.4 on the
// development host; older engines may lack child-session event shapes or REST
// routes the tree demux depends on.
const MinVersion = "1.18.0"

// KnownGoodVersion is the OpenCode release this provider has actually been
// assessed against, end to end: HTTP path/operation/schema/Event sets, tool
// classification, catalog defaults and the live session loop (MADR 0112 D1).
//
// It is deliberately a *different policy* from MinVersion. MinVersion is a hard
// floor — below it, session-tree mode cannot work at all. KnownGoodVersion is a
// statement about what has been verified: OpenCode releases frequently, and a
// newer engine is far more likely to be fine than broken, so drifting off the
// pin produces one warning per engine boot rather than an outage.
//
// Evidence for 1.18.26: internal/provider/opencode/testdata/wire/1.18.26/, a
// live turn captured from the SSE stream — 85 frames covering
// message.part.delta, message.part.updated, message.updated and 45
// `plugin.added` frames (MADR 0137 Phase 1).
const KnownGoodVersion = "1.18.26"

// VersionIsKnownGood reports whether engineVersion is exactly the release this
// provider was assessed against, by full SemVer (provider.SameVersion): so
// "v1.18.21" and "1.18.21+build" both match, a prerelease does not, and an
// unparseable version never does.
func VersionIsKnownGood(engineVersion string) bool {
	return provider.SameVersion(engineVersion, KnownGoodVersion)
}

// VersionMeetsMin reports whether engineVersion is at least MinVersion. A
// prerelease of the floor ranks below it (MADR 0186 D8, 0207 D5 Q2), and an
// unparseable version is below every floor.
func VersionMeetsMin(engineVersion string) bool {
	return provider.CompareVersions(engineVersion, MinVersion) >= 0
}

// VersionPinError describes an engine that is too old for session-tree mode.
func VersionPinError(engineVersion string) error {
	return fmt.Errorf("opencode engine version %q is below minimum %s (MADR 0020 KD10); upgrade opencode or set providers.opencode.session_tree=false",
		engineVersion, MinVersion)
}
