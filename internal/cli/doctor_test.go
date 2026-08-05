package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/tcc"
)

// MADR 0069 P5 (U6) — doctor renders every probe state with the right
// guidance, without owning the host's TCC database.
func TestRenderDoctor(t *testing.T) {
	render := func(goos string, res tcc.ProbeResult) string {
		var b bytes.Buffer
		renderDoctor(&b, goos, res)
		return b.String()
	}

	if out := render("linux", tcc.NotApplicable); !strings.Contains(out, "not applicable") {
		t.Fatalf("linux: %s", out)
	}
	granted := render("darwin", tcc.Granted)
	if !strings.Contains(granted, "OK:") || !strings.Contains(granted, "lower-bound") {
		t.Fatalf("granted must carry the lower-bound caveat: %s", granted)
	}
	denied := render("darwin", tcc.Denied)
	for _, want := range []string{
		"DENIED", "Full Disk Access", "tccutil reset SystemPolicyAllFiles",
		"com.apple.TCC", "ops-macos-tcc.md",
	} {
		if !strings.Contains(denied, want) {
			t.Fatalf("denied missing %q: %s", want, denied)
		}
	}
	// Attribution caveat on every darwin verdict: a terminal's grant does
	// not transfer to the LaunchAgent.
	for _, out := range []string{granted, denied} {
		if !strings.Contains(out, "startup log") {
			t.Fatalf("missing responsible-process caveat: %s", out)
		}
	}
	if out := render("darwin", tcc.Unknown); !strings.Contains(out, "UNKNOWN") {
		t.Fatalf("unknown: %s", out)
	}
}
