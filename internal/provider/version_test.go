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
		{"1.0.13+abc", "1.0.13", true},
		{"1.0.13-rc.1", "1.0.13", true},
		{"1.0.13 (extra)", "1.0.13", true},
		{"1.0.14", "1.0.13", false},
		{"1.1.13", "1.0.13", false},
		{"2.0.13", "1.0.13", false},
		{"1.0", "1.0.0", true},
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
