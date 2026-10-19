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

// TestRelaySetupServiceUninstallConflicts: mcrelay's --uninstall cannot be
// combined with --refresh or --remove (MADR 0054 PLAN S5e).
func TestRelaySetupServiceUninstallConflicts(t *testing.T) {
	defer service.OverrideInstallOS("linux")()
	for _, f := range []setupServiceFlags{{uninstall: true, refresh: true}, {uninstall: true, remove: true}} {
		cfg, lvl, fmtv := "", "", ""
		cmd := newSetupServiceCmd(&cfg, &lvl, &fmtv)
		cmd.SetOut(&bytes.Buffer{})
		if err := runSetupService(cmd, f, cfg, lvl, fmtv); err == nil || !strings.Contains(err.Error(), "--uninstall cannot be combined") {
			t.Errorf("%+v: %v, want a refusal", f, err)
		}
	}
}

// TestRelaySetupServiceFromInstaller: mcrelay's --from-installer refuses the
// other modes, and with no relay service it sets none up (MADR 0054 PLAN
// S7a).
func TestRelaySetupServiceFromInstaller(t *testing.T) {
	defer service.OverrideInstallOS("linux")()
	defer service.OverrideDetectedKind(service.KindNone)()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(k, home+"/"+k)
	}
	for _, f := range []setupServiceFlags{
		{fromInstaller: true, refresh: true}, {fromInstaller: true, remove: true},
		{fromInstaller: true, uninstall: true}, {fromInstaller: true, printOnly: true},
	} {
		cfg, lvl, fmtv := "", "", ""
		cmd := newSetupServiceCmd(&cfg, &lvl, &fmtv)
		cmd.SetOut(&bytes.Buffer{})
		if err := runSetupService(cmd, f, cfg, lvl, fmtv); err == nil || !strings.Contains(err.Error(), "--from-installer cannot be combined") {
			t.Errorf("%+v: %v, want a refusal", f, err)
		}
	}
	var out bytes.Buffer
	cfg, lvl, fmtv := "", "", ""
	cmd := newSetupServiceCmd(&cfg, &lvl, &fmtv)
	cmd.SetOut(&out)
	if err := runSetupService(cmd, setupServiceFlags{fromInstaller: true}, cfg, lvl, fmtv); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no mcrelay service is set up") {
		t.Fatalf("output:\n%s", out.String())
	}
}

// TestRelaySetupServiceRefusesABadInstallerVariable: mcrelay's setup-service
// reads the linger and force variables before it sets anything up, and
// refuses a value that is not 1 or 0 (MADR 0054 PLAN S5d).
func TestRelaySetupServiceRefusesABadInstallerVariable(t *testing.T) {
	defer service.OverrideInstallOS("linux")()
	defer service.OverrideDetectedKind(service.KindNone)()
	for _, name := range []string{service.EnvNoLinger, service.EnvForceService} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "yes")
			cfg, lvl, fmtv := "", "", ""
			cmd := newSetupServiceCmd(&cfg, &lvl, &fmtv)
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			err := runSetupService(cmd, setupServiceFlags{}, cfg, lvl, fmtv)
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("%s=yes: %v, want a refusal naming it", name, err)
			}
			if buf.Len() != 0 {
				t.Fatalf("%s=yes printed a result, so setup ran:\n%s", name, buf.String())
			}
		})
	}
}
