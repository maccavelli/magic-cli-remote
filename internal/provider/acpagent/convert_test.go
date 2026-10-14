package acpagent

import (
	"math"
	"testing"
)

// The boundary tests of MADR 0184 D3's conversions: each at its type's
// maximum, where a bare cast would wrap or truncate.

func TestSaturatingInt(t *testing.T) {
	for _, tc := range []struct {
		in   uint64
		want int
	}{
		{0, 0},
		{5, 5},
		{math.MaxInt, math.MaxInt},
		{math.MaxInt + 1, math.MaxInt},
		{math.MaxUint64, math.MaxInt},
	} {
		if got := saturatingInt(tc.in); got != tc.want {
			t.Errorf("saturatingInt(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestTerminalBufferLimit(t *testing.T) {
	u := func(v uint64) *uint64 { return &v }
	for _, tc := range []struct {
		name string
		in   *uint64
		want int
	}{
		{"absent", nil, defaultTerminalBuffer},
		{"zero", u(0), defaultTerminalBuffer},
		{"small", u(4096), 4096},
		{"at the cap", u(maxTerminalBuffer), maxTerminalBuffer},
		{"past the cap", u(maxTerminalBuffer + 1), maxTerminalBuffer},
		{"maximum", u(math.MaxUint64), maxTerminalBuffer},
	} {
		if got := terminalBufferLimit(tc.in); got != tc.want {
			t.Errorf("%s: terminalBufferLimit = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestLineWindow(t *testing.T) {
	u := func(v uint32) *uint32 { return &v }
	const n = 3
	for _, tc := range []struct {
		name        string
		line, limit *uint32
		start, end  int
	}{
		{"whole file", nil, nil, 0, n},
		{"line zero is the start", u(0), nil, 0, n},
		{"from line 2", u(2), nil, 1, n},
		{"one line from line 2", u(2), u(1), 1, 2},
		{"limit zero is the end", u(2), u(0), 1, n},
		{"line past the end", u(10), nil, n, n},
		{"line at the maximum", u(math.MaxUint32), u(math.MaxUint32), n, n},
		{"limit at the maximum", u(1), u(math.MaxUint32), 0, n},
	} {
		start, end := lineWindow(tc.line, tc.limit, n)
		if start != tc.start || end != tc.end {
			t.Errorf("%s: lineWindow = [%d, %d), want [%d, %d)", tc.name, start, end, tc.start, tc.end)
		}
		if start < 0 || end > n || start > end {
			t.Errorf("%s: [%d, %d) is outside the file's %d lines", tc.name, start, end, n)
		}
	}
}

func TestExitCode(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   int64 // -1 for no code
	}{
		{-1, -1},
		{math.MinInt32, -1},
		{0, 0},
		{1, 1},
		{255, 255},
		{math.MaxUint32, math.MaxUint32},
	} {
		got := exitCode(tc.status)
		switch {
		case tc.want < 0 && got != nil:
			t.Errorf("exitCode(%d) = %d, want no code", tc.status, *got)
		case tc.want >= 0 && (got == nil || int64(*got) != tc.want):
			t.Errorf("exitCode(%d) = %v, want %d", tc.status, got, tc.want)
		}
	}
	if math.MaxInt > math.MaxUint32 {
		var wide uint64 = math.MaxUint32 + 1
		big := int(wide)
		if got := exitCode(big); got != nil {
			t.Errorf("exitCode(%d) = %d, want no code: it does not fit", big, *got)
		}
	}
}
