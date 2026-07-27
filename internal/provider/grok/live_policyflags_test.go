//go:build live_grok

package grok_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/maccavelli/magic-cli-remote/internal/provider/grok"
)

// Run with: go test -tags live_grok ./internal/provider/grok/ -run PolicyFlags -count=1
//
// Pins D4: asserts grok agent --help lists policy flags.
func TestLiveGrokPolicyFlagsWireContract(t *testing.T) {
	p := grok.New(grok.Config{})
	if !p.Ready() {
		t.Skip("grok not in PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "grok", "--help")
	outBuf, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("grok agent --help failed: %v", err)
	}
	out := string(outBuf)

	flags := []string{
		"--permission-mode",
		"--tools",
		"--disallowed-tools",
		"--allow",
		"--deny",
		"--no-subagents",
		"--disable-web-search",
	}

	for _, flag := range flags {
		if !strings.Contains(out, flag) {
			t.Errorf("grok agent --help does not list required policy flag %s", flag)
		}
	}
}
