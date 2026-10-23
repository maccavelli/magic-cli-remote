package provider

import "testing"

// TestSameVersionRefusesUnparseableInput is the property that makes a pin
// worth having: two versions nobody can read are not "the same release", and a
// comparison that says they are reports agreement it never established.
func TestSameVersionRefusesUnparseableInput(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.0.13", "1.0.13", true},
		{"v1.0.13", "1.0.13", true},
		{"V1.0.13", "1.0.13", true},
		{"  1.0.13  ", "1.0.13", true},
		{"1.0.13+abc", "1.0.13", true},
		// A prerelease is not the release it precedes (MADR 0186 D8).
		{"1.0.13-rc.1", "1.0.13", false},
		{"1.0.13 (extra)", "1.0.13", true},
		{"1.0.14", "1.0.13", false},
		{"1.1.13", "1.0.13", false},
		{"2.0.13", "1.0.13", false},
		{"1.0", "1.0.0", true},
		// A bare major is not a version, though semver reads "v1" as v1.0.0
		// (0207 D5, V1 deviation 1).
		{"1", "1.0.0", false},
		{"v1", "1", false},
		// The cases that matter: nothing readable is equal to anything.
		{"", "", false},
		{"", "1.0.13", false},
		{"1.0.13", "", false},
		{"nonsense", "nonsense", false},
		{"nonsense", "1.0.13", false},
		{"codex-test-agent", "0.152.1", false},
	}
	for _, c := range cases {
		if got := SameVersion(c.a, c.b); got != c.want {
			t.Errorf("SameVersion(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestPrereleaseNeverMatchesARelease is MADR 0186 D8 (gobble finding M7): a
// prerelease or a pseudo-version of a release ranks below it and never
// matches its pin, while build metadata does not make a different release.
func TestPrereleaseNeverMatchesARelease(t *testing.T) {
	cases := []struct {
		v, pin string
		want   int
	}{
		{"1.3.0-rc.1", "1.3.0", -1},
		{"v1.2.4-0.20261013000000-abcdef123456", "1.2.4", -1},
		{"v1.2.4-0.20261013000000-abcdef123456", "1.2.3", 1},
		{"1.18.0-rc.1", "1.18.0", -1},
		{"v1.3.0", "1.3.0", 0},
		{"1.3.0+build", "1.3.0", 0},
		{"1.3.0", "1.3.0", 0},
	}
	for _, c := range cases {
		if got := CompareVersions(c.v, c.pin); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.v, c.pin, got, c.want)
		}
		if same := SameVersion(c.v, c.pin); same != (c.want == 0) {
			t.Errorf("SameVersion(%q, %q) = %v, want %v", c.v, c.pin, same, c.want == 0)
		}
	}
}

// TestCompareVersionsOrdersUnparseableLowest keeps opencode's floor safe: a
// garbage health body can never read as new enough.
func TestCompareVersionsOrdersUnparseableLowest(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"nonsense", "1.18.21", -1},
		{"1.18.21", "nonsense", 1},
		{"nonsense", "garbage", 0},
		{"", "1.18.21", -1},
		{"1.17.99", "1.18.21", -1},
		{"1.19.0", "1.18.21", 1},
		{"1.18", "1.18.0", 0},
		{"1", "1.0.0", -1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestParseSemver reads major.minor.patch, keeping it for a prerelease, and
// refuses what is not a version. Moved from opencode with its copy (0207 D5).
func TestParseSemver(t *testing.T) {
	cases := []struct {
		in            string
		maj, min, pat int
		ok            bool
	}{
		{"1.18.4", 1, 18, 4, true},
		{"v1.18.4", 1, 18, 4, true},
		{"1.18.4-beta", 1, 18, 4, true},
		{"0.152.1-rc1", 0, 152, 1, true},
		{"1.18", 1, 18, 0, true},
		{"", 0, 0, 0, false},
		{"not-a-version", 0, 0, 0, false},
		{"codex-test-agent", 0, 0, 0, false},
		{"1", 0, 0, 0, false},
		{"1-rc.1", 0, 0, 0, false},
	}
	for _, c := range cases {
		maj, min, pat, ok := ParseSemver(c.in)
		if ok != c.ok || maj != c.maj || min != c.min || pat != c.pat {
			t.Errorf("ParseSemver(%q)=%d.%d.%d ok=%v want %d.%d.%d ok=%v",
				c.in, maj, min, pat, ok, c.maj, c.min, c.pat, c.ok)
		}
	}
}
