package codex

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestVersionFromUserAgent pins the parse against codex's documented shape,
// `<originator>/<CARGO_PKG_VERSION> (<os>; <arch>) …`
// (codex-rs/login/src/auth/default_client.rs:164-170).
//
// The refusal cases carry the weight. A userAgent is a free-form string a
// future codex may reshape, and a pin that reports "codex-test-agent" or an
// originator with a slash in it as a release would warn about a version that
// does not exist. Empty means "the engine reported nothing", which produces no
// warning at all (MADR 0137, ninth amendment).
func TestVersionFromUserAgent(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// The exact string codex 0.152.1 returned on the fixture host.
		{"mcremote/0.152.1 (Mac OS 26.6.2; arm64) WezTerm/20240203-110809-5046fc22 (mcremote; dev)", "0.152.1"},
		{"codex_cli_rs/9.8.7-test (Test OS; x86_64) rust", "9.8.7-test"},
		{"codex_app_server/1.2.3", "1.2.3"},
		{"  codex_cli_rs/0.152.1 (x)  ", "0.152.1"},
		// Refusals.
		{"codex-test-agent", ""},
		{"", ""},
		{"/0.152.1", "0.152.1"},
		{"codex_cli_rs/", ""},
		{"codex_cli_rs/notaversion (x)", ""},
		{"codex_cli_rs/1 (x)", ""},
	}
	for _, c := range cases {
		if got := versionFromUserAgent(c.in); got != c.want {
			t.Errorf("versionFromUserAgent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestPinMatchesTheFixtureItCites is the check that keeps the constant honest.
//
// A pin is a claim that the wire shapes were verified against that release. The
// fixture directory name is the evidence, and codex states its own version
// inside the capture as `cliVersion` — an independent second source from the
// userAgent the reader parses. If a later bump edits the constant and forgets
// the fixture, or re-records the fixture from a different engine, this fails.
func TestPinMatchesTheFixtureItCites(t *testing.T) {
	dir := "testdata/wire/" + KnownGoodVersion
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("KnownGoodVersion is %q but %s does not exist: a pin without a "+
			"fixture is a claim that nothing changed: %v", KnownGoodVersion, dir, err)
	}
	data, err := os.ReadFile(dir + "/frames.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	found := ""
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "cliVersion") {
			continue
		}
		var frame map[string]json.RawMessage
		if json.Unmarshal([]byte(line), &frame) != nil {
			continue
		}
		// The value is nested; a substring scan over the raw line is enough to
		// read it, and does not depend on where in the thread object it sits.
		i := strings.Index(line, `"cliVersion":`)
		rest := line[i+len(`"cliVersion":`):]
		rest = strings.TrimLeft(rest, " ")
		if !strings.HasPrefix(rest, `"`) {
			continue
		}
		rest = rest[1:]
		if j := strings.Index(rest, `"`); j >= 0 {
			found = rest[:j]
			break
		}
	}
	if found == "" {
		t.Fatalf("no cliVersion in %s/frames.jsonl: the fixture cannot corroborate the pin", dir)
	}
	if found != KnownGoodVersion {
		t.Fatalf("fixture reports cliVersion %q but KnownGoodVersion is %q: "+
			"the pin cites evidence from a different engine", found, KnownGoodVersion)
	}
}
