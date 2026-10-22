package output

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClip(t *testing.T) {
	long := strings.Repeat("a", 40)
	cases := []struct {
		name, in string
		max      int
		want     string
		cut      bool
	}{
		{name: "empty", in: "", max: 10, want: ""},
		{name: "fits", in: "line 1\nline 2", max: 100, want: "line 1\nline 2"},
		{name: "exact fit", in: "abcde", max: 5, want: "abcde"},
		{name: "trailing whitespace only", in: "ok\n\n  \t", max: 2, want: "ok"},
		{name: "keeps inner spacing", in: "a    b\n\tc", max: 100, want: "a    b\n\tc"},
		{name: "cuts on a line break", in: "one\ntwo\nthree\nfour", max: 12, want: "one\ntwo" + Truncated, cut: true},
		{name: "break exactly at max", in: "one\ntwo\nthree", max: 7, want: "one\ntwo" + Truncated, cut: true},
		{name: "one long line cuts on a rune", in: long, max: 10, want: long[:10] + Truncated, cut: true},
		{name: "early break does not waste the budget", in: "x\n" + long, max: 20, want: ("x\n" + long)[:20] + Truncated, cut: true},
		{name: "multi-byte rune at the boundary", in: "abc" + strings.Repeat("é", 10), max: 4, want: "abc" + Truncated, cut: true},
		{name: "no limit", in: long, max: 0, want: long},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, cut := Clip(tc.in, tc.max)
			if got != tc.want || cut != tc.cut {
				t.Errorf("Clip(%q, %d) = %q, %v; want %q, %v", tc.in, tc.max, got, cut, tc.want, tc.cut)
			}
			if !utf8.ValidString(got) {
				t.Errorf("Clip produced invalid UTF-8: %q", got)
			}
		})
	}
}

func TestLine(t *testing.T) {
	cases := []struct {
		name, in string
		max      int
		want     string
	}{
		{name: "empty", in: "", max: 10, want: ""},
		{name: "one line", in: "  Fix the build  ", max: 40, want: "Fix the build"},
		{name: "keeps inner spacing", in: "a  b", max: 40, want: "a  b"},
		{name: "first non-empty line", in: "\n\n  title\nbody", max: 40, want: "title…"},
		{name: "trailing blank lines are not more", in: "title\n\n  \n", max: 40, want: "title"},
		{name: "cut", in: "abcdefghij", max: 4, want: "abcd…"},
		{name: "multi-byte rune at the boundary", in: "abc" + strings.Repeat("é", 4), max: 4, want: "abc…"},
		{name: "no limit", in: "abc\ndef", max: 0, want: "abc…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Line(tc.in, tc.max); got != tc.want {
				t.Errorf("Line(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
		})
	}
}
