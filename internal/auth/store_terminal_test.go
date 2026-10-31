package auth

import "testing"

// A device name is chosen by whoever holds a pair code, and reaches the
// operator's terminal. sanitizeDeviceName must remove what a terminal still
// acts on: bidirectional overrides and isolates, invisible format characters,
// and whole escape sequences, not only their ESC (MADR 0219 F6, D2).
func TestSanitizeDeviceNameRemovesTerminalControls(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"right-to-left override", "phone\u202eenohp", "phoneenohp"},
		{"isolates", "ab\u2066cd\u2069ef", "abcdef"},
		// The zero-width joiner is kept: emoji sequences need it.
		{"zero-width space and joiner", "pixel\u200b\u200dname", "pixel\u200dname"},
		{"line separator", "x\u2028y", "x y"},
		{"CSI SGR", "red\x1b[31mtext\x1b[0m", "redtext"},
		{"C1 CSI introducer", "c1\u009b31mtext", "c1text"},
		{"invalid UTF-8", "bad\xffutf8", "bad\ufffdutf8"},
		{"plain", "plain name", "plain name"},
		{"brackets are text", "device [work]", "device [work]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeDeviceName(tc.in); got != tc.want {
				t.Fatalf("sanitizeDeviceName(%+q) = %+q, want %+q", tc.in, got, tc.want)
			}
		})
	}
}
