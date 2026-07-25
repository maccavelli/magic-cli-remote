package cli

import (
	"strings"
	"testing"
)

func TestExampleConstantsNonEmpty(t *testing.T) {
	cases := []struct {
		name string
		val  string
	}{
		{"rootExample", rootExample},
		{"serveExample", serveExample},
		{"pairExample", pairExample},
		{"pairCodeExample", pairCodeExample},
		{"pairCreateExample", pairCreateExample},
		{"pairListExample", pairListExample},
		{"pairRevokeExample", pairRevokeExample},
		{"setupServiceExample", setupServiceExample},
		{"versionExample", versionExample},
		{"enginesExample", enginesExample},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if strings.TrimSpace(tc.val) == "" {
				t.Fatalf("%s should not be empty", tc.name)
			}
			if !strings.Contains(tc.val, "mcremote") {
				t.Fatalf("%s should contain mcremote examples", tc.name)
			}
		})
	}
}
