package provider

import (
	"strconv"
	"strings"
)

// SameVersion reports whether two engine version strings name the same
// release, comparing major.minor.patch and ignoring a leading "v" and any
// pre-release or build metadata. It is the comparison a known-good pin needs:
// "v1.0.13" and "1.0.13+abc" are the release the wire shapes were checked
// against, and "1.0.14" is not.
//
// An unparseable version is never equal to anything, including another
// unparseable one. A pin whose comparison silently succeeds on garbage is
// worse than no pin, because it reports agreement it never established
// (MADR 0137).
func SameVersion(a, b string) bool {
	am, an, ap, aok := ParseSemver(a)
	bm, bn, bp, bok := ParseSemver(b)
	if !aok || !bok {
		return false
	}
	return am == bm && an == bn && ap == bp
}

// ParseSemver extracts major.minor.patch from a version string such as
// "1.18.4", "v1.0.13" or "0.152.1-rc1". ok is false when no leading numeric
// major.minor can be read, in which case the caller must treat the version as
// unknown rather than as zero.
//
// kilo and opencode carry their own older copies of this logic, kept as they
// are because changing a shipped gate is not this record's business. New pins
// use this one.
func ParseSemver(v string) (major, minor, patch int, ok bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
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
