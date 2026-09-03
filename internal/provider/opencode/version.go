package opencode

import (
	"fmt"
	"strconv"
	"strings"
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
// provider was assessed against. Comparison is semantic, so "v1.18.21" and
// "1.18.21+build" both match; an unparseable version never does.
func VersionIsKnownGood(engineVersion string) bool {
	if _, _, _, ok := parseSemver(engineVersion); !ok {
		return false
	}
	return CompareVersions(engineVersion, KnownGoodVersion) == 0
}

// parseSemver extracts major.minor.patch from a version string such as
// "1.18.4" or "v1.18.4-beta". Returns ok=false when no leading numeric triple
// can be parsed.
func parseSemver(v string) (major, minor, patch int, ok bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	// Drop pre-release / build metadata.
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return 0, 0, 0, false
	}
	var err error
	if major, err = strconv.Atoi(parts[0]); err != nil {
		return 0, 0, 0, false
	}
	if minor, err = strconv.Atoi(parts[1]); err != nil {
		return 0, 0, 0, false
	}
	if len(parts) >= 3 {
		if patch, err = strconv.Atoi(parts[2]); err != nil {
			return 0, 0, 0, false
		}
	}
	return major, minor, patch, true
}

// CompareVersions returns -1 if a < b, 0 if a == b, 1 if a > b.
// Unparseable versions compare as equal to themselves and less than parseable ones.
func CompareVersions(a, b string) int {
	am, an, ap, aok := parseSemver(a)
	bm, bn, bp, bok := parseSemver(b)
	if !aok && !bok {
		return 0
	}
	if !aok {
		return -1
	}
	if !bok {
		return 1
	}
	if am != bm {
		if am < bm {
			return -1
		}
		return 1
	}
	if an != bn {
		if an < bn {
			return -1
		}
		return 1
	}
	if ap != bp {
		if ap < bp {
			return -1
		}
		return 1
	}
	return 0
}

// VersionMeetsMin reports whether engineVersion is at least MinVersion.
func VersionMeetsMin(engineVersion string) bool {
	return CompareVersions(engineVersion, MinVersion) >= 0
}

// VersionPinError describes an engine that is too old for session-tree mode.
func VersionPinError(engineVersion string) error {
	return fmt.Errorf("opencode engine version %q is below minimum %s (MADR 0020 KD10); upgrade opencode or set providers.opencode.session_tree=false",
		engineVersion, MinVersion)
}
