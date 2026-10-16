//go:build live_grok

package grok_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Run with: go test -tags live_grok ./internal/provider/grok/ -run ShipsNoSubagentSessions -count=1 -v
//
// 0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md, S2.
// Magic defers ACP's unstable subagent sessions because no engine it runs
// sends them: grok 1.0.50's binary does not contain the string
// `subagent_update`. This pins that premise. When it fails, grok has shipped
// support, and the record's S3 trigger has fired.
//
// It reads the installed binary: no process starts and no tokens are spent.
// A string's absence is strong evidence grok cannot send the update; its
// presence does not prove grok sends it, which is why the trigger reopens the
// record rather than changing behavior.
func TestLiveGrokShipsNoSubagentSessions(t *testing.T) {
	bin, err := exec.LookPath("grok")
	if err != nil {
		t.Skip("grok not in PATH")
	}
	path, err := filepath.EvalSymlinks(bin)
	if err != nil {
		t.Fatalf("resolve %s: %v", bin, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	n := bytes.Count(data, []byte("subagent_update"))
	t.Logf("%s: %d MiB, subagent_update appears %d time(s)", filepath.Base(path), len(data)>>20, n)
	if n != 0 {
		t.Fatalf("%s contains subagent_update %d time(s): grok may now send ACP subagent sessions. "+
			"Reopen 0195-MADR-defer-acp-subagent-sessions-until-an-engine-sends-them.md (S3)", path, n)
	}
}
