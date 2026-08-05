// Package update implements GitHub release discovery, verification, and
// binary swap for mcremote/mcrelay (MADR 0065).
package update

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseBase extracts the three-part release base from a version string.
// Examples: "0.6.7" → (0,6,7,false); "0.6.7.4.gf7fe252" → (0,6,7,true);
// "v0.6.7" → (0,6,7,false).
func ParseBase(v string) (maj, min, pat int, dev bool, err error) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if v == "" || v == "dev" || v == "debug" {
		return 0, 0, 0, true, nil
	}
	parts := strings.Split(v, ".")
	if len(parts) < 3 {
		return 0, 0, 0, false, fmt.Errorf("version %q: need at least major.minor.patch", v)
	}
	maj, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, false, fmt.Errorf("version major: %w", err)
	}
	min, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, false, fmt.Errorf("version minor: %w", err)
	}
	// Patch may be "7" or "7g…" — take leading digits only for the third field.
	patStr := parts[2]
	if i := strings.IndexFunc(patStr, func(r rune) bool { return r < '0' || r > '9' }); i > 0 {
		patStr = patStr[:i]
		dev = true
	}
	pat, err = strconv.Atoi(patStr)
	if err != nil {
		return 0, 0, 0, false, fmt.Errorf("version patch: %w", err)
	}
	if len(parts) > 3 {
		dev = true
	}
	// ".gXXXX" or ".N" serial after three-part base.
	if strings.Contains(v, ".g") || strings.Count(v, ".") > 2 {
		dev = true
	}
	return maj, min, pat, dev, nil
}

// NewerBase reports whether remote is a strictly newer three-part base than
// local (MADR 0065 D2). Dev suffixes are ignored for the numeric compare;
// callers refuse to apply over a local dev build unless --force.
func NewerBase(remote, local string) (bool, error) {
	rm, rn, rp, _, err := ParseBase(remote)
	if err != nil {
		return false, fmt.Errorf("remote: %w", err)
	}
	lm, ln, lp, _, err := ParseBase(local)
	if err != nil {
		return false, fmt.Errorf("local: %w", err)
	}
	if rm != lm {
		return rm > lm, nil
	}
	if rn != ln {
		return rn > ln, nil
	}
	return rp > lp, nil
}

// BaseString returns "maj.min.pat" for a version, or "" on parse failure.
func BaseString(v string) string {
	m, n, p, _, err := ParseBase(v)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", m, n, p)
}
