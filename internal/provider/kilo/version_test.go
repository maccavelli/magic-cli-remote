package kilo

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestKnownGoodPinComparesFullSemver is 0207 D5's answer to Q1: kilo's pin
// compares like every other pin (MADR 0186 D8), not as an exact string. The
// pinned release with a "v" or build metadata is not drift; a prerelease of
// it, or another release, is.
func TestKnownGoodPinComparesFullSemver(t *testing.T) {
	const drift = "kilo engine version differs from known-good pin"
	cases := []struct {
		version string
		drift   bool
	}{
		{"7.5.6", false},
		{"v7.5.6", false},
		{"7.5.6+build", false},
		{"7.5.6-rc.1", true},
		{"7.5.7", true},
		{"nonsense", true},
	}
	for _, c := range cases {
		t.Run(c.version, func(t *testing.T) {
			var buf bytes.Buffer
			d := &httpDialect{log: slog.New(slog.NewTextHandler(&buf, nil))}
			if err := d.OnHealthy([]byte(`{"healthy":true,"version":"` + c.version + `"}`)); err != nil {
				t.Fatalf("OnHealthy: %v", err)
			}
			if got := strings.Contains(buf.String(), drift); got != c.drift {
				t.Fatalf("drift warning logged = %v, want %v; log:\n%s", got, c.drift, buf.String())
			}
		})
	}
}
