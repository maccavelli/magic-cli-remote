package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/procutil"
)

func TestNewEnginesCmdProperties(t *testing.T) {
	cmd := newEnginesCmd()
	if cmd.Use != "engines" {
		t.Fatalf("Use = %q, want engines", cmd.Use)
	}
	if cmd.Example == "" {
		t.Fatal("Example should not be empty")
	}
}

func TestNewEnginesCmdHasReapFlag(t *testing.T) {
	cmd := newEnginesCmd()
	f := cmd.Flags().Lookup("reap")
	if f == nil {
		t.Fatal("missing --reap flag")
	}
	if f.DefValue != "false" {
		t.Fatalf("--reap default = %q, want false", f.DefValue)
	}
}

// TestEnginesNamesEachProvider: `mcremote engines` prints each record's
// provider, so an `opencode acp` agent and OpenCode's control plane read apart
// (0214 PLAN P8 Deviation 1). Both records name this test's own process, which
// is live.
func TestEnginesNamesEachProvider(t *testing.T) {
	self, owner := os.Getpid(), procutil.OwnerToken()
	rows, _ := recordRows([]procutil.EngineRecord{
		{ID: "acp-agent", Provider: "opencode", PID: self, Owner: owner},
		{ID: "control-plane", Provider: "opencode-control", PID: self, Owner: owner},
		{ID: "old-record", PID: self, Owner: owner},
	})
	var b bytes.Buffer
	if err := renderEngineRows(&b, rows); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	providers := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if f := strings.Fields(line); len(f) == 5 {
			providers[f[4]] = f[3]
		}
	}
	if !strings.Contains(out, "\tPROVIDER") && !strings.Contains(out, " PROVIDER ") {
		t.Errorf("the table has no PROVIDER column:\n%s", out)
	}
	for id, want := range map[string]string{
		"acp-agent": "opencode", "control-plane": "opencode-control", "old-record": "(unrecorded)",
	} {
		if got := providers[id]; got != want {
			t.Errorf("%s was listed as provider %q, want %q:\n%s", id, got, want, out)
		}
	}
}
