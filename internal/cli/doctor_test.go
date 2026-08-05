package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
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

// MADR 0072 P2 — service section surfaces bootout-left-down.
func TestRenderServiceDoctor(t *testing.T) {
	var b bytes.Buffer
	renderServiceDoctor(&b, service.Status{
		PlistOrUnit:  "/tmp/com.magiccliremote.mcremote.plist",
		PlistPresent: true,
		Loaded:       false,
		Active:       false,
		Hint:         "plist present but not loaded (bootout left down) — run: mcremote setup-service --force",
	})
	out := b.String()
	for _, want := range []string{
		"mcremote user service",
		"present: yes",
		"loaded:  no",
		"active:  no",
		"bootout left down",
		"setup-service --force",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}

	b.Reset()
	renderServiceDoctor(&b, service.Status{
		PlistOrUnit:  "x",
		PlistPresent: true,
		Loaded:       true,
		Active:       true,
	})
	if !strings.Contains(b.String(), "OK: service is loaded and running") {
		t.Fatalf("active OK: %s", b.String())
	}
}
