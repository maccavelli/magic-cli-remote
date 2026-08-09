package receipt

import (
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/config"
)

func TestShouldReceipt(t *testing.T) {
	cases := []struct {
		name       string
		cfg        config.ReceiptsConfig
		toolName   string
		detail     string
		wantResult bool
	}{
		{
			name: "disabled is always false regardless of patterns",
			cfg: config.ReceiptsConfig{
				Enabled:       false,
				AllowPatterns: []string{"*"},
			},
			toolName:   "bash",
			detail:     "rm -rf /tmp/x",
			wantResult: false,
		},
		{
			name: "allow-only match",
			cfg: config.ReceiptsConfig{
				Enabled:       true,
				AllowPatterns: []string{"bash *rm -rf*"},
			},
			toolName:   "bash",
			detail:     "rm -rf /tmp/x",
			wantResult: true,
		},
		{
			name: "allow-only no-match",
			cfg: config.ReceiptsConfig{
				Enabled:       true,
				AllowPatterns: []string{"bash *rm -rf*"},
			},
			toolName:   "bash",
			detail:     "ls -la",
			wantResult: false,
		},
		{
			name: "deny-only match",
			cfg: config.ReceiptsConfig{
				Enabled:      true,
				DenyPatterns: []string{"bash *echo*"},
			},
			toolName:   "bash",
			detail:     "echo hi",
			wantResult: false,
		},
		{
			name: "deny-only no-match falls through to no allow rules, still false",
			cfg: config.ReceiptsConfig{
				Enabled:      true,
				DenyPatterns: []string{"bash *echo*"},
			},
			toolName:   "bash",
			detail:     "rm -rf /tmp/x",
			wantResult: false,
		},
		{
			name: "both match on the same input: deny wins",
			cfg: config.ReceiptsConfig{
				Enabled:       true,
				AllowPatterns: []string{"bash *"},
				DenyPatterns:  []string{"bash *rm -rf*"},
			},
			toolName:   "bash",
			detail:     "rm -rf /tmp/x",
			wantResult: false,
		},
		{
			name: "both configured, only allow matches this input",
			cfg: config.ReceiptsConfig{
				Enabled:       true,
				AllowPatterns: []string{"bash *"},
				DenyPatterns:  []string{"bash *rm -rf*"},
			},
			toolName:   "bash",
			detail:     "git push",
			wantResult: true,
		},
		{
			name: "malformed allow pattern warns, treated as non-match, no panic",
			cfg: config.ReceiptsConfig{
				Enabled:       true,
				AllowPatterns: []string{"["},
			},
			toolName:   "bash",
			detail:     "rm -rf /tmp/x",
			wantResult: false,
		},
		{
			name: "malformed deny pattern warns, treated as non-match, no panic — allow still applies",
			cfg: config.ReceiptsConfig{
				Enabled:       true,
				AllowPatterns: []string{"bash *"},
				DenyPatterns:  []string{"["},
			},
			toolName:   "bash",
			detail:     "rm -rf /tmp/x",
			wantResult: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldReceipt(tc.cfg, tc.toolName, tc.detail); got != tc.wantResult {
				t.Fatalf("ShouldReceipt() = %v, want %v", got, tc.wantResult)
			}
		})
	}
}

// TestShouldReceiptMatchesAcrossSlash guards the exact bug found while
// writing this matcher: Go stdlib path.Match's `*` refuses to cross a `/`,
// so MADR 0077 D10's own worked example — allow_patterns: ["*rm -rf*"]
// matching detail "rm -rf ./build" — would silently never fire under
// path.Match, since path.Match("*rm -rf*", "bash rm -rf ./build") returns
// (false, nil): no error, just a receipt that never gets written. This test
// is the regression guard for that fix (glob-to-regexp translation instead).
func TestShouldReceiptMatchesAcrossSlash(t *testing.T) {
	cfg := config.ReceiptsConfig{
		Enabled:       true,
		AllowPatterns: []string{"*rm -rf*"},
	}
	if !ShouldReceipt(cfg, "bash", "rm -rf ./build") {
		t.Fatal("want a match: '*rm -rf*' must match detail containing '/' after the matched text")
	}
}
