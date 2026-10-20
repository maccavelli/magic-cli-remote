//go:build unix

// These tests assert systemd unit text, launchd plist XML and the
// ~/.local/bin discovery that goes with them. Windows runs a Task
// Scheduler task instead (MADR 0116 D12), covered by schtasks_test.go.

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
	"github.com/maccavelli/magic-cli-remote/internal/testexec"
)

func TestSetupServiceFlagsToOptionsDefaults(t *testing.T) {
	var f setupServiceFlags
	opts := f.toOptions()
	if opts.UnitName != "" {
		t.Fatalf("UnitName = %q, want empty (let normalize fill it)", opts.UnitName)
	}
	if opts.ConfigPath != "" {
		t.Fatalf("ConfigPath = %q, want empty (falls back to cfgFile)", opts.ConfigPath)
	}
}

func TestSetupServiceFlagsToOptionsConfigPathFallback(t *testing.T) {
	oldCfg := cfgFile
	cfgFile = "/fallback/config.yaml"
	defer func() { cfgFile = oldCfg }()

	var f setupServiceFlags
	opts := f.toOptions()
	if opts.ConfigPath != "/fallback/config.yaml" {
		t.Fatalf("ConfigPath = %q, want /fallback/config.yaml", opts.ConfigPath)
	}
}

func TestSetupServiceFlagsToOptionsExplicitConfigPath(t *testing.T) {
	f := setupServiceFlags{configPath: "/explicit/config.yaml"}
	opts := f.toOptions()
	if opts.ConfigPath != "/explicit/config.yaml" {
		t.Fatalf("ConfigPath = %q, want /explicit/config.yaml", opts.ConfigPath)
	}
}

func TestSetupServiceFlagsToOptionsCarriesAllFields(t *testing.T) {
	f := setupServiceFlags{
		unitName:   "myapp",
		binary:     "/usr/bin/mcremote",
		dataDir:    "/var/lib/mcremote",
		listenHost: "100.64.0.1",
		listenPort: 7531,
		workingDir: "/home/user",
		printOnly:  true,
		force:      true,
		noEnable:   true,
		noStart:    true,
		noLinger:   true,
		envPairs:   []string{"FOO=bar"},
	}
	opts := f.toOptions()
	if opts.UnitName != "myapp" {
		t.Fatalf("UnitName = %q, want myapp", opts.UnitName)
	}
	if opts.Binary != "/usr/bin/mcremote" {
		t.Fatalf("Binary = %q, want /usr/bin/mcremote", opts.Binary)
	}
}

func TestBindSetupServiceFlagsDedup(t *testing.T) {
	cmd := newSetupServiceCmd()
	// Binding twice must not panic or duplicate.
	var f setupServiceFlags
	bindSetupServiceFlags(cmd, &f)
	bindSetupServiceFlags(cmd, &f)
}

func TestNewSetupServiceCmdProperties(t *testing.T) {
	cmd := newSetupServiceCmd()
	if cmd.Use != "setup-service" {
		t.Fatalf("Use = %q, want setup-service", cmd.Use)
	}
	if cmd.Args == nil {
		t.Fatal("Args must not be nil")
	}
}

// TestRunSetupServiceWithNothingToSuperviseSucceeds: setup-service on a host
// with no service manager it installs under exits 0 and prints the advice, so
// the installer's after_install hook does not fail a working install (MADR
// 0054 amendment 4, PLAN S5a).
func TestRunSetupServiceWithNothingToSuperviseSucceeds(t *testing.T) {
	defer service.OverrideInstallOS("linux")()
	defer service.OverrideDetectedKind(service.KindNone)()
	t.Setenv("HOME", t.TempDir())
	// Setup checks only the executable bit; the stub is never run here.
	bin := testexec.WriteShellStub(t, filepath.Join(t.TempDir(), "mcremote"), "")
	cfgFile = ""
	f := setupServiceFlags{binary: bin}
	var buf bytes.Buffer
	cmd := newSetupServiceCmd()
	cmd.SetOut(&buf)
	if err := runSetupService(cmd, f); err != nil {
		t.Fatalf("setup-service with nothing to supervise: %v, want success", err)
	}
	out := buf.String()
	if !strings.Contains(out, "service:  not configured (no supported service manager detected: none)") ||
		!strings.Contains(out, "run it:   "+bin+" serve") {
		t.Fatalf("output lacks the advice:\n%s", out)
	}
}

// TestRunSetupServiceRefusesABadInstallerVariable: each installer variable is
// read before anything is set up, and a value that is not 1 or 0 stops the
// command, naming it (MADR 0054 PLAN S5d).
func TestRunSetupServiceRefusesABadInstallerVariable(t *testing.T) {
	defer service.OverrideInstallOS("linux")()
	defer service.OverrideDetectedKind(service.KindNone)()
	t.Setenv("HOME", t.TempDir())
	bin := testexec.WriteShellStub(t, filepath.Join(t.TempDir(), "mcremote"), "")
	for _, name := range []string{service.EnvNoLinger, service.EnvForceService, service.EnvWithRelayService} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "yes")
			var buf bytes.Buffer
			cmd := newSetupServiceCmd()
			cmd.SetOut(&buf)
			cfgFile = ""
			err := runSetupService(cmd, setupServiceFlags{binary: bin})
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("%s=yes: %v, want a refusal naming it", name, err)
			}
			if buf.Len() != 0 {
				t.Fatalf("%s=yes printed a result, so setup ran:\n%s", name, buf.String())
			}
		})
	}
}

// TestRunSetupServiceWithRelayOnAHostWithNothingToSupervise: the relay is not
// set up where mcremote could not be, and the output says so.
func TestRunSetupServiceWithRelayOnAHostWithNothingToSupervise(t *testing.T) {
	defer service.OverrideInstallOS("linux")()
	defer service.OverrideDetectedKind(service.KindNone)()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(service.EnvWithRelayService, "1")
	bin := testexec.WriteShellStub(t, filepath.Join(t.TempDir(), "mcremote"), "")
	var buf bytes.Buffer
	cmd := newSetupServiceCmd()
	cmd.SetOut(&buf)
	cfgFile = ""
	if err := runSetupService(cmd, setupServiceFlags{binary: bin}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "mcrelay ("+service.EnvWithRelayService+"=1): not set up either") {
		t.Fatalf("output does not say mcrelay was not set up:\n%s", buf.String())
	}
}

// TestRunSetupServiceReadsTheInstallerVariables: MCREMOTE_NO_LINGER reaches the
// options (no loginctl, "Linger: skipped"), and MCREMOTE_WITH_RELAY_SERVICE=1
// sets mcrelay up from the binary beside mcremote's (MADR 0054 PLAN S5d).
func TestRunSetupServiceReadsTheInstallerVariables(t *testing.T) {
	defer service.OverrideInstallOS("linux")()
	defer service.OverrideRunSystemctl(func(...string) error { return nil })()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(k, filepath.Join(home, k))
	}
	t.Setenv(service.EnvNoLinger, "1")
	t.Setenv(service.EnvWithRelayService, "1")
	dir := t.TempDir()
	// Setup checks only the executable bit; the stubs are never run here.
	bin := testexec.WriteShellStub(t, filepath.Join(dir, "mcremote"), "")
	testexec.WriteShellStub(t, filepath.Join(dir, "mcrelay"), "")
	var buf bytes.Buffer
	cmd := newSetupServiceCmd()
	cmd.SetOut(&buf)
	cfgFile = ""
	if err := runSetupService(cmd, setupServiceFlags{binary: bin, noStart: true}); err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	out := buf.String()
	if strings.Count(out, "Linger:            skipped") != 2 {
		t.Errorf("want Linger skipped for both services:\n%s", out)
	}
	if !strings.Contains(out, "mcrelay ("+service.EnvWithRelayService+"=1):") ||
		!strings.Contains(out, "ExecStart binary:  "+filepath.Join(dir, "mcrelay")) {
		t.Errorf("want mcrelay set up from %s:\n%s", filepath.Join(dir, "mcrelay"), out)
	}
	unit := filepath.Join(home, "XDG_CONFIG_HOME", "systemd", "user", "mcrelay.service")
	if b, err := os.ReadFile(unit); err != nil || !strings.Contains(string(b), filepath.Join(dir, "mcrelay")+" serve") {
		t.Errorf("mcrelay unit %s: %v\n%s", unit, err, b)
	}
}

// TestRunSetupServiceUninstall: --uninstall cannot be combined with --refresh
// or --remove, and on a host with nothing installed it says so and succeeds
// (MADR 0054 PLAN S5e).
func TestRunSetupServiceUninstall(t *testing.T) {
	defer service.OverrideInstallOS("linux")()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(k, filepath.Join(home, k))
	}
	for _, f := range []setupServiceFlags{{uninstall: true, refresh: true}, {uninstall: true, remove: true}} {
		cmd := newSetupServiceCmd()
		cmd.SetOut(&bytes.Buffer{})
		if err := runSetupService(cmd, f); err == nil || !strings.Contains(err.Error(), "--uninstall cannot be combined") {
			t.Errorf("%+v: %v, want a refusal", f, err)
		}
	}
	var buf bytes.Buffer
	cmd := newSetupServiceCmd()
	cmd.SetOut(&buf)
	if err := runSetupService(cmd, setupServiceFlags{uninstall: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no service definition of mcremote or mcrelay was found") {
		t.Fatalf("output:\n%s", buf.String())
	}
}

// TestRunSetupServiceFromInstaller: --from-installer refuses the other modes,
// and with nothing installed it is a first install, which runs as plain
// setup-service does: here, on a host with nothing to supervise, the advice
// (MADR 0054 PLAN S7a).
func TestRunSetupServiceFromInstaller(t *testing.T) {
	defer service.OverrideInstallOS("linux")()
	defer service.OverrideDetectedKind(service.KindNone)()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(k, filepath.Join(home, k))
	}
	for _, f := range []setupServiceFlags{
		{fromInstaller: true, refresh: true}, {fromInstaller: true, remove: true},
		{fromInstaller: true, uninstall: true}, {fromInstaller: true, printOnly: true},
	} {
		cmd := newSetupServiceCmd()
		cmd.SetOut(&bytes.Buffer{})
		if err := runSetupService(cmd, f); err == nil || !strings.Contains(err.Error(), "--from-installer cannot be combined") {
			t.Errorf("%+v: %v, want a refusal", f, err)
		}
	}
	// Setup checks only the executable bit; the stub is never run here.
	bin := testexec.WriteShellStub(t, filepath.Join(t.TempDir(), "mcremote"), "")
	cfgFile = ""
	var buf bytes.Buffer
	cmd := newSetupServiceCmd()
	cmd.SetOut(&buf)
	if err := runSetupService(cmd, setupServiceFlags{fromInstaller: true, binary: bin}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "service:  not configured") {
		t.Fatalf("a first install did not run as setup-service:\n%s", buf.String())
	}
}

// TestRunSetupServiceFromInstallerPrintsTheAdvisories: a first install, which
// runs as plain setup, ends with the install notices; plain setup-service does
// not print them (MADR 0054 amendment S7d, A1).
func TestRunSetupServiceFromInstallerPrintsTheAdvisories(t *testing.T) {
	defer service.OverrideInstallOS("linux")()
	defer service.OverrideDetectedKind(service.KindNone)()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(k, filepath.Join(home, k))
	}
	ap := filepath.Join(t.TempDir(), "apparmor_restrict_unprivileged_userns")
	if err := os.WriteFile(ap, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer service.OverrideApparmorRestrictPath(ap)()
	bin := testexec.WriteShellStub(t, filepath.Join(t.TempDir(), "mcremote"), "")
	cfgFile = ""
	for _, tc := range []struct {
		f    setupServiceFlags
		want bool
	}{{setupServiceFlags{fromInstaller: true, binary: bin}, true}, {setupServiceFlags{binary: bin}, false}} {
		var buf bytes.Buffer
		cmd := newSetupServiceCmd()
		cmd.SetOut(&buf)
		if err := runSetupService(cmd, tc.f); err != nil {
			t.Fatal(err)
		}
		if has := strings.Contains(buf.String(), "AppArmor restricts"); has != tc.want {
			t.Errorf("fromInstaller=%v: notice shown %v, want %v:\n%s", tc.f.fromInstaller, has, tc.want, buf.String())
		}
	}
}

func TestRunSetupServicePrintOnly(t *testing.T) {
	// This test asserts systemd unit text, so pin the install OS instead of
	// inheriting the host's. On macOS the renderer correctly emits a launchd
	// plist and every systemd assertion below would fail for the right reason.
	defer service.OverrideInstallOS("linux")()

	cfgFile = ""
	var f setupServiceFlags
	f.printOnly = true
	f.unitName = "test-mcremote"

	var buf bytes.Buffer
	cmd := newSetupServiceCmd()
	cmd.SetOut(&buf)

	err := runSetupService(cmd, f)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "[Unit]") {
		t.Fatalf("print-only output missing [Unit] section:\n%s", out)
	}
	if !strings.Contains(out, "Description=mcremote multi-CLI remote control daemon") {
		t.Fatalf("print-only output missing expected Description:\n%s", out)
	}
}

func TestRunSetupServicePrintOnlyRespectsFields(t *testing.T) {
	cfgFile = ""
	var f setupServiceFlags
	f.printOnly = true
	f.unitName = "custom-unit"
	f.binary = "/opt/bin/mcremote"
	f.dataDir = "/opt/data"
	f.listenHost = "0.0.0.0"
	f.listenPort = 8443

	var buf bytes.Buffer
	cmd := newSetupServiceCmd()
	cmd.SetOut(&buf)

	err := runSetupService(cmd, f)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "custom-unit") {
		t.Fatalf("output missing custom unit name:\n%s", out)
	}
	if !strings.Contains(out, "/opt/bin/mcremote") {
		t.Fatalf("output missing custom binary:\n%s", out)
	}
}

func TestRunSetupServicePrintOnlyThroughCobra(t *testing.T) {
	// Asserts systemd unit text — pin the install OS (see TestRunSetupServicePrintOnly).
	defer service.OverrideInstallOS("linux")()

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"setup-service", "--print-only", "--unit-name", "ci-test"})
	err := cmd.Execute()
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "[Unit]") {
		t.Fatalf("cobra print-only output missing [Unit]:\n%s", out)
	}
}

func TestRunSetupServiceValidateUnitName(t *testing.T) {
	// An invalid unit name should be rejected before any side effects.
	var f setupServiceFlags
	f.printOnly = true
	f.unitName = "bad name with spaces"

	var buf bytes.Buffer
	cmd := newSetupServiceCmd()
	cmd.SetOut(&buf)

	err := runSetupService(cmd, f)
	if err == nil {
		t.Fatal("expected error for invalid unit name")
	}
	if !strings.Contains(err.Error(), "unit name") {
		t.Fatalf("error = %v, want unit-name validation", err)
	}
}

func TestRenderUnitRoundTrip(t *testing.T) {
	body, err := service.RenderUnit(service.Options{
		UnitName:  "unit-test",
		PrintOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "[Unit]") {
		t.Fatalf("rendered unit missing [Unit]:\n%s", body)
	}
}

// --refresh takes nothing from the caller: it reads the installed definition,
// re-renders it from this binary's template, and reports one verdict. These
// tests pin XDG_CONFIG_HOME so they never read the developer's real unit dir.

func TestRunSetupServiceRefreshPrintsVerdict(t *testing.T) {
	defer service.OverrideInstallOS("linux")()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfgFile = ""
	var f setupServiceFlags
	f.refresh = true

	var buf bytes.Buffer
	cmd := newSetupServiceCmd()
	cmd.SetOut(&buf)

	if err := runSetupService(cmd, f); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "no service definition installed for mcremote") {
		t.Fatalf("unexpected verdict line:\n%s", out)
	}
}

func TestRunSetupServiceRefreshJSONShape(t *testing.T) {
	defer service.OverrideInstallOS("linux")()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfgFile = ""
	var f setupServiceFlags
	f.refresh = true
	f.jsonOut = true

	var buf bytes.Buffer
	cmd := newSetupServiceCmd()
	cmd.SetOut(&buf)

	if err := runSetupService(cmd, f); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Verdict  string `json:"verdict"`
		Path     string `json:"path"`
		Changed  bool   `json:"changed"`
		Reloaded bool   `json:"reloaded"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not one JSON object: %v\n%s", err, buf.String())
	}
	// The field names are a cross-version contract: `update` in release N reads
	// what --refresh prints in release N+1.
	if got.Verdict != "none" || got.Changed || got.Reloaded {
		t.Fatalf("got %+v, want verdict=none with no side effects", got)
	}
	if !strings.HasSuffix(got.Path, "/systemd/user/mcremote.service") {
		t.Fatalf("path = %q", got.Path)
	}
}

func TestSetupServiceRefreshConflictsWithRemove(t *testing.T) {
	defer service.OverrideInstallOS("linux")()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfgFile = ""
	var f setupServiceFlags
	f.refresh = true
	f.remove = true

	cmd := newSetupServiceCmd()
	cmd.SetOut(&bytes.Buffer{})

	err := runSetupService(cmd, f)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("err = %v, want a mutual-exclusion error", err)
	}
}

func TestSetupServiceFlagsBindRefresh(t *testing.T) {
	cmd := newSetupServiceCmd()
	for _, name := range []string{"refresh", "json"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("--%s is not bound", name)
		}
	}
}
