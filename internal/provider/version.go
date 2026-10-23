package provider

import (
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
)

// SameVersion reports whether two engine version strings name the same
// release, by full SemVer (MADR 0186 D8): a leading "v" or "V" and build
// metadata are ignored, so "v1.0.13" and "1.0.13+abc" are the release the wire
// shapes were checked against, and "1.0.14" is not. A prerelease or a
// pseudo-version is not the release it precedes: "1.0.13-rc.1" does not match
// a "1.0.13" pin.
//
// An unparseable version is never equal to anything, including another
// unparseable one. A pin whose comparison silently succeeds on garbage is
// worse than no pin, because it reports agreement it never established
// (MADR 0137).
func SameVersion(a, b string) bool {
	na, nb := normalizeVersion(a), normalizeVersion(b)
	if !semver.IsValid(na) || !semver.IsValid(nb) {
		return false
	}
	return semver.Compare(na, nb) == 0
}

// CompareVersions returns -1, 0 or 1 as a is below, equal to or above b, by
// SemVer precedence: a prerelease ranks below its release, and build metadata
// is ignored. An unparseable version ranks below every parseable one, and two
// unparseable versions compare equal, so a garbage version never reads as new
// enough for a floor.
func CompareVersions(a, b string) int {
	return semver.Compare(normalizeVersion(a), normalizeVersion(b))
}

// ParseSemver extracts major.minor.patch from a version string such as
// "1.18.4", "v1.0.13" or "0.152.1-rc1", keeping a prerelease's numbers. ok is
// false when the string is not a SemVer version, in which case the caller
// must treat the version as unknown rather than as zero.
func ParseSemver(v string) (major, minor, patch int, ok bool) {
	c := semver.Canonical(normalizeVersion(v))
	if c == "" {
		return 0, 0, 0, false
	}
	if i := strings.IndexAny(c, "-+"); i >= 0 {
		c = c[:i]
	}
	parts := strings.Split(strings.TrimPrefix(c, "v"), ".")
	major, _ = strconv.Atoi(parts[0])
	minor, _ = strconv.Atoi(parts[1])
	patch, _ = strconv.Atoi(parts[2])
	return major, minor, patch, true
}

// normalizeVersion turns an engine's version string into the "v"-prefixed form
// golang.org/x/mod/semver reads, or "" when it is not a version. Surrounding
// space is trimmed, and the version ends at the first space, so
// "1.0.13 (extra)" is 1.0.13. A leading "v" or "V" is dropped before "v" is
// added.
//
// A version needs at least major.minor. semver reads a bare "v1" as v1.0.0,
// but a lone number in a userAgent is not a release (0207 D5, V1 deviation 1).
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, " \t"); i >= 0 {
		v = v[:i]
	}
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	core := v
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core = core[:i]
	}
	if !strings.Contains(core, ".") {
		return ""
	}
	return "v" + v
}
