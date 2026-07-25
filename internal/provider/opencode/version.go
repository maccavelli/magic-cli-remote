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
