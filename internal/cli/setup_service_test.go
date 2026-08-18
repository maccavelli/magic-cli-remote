package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/maccavelli/magic-cli-remote/internal/cli/service"
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
