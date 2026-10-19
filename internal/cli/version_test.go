package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/maccavelli/go-selfupdate-lib/buildinfo"
)

// TestVersionStringIsTheBuildIdentity: `version` and `--version` print
// buildinfo.Identity(), the line the library's build workflow and installers
// require (MADR 0054 S2). internal/identity_test.go builds the binary with
// real stamps; this pins the wiring.
func TestVersionStringIsTheBuildIdentity(t *testing.T) {
	if got, want := VersionString(), buildinfo.Identity().String(); got != want {
		t.Fatalf("VersionString() = %q, want buildinfo.Identity() %q", got, want)
	}
	if got, want := runningVersion(), buildinfo.Identity().Current(); got != want {
		t.Fatalf("runningVersion() = %q, want buildinfo's version %q", got, want)
	}
}

func TestVersionCommandOutput(t *testing.T) {
	cmd := newVersionCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != VersionString() {
		t.Fatalf("version output = %q, want %q", got, VersionString())
	}
}
