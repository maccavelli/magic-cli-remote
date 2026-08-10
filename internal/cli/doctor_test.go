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

// MADR 0074 §9: doctor output is meant to be pasteable into an issue, so it
// reports which upstreams exist and never what their credentials are.
func TestRenderCredentialDoctorPrintsNoValues(t *testing.T) {
	var buf bytes.Buffer
	renderCredentialDoctor(&buf, []credentialStore{
		{
			Agent:     "opencode",
			Path:      "/home/u/.local/share/opencode/auth.json",
			Present:   true,
			Upstreams: []string{"opencode", "opencode-go"},
		},
		{
			Agent:   "codex",
			Path:    "/home/u/.codex/auth.json",
			Present: false,
			Note:    "device sign-in deletes this file at start (MADR 0074 D8)",
		},
	})
	out := buf.String()

	for _, want := range []string{
		"opencode-go", "present:   yes", "present:   no", "MADR 0074 D8",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output missing %q:\n%s", want, out)
		}
	}
	// The struct has no field that could carry a value, but assert the shape
	// anyway: a future field named key/secret/token must not reach this text.
	for _, forbidden := range []string{"sk-", "key:", "secret", "token"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("doctor output contains %q:\n%s", forbidden, out)
		}
	}
}

// The probe must not fail on a host where an agent was never installed.
func TestProbeCredentialStoresOnColdHost(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	stores := probeCredentialStores()
	if len(stores) == 0 {
		t.Fatal("no stores reported")
	}
	for _, s := range stores {
		if s.Present {
			t.Errorf("%s reported present on a cold host", s.Agent)
		}
	}
}
