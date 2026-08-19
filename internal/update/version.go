// Package update implements GitHub release discovery, verification, and
// binary swap for mcremote/mcrelay (MADR 0065, amended 0103).
package update

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a stamped mcremote/mcrelay version (MADR 0103).
// N is 0 for a legacy three-part string ("0.13.9").
// Local is true for a locally compiled/installed binary, not a GitHub asset.
type Version struct {
	Major, Minor, Patch, N int
	Local                  bool
}

// String renders major.minor.patch or major.minor.patch.N (N>0). It does
// not reconstruct a local suffix.
func (v Version) String() string {
	if v.N <= 0 {
		return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	}
	return fmt.Sprintf("%d.%d.%d.%d", v.Major, v.Minor, v.Patch, v.N)
}

// ParseVersion splits a stamped version.
//
//	"0.13.9"              → N=0, Local=false
//	"v0.13.9.1"           → N=1, Local=false   // published release
//	"0.13.9.1.gdeadbee"   → N=1, Local=true    // make install, offline
//	"0.13.9.1.ci123"      → N=1, Local=true
//	"dev" / "debug" / ""  → Local=true
func ParseVersion(s string) (Version, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" || s == "dev" || s == "debug" {
		return Version{Local: true}, nil
	}
	parts := strings.Split(s, ".")
	if len(parts) < 3 {
		return Version{}, fmt.Errorf("version %q: need at least major.minor.patch", s)
	}
	atoi := func(field, name string) (int, error) {
		n, err := strconv.Atoi(field)
		if err != nil {
			return 0, fmt.Errorf("version %s: %w", name, err)
		}
		return n, nil
	}
	maj, err := atoi(parts[0], "major")
	if err != nil {
		return Version{}, err
	}
	min, err := atoi(parts[1], "minor")
	if err != nil {
		return Version{}, err
	}
	pat, patRest, err := leadingInt(parts[2])
	if err != nil {
		return Version{}, fmt.Errorf("version patch: %w", err)
	}
	out := Version{Major: maj, Minor: min, Patch: pat, Local: patRest != ""}
	if len(parts) == 3 {
		return out, nil
	}
	n, nRest, nerr := leadingInt(parts[3])
	if nerr != nil || nRest != "" {
		out.Local = true
		return out, nil
	}
	out.N = n
	if len(parts) > 4 {
		out.Local = true
	}
	return out, nil
}

func leadingInt(s string) (n int, rest string, err error) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, s, fmt.Errorf("%q has no leading digits", s)
	}
	n, err = strconv.Atoi(s[:i])
	return n, s[i:], err
}

// ParseBase extracts the three-part release base from a version string.
// The fourth result is Local (MADR 0103): a published BASE.N such as
// "0.13.9.1" is not a local build.
func ParseBase(v string) (maj, min, pat int, dev bool, err error) {
	pv, err := ParseVersion(v)
	if err != nil {
		return 0, 0, 0, false, err
	}
	return pv.Major, pv.Minor, pv.Patch, pv.Local, nil
}

// NewerBase reports whether remote is a strictly newer three-part base than
// local (three-part compare; Run uses NewerPublished, MADR 0103).
func NewerBase(remote, local string) (bool, error) {
	r, err := ParseVersion(remote)
	if err != nil {
		return false, fmt.Errorf("remote: %w", err)
	}
	l, err := ParseVersion(local)
	if err != nil {
		return false, fmt.Errorf("local: %w", err)
	}
	if r.Major != l.Major {
		return r.Major > l.Major, nil
	}
	if r.Minor != l.Minor {
		return r.Minor > l.Minor, nil
	}
	return r.Patch > l.Patch, nil
}

// NewerPublished reports whether remote is a strictly newer published
// version than local (major, minor, patch, then N). Local-ness is
// ignored here; callers refuse local builds separately (MADR 0103).
func NewerPublished(remote, local string) (bool, error) {
	r, err := ParseVersion(remote)
	if err != nil {
		return false, fmt.Errorf("remote: %w", err)
	}
	l, err := ParseVersion(local)
	if err != nil {
		return false, fmt.Errorf("local: %w", err)
	}
	if r.Major != l.Major {
		return r.Major > l.Major, nil
	}
	if r.Minor != l.Minor {
		return r.Minor > l.Minor, nil
	}
	if r.Patch != l.Patch {
		return r.Patch > l.Patch, nil
	}
	return r.N > l.N, nil
}

// BaseString returns "maj.min.pat" for a version, or "" on parse failure.
func BaseString(v string) string {
	m, n, p, _, err := ParseBase(v)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", m, n, p)
}
