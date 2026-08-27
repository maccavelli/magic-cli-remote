package relay

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

// mcrelay carries its own copy of the setup-service flag set, so --refresh has
// to be wired twice and can regress on one side alone (MADR 0100 Phase 2).

func TestRelaySetupServiceBindsRefresh(t *testing.T) {
	cfg, lvl, fmtv := "", "", ""
	cmd := newSetupServiceCmd(&cfg, &lvl, &fmtv)
	for _, name := range []string{"refresh", "json"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("--%s is not bound on mcrelay setup-service", name)
		}
	}
}

func TestRelayRunSetupServiceRefreshJSON(t *testing.T) {
	testexec.SkipIfNoUnixServiceManager(t)
	defer service.OverrideInstallOS("linux")()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var f setupServiceFlags
	f.refresh = true
	f.jsonOut = true

	cfg, lvl, fmtv := "", "", ""
	cmd := newSetupServiceCmd(&cfg, &lvl, &fmtv)
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := runSetupService(cmd, f, cfg, lvl, fmtv); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Verdict string `json:"verdict"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not one JSON object: %v\n%s", err, buf.String())
	}
	if got.Verdict != "none" {
		t.Fatalf("verdict = %q, want none", got.Verdict)
	}
	if !strings.HasSuffix(got.Path, "/systemd/user/mcrelay.service") {
		t.Fatalf("path = %q, want the mcrelay unit", got.Path)
	}
}

func TestRelaySetupServiceRefreshConflictsWithRemove(t *testing.T) {
	defer service.OverrideInstallOS("linux")()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var f setupServiceFlags
	f.refresh = true
	f.remove = true

	cfg, lvl, fmtv := "", "", ""
	cmd := newSetupServiceCmd(&cfg, &lvl, &fmtv)
	cmd.SetOut(&bytes.Buffer{})

	err := runSetupService(cmd, f, cfg, lvl, fmtv)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("err = %v, want a mutual-exclusion error", err)
	}
}
