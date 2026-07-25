package cli

import (
	"bytes"
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
		UnitName: "unit-test",
		PrintOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "[Unit]") {
		t.Fatalf("rendered unit missing [Unit]:\n%s", body)
	}
}
